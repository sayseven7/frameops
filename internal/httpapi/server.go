package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sayseven7/frameops/internal/store/postgres"
)

const sessionCookieName = "__Host-frameops_session"

type Server struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) http.Handler {
	return Server{pool: pool}
}

func (server Server) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	switch {
	case request.Method == http.MethodPost && request.URL.Path == "/v1/session/login":
		server.login(response, request)
	case request.Method == http.MethodGet && request.URL.Path == "/v1/csrf":
		server.csrf(response, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/session/logout":
		server.logout(response, request)
	case request.URL.Path == "/v1/clients":
		server.clients(response, request)
	case strings.HasPrefix(request.URL.Path, "/v1/clients/") && strings.HasSuffix(request.URL.Path, "/engagements"):
		server.engagements(response, request)
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

func (server Server) login(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(request, &input) || strings.TrimSpace(input.Email) == "" || input.Password == "" {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return
	}
	token, err := postgres.Authenticate(request.Context(), server.pool, input.Email, input.Password)
	if errors.Is(err, postgres.ErrUnauthorized) {
		writeError(response, http.StatusUnauthorized, "invalid_credentials")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	http.SetCookie(response, &http.Cookie{Name: sessionCookieName, Value: token, Path: "/", MaxAge: int((12 * time.Hour).Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	response.WriteHeader(http.StatusNoContent)
}

func (server Server) csrf(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, false)
	if !ok {
		return
	}
	token, err := postgres.IssueCSRF(request.Context(), server.pool, session)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, map[string]string{"token": token})
}

func (server Server) logout(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, true)
	if !ok {
		return
	}
	if err := postgres.RevokeSession(request.Context(), server.pool, session); err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	http.SetCookie(response, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	response.WriteHeader(http.StatusNoContent)
}

func (server Server) clients(response http.ResponseWriter, request *http.Request) {
	session, ok := server.session(response, request, request.Method == http.MethodPost)
	if !ok {
		return
	}
	switch request.Method {
	case http.MethodGet:
		items, err := postgres.ListClients(request.Context(), server.pool, session)
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var input struct {
			Name string `json:"name"`
		}
		if !decodeJSON(request, &input) || strings.TrimSpace(input.Name) == "" {
			writeError(response, http.StatusBadRequest, "invalid_request")
			return
		}
		client, err := postgres.CreateClient(request.Context(), server.pool, session, input.Name)
		switch {
		case errors.Is(err, postgres.ErrForbidden):
			writeError(response, http.StatusForbidden, "forbidden")
		case err != nil:
			writeError(response, http.StatusInternalServerError, "internal_error")
		default:
			writeJSON(response, http.StatusCreated, client)
		}
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

func (server Server) engagements(response http.ResponseWriter, request *http.Request) {
	clientID := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/v1/clients/"), "/engagements")
	if clientID == "" || strings.Contains(clientID, "/") {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, request.Method == http.MethodPost)
	if !ok {
		return
	}
	switch request.Method {
	case http.MethodGet:
		items, err := postgres.ListEngagements(request.Context(), server.pool, session, clientID)
		if errors.Is(err, postgres.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var input struct {
			Name string `json:"name"`
		}
		if !decodeJSON(request, &input) || strings.TrimSpace(input.Name) == "" {
			writeError(response, http.StatusBadRequest, "invalid_request")
			return
		}
		engagement, err := postgres.CreateEngagement(request.Context(), server.pool, session, clientID, input.Name)
		switch {
		case errors.Is(err, postgres.ErrNotFound):
			writeError(response, http.StatusNotFound, "not_found")
		case errors.Is(err, postgres.ErrForbidden):
			writeError(response, http.StatusForbidden, "forbidden")
		case err != nil:
			writeError(response, http.StatusInternalServerError, "internal_error")
		default:
			writeJSON(response, http.StatusCreated, engagement)
		}
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

func (server Server) session(response http.ResponseWriter, request *http.Request, requireCSRF bool) (postgres.Session, bool) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return postgres.Session{}, false
	}
	session, err := postgres.SessionForToken(request.Context(), server.pool, cookie.Value)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return postgres.Session{}, false
	}
	if requireCSRF && !postgres.ValidCSRF(session, request.Header.Get("X-CSRF-Token")) {
		writeError(response, http.StatusForbidden, "forbidden")
		return postgres.Session{}, false
	}
	return session, true
}

func decodeJSON(request *http.Request, destination any) bool {
	if request.ContentLength > 1<<20 {
		return false
	}
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}

func writeError(response http.ResponseWriter, status int, code string) {
	writeJSON(response, status, map[string]string{"error": code})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
