package worker

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrentSegmentFileSnapshotRequiresSameProbedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "episode.mkv")
	require.NoError(t, os.WriteFile(path, []byte("unchanged release bytes"), 0o600))
	info, err := os.Stat(path)
	require.NoError(t, err)

	file := sqlc.LibraryFile{
		Path:      path,
		Size:      info.Size(),
		Mtime:     pgtype.Timestamptz{Time: info.ModTime().Truncate(time.Microsecond), Valid: true},
		MediaInfo: []byte(`{"duration":123.456}`),
	}

	snapshot, reason, err := currentSegmentFileSnapshot(file)
	require.NoError(t, err)
	assert.Empty(t, reason)
	assert.EqualValues(t, 123456, snapshot.DurationMs)
	assert.Equal(t, path, snapshot.Path)

	changedRow := file
	changedRow.Size++
	_, reason, err = currentSegmentFileSnapshot(changedRow)
	require.NoError(t, err)
	assert.Equal(t, "file_changed_since_scan", reason)

	unprobed := file
	unprobed.MediaInfo = []byte(`{}`)
	_, reason, err = currentSegmentFileSnapshot(unprobed)
	require.NoError(t, err)
	assert.Equal(t, "file_not_probed", reason)

	softDeleted := file
	softDeleted.DeletedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	_, reason, err = currentSegmentFileSnapshot(softDeleted)
	require.NoError(t, err)
	assert.Equal(t, "file_soft_deleted", reason)

	require.NoError(t, os.Remove(path))
	_, reason, err = currentSegmentFileSnapshot(file)
	require.NoError(t, err)
	assert.Equal(t, "file_missing", reason)
}

func TestSegmentFileSnapshotDetectsReplacementAfterFetch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.mkv")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0o600))
	info, err := os.Stat(path)
	require.NoError(t, err)

	file := sqlc.LibraryFile{
		Path:      path,
		Size:      info.Size(),
		Mtime:     pgtype.Timestamptz{Time: info.ModTime().Truncate(time.Microsecond), Valid: true},
		MediaInfo: []byte(`{"duration":90}`),
	}
	before, reason, err := currentSegmentFileSnapshot(file)
	require.NoError(t, err)
	require.Empty(t, reason)

	// A replacement that has not yet been observed by the scanner must not be
	// accepted using the old database duration/snapshot.
	require.NoError(t, os.WriteFile(path, []byte("replacement with a different cut"), 0o600))
	_, reason, err = currentSegmentFileSnapshot(file)
	require.NoError(t, err)
	assert.Equal(t, "file_changed_since_scan", reason)

	replacementInfo, err := os.Stat(path)
	require.NoError(t, err)
	replacement := file
	replacement.Size = replacementInfo.Size()
	replacement.Mtime = pgtype.Timestamptz{Time: replacementInfo.ModTime().Truncate(time.Microsecond), Valid: true}
	replacement.MediaInfo = []byte(`{"duration":95}`)
	after, reason, err := currentSegmentFileSnapshot(replacement)
	require.NoError(t, err)
	require.Empty(t, reason)
	assert.False(t, before.Equal(after), "a newly probed replacement must invalidate the fetched timings")
}
