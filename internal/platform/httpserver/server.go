package httpserver

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/dhkimxx/crowsnest/internal/application"
)

const defaultMaxBodyBytes = 2 << 20

type Server struct {
	service       *application.IngestService
	webhookSecret string
	maxBodyBytes  int64
	logger        *slog.Logger
}

func New(service *application.IngestService, webhookSecret string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{service: service, webhookSecret: webhookSecret, maxBodyBytes: defaultMaxBodyBytes, logger: logger}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.health)
	mux.HandleFunc("POST /webhook/gitlab", s.webhook)
	return mux
}

func (s *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	_, _ = writer.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) webhook(writer http.ResponseWriter, request *http.Request) {
	if s.webhookSecret == "" {
		http.Error(writer, "webhook authentication is not configured", http.StatusServiceUnavailable)
		return
	}
	provided := request.Header.Get("X-Gitlab-Token")
	if subtle.ConstantTimeCompare([]byte(provided), []byte(s.webhookSecret)) != 1 {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.service == nil {
		http.Error(writer, "webhook service is not configured", http.StatusServiceUnavailable)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, s.maxBodyBytes)
	body, err := io.ReadAll(request.Body)
	if err != nil {
		if errors.Is(err, http.ErrBodyReadAfterClose) {
			http.Error(writer, "invalid request body", http.StatusBadRequest)
			return
		}
		http.Error(writer, "request body could not be read", http.StatusBadRequest)
		return
	}
	result, err := s.service.Handle(request.Context(), request.Header, body)
	if err != nil {
		s.logger.Error("webhook handling failed", "source_event", request.Header.Get("X-Gitlab-Event"), "error", err)
		http.Error(writer, "webhook processing failed", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(result)
}

func (s *Server) SetMaxBodyBytes(value int64) {
	if value > 0 {
		s.maxBodyBytes = value
	}
}

func (s *Server) String() string {
	return fmt.Sprintf("gitlab webhook server secret_configured=%t", strings.TrimSpace(s.webhookSecret) != "")
}
