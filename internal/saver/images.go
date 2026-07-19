package saver

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/karbowiak/heya/internal/atomicfile"
	"github.com/karbowiak/heya/internal/generatedwrite"
	"github.com/karbowiak/heya/internal/images"
	"github.com/rs/zerolog/log"
)

var assetTypeToFilename = map[string]string{
	"poster":   "poster",
	"backdrop": "fanart",
	"banner":   "banner",
	"art":      "clearart",
	"logo":     "logo",
	"thumb":    "landscape",
	"disc":     "disc",
}

// albumCoverSidecars mirrors the scanner's recognized cover filenames — when
// any of these already sits in the release directory, the folder owns its
// art and the export must not add a duplicate.
var albumCoverSidecars = []string{"cover.jpg", "cover.png", "cover.jpeg", "folder.jpg", "folder.png", "folder.jpeg"}

var backdropSidecarName = regexp.MustCompile(`^(fanart|backdrop)[0-9]*\.(jpg|jpeg|png|webp|gif)$`)

// SaveAlbumCoverToDir copies a cached album cover into the release directory
// as cover.<ext>, skipping when the folder already carries recognized cover
// art (user-provided files always win).
func SaveAlbumCoverToDir(albumDir, cachedPath string) error {
	_, err := SaveAlbumCoverToDirWithResult(albumDir, cachedPath)
	return err
}

func SaveAlbumCoverToDirWithResult(albumDir, cachedPath string) (generatedwrite.Output, error) {
	destPath := AlbumCoverPath(albumDir, cachedPath)
	if exists, err := sidecarPathExists(destPath); err != nil {
		return generatedwrite.Output{}, err
	} else if exists {
		// A failed acknowledgement retry reaches this exact destination. Hash
		// both files and attest only when its bytes still equal the cached source.
		return copyFileWithResult(cachedPath, destPath)
	}
	for _, name := range albumCoverSidecars {
		if exists, err := sidecarPathExists(filepath.Join(albumDir, name)); err != nil {
			return generatedwrite.Output{}, err
		} else if exists {
			// Another recognized cover remains user-owned even if it happens to
			// be visually or byte-identical. It is not our expected destination.
			return generatedwrite.Output{}, nil
		}
	}
	return copyFileWithResult(cachedPath, destPath)
}

func PrepareAlbumCoverToDir(albumDir, cachedPath string) (*generatedwrite.Prepared, error) {
	destination := AlbumCoverPath(albumDir, cachedPath)
	if exists, err := sidecarPathExists(destination); err != nil {
		return nil, err
	} else if !exists {
		for _, name := range albumCoverSidecars {
			if name == filepath.Base(destination) {
				continue
			}
			if occupied, inspectErr := sidecarPathExists(filepath.Join(albumDir, name)); inspectErr != nil {
				return nil, inspectErr
			} else if occupied {
				return nil, nil
			}
		}
	}
	return generatedwrite.PrepareFile(destination, 0o644, cachedPath)
}

func SaveImageToMediaDir(mediaDir, cachedPath, assetType string, sortOrder int) error {
	_, err := SaveImageToMediaDirWithResult(mediaDir, cachedPath, assetType, sortOrder)
	return err
}

func SaveImageToMediaDirWithResult(mediaDir, cachedPath, assetType string, sortOrder int) (generatedwrite.Output, error) {
	destPath, ok := ImageSidecarPath(mediaDir, cachedPath, assetType, sortOrder)
	if !ok {
		return generatedwrite.Output{}, nil
	}

	if exists, err := sidecarPathExists(destPath); err != nil {
		return generatedwrite.Output{}, err
	} else if exists {
		return copyFileWithResult(cachedPath, destPath)
	}
	if assetType == "backdrop" {
		duplicate, err := equivalentBackdropSidecarExists(mediaDir, cachedPath)
		if err == nil && duplicate {
			return generatedwrite.Output{}, nil
		}
	}

	return copyFileWithResult(cachedPath, destPath)
}

func PrepareImageToMediaDir(mediaDir, cachedPath, assetType string, sortOrder int) (*generatedwrite.Prepared, error) {
	destination, ok := ImageSidecarPath(mediaDir, cachedPath, assetType, sortOrder)
	if !ok {
		return nil, nil
	}
	if exists, err := sidecarPathExists(destination); err != nil {
		return nil, err
	} else if !exists && assetType == "backdrop" {
		duplicate, duplicateErr := equivalentBackdropSidecarExists(mediaDir, cachedPath)
		if duplicateErr != nil {
			return nil, duplicateErr
		}
		if duplicate {
			return nil, nil
		}
	}
	return generatedwrite.PrepareFile(destination, 0o644, cachedPath)
}

