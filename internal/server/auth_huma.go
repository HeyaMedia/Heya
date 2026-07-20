package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/requestmeta"
	"github.com/karbowiak/heya/internal/securityevents"
	"github.com/karbowiak/heya/internal/service"
	"github.com/rs/zerolog/log"
)

type userView struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	IsAdmin  bool   `json:"is_admin"`
}

// registerAuthRoutes mounts /api/auth/* and the /api/auth/me lookup.
func registerAuthRoutes(api huma.API, app *service.App, cfg *config.Config) {
	huma.Register(api, op(http.MethodGet, "/api/auth/registration", "registration-status", "Whether first-user registration is available", "Authentication"),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[registrationStatusBody], error) {
			if cfg == nil || !cfg.EnableRegistration.Value {
				return noStoreJSON(registrationStatusBody{Enabled: false}), nil
			}
			enabled, err := app.RegistrationAvailable(ctx)
			if err != nil {
				return nil, huma.Error503ServiceUnavailable("registration status unavailable")
			}
			return noStoreJSON(registrationStatusBody{Enabled: enabled}), nil
		})

	huma.Register(api, op(http.MethodPost, "/api/auth/register", "register", "Register a new user", "Authentication"),
		func(ctx context.Context, in *registerInput) (*authOutput, error) {
			if cfg == nil || !cfg.EnableRegistration.Value {
				return nil, huma.Error403Forbidden("registration is disabled")
			}
			if in.Body.Username == "" || in.Body.Password == "" || in.Body.Email == "" {
				return nil, huma.Error400BadRequest("username, email, and password are required")
			}
			guard := app.LoginGuard()
			clientIP := requestmeta.ClientIP(ctx)
			registrationAccount := "registration:" + in.Body.Username
			accountKey := auth.AccountKey(registrationAccount)
			if !guard.Allow(clientIP, registrationAccount) {
				app.SecurityEvents().Record(securityevents.SecurityEvent{
					Kind: securityevents.KindRegistrationThrottled, Surface: "registration",
					ClientIP: clientIP, AccountKey: accountKey, Action: "throttled",
				})
				log.Warn().Str("surface", "registration").Str("client_ip", clientIP).Msg("registration throttled")
				return nil, huma.Error429TooManyRequests("too many registration attempts; try again later")
			}
			release, ok := guard.BeginPasswordCheck()
			if !ok {
				app.SecurityEvents().Record(securityevents.SecurityEvent{
					Kind: securityevents.KindVerifierSaturated, Surface: "registration",
					ClientIP: clientIP, AccountKey: accountKey, Action: "rejected",
				})
				return nil, huma.Error429TooManyRequests("too many registration attempts; try again later")
			}
			defer release()
			user, err := app.RegisterFirstUser(ctx, in.Body.Username, in.Body.Email, in.Body.Password)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusConflict)
			}
			token, err := app.CreateAuthSession(ctx, user.ID, in.UserAgent, "")
			if err != nil {
				return nil, huma.Error500InternalServerError("failed to create session")
			}
			return newAuthOutput(ctx, token, user, in.ClientSurface), nil
		})

	huma.Register(api, op(http.MethodPost, "/api/auth/login", "login", "Login", "Authentication"),
		func(ctx context.Context, in *loginInput) (*authOutput, error) {
			if in.Body.Username == "" || in.Body.Password == "" {
				return nil, huma.Error400BadRequest("username and password are required")
			}
			guard := app.LoginGuard()
			clientIP := requestmeta.ClientIP(ctx)
			accountKey := auth.AccountKey(in.Body.Username)
			if !guard.Allow(clientIP, in.Body.Username) {
				app.SecurityEvents().Record(securityevents.SecurityEvent{
					Kind: securityevents.KindLoginThrottled, Surface: "heya",
					ClientIP: clientIP, AccountKey: accountKey, Action: "throttled",
				})
				log.Warn().Str("surface", "heya").Str("client_ip", clientIP).Str("account_key", accountKey).
					Msg("login throttled")
				return nil, huma.Error429TooManyRequests("too many login attempts; try again later")
			}
			release, ok := guard.BeginPasswordCheck()
			if !ok {
				app.SecurityEvents().Record(securityevents.SecurityEvent{
					Kind: securityevents.KindVerifierSaturated, Surface: "heya",
					ClientIP: clientIP, AccountKey: accountKey, Action: "rejected",
				})
				log.Warn().Str("surface", "heya").Str("client_ip", clientIP).Msg("password verifier saturated")
				return nil, huma.Error429TooManyRequests("too many login attempts; try again later")
			}
			defer release()

			user, err := app.Authenticate(ctx, in.Body.Username, in.Body.Password)
			if err != nil {
				app.SecurityEvents().Record(securityevents.SecurityEvent{
					Kind: securityevents.KindLoginFailed, Surface: "heya",
					ClientIP: clientIP, AccountKey: accountKey, Action: "rejected",
				})
				log.Warn().Str("surface", "heya").Str("client_ip", clientIP).Str("account_key", accountKey).
					Msg("login failed")
				return nil, huma.Error401Unauthorized("invalid credentials")
			}
			guard.ClearAccount(in.Body.Username)
			token, err := app.CreateAuthSession(ctx, user.ID, in.UserAgent, clientIP)
			if err != nil {
				return nil, huma.Error500InternalServerError("failed to create session")
			}
			return newAuthOutput(ctx, token, user, in.ClientSurface), nil
		})

	huma.Register(api, op(http.MethodPost, "/api/auth/logout", "logout", "Logout", "Authentication"),
		func(ctx context.Context, in *logoutInput) (*logoutOutput, error) {
			token := in.tokenFromHeader()
			if token != "" {
				_ = app.DeleteSession(ctx, token)
			}
			out := &logoutOutput{SetCookie: expiredSessionCookie(ctx), CacheControl: "no-store"}
			out.Body.Status = "logged out"
			return out, nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/auth/me", "me", "Current user", "Authentication")),
		func(ctx context.Context, in *meInput) (*userAuthOutput, error) {
			out := &userAuthOutput{CacheControl: "no-store", Body: toUserView(userFrom(ctx))}
			if isWebClientSurface(in.ClientSurface) {
				out.SetCookie = sessionCookie(ctx, auth.TokenFromContext(ctx))
			}
			return out, nil
		})
}

