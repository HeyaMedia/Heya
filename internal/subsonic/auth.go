package subsonic

import (
	"context"
	"crypto/md5" //nolint:gosec // G501: the Subsonic protocol mandates md5(password+salt) token auth
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

// Subsonic authentication, per request (the protocol is stateless — every
// call carries credentials):
//
//	u + p          plaintext password ("enc:"-hex form included)
//	u + t + s      t = md5(password + s)   — the 1.13.0+ token scheme
//	apiKey         OpenSubsonic apiKeyAuthentication extension
//
// All three verify against the user's *Subsonic credential* (a
// server-minted app password, internal/service/subsonic_credentials.go) —
// never the Heya login password: bcrypt hashes can't answer the md5 token,
// and a per-app secret is revocable without a password change anyway.
//
// Error codes follow the spec: 10 missing params, 40 wrong credentials,
// 43 conflicting mechanisms (apiKey + u/p/t/s), 44 invalid API key.

type ctxKey int

const ctxUser ctxKey = iota

// userFrom returns the authenticated user injected by requireAuth.
func userFrom(ctx context.Context) (sqlc.User, bool) {
	u, ok := ctx.Value(ctxUser).(sqlc.User)
	return u, ok
}

// authError pairs a Subsonic error code with its message.
type authError struct {
	code    int
	message string
}

// authenticate resolves the request's credentials to a user.
func (s *Server) authenticate(r *http.Request) (sqlc.User, *authError) {
	ctx := r.Context()
	username := param(r, "u")
	password := param(r, "p")
	token := param(r, "t")
	salt := param(r, "s")
	apiKey := param(r, "apiKey")

	if apiKey != "" {
		// The extension is explicit: apiKey must stand alone.
		if username != "" || password != "" || token != "" {
			return sqlc.User{}, &authError{errAuthConflict, "conflicting authentication mechanisms"}
		}
		user, err := s.app.SubsonicAuthBySecret(ctx, apiKey)
		if err != nil {
			if errors.Is(err, service.ErrSubsonicNoCredential) {
				return sqlc.User{}, &authError{errInvalidAPIKey, "invalid API key"}
			}
			return sqlc.User{}, &authError{errGeneric, "authentication backend error"}
		}
		s.app.TouchSubsonicCredential(user.ID)
		return user, nil
	}

	if username == "" {
		return sqlc.User{}, &authError{errMissingParameter, `required parameter "u" is missing`}
	}
	if password == "" && (token == "" || salt == "") {
		return sqlc.User{}, &authError{errMissingParameter, `provide either "p" or both "t" and "s"`}
	}

	user, secret, err := s.app.SubsonicAuthByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, service.ErrSubsonicNoCredential) {
			// Unknown user and known-user-without-credential answer alike —
			// no account enumeration. The message still hints at the one
			// self-serve fix a legitimate user needs.
			return sqlc.User{}, &authError{errWrongCredentials,
				"wrong username or password (Subsonic clients sign in with the app password from Settings, not the Heya login password)"}
		}
		return sqlc.User{}, &authError{errGeneric, "authentication backend error"}
	}

	if !verifySecret(secret, password, token, salt) {
		return sqlc.User{}, &authError{errWrongCredentials, "wrong username or password"}
	}
	s.app.TouchSubsonicCredential(user.ID)
	return user, nil
}

// verifySecret checks p= (plain or enc:hex) or t/s against the stored
// secret. Comparisons are constant-time; the md5 use is protocol-mandated,
// not a design choice.
func verifySecret(secret, password, token, salt string) bool {
	if password != "" {
		if strings.HasPrefix(password, "enc:") {
			decoded, err := hex.DecodeString(password[len("enc:"):])
			if err != nil {
				return false
			}
			password = string(decoded)
		}
		return subtle.ConstantTimeCompare([]byte(password), []byte(secret)) == 1
	}
	sum := md5.Sum([]byte(secret + salt)) //nolint:gosec // G401: protocol-mandated
	want := hex.EncodeToString(sum[:])
	// ConstantTimeCompare returns 0 on length mismatch, which is fine here:
	// token length is not a secret.
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(token)), []byte(want)) == 1
}

// requireAuth wraps a handler with credential verification. Failures still
// answer HTTP 200 — the error lives in the envelope, per protocol.
func (s *Server) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, aerr := s.authenticate(r)
		if aerr != nil {
			respondError(w, r, aerr.code, aerr.message)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), ctxUser, user)))
	}
}

// requireAdmin further gates on Heya's is_admin flag (Subsonic error 50).
func (s *Server) requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		u, _ := userFrom(r.Context())
		if !u.IsAdmin {
			respondError(w, r, errNotAuthorized, "admin only")
			return
		}
		h(w, r)
	})
}

// param reads a request parameter from query or form body (the OpenSubsonic
// formPost extension — some clients POST x-www-form-urlencoded to keep
// credentials out of URLs). ParseForm merges both; it is idempotent.
func param(r *http.Request, name string) string {
	_ = r.ParseForm()
	return r.Form.Get(name)
}

// paramAll returns every value of a repeated parameter (scrobble id=...).
func paramAll(r *http.Request, name string) []string {
	_ = r.ParseForm()
	return r.Form[name]
}
