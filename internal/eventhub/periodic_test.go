package eventhub

import (
	"strings"
	"testing"
)

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
