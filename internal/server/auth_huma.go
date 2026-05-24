package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

type userView struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	IsAdmin  bool   `json:"is_admin"`
}

// registerAuthRoutes mounts /api/auth/* and the /api/auth/me lookup.
func registerAuthRoutes(api huma.API, app *service.App) {
	huma.Register(api, op(http.MethodPost, "/api/auth/register", "register", "Register a new user", "Authentication"),
		func(ctx context.Context, in *registerInput) (*JSONOutput[authBody], error) {
			if in.Body.Username == "" || in.Body.Password == "" || in.Body.Email == "" {
				return nil, huma.Error400BadRequest("username, email, and password are required")
			}
			user, err := app.CreateUser(ctx, in.Body.Username, in.Body.Email, in.Body.Password, false)
			if err != nil {
				return nil, huma.Error409Conflict(err.Error())
			}
			token, err := app.CreateSession(ctx, user.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError("failed to create session")
			}
			return noStoreJSON(authBody{Token: token, User: toUserView(user)}), nil
		})

	huma.Register(api, op(http.MethodPost, "/api/auth/login", "login", "Login", "Authentication"),
		func(ctx context.Context, in *loginInput) (*JSONOutput[authBody], error) {
			if in.Body.Username == "" || in.Body.Password == "" {
				return nil, huma.Error400BadRequest("username and password are required")
			}
			user, err := app.Authenticate(ctx, in.Body.Username, in.Body.Password)
			if err != nil {
				return nil, huma.Error401Unauthorized("invalid credentials")
			}
			token, err := app.CreateSession(ctx, user.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError("failed to create session")
			}
			return noStoreJSON(authBody{Token: token, User: toUserView(user)}), nil
		})

	huma.Register(api, op(http.MethodPost, "/api/auth/logout", "logout", "Logout", "Authentication"),
		func(ctx context.Context, in *logoutInput) (*StatusOutput, error) {
			token := in.tokenFromHeader()
			if token != "" {
				_ = app.DeleteSession(ctx, token)
			}
			return statusOK("logged out"), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/auth/me", "me", "Current user", "Authentication")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[userView], error) {
			return noStoreJSON(toUserView(userFrom(ctx))), nil
		})
}

type registerInput struct {
	Body struct {
		Username string `json:"username" minLength:"1" maxLength:"64" example:"alice" doc:"Username"`
		Email    string `json:"email" minLength:"1" maxLength:"254" format:"email" example:"alice@example.com" doc:"Email address"`
		Password string `json:"password" minLength:"8" maxLength:"256" example:"hunter2hunter2" doc:"Password"`
	}
}

type loginInput struct {
	Body struct {
		Username string `json:"username" minLength:"1" maxLength:"64" example:"alice" doc:"Username"`
		Password string `json:"password" minLength:"1" maxLength:"256" example:"hunter2hunter2" doc:"Password"`
	}
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
	Token string   `json:"token" doc:"Session token"`
	User  userView `json:"user"`
}

func toUserView(u sqlc.User) userView {
	return userView{
		ID:       u.ID,
		Username: u.Username,
		Email:    u.Email,
		IsAdmin:  u.IsAdmin,
	}
}
