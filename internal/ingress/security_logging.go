package ingress

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/karbowiak/heya/internal/securityevents"
	"go.uber.org/zap/zapcore"
)

// heyaSecurityLogCore is a Caddy logging tee. Coraza emits matched-rule
// details through its Caddy logger even in DetectionOnly mode; this core
// extracts a deliberately small safe subset for the admin dashboard while the
// ordinary Caddy log keeps flowing to stderr unchanged.
type heyaSecurityLogCore struct {
	fields []zapcore.Field
}

func (heyaSecurityLogCore) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.logging.cores.heya_security",
		New: func() caddy.Module { return new(heyaSecurityLogCore) },
	}
}

func (*heyaSecurityLogCore) Enabled(zapcore.Level) bool { return true }

func (c *heyaSecurityLogCore) With(fields []zapcore.Field) zapcore.Core {
	next := &heyaSecurityLogCore{fields: make([]zapcore.Field, 0, len(c.fields)+len(fields))}
	next.fields = append(next.fields, c.fields...)
	next.fields = append(next.fields, fields...)
	return next
}

func (c *heyaSecurityLogCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if strings.Contains(entry.LoggerName, "http.handlers.waf") {
		return checked.AddCore(entry, c)
	}
	return checked
}

func (c *heyaSecurityLogCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	manager := activeManager.Load()
	if manager == nil || manager.securityEvents == nil || !strings.Contains(entry.LoggerName, "http.handlers.waf") {
		return nil
	}
	allFields := make([]zapcore.Field, 0, len(c.fields)+len(fields))
	allFields = append(allFields, c.fields...)
	allFields = append(allFields, fields...)
	if entry.Message == "WAF rule violation detected" {
		values := zapFields(allFields)
		manager.securityEvents.Record(securityevents.SecurityEvent{
			Kind:          securityevents.KindWAFBlock,
			Surface:       "caddy",
			ClientIP:      normalizePeer(stringValue(values["client_ip"])),
			Action:        "blocked",
			Message:       "Request blocked by the OWASP Core Rule Set",
			Path:          safeRequestPath(stringValue(values["uri"])),
			TransactionID: stringValue(values["unique_id"]),
		})
		return nil
	}

	parsed := parseCorazaError(entry.Message)
	if parsed["id"] == "" {
		return nil
	}
	manager.securityEvents.Record(securityevents.SecurityEvent{
		Kind:          securityevents.KindWAFMatch,
		Surface:       "caddy",
		ClientIP:      normalizePeer(parsed["client"]),
		Action:        "detected",
		RuleID:        parsed["id"],
		Severity:      strings.ToLower(parsed["severity"]),
		Message:       parsed["msg"],
		Path:          safeRequestPath(parsed["uri"]),
		TransactionID: parsed["unique_id"],
	})
	return nil
}

func (*heyaSecurityLogCore) Sync() error { return nil }

func zapFields(fields []zapcore.Field) map[string]any {
	encoder := zapcore.NewMapObjectEncoder()
	for _, field := range fields {
		field.AddTo(encoder)
	}
	return encoder.Fields
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

// parseCorazaError extracts only quoted ModSecurity-style metadata fields.
// The potentially sensitive [data] and matched-variable text are ignored.
func parseCorazaError(message string) map[string]string {
	fields := make(map[string]string)
	for cursor := 0; cursor < len(message); {
		open := strings.IndexByte(message[cursor:], '[')
		if open < 0 {
			break
		}
		open += cursor
		space := strings.IndexByte(message[open+1:], ' ')
		if space < 0 {
			break
		}
		space += open + 1
		key := message[open+1 : space]
		if space+2 >= len(message) || message[space+1] != '"' {
			cursor = space + 1
			continue
		}
		quotedEnd := findQuotedEnd(message, space+1)
		if quotedEnd < 0 {
			break
		}
		if key == "client" || key == "id" || key == "msg" || key == "severity" || key == "uri" || key == "unique_id" {
			quoted := message[space+1 : quotedEnd+1]
			if value, err := strconv.Unquote(quoted); err == nil {
				fields[key] = value
			}
		}
		cursor = quotedEnd + 1
	}
	return fields
}

func findQuotedEnd(value string, start int) int {
	escaped := false
	for i := start + 1; i < len(value); i++ {
		if escaped {
			escaped = false
			continue
		}
		if value[i] == '\\' {
			escaped = true
			continue
		}
		if value[i] == '"' {
			return i
		}
	}
	return -1
}

func safeRequestPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Path == "" {
		if idx := strings.IndexByte(raw, '?'); idx >= 0 {
			raw = raw[:idx]
		}
		return raw
	}
	return parsed.EscapedPath()
}

func normalizePeer(peer string) string {
	peer = strings.TrimSpace(peer)
	if host, _, err := net.SplitHostPort(peer); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(peer, "[]")
}

var _ caddy.Module = (*heyaSecurityLogCore)(nil)
var _ zapcore.Core = (*heyaSecurityLogCore)(nil)
