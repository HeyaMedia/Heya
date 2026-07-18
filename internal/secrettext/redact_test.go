package secrettext

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestRedact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "credentialed URL",
			input: "https://alice:correct-horse@example.test:8443/media/Music/Album/file.flac",
			want:  "https://xxxxx@example.test:8443/media/Music/Album/file.flac",
		},
		{name: "username only", input: "ftp://guest@example.test/share", want: "ftp://xxxxx@example.test/share"},
		{
			name:  "percent encoded userinfo",
			input: "sftp://domain%5Cuser:p%40ss%2Fword@example.test/share/a%20file.mkv",
			want:  "sftp://xxxxx@example.test/share/a%20file.mkv",
		},
		{
			name:  "raw URL subdelimiters in userinfo",
			input: "webdav+https://domain;user:p!$&'()*+,;=@example.test/share",
			want:  "webdav+https://xxxxx@example.test/share",
		},
		{name: "raw at in malformed password", input: "https://user:pa@ss@example.test/share", want: "https://xxxxx@example.test/share"},
		{name: "malformed repeated at", input: "open https://user:pass@@example.test/share failed", want: "open https://xxxxx@example.test/share failed"},
		{name: "malformed missing host", input: "bad https://user:pass@", want: "bad https://xxxxx@"},
		{
			name:  "multiple URLs",
			input: "from sftp://u1:p1@one.example/share to https://u2:p%402@two.example/path",
			want:  "from sftp://xxxxx@one.example/share to https://xxxxx@two.example/path",
		},
		{
			name:  "adjacent URLs",
			input: "ftp://u1:p1@one.example,https://u2:p2@two.example",
			want:  "ftp://xxxxx@one.example,https://xxxxx@two.example",
		},
		{
			name:  "literal path delimiters survive",
			input: "https://u:p@example.test/share/Foreign/$#-! title?.mkv",
			want:  "https://xxxxx@example.test/share/Foreign/$#-! title?.mkv",
		},
		{name: "public URL", input: "https://example.com/a?b=c", want: "https://example.com/a?b=c"},
		{name: "local path", input: "/srv/media/user:pass@host/file", want: "/srv/media/user:pass@host/file"},
		{name: "email-like text", input: "user:pass@example.com", want: "user:pass@example.com"},
		{name: "empty", input: "", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := Redact(test.input); got != test.want {
				t.Fatalf("Redact(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestRedactError(t *testing.T) {
	t.Parallel()
	err := RedactError(errors.New("open https://user:pass@example.test/share: denied"))
	if got, want := err.Error(), "open https://xxxxx@example.test/share: denied"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if RedactError(nil) != nil {
		t.Fatal("nil error must remain nil")
	}
}

func TestRedactStringsReturnsIndependentCopy(t *testing.T) {
	t.Parallel()

	input := []string{"https://user:pass@example.test/share", "/srv/media"}
	got := RedactStrings(input)
	if got[0] != "https://xxxxx@example.test/share" || got[1] != input[1] {
		t.Fatalf("unexpected redacted strings: %#v", got)
	}
	got[1] = "changed"
	if input[0] != "https://user:pass@example.test/share" || input[1] != "/srv/media" {
		t.Fatalf("source slice mutated: %#v", input)
	}
}

func TestRedactJSONOrText(t *testing.T) {
	t.Parallel()

	input := `{"path":"https://user:pass@example.test/share/file.mkv","nested":[{"error":"open sftp://a:b@host.test/share"},42,true],"public":"https://example.com/x"}`
	got := RedactJSONOrText(input)

	var value map[string]any
	if err := json.Unmarshal([]byte(got), &value); err != nil {
		t.Fatalf("result is not JSON: %v (%q)", err, got)
	}
	if value["path"] != "https://xxxxx@example.test/share/file.mkv" {
		t.Fatalf("path = %#v", value["path"])
	}
	nested := value["nested"].([]any)
	if nested[0].(map[string]any)["error"] != "open sftp://xxxxx@host.test/share" {
		t.Fatalf("nested error = %#v", nested[0])
	}
	if value["public"] != "https://example.com/x" {
		t.Fatalf("public URL changed: %#v", value["public"])
	}
}

func TestRedactJSONOrTextFallsBackToText(t *testing.T) {
	t.Parallel()
	input := "not json: https://user:pass@example.test/share"
	want := "not json: https://xxxxx@example.test/share"
	if got := RedactJSONOrText(input); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRedactMapClonesNestedValues(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"https://key:secret@host.test/share": []any{"sftp://user:pass@nas.test/media", map[string]any{
			"error": "open ftp://a:b@other.test/share: failed",
		}},
		"paths": []string{"https://list:secret@nas.test/media"},
		"labels": map[string]string{
			"https://map-key:secret@nas.test/share": "https://map-value:secret@nas.test/share",
		},
	}
	got := RedactMap(input)
	values, ok := got["https://xxxxx@host.test/share"].([]any)
	if !ok {
		t.Fatalf("redacted key/value missing: %#v", got)
	}
	if values[0] != "sftp://xxxxx@nas.test/media" {
		t.Fatalf("nested path = %#v", values[0])
	}
	if input["https://key:secret@host.test/share"].([]any)[0] != "sftp://user:pass@nas.test/media" {
		t.Fatal("redaction mutated the source structure")
	}
	if got["paths"].([]string)[0] != "https://xxxxx@nas.test/media" {
		t.Fatalf("string slice was not redacted: %#v", got["paths"])
	}
	labels := got["labels"].(map[string]string)
	if labels["https://xxxxx@nas.test/share"] != "https://xxxxx@nas.test/share" {
		t.Fatalf("string map was not redacted: %#v", labels)
	}
}
