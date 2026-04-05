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
// This runs alongside the gRPC server for CLI connectivity
// without requiring protobuf code generation.
type HTTPServer struct {
	service *ValidationServiceImpl
	server  *http.Server
	port    int
}

// HTTPServerConfig holds the configuration for the HTTP API server.
type HTTPServerConfig struct {
	Cache    *operator.StateCache
	Port     int
	Notifier notify.Notifier
}

// NewHTTPServer creates an HTTP API server.
func NewHTTPServer(cfg HTTPServerConfig) *HTTPServer {
	return &HTTPServer{
		service: &ValidationServiceImpl{
			cache:    cfg.Cache,
			notifier: cfg.Notifier,
		},
		port: cfg.Port,
	}
}

// Start begins serving HTTP requests.
func (s *HTTPServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/state", s.handleGetClusterState)
	mux.HandleFunc("/api/v1/plan", s.handleComputePlan)
	mux.HandleFunc("/api/v1/drift", s.handleGetDrift)
	mux.HandleFunc("/api/v1/resource", s.handleGetResourceStatus)

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

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
