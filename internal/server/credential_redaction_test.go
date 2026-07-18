package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

const credentialedURLPath = "https://alice:correct-horse@storage.example/media/Music/Album/file.flac"

func TestLibraryViewsRedactCredentialsByDefault(t *testing.T) {
	t.Parallel()

	lib := sqlc.Library{
		ID:        7,
		Name:      "Music",
		MediaType: sqlc.MediaTypeMusic,
		Paths:     []string{credentialedURLPath, "/srv/media"},
		Settings:  []byte(`{}`),
	}
	view := toLibraryView(lib, service.EnvManagedLibrary{})
	if got, want := view.Paths[0], "https://xxxxx@storage.example/media/Music/Album/file.flac"; got != want {
		t.Fatalf("redacted path = %q, want %q", got, want)
	}
	if view.Paths[1] != "/srv/media" {
		t.Fatalf("local path changed: %q", view.Paths[1])
	}
	if lib.Paths[0] != credentialedURLPath {
		t.Fatal("response redaction mutated the stored model")
	}
}

func TestLibraryFileResponseRedactsPathAndError(t *testing.T) {
	t.Parallel()

	file := sqlc.LibraryFile{
		Path:         credentialedURLPath,
		ErrorMessage: "open " + credentialedURLPath + ": permission denied",
	}
	got := redactLibraryFileForResponse(file)
	if strings.Contains(got.Path, "alice") || strings.Contains(got.ErrorMessage, "correct-horse") {
		t.Fatalf("credentials remain in response: %#v", got)
	}
	if file.Path != credentialedURLPath {
		t.Fatal("response redaction mutated source row")
	}
}

func TestStorageAndMediaFileViewsRedactCredentials(t *testing.T) {
	t.Parallel()

	storage := pathStorage("Music", credentialedURLPath)
	if strings.Contains(storage.Path, "alice") || strings.Contains(storage.Error, "correct-horse") {
		t.Fatalf("credentials remain in storage response: %#v", storage)
	}
	media := buildMediaFileInfo(sqlc.LibraryFile{ID: 1, Path: credentialedURLPath})
	if strings.Contains(media.Path, "alice") || strings.Contains(media.Path, "correct-horse") {
		t.Fatalf("credentials remain in media-file response: %#v", media)
	}
}

func TestTaskItemsResponseRedactsWithoutMutatingServiceResult(t *testing.T) {
	t.Parallel()

	result := &service.TaskItemsResult{Items: []service.TaskItem{{
		Path:  credentialedURLPath,
		Error: "probe " + credentialedURLPath + ": failed",
	}}}
	got := redactTaskItemsForResponse(result)
	if strings.Contains(got.Items[0].Path+got.Items[0].Error, "correct-horse") {
		t.Fatalf("credentials remain in task response: %#v", got.Items[0])
	}
	if result.Items[0].Path != credentialedURLPath {
		t.Fatal("response redaction mutated the service result")
	}
}

func TestHumaErrorTransformerIsFinalCredentialBoundary(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	api := newHumaAPI(mux, nil)
	huma.Register(api, op(http.MethodGet, "/credential-error", "credential-error", "Credential error", "Test"),
		func(context.Context, *struct{}) (*JSONOutput[struct{}], error) {
			return nil, huma.Error500InternalServerError(
				"open "+credentialedURLPath,
				errors.New("nested "+credentialedURLPath),
			)
		})
	response := humatest.Wrap(t, api).Get("/credential-error")
	result := response.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", result.StatusCode, http.StatusInternalServerError)
	}
	body := response.Body.String()
	if strings.Contains(body, "alice") || strings.Contains(body, "correct-horse") {
		t.Fatalf("credentials remain in Huma error response: %s", body)
	}
	if !strings.Contains(body, "https://xxxxx@storage.example") {
		t.Fatalf("redacted path missing from Huma error response: %s", body)
	}
}

func TestRedactHumaErrorResponseDoesNotMutateSource(t *testing.T) {
	t.Parallel()

	source := &huma.ErrorModel{
		Detail: credentialedURLPath,
		Errors: []*huma.ErrorDetail{{
			Message: credentialedURLPath,
			Value:   map[string]any{"path": credentialedURLPath},
		}},
	}
	value, err := redactHumaErrorResponse(nil, "500", source)
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}
	redacted := value.(*huma.ErrorModel)
	if strings.Contains(redacted.Detail, "correct-horse") || strings.Contains(redacted.Errors[0].Message, "alice") {
		t.Fatalf("transformed model still contains credentials: %#v", redacted)
	}
	if source.Detail != credentialedURLPath || source.Errors[0].Message != credentialedURLPath {
		t.Fatal("transformer mutated the source error model")
	}
}