type registrationStatusBody struct {
	Enabled bool `json:"enabled"`
}

type registerInput struct {
	UserAgent     string `header:"User-Agent" required:"false" doc:"Captured into the session so the user can recognise this device on the My Sessions page"`
	ClientSurface string `header:"X-Heya-Client-Surface" required:"false" doc:"Untrusted client UX metadata; browser and tauri surfaces receive an HttpOnly cookie"`
	Body          struct {
		Username string `json:"username" minLength:"1" maxLength:"64" example:"alice" doc:"Username"`
		Email    string `json:"email" minLength:"1" maxLength:"254" format:"email" example:"alice@example.com" doc:"Email address"`
		Password string `json:"password" minLength:"15" maxLength:"256" example:"correct horse battery staple" doc:"Password"`
	}
}

type loginInput struct {
	UserAgent     string `header:"User-Agent" required:"false" doc:"Captured into the session so the user can recognise this device on the My Sessions page"`
	ClientSurface string `header:"X-Heya-Client-Surface" required:"false" doc:"Untrusted client UX metadata; browser and tauri surfaces receive an HttpOnly cookie"`
	Body          struct {
		Username string `json:"username" minLength:"1" maxLength:"64" example:"alice" doc:"Username"`
		Password string `json:"password" minLength:"1" maxLength:"256" example:"hunter2hunter2" doc:"Password"`
	}
}

type meInput struct {
	ClientSurface string `header:"X-Heya-Client-Surface" required:"false"`
}

type logoutInput struct {
	Authorization string `header:"Authorization" required:"false" doc:"Bearer <token>"`
	Cookie        string `header:"Cookie" required:"false"`
}

func (l *logoutInput) tokenFromHeader() string {
	if strings.HasPrefix(l.Authorization, "Bearer ") {
		return strings.TrimPrefix(l.Authorization, "Bearer ")
	}
	for _, part := range strings.Split(l.Cookie, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && kv[0] == "session_token" {
			return kv[1]
		}
	}
	return ""
}

type authBody struct {
	Token string   `json:"token,omitempty" doc:"Session token; omitted for browser/Tauri cookie sessions"`
	User  userView `json:"user"`
}

type authOutput struct {
	CacheControl string `header:"Cache-Control"`
	SetCookie    string `header:"Set-Cookie"`
	Body         authBody
}

type userAuthOutput struct {
	CacheControl string `header:"Cache-Control"`
	SetCookie    string `header:"Set-Cookie"`
	Body         userView
}

type logoutOutput struct {
	CacheControl string `header:"Cache-Control"`
	SetCookie    string `header:"Set-Cookie"`
	Body         struct {
		Status string `json:"status"`
	}
}

func newAuthOutput(ctx context.Context, token string, user sqlc.User, clientSurface string) *authOutput {
	bodyToken := token
	setCookie := ""
	if isWebClientSurface(clientSurface) {
		bodyToken = ""
		setCookie = sessionCookie(ctx, token)
	}
	return &authOutput{
		CacheControl: "no-store",
		SetCookie:    setCookie,
		Body:         authBody{Token: bodyToken, User: toUserView(user)},
	}
}

func isWebClientSurface(value string) bool {
	return value == "browser" || value == "tauri"
}

func sessionCookie(ctx context.Context, token string) string {
	return (&http.Cookie{ //nolint:gosec // Secure is derived from Heya's trusted ingress context; plaintext is required for local dev.
		Name:     "session_token",
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(30 * 24 * time.Hour),
		MaxAge:   30 * 24 * 60 * 60,
		HttpOnly: true,
		Secure:   requestmeta.SecureTransport(ctx),
		SameSite: http.SameSiteStrictMode,
	}).String()
}

func expiredSessionCookie(ctx context.Context) string {
	return (&http.Cookie{ //nolint:gosec // Match the original cookie's trusted-ingress Secure policy so deletion works in dev and production.
		Name:     "session_token",
		Path:     "/",
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestmeta.SecureTransport(ctx),
		SameSite: http.SameSiteStrictMode,
	}).String()
}

func toUserView(u sqlc.User) userView {
	return userView{
		ID:       u.ID,
		Username: u.Username,
		Email:    u.Email,
		IsAdmin:  u.IsAdmin,
	}
}
