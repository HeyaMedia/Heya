// Package secrettext removes credentials from text that may cross a logging
// or response boundary. It is deliberately dependency-free so low-level
// packages (including vfs) and higher-level API packages can share it.
package secrettext

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const redactedUserinfo = "xxxxx"

// Match standard URL schemes without assigning transport-specific meaning.
// Library URLs are rejected elsewhere, but credentials may still appear in
// provider errors, legacy database rows, or other diagnostic text.
var urlScheme = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://`)

// Redact replaces URL userinfo in arbitrary text while preserving the URL's
// scheme, host, port, path, query, and fragment. It intentionally does not
// parse whole URLs: filesystem paths and error strings can contain malformed
// URLs or literal '#', '?', and '%' bytes, and those must survive unchanged.
//
// The last '@' in an authority is the delimiter. This safely handles raw '@'
// bytes in malformed passwords and percent-encoded credentials without ever
// copying any part of the original userinfo to the result.
func Redact(input string) string {
	searchFrom := 0
	lastWrite := 0
	changed := false
	var out strings.Builder

	for searchFrom < len(input) {
		match := urlScheme.FindStringIndex(input[searchFrom:])
		if match == nil {
			break
		}
		schemeStart := searchFrom + match[0]
		authorityStart := searchFrom + match[1]
		authorityEnd := authorityBoundary(input, authorityStart)

		// Arbitrary text can contain adjacent URLs separated only by punctuation.
		// Do not let the later URL's '@' become the delimiter for the first one.
		if next := urlScheme.FindStringIndex(input[authorityStart:]); next != nil && next[0] > 0 && authorityStart+next[0] < authorityEnd {
			authorityEnd = authorityStart + next[0]
		}

		authority := input[authorityStart:authorityEnd]
		if at := strings.LastIndexByte(authority, '@'); at >= 0 {
			if !changed {
				out.Grow(len(input))
			}
			out.WriteString(input[lastWrite:authorityStart])
			out.WriteString(redactedUserinfo)
			out.WriteByte('@')
			lastWrite = authorityStart + at + 1
			changed = true
		}

		// Always make progress, including malformed URL-like values.
		searchFrom = authorityEnd
		if searchFrom <= schemeStart {
			searchFrom = authorityStart
		}
	}

	if !changed {
		return input
	}
	out.WriteString(input[lastWrite:])
	return out.String()
}

// RedactStrings returns a redacted copy of input. It never aliases or mutates
// the source slice, which makes it safe for response and logging views.
func RedactStrings(input []string) []string {
	redacted := make([]string, len(input))
	for i, value := range input {
		redacted[i] = Redact(value)
	}
	return redacted
}

func authorityBoundary(input string, start int) int {
	for i := start; i < len(input); {
		r, size := utf8.DecodeRuneInString(input[i:])
		if unicode.IsSpace(r) || r == '/' || r == '?' || r == '#' || r == '"' || r == '<' || r == '>' || r == '`' {
			return i
		}
		i += size
	}
	return len(input)
}

// RedactJSONOrText recursively redacts every string in a JSON document. If
// input is not exactly one JSON value, it is treated as ordinary text. This
// is suitable for River's JSON args/errors columns without changing the
// stored values or their execution semantics.
func RedactJSONOrText(input string) string {
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return Redact(input)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Redact(input)
	}

	value = redactJSONValue(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return Redact(input)
	}
	return string(encoded)
}

// RedactMap returns a recursively cloned, JSON-shaped map with URL credentials
// removed from every string key and value. Structured log fields and
// diagnostic JSON can use it without mutating retained source data.
func RedactMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	return redactJSONValue(input).(map[string]any)
}

// RedactError returns an error safe to attach to logs or expose in a response.
// It is intentionally for presentation boundaries only: callers that need
// errors.Is/errors.As semantics should inspect the original error first.
func RedactError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	redacted := Redact(message)
	if redacted == message {
		return err
	}
	return errors.New(redacted)
}

func redactJSONValue(value any) any {
	switch value := value.(type) {
	case string:
		return Redact(value)
	case []any:
		redacted := make([]any, len(value))
		for i := range value {
			redacted[i] = redactJSONValue(value[i])
		}
		return redacted
	case []string:
		return RedactStrings(value)
	case map[string]any:
		redacted := make(map[string]any, len(value))
		for key, child := range value {
			redacted[Redact(key)] = redactJSONValue(child)
		}
		return redacted
	case map[string]string:
		redacted := make(map[string]string, len(value))
		for key, child := range value {
			redacted[Redact(key)] = Redact(child)
		}
		return redacted
	default:
		return value
	}
}
