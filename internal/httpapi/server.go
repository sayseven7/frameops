package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sayseven7/frameops/internal/domain"
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
	case strings.HasPrefix(request.URL.Path, "/v1/engagements/") && strings.HasSuffix(request.URL.Path, "/findings"):
		server.findings(response, request)
	case strings.HasPrefix(request.URL.Path, "/v1/engagements/") && strings.HasSuffix(request.URL.Path, "/assets"):
		server.assets(response, request)
	case strings.HasPrefix(request.URL.Path, "/v1/findings/") && strings.HasSuffix(request.URL.Path, "/assets"):
		server.findingAssets(response, request)
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

func (server Server) findings(response http.ResponseWriter, request *http.Request) {
	engagementID := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/v1/engagements/"), "/findings")
	if engagementID == "" || strings.Contains(engagementID, "/") {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, request.Method == http.MethodPost)
	if !ok {
		return
	}
	switch request.Method {
	case http.MethodGet:
		items, err := postgres.ListFindings(request.Context(), server.pool, session, engagementID)
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
			Title        string `json:"title"`
			Description  string `json:"description"`
			Impact       string `json:"impact"`
			Remediation  string `json:"remediation"`
			Reproduction string `json:"reproduction"`
			CVSSVector   string `json:"cvssVector"`
		}
		if !decodeJSON(request, &input) || strings.TrimSpace(input.Title) == "" {
			writeError(response, http.StatusBadRequest, "invalid_request")
			return
		}
		cvss, err := domain.ParseCVSS31(input.CVSSVector)
		if err != nil {
			writeError(response, http.StatusBadRequest, "invalid_cvss_vector")
			return
		}
		finding, err := postgres.CreateFinding(request.Context(), server.pool, session, engagementID, postgres.Finding{Title: input.Title, Description: input.Description, Impact: input.Impact, Remediation: input.Remediation, Reproduction: input.Reproduction, CVSSVector: cvss.Vector, CVSSScore: cvss.Score})
		if errors.Is(err, postgres.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusCreated, finding)
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

// maxFindingAssets bounds one finding-to-asset replacement so a single request
// cannot be used to enumerate or rewrite an unbounded set of identifiers.
const maxFindingAssets = 64

func (server Server) assets(response http.ResponseWriter, request *http.Request) {
	engagementID := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/v1/engagements/"), "/assets")
	if engagementID == "" || strings.Contains(engagementID, "/") {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, request.Method == http.MethodPost)
	if !ok {
		return
	}
	switch request.Method {
	case http.MethodGet:
		items, err := postgres.ListAssets(request.Context(), server.pool, session, engagementID)
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
		asset, err := postgres.CreateAsset(request.Context(), server.pool, session, engagementID, input.Name)
		if errors.Is(err, postgres.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusCreated, asset)
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

func (server Server) findingAssets(response http.ResponseWriter, request *http.Request) {
	findingID := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/v1/findings/"), "/assets")
	if findingID == "" || strings.Contains(findingID, "/") {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, request.Method == http.MethodPut)
	if !ok {
		return
	}
	switch request.Method {
	case http.MethodGet:
		items, err := postgres.ListFindingAssets(request.Context(), server.pool, session, findingID)
		if errors.Is(err, postgres.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": items})
	case http.MethodPut:
		var input struct {
			AssetIDs []string `json:"assetIds"`
		}
		if !decodeJSON(request, &input) || len(input.AssetIDs) > maxFindingAssets {
			writeError(response, http.StatusBadRequest, "invalid_request")
			return
		}
		items, err := postgres.ReplaceFindingAssets(request.Context(), server.pool, session, findingID, input.AssetIDs)
		if errors.Is(err, postgres.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": items})
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