func AlbumCoverPath(albumDir, cachedPath string) string {
	ext := filepath.Ext(cachedPath)
	if ext == "" {
		ext = ".jpg"
	}
	return filepath.Join(albumDir, "cover"+ext)
}

func ImageSidecarPath(mediaDir, cachedPath, assetType string, sortOrder int) (string, bool) {
	baseName, ok := assetTypeToFilename[assetType]
	if !ok {
		return "", false
	}
	if sortOrder > 0 {
		baseName = fmt.Sprintf("%s%d", baseName, sortOrder)
	}
	ext := filepath.Ext(cachedPath)
	if ext == "" {
		ext = ".jpg"
	}
	return filepath.Join(mediaDir, baseName+ext), true
}

// equivalentBackdropSidecarExists prevents save_images from writing another
// fanartN/backdropN file when the folder already contains the same visual at a
// different size or JPEG quality. Existing source sidecars are never modified.
func equivalentBackdropSidecarExists(mediaDir, candidatePath string) (bool, error) {
	candidate, err := images.FingerprintFile(candidatePath)
	if err != nil {
		return false, err
	}
	entries, err := os.ReadDir(mediaDir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !backdropSidecarName.MatchString(strings.ToLower(entry.Name())) {
			continue
		}
		existing, fingerprintErr := images.FingerprintFile(filepath.Join(mediaDir, entry.Name()))
		if fingerprintErr == nil && images.VisuallyEquivalent(candidate, existing) {
			return true, nil
		}
	}
	return false, nil
}

func copyFile(src, dst string) error {
	_, err := copyFileWithResult(src, dst)
	return err
}

func copyFileWithResult(src, dst string) (generatedwrite.Output, error) {
	in, err := os.Open(src) //nolint:gosec // source is a database-backed media asset selected by the scanner
	if err != nil {
		return generatedwrite.Output{}, err
	}
	defer func() { _ = in.Close() }()

	// dst is composed from the scanner-selected media directory and a fixed
	// sidecar basename; cachedPath contributes only a file extension.
	hasher := sha256.New()
	size, created, err := atomicfile.CopyIfAbsent(dst, 0o644, io.TeeReader(in, hasher))
	if err != nil {
		return generatedwrite.Output{}, err
	}

	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	if !created {
		output, _, err := attestExistingSignature(dst, size, digest)
		if err != nil {
			return generatedwrite.Output{}, err
		}
		if output.Attested {
			log.Debug().Str("src", filepath.Base(src)).Str("dst", dst).Msg("exact image sidecar already present; attesting without rewrite")
		} else {
			log.Debug().Str("dst", dst).Msg("different image sidecar already exists; preserving user-owned file")
		}
		return output, nil
	}

	log.Debug().Str("src", filepath.Base(src)).Str("dst", dst).Msg("image saved to media dir")
	return generatedwrite.Published(dst, size, digest), nil
}

func sidecarPathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// attestExistingSignature verifies that path still names one regular file
// throughout hashing. A symlink, replacement, size mismatch, or digest mismatch
// remains user-owned and yields unattested output.
func attestExistingSignature(path string, expectedSize int64, expectedDigest [sha256.Size]byte) (generatedwrite.Output, bool, error) {
	pathInfoBefore, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return generatedwrite.Output{}, false, nil
		}
		return generatedwrite.Output{}, false, err
	}
	if !pathInfoBefore.Mode().IsRegular() || pathInfoBefore.Size() != expectedSize {
		return generatedwrite.Output{Path: path}, true, nil
	}

	file, err := os.Open(path) //nolint:gosec // destination is a fixed sidecar path inside the selected media directory
	if err != nil {
		return generatedwrite.Output{}, true, err
	}
	defer func() { _ = file.Close() }()
	openedInfo, err := file.Stat()
	if err != nil {
		return generatedwrite.Output{}, true, err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(pathInfoBefore, openedInfo) {
		return generatedwrite.Output{Path: path}, true, nil
	}
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return generatedwrite.Output{}, true, err
	}
	openedAfter, err := file.Stat()
	if err != nil {
		return generatedwrite.Output{}, true, err
	}
	pathInfoAfter, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return generatedwrite.Output{}, false, nil
		}
		return generatedwrite.Output{}, true, err
	}
	if size != expectedSize || openedAfter.Size() != expectedSize ||
		!openedAfter.ModTime().Equal(openedInfo.ModTime()) ||
		!pathInfoAfter.Mode().IsRegular() || !os.SameFile(openedAfter, pathInfoAfter) {
		return generatedwrite.Output{Path: path}, true, nil
	}
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	if digest != expectedDigest {
		return generatedwrite.Output{Path: path}, true, nil
	}
	return generatedwrite.Attest(path, size, digest), true, nil
}
