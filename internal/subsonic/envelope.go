package subsonic

import (
	"encoding/xml"
	"net/http"
	"regexp"
	"time"

	json "github.com/goccy/go-json"
)

// One envelope, two serializations. Subsonic responses are XML by default
// and JSON when f=json (JSONP when f=jsonp&callback=...), all wrapped in
// <subsonic-response> / {"subsonic-response": {...}}. OpenSubsonic adds the
// mandatory type/serverVersion/openSubsonic envelope fields. Every DTO in
// dto.go carries both xml and json tags; scalar fields are XML attributes,
// nested entities are child elements — and goccy (which ignores omitzero)
// only ever sees pointer-+-omitempty optionals, never omitzero.
//
// Protocol quirk worth its comment: the HTTP status is ALWAYS 200, even for
// errors — clients key exclusively off the envelope's status + error code.
// Only binary endpoints (stream, download, getCoverArt) speak real HTTP.

const (
	apiVersion    = "1.16.1"
	serverType    = "heya"
	serverVersion = "0.1.x"
	xmlns         = "http://subsonic.org/restapi"
)

// Subsonic error codes (api.jsp) + OpenSubsonic additions (42-44).
const (
	errGeneric          = 0
	errMissingParameter = 10
	errClientTooOld     = 20
	errServerTooOld     = 30
	errWrongCredentials = 40
	errTokenUnsupported = 41
	errAuthMechanism    = 42 // provided authentication mechanism not supported
	errAuthConflict     = 43 // multiple conflicting auth mechanisms
	errInvalidAPIKey    = 44
	errNotAuthorized    = 50
	errTrialExpired     = 60
	errNotFound         = 70
)

// subTime renders timestamps the way Subsonic clients expect (ISO 8601,
// UTC) in both serializations.
type subTime time.Time

func (t subTime) format() string {
	return time.Time(t).UTC().Format("2006-01-02T15:04:05Z")
}

func (t subTime) MarshalJSON() ([]byte, error) {
	return []byte(`"` + t.format() + `"`), nil
}

func (t subTime) MarshalXMLAttr(name xml.Name) (xml.Attr, error) {
	return xml.Attr{Name: name, Value: t.format()}, nil
}

func subTimePtr(t time.Time) *subTime {
	if t.IsZero() {
		return nil
	}
	st := subTime(t)
	return &st
}

// subError is the <error code=".." message=".."/> payload.
type subError struct {
	XMLName xml.Name `xml:"error" json:"-"`
	Code    int      `xml:"code,attr" json:"code"`
	Message string   `xml:"message,attr,omitempty" json:"message,omitempty"`
}

// xmlEnvelope is the XML serialization shell. Payload is any DTO from
// dto.go — encoding/xml names the element from the DTO's XMLName field; a
// nil interface writes nothing (bare ok responses).
type xmlEnvelope struct {
	XMLName       xml.Name `xml:"subsonic-response"`
	Xmlns         string   `xml:"xmlns,attr"`
	Status        string   `xml:"status,attr"`
	Version       string   `xml:"version,attr"`
	Type          string   `xml:"type,attr"`
	ServerVersion string   `xml:"serverVersion,attr"`
	OpenSubsonic  bool     `xml:"openSubsonic,attr"`
	Payload       any
}

// responseFormat picks the serialization from the f parameter.
type responseFormat struct {
	json     bool
	callback string // non-empty = JSONP
}

func formatOf(r *http.Request) responseFormat {
	switch param(r, "f") {
	case "json":
		return responseFormat{json: true}
	case "jsonp":
		cb := param(r, "callback")
		if !jsonpCallbackRe.MatchString(cb) {
			cb = "callback" // spec has no defined fallback; a safe name beats reflected script
		}
		return responseFormat{json: true, callback: cb}
	}
	return responseFormat{}
}

// jsonpCallbackRe keeps JSONP from becoming an XSS reflector: plain
// identifier-with-dots callbacks only.
var jsonpCallbackRe = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*(\.[A-Za-z_$][A-Za-z0-9_$]*)*$`)

// respond writes a success envelope. key names the payload member in JSON
// ("artists", "album", ...); payload may be nil for bare `ok` responses
// (ping, scrobble, star...). The XML element name comes from the payload
// struct's XMLName and must match key — dto.go keeps them aligned.
func respond(w http.ResponseWriter, r *http.Request, key string, payload any) {
	writeEnvelope(w, r, "ok", key, payload, nil)
}

// respondError writes a failed envelope with the given Subsonic error code.
func respondError(w http.ResponseWriter, r *http.Request, code int, message string) {
	writeEnvelope(w, r, "failed", "", nil, &subError{Code: code, Message: message})
}

func writeEnvelope(w http.ResponseWriter, r *http.Request, status, key string, payload any, subErr *subError) {
	f := formatOf(r)
	if f.json {
		body := map[string]any{
			"status":        status,
			"version":       apiVersion,
			"type":          serverType,
			"serverVersion": serverVersion,
			"openSubsonic":  true,
		}
		if subErr != nil {
			body["error"] = subErr
		}
		if payload != nil && key != "" {
			body[key] = payload
		}
		buf, err := json.Marshal(map[string]any{"subsonic-response": body})
		if err != nil {
			http.Error(w, "marshal failure", http.StatusInternalServerError)
			return
		}
		if f.callback != "" {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(f.callback + "("))
			_, _ = w.Write(buf)
			_, _ = w.Write([]byte(");"))
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf)
		return
	}

	env := xmlEnvelope{
		Xmlns:         xmlns,
		Status:        status,
		Version:       apiVersion,
		Type:          serverType,
		ServerVersion: serverVersion,
		OpenSubsonic:  true,
	}
	if subErr != nil {
		env.Payload = subErr
	} else {
		env.Payload = payload
	}
	buf, err := xml.Marshal(env)
	if err != nil {
		http.Error(w, "marshal failure", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(buf)
}
