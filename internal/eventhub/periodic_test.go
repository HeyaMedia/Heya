package eventhub

import (
	"strings"
	"testing"

	"github.com/karbowiak/heya/internal/queueops"
)

func TestQueueSnapshotDoesNotPresentRemotePollAsDiskScan(t *testing.T) {
	status, active, _, _ := queueSnapshotCounts([]queueops.TaskKindCounts{{
		Kind: "fetch_metadata", LibraryID: 4, Poll: true, Pending: 9, Running: 1,
	}})
	if status.Pending != 9 || status.Running != 1 {
		t.Fatalf("poll work disappeared from queue totals: %+v", status)
	}
	if len(active) != 0 {
		t.Fatalf("remote poll work presented as active disk scan: %+v", active)
	}
}

func TestRedactActiveJobArguments(t *testing.T) {
	t.Parallel()

	job := redactActiveJob(ActiveJob{
		ID:       7,
		ArgsJSON: `{"file_path":"https://reader:super-secret@storage.test/share/movie.mkv","nested":{"scope_paths":["sftp://other:hunter2@storage.test/music"]}}`,
	})

	if strings.Contains(job.ArgsJSON, "super-secret") || strings.Contains(job.ArgsJSON, "hunter2") {
		t.Fatalf("active job arguments retained URL credentials: %s", job.ArgsJSON)
	}
	if !strings.Contains(job.ArgsJSON, "storage.test/share/movie.mkv") {
		t.Fatalf("active job argument redaction removed the useful path: %s", job.ArgsJSON)
	}
}
