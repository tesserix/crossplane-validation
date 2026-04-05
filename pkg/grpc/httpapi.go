package grpc

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/tesserix/crossplane-validation/pkg/notify"
	"github.com/tesserix/crossplane-validation/pkg/operator"
)

// HTTPServer serves the validation API over HTTP/JSON.
type HTTPServer struct {
	service  *ValidationServiceImpl
	server   *http.Server
	port     int
	apiToken string
}

// HTTPServerConfig holds the configuration for the HTTP API server.
type HTTPServerConfig struct {
	Cache    *operator.StateCache
	Port     int
	Notifier notify.Notifier
	APIToken string
}

// NewHTTPServer creates an HTTP API server.
func NewHTTPServer(cfg HTTPServerConfig) *HTTPServer {
	return &HTTPServer{
		service: &ValidationServiceImpl{
			cache:    cfg.Cache,
			notifier: cfg.Notifier,
		},
		port:     cfg.Port,
		apiToken: cfg.APIToken,
	}
}

// Start begins serving HTTP requests.
func (s *HTTPServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/state", s.requireAuth(s.handleGetClusterState))
	mux.HandleFunc("/api/v1/plan", s.requireAuth(s.handleComputePlan))
	mux.HandleFunc("/api/v1/drift", s.requireAuth(s.handleGetDrift))
	mux.HandleFunc("/api/v1/resource", s.requireAuth(s.handleGetResourceStatus))
	mux.HandleFunc("/api/v1/resolve", s.requireAuth(s.handleResolveResources))

	s.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", s.port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
	}

	lis, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("listening on port %d: %w", s.port, err)
	}

	log.Printf("HTTP API server listening on :%d", s.port)
	return s.server.Serve(lis)
}

// Stop gracefully shuts down the HTTP server.
func (s *HTTPServer) Stop() {
	if s.server != nil {
		s.server.Close()
	}
}

func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp, err := s.service.Health(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, resp)
}

func (s *HTTPServer) handleGetClusterState(w http.ResponseWriter, r *http.Request) {
	namespace := r.URL.Query().Get("namespace")
	kind := r.URL.Query().Get("kind")
	apiGroup := r.URL.Query().Get("apiGroup")

	resp, err := s.service.GetClusterState(r.Context(), namespace, kind, apiGroup)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, resp)
}

func (s *HTTPServer) handleComputePlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("POST required"))
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestPayload+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	showSensitive := r.URL.Query().Get("showSensitive") == "true"

	resp, err := s.service.ComputePlan(r.Context(), body, showSensitive)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, resp)
}

func (s *HTTPServer) handleGetDrift(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("POST required"))
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestPayload+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	resp, err := s.service.GetDrift(r.Context(), body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, resp)
}

func (s *HTTPServer) handleGetResourceStatus(w http.ResponseWriter, r *http.Request) {
	apiVersion := r.URL.Query().Get("apiVersion")
	kind := r.URL.Query().Get("kind")
	name := r.URL.Query().Get("name")
	namespace := r.URL.Query().Get("namespace")

	resp, err := s.service.GetResourceStatus(r.Context(), apiVersion, kind, name, namespace)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, resp)
}

func (s *HTTPServer) handleResolveResources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("POST required"))
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestPayload+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	resp, err := s.service.ResolveResources(r.Context(), body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, resp)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// requireAuth wraps a handler with bearer token authentication.
// If no API token is configured, all requests are allowed (in-cluster use).
// When a token is set, requests must include "Authorization: Bearer <token>".
func (s *HTTPServer) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.apiToken == "" {
			next(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" {
			writeError(w, http.StatusUnauthorized, fmt.Errorf("authorization header required"))
			return
		}

		const prefix = "Bearer "
		if len(auth) < len(prefix) || auth[:len(prefix)] != prefix {
			writeError(w, http.StatusUnauthorized, fmt.Errorf("bearer token required"))
			return
		}

		token := auth[len(prefix):]
		if token != s.apiToken {
			writeError(w, http.StatusForbidden, fmt.Errorf("invalid token"))
			return
		}

		next(w, r)
	}
}
