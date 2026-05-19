package server

import (
	"net/http"

	"github.com/karbowiak/kura/internal/auth"
	"github.com/karbowiak/kura/internal/database/sqlc"
	"github.com/karbowiak/kura/internal/service"
)

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authResponse struct {
	Token string   `json:"token"`
	User  userView `json:"user"`
}

type userView struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	IsAdmin  bool   `json:"is_admin"`
}

func handleRegister(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req registerRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.Username == "" || req.Password == "" || req.Email == "" {
			writeError(w, http.StatusBadRequest, "username, email, and password are required")
			return
		}

		user, err := app.CreateUser(r.Context(), req.Username, req.Email, req.Password, false)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}

		token, err := app.CreateSession(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create session")
			return
		}

		writeJSON(w, http.StatusCreated, authResponse{
			Token: token,
			User:  toUserView(user),
		})
	}
}

func handleLogin(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.Username == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "username and password are required")
			return
		}

		user, err := app.Authenticate(r.Context(), req.Username, req.Password)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}

		token, err := app.CreateSession(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create session")
			return
		}

		writeJSON(w, http.StatusOK, authResponse{
			Token: token,
			User:  toUserView(user),
		})
	}
}

func handleLogout(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if len(token) > 7 {
			token = token[7:]
		}
		if c, err := r.Cookie("session_token"); err == nil && token == "" {
			token = c.Value
		}

		if token != "" {
			app.DeleteSession(r.Context(), token)
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
	}
}

func handleMe(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		writeJSON(w, http.StatusOK, toUserView(user))
	}
}

func toUserView(u sqlc.User) userView {
	return userView{
		ID:       u.ID,
		Username: u.Username,
		Email:    u.Email,
		IsAdmin:  u.IsAdmin,
	}
}
