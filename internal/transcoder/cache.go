package transcoder

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	hlsCacheNamespace   = "hls"
	audioCacheNamespace = "audio"
)

type CacheStats struct {
	TotalSize int64
	ItemCount int
	MaxSizeGB int
}

type CacheManager struct {
	mu       sync.RWMutex
	baseDir  string
	maxBytes int64
	pins     map[string]int
}

// CacheLease keeps one or more cache paths out of automatic LRU eviction.
// Release is idempotent, which makes leases safe to hand to response/file
// lifetimes that may have more than one cleanup path.
//
// Explicit CacheManager.Clear deliberately ignores leases: it is the
// administrative clear-all operation. Producers recreate their leased
// directories before writing, while open files remain valid on platforms
// with Unix file semantics.
type CacheLease struct {
	cache   *CacheManager
	path    string
	paths   []string
	release sync.Once
}

// Path is the primary file or directory reserved by the lease.
func (l *CacheLease) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Release drops the lease exactly once.
func (l *CacheLease) Release() {
	if l == nil || l.cache == nil {
		return
	}
	l.release.Do(func() {
		l.cache.mu.Lock()
		defer l.cache.mu.Unlock()
		for _, path := range l.paths {
			if l.cache.pins[path] <= 1 {
				delete(l.cache.pins, path)
			} else {
				l.cache.pins[path]--
			}
		}
	})
}

// Close lets CacheLease participate in conventional defer/cleanup stacks.
func (l *CacheLease) Close() error {
	l.Release()
	return nil
}

func NewCacheManager(baseDir string, maxSizeGB int) *CacheManager {
	if err := os.MkdirAll(baseDir, 0o750); err != nil {
		// The historical constructor cannot return an error. Producers still
		// retry directory creation and return their own actionable errors.
		log.Warn().Err(err).Msg("create transcoder cache directory")
	}
	return &CacheManager{
		baseDir:  baseDir,
		maxBytes: int64(maxSizeGB) * 1024 * 1024 * 1024,
		pins:     make(map[string]int),
	}
}

func (c *CacheManager) BaseDir() string { return c.baseDir }

// SetMaxSizeGB changes the live eviction cap. Zero disables the cap. Existing
// cache contents are left in place; the session manager's next cleanup pass
// enforces a newly lowered cap while protecting active sessions and producers.
func (c *CacheManager) SetMaxSizeGB(maxSizeGB int) {
	c.mu.Lock()
	c.maxBytes = int64(maxSizeGB) * 1024 * 1024 * 1024
	c.mu.Unlock()
}

func (c *CacheManager) SegmentDir(key string) string {
	dir := c.segmentDirPath(key)
	c.mu.Lock()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		// Keep the path-returning compatibility API; write paths use the
		// error-returning reservation methods below.
		log.Warn().Err(err).Str("key", safeCacheKey(key)).Msg("create transcode segment directory")
	}
	c.mu.Unlock()
	return dir
}

func (c *CacheManager) segmentDirPath(key string) string {
	return filepath.Join(c.baseDir, hlsCacheNamespace, safeCacheKey(key))
}

// reserveSegmentDir creates and leases an HLS output directory under the
// same cache lock used by EvictLRU. There is therefore no discoverable,
// unpinned directory between mkdir and publication of the lease.
//
// A lease is returned even when mkdir fails so the SessionManager can retain
// ownership of the intended path and a later transcode head can recover from
// a transient/explicit clear by recreating it before writing.
func (c *CacheManager) reserveSegmentDir(key string) (*CacheLease, error) {
	dir := c.segmentDirPath(key)
	c.mu.Lock()
	err := os.MkdirAll(dir, 0o750)
	lease := c.leaseLocked(dir, dir)
	c.mu.Unlock()
	return lease, err
}

// ReserveSegmentFile creates and leases a key-scoped cache directory and one
// file path within it. The directory and file remain protected until the
// caller releases the lease, so extraction and HTTP serving form one cache
// lifetime rather than two operations separated by an eviction gap.
func (c *CacheManager) ReserveSegmentFile(key, name string) (*CacheLease, error) {
	if name == "" || filepath.Base(name) != name {
		return nil, fmt.Errorf("invalid cache filename")
	}
	dir := c.segmentDirPath(key)
	path := filepath.Join(dir, name)
	c.mu.Lock()
	err := os.MkdirAll(dir, 0o750)
	lease := c.leaseLocked(path, dir, path)
	c.mu.Unlock()
	return lease, err
}

func safeCacheKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (c *CacheManager) HasSegment(key, segmentName string) bool {
	path := filepath.Join(c.SegmentDir(key), segmentName)
	_, err := os.Stat(path)
	return err == nil
}

func (c *CacheManager) SegmentPath(key, segmentName string) string {
	return filepath.Join(c.SegmentDir(key), segmentName)
}

func (c *CacheManager) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return readCacheStats(c.baseDir, c.maxBytes)
}

// ReadCacheStats reports cache usage without constructing a CacheManager or
// creating the directory. It is intended for read-only command paths such as
// `heya doctor`, where NewCacheManager's MkdirAll side effect is forbidden.
func ReadCacheStats(baseDir string, maxSizeGB int) CacheStats {
	return readCacheStats(baseDir, int64(maxSizeGB)*1024*1024*1024)
}

func readCacheStats(baseDir string, maxBytes int64) CacheStats {
	stats := CacheStats{MaxSizeGB: int(maxBytes / (1024 * 1024 * 1024))}
	_ = filepath.WalkDir(baseDir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		stats.TotalSize += info.Size()
		stats.ItemCount++
		return nil
	})
	return stats
}

