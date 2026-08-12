package runtimegateway

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"strings"
)

type Server struct {
	tokenHash [32]byte
	runtimes  RuntimeProvider
}

type runtimeActivityTracker interface {
	BeginUse(string) func()
}

func NewServer(token string, runtimes RuntimeProvider) *Server {
	return &Server{tokenHash: sha256.Sum256([]byte(token)), runtimes: runtimes}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeGatewayJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.Handle("/user/{userID}/{$}", s.authenticate(http.HandlerFunc(s.proxy)))
	mux.Handle("/user/{userID}/{path...}", s.authenticate(http.HandlerFunc(s.proxy)))
	return mux
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := strings.Fields(r.Header.Get("Authorization"))
		provided := ""
		if len(authorization) == 2 && strings.EqualFold(authorization[0], "Bearer") {
			provided = authorization[1]
		}
		providedHash := sha256.Sum256([]byte(provided))
		if provided == "" || subtle.ConstantTimeCompare(providedHash[:], s.tokenHash[:]) != 1 {
			writeGatewayJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) proxy(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	if !validUserID(userID) {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid user"})
		return
	}
	release := func() {}
	if tracker, ok := s.runtimes.(runtimeActivityTracker); ok {
		release = tracker.BeginUse(userID)
	}
	defer release()
	target, err := s.runtimes.Ensure(r.Context(), userID)
	if err != nil {
		slog.Error("runtime gateway: ensure Agent runtime failed", "user_id", userID, "error", err)
		writeGatewayJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Agent runtime unavailable"})
		return
	}
	upstreamPath := r.PathValue("path")
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target.URL)
			request.Out.URL.Path = "/" + strings.TrimPrefix(upstreamPath, "/")
			request.Out.URL.RawPath = ""
			request.Out.Host = target.URL.Host
			request.Out.Header.Del("Authorization")
			request.Out.SetBasicAuth(target.Username, target.Password)
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			slog.Error("runtime gateway: upstream proxy failed", "user_id", userID, "error", err)
			writeGatewayJSON(w, http.StatusBadGateway, map[string]any{"error": "Agent runtime request failed"})
		},
	}
	proxy.ServeHTTP(w, r)
}

func writeGatewayJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
