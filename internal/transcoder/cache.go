package transcoder

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
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
}

func NewCacheManager(baseDir string, maxSizeGB int) *CacheManager {
	os.MkdirAll(baseDir, 0o755)
	return &CacheManager{
		baseDir:  baseDir,
		maxBytes: int64(maxSizeGB) * 1024 * 1024 * 1024,
	}
}

func (c *CacheManager) BaseDir() string { return c.baseDir }

func (c *CacheManager) SegmentDir(key string) string {
	safe := safeCacheKey(key)
	dir := filepath.Join(c.baseDir, safe)
	os.MkdirAll(dir, 0o755)
	return dir
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

	var total int64
	var count int

	filepath.WalkDir(c.baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		count++
		return nil
	})

	return CacheStats{
		TotalSize: total,
		ItemCount: count,
		MaxSizeGB: int(c.maxBytes / (1024 * 1024 * 1024)),
	}
}

func (c *CacheManager) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries, err := os.ReadDir(c.baseDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		os.RemoveAll(filepath.Join(c.baseDir, e.Name()))
	}
	return nil
}

func (c *CacheManager) Evict(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	safe := safeCacheKey(key)
	return os.RemoveAll(filepath.Join(c.baseDir, safe))
}

type dirEntry struct {
	path    string
	modTime time.Time
	size    int64
}

func (c *CacheManager) EvictLRU() error {
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

	var dirs []dirEntry
	entries, err := os.ReadDir(c.baseDir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(c.baseDir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		var size int64
		filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			fi, _ := d.Info()
			if fi != nil {
				size += fi.Size()
			}
			return nil
		})
		dirs = append(dirs, dirEntry{path: path, modTime: info.ModTime(), size: size})
	}

	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].modTime.Before(dirs[j].modTime)
	})

	freed := stats.TotalSize
	for _, d := range dirs {
		if freed <= c.maxBytes {
			break
		}
		os.RemoveAll(d.path)
		freed -= d.size
	}

	return nil
}

func (c *CacheManager) statsLocked() CacheStats {
	var total int64
	var count int
	filepath.WalkDir(c.baseDir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, _ := d.Info()
		if info != nil {
			total += info.Size()
			count++
		}
		return nil
	})
	return CacheStats{TotalSize: total, ItemCount: count, MaxSizeGB: int(c.maxBytes / (1024 * 1024 * 1024))}
}