func (c *CacheManager) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries, err := os.ReadDir(c.baseDir)
	if err != nil {
		return err
	}
	var clearErr error
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(c.baseDir, entry.Name())); err != nil {
			clearErr = errors.Join(clearErr, fmt.Errorf("remove cache entry %q: %w", entry.Name(), err))
		}
	}
	return clearErr
}

func (c *CacheManager) Evict(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	safe := safeCacheKey(key)
	return os.RemoveAll(filepath.Join(c.baseDir, hlsCacheNamespace, safe))
}

type dirEntry struct {
	path    string
	modTime time.Time
	size    int64
}

// EvictLRU deletes least-recently-modified cache items until the total size
// fits under the cap. A cache item is one HLS session directory, one encoded
// audio file, or one legacy top-level entry. Namespace roots are deliberately
// not cache items: treating audio/ as a single candidate allowed one old AAC
// file to evict every cached track at once.
//
// Paths in skipDirs (live transcode sessions) and paths pinned by active cache
// producers are never evicted. Deleting either from under a running process
// leaves in-memory state pointing at vanished files and can also make ffmpeg's
// eventual atomic rename fail.
func (c *CacheManager) EvictLRU(skipDirs map[string]bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// maxBytes <= 0 means the cap is disabled (HEYA_TRANSCODE_CACHE_MAX_GB=0 =
	// "unlimited"). Without this guard TotalSize is always > 0 >= maxBytes, so
	// every call would evict the *entire* cache — including live session dirs —
	// which the newly-wired 10s cleanup tick would do continuously. Skip early
	// (also avoids the full-tree walk on the disabled path).
	if c.maxBytes <= 0 {
		return nil
	}

	stats := c.statsLocked()
	if stats.TotalSize <= c.maxBytes {
		return nil
	}

	items, err := c.cacheItemsLocked()
	if err != nil {
		return err
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].modTime.Before(items[j].modTime)
	})

	remaining := stats.TotalSize
	for _, item := range items {
		if remaining <= c.maxBytes {
			break
		}
		if pathProtected(item.path, skipDirs) || c.pathPinnedLocked(item.path) {
			continue
		}
		if err := os.RemoveAll(item.path); err != nil {
			return err
		}
		remaining -= item.size
	}

	return nil
}

// pin protects paths while a cache producer is actively writing them. The
// returned release function is idempotent and must be called by the producer.
// Explicit Clear intentionally ignores pins: Clear is the administrative
// clear-all operation, whereas pins only constrain automatic LRU eviction.
func (c *CacheManager) pin(paths ...string) func() {
	if c == nil || len(paths) == 0 {
		return func() {}
	}

	c.mu.Lock()
	lease := c.leaseLocked(paths[0], paths...)
	c.mu.Unlock()
	return lease.Release
}

func (c *CacheManager) lease(paths ...string) *CacheLease {
	if c == nil || len(paths) == 0 {
		return &CacheLease{}
	}
	c.mu.Lock()
	lease := c.leaseLocked(paths[0], paths...)
	c.mu.Unlock()
	return lease
}

func (c *CacheManager) leaseLocked(primary string, paths ...string) *CacheLease {
	return &CacheLease{cache: c, path: filepath.Clean(primary), paths: c.pinLocked(paths...)}
}

// pinLocked registers paths while c.mu is already held. It lets a producer
// create its temporary output and publish the eviction pin in one critical
// section, leaving no instant in which LRU can discover and delete the new
// file before it becomes protected.
func (c *CacheManager) pinLocked(paths ...string) []string {
	cleaned := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		path = filepath.Clean(path)
		c.pins[path]++
		cleaned = append(cleaned, path)
	}
	return cleaned
}

func (c *CacheManager) releasePins(cleaned []string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			for _, path := range cleaned {
				if c.pins[path] <= 1 {
					delete(c.pins, path)
				} else {
					c.pins[path]--
				}
			}
		})
	}
}

func (c *CacheManager) pathPinnedLocked(item string) bool {
	for path := range c.pins {
		if pathContains(item, path) {
			return true
		}
	}
	return false
}

func pathProtected(item string, protected map[string]bool) bool {
	for path, enabled := range protected {
		if enabled && pathContains(item, filepath.Clean(path)) {
			return true
		}
	}
	return false
}

// pathContains reports whether child is item itself or is below item. Both
// values are filesystem paths, not user-controlled URL paths.
func pathContains(item, child string) bool {
	rel, err := filepath.Rel(filepath.Clean(item), filepath.Clean(child))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (c *CacheManager) cacheItemsLocked() ([]dirEntry, error) {
	entries, err := os.ReadDir(c.baseDir)
	if err != nil {
		return nil, err
	}

	var items []dirEntry
	for _, entry := range entries {
		path := filepath.Join(c.baseDir, entry.Name())
		if entry.IsDir() && (entry.Name() == hlsCacheNamespace || entry.Name() == audioCacheNamespace) {
			children, err := os.ReadDir(path)
			if err != nil {
				return nil, err
			}
			for _, child := range children {
				item, ok := cacheItem(filepath.Join(path, child.Name()))
				if ok {
					items = append(items, item)
				}
			}
			continue
		}

		item, ok := cacheItem(path)
		if ok {
			items = append(items, item)
		}
	}
	return items, nil
}

func cacheItem(path string) (dirEntry, bool) {
	var item dirEntry
	item.path = path
	err := filepath.WalkDir(path, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(item.modTime) {
			item.modTime = info.ModTime()
		}
		if !entry.IsDir() {
			item.size += info.Size()
		}
		return nil
	})
	return item, err == nil
}

func (c *CacheManager) statsLocked() CacheStats {
	return readCacheStats(c.baseDir, c.maxBytes)
}
