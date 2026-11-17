// Package httpapi exposes the rate server over a small read-only JSON API.
package httpapi

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/config"
	"github.com/alisaitteke/simple-rate-server/internal/engine"
	"github.com/alisaitteke/simple-rate-server/internal/model"
	"github.com/alisaitteke/simple-rate-server/internal/version"
)

// Server wires the engine into HTTP handlers.
type Server struct {
	engine *engine.Engine
	cfg    config.Config
	log    *slog.Logger
}

// New builds an HTTP server around eng.
func New(eng *engine.Engine, cfg config.Config, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{engine: eng, cfg: cfg, log: log}
}

// Handler returns the fully wrapped HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /v1/latest", s.handleLatest)
	mux.HandleFunc("GET /v1/currencies", s.handleCurrencies)
	mux.HandleFunc("GET /v1/sources", s.handleSources)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)

	var h http.Handler = mux
	h = s.cors(h)
	h = s.recoverer(h)
	h = s.requestLogger(h)
	return h
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":    "rate-server",
		"version": version.Version,
		"endpoints": []string{
			"GET /v1/latest?base=USD&symbols=EUR,TRY,BTC",
			"GET /v1/currencies?kind=fiat|crypto",
			"GET /v1/sources",
			"GET /healthz",
			"GET /readyz",
		},
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	st := s.engine.State()
	if st == nil || !st.HasData() {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "no rates loaded yet", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ready",
		"version":  st.Version,
		"built_at": st.BuiltAt.Format(time.RFC3339),
		"assets":   len(st.Assets),
	})
}

func (s *Server) handleCurrencies(w http.ResponseWriter, r *http.Request) {
	var kind model.Kind
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind"))) {
	case "":
	case "fiat":
		kind = model.KindFiat
	case "crypto":
		kind = model.KindCrypto
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "kind must be fiat or crypto", "kind")
		return
	}
	list := s.engine.Assets(kind)
	writeJSON(w, http.StatusOK, map[string]any{
		"count":  len(list),
		"assets": list,
	})
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	st := s.engine.State()
	ready := st != nil && st.HasData()
	fiatStrategy, cryptoStrategy := s.engine.Strategies()
	resp := map[string]any{
		"ready":   ready,
		"version": s.engine.Version(),
		"strategy": map[string]string{
			"fiat":   string(fiatStrategy),
			"crypto": string(cryptoStrategy),
		},
		"sources": s.engine.Status(),
	}
	if st != nil {
		resp["built_at"] = st.BuiltAt.Format(time.RFC3339)
		resp["assets"] = len(st.Assets)
	}
	writeJSON(w, http.StatusOK, resp)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		}
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			level = slog.LevelDebug
		}
		s.log.Log(r.Context(), level, "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration", time.Since(start).String(),
			"remote", r.RemoteAddr,
		)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic serving request", "error", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal", "internal server error", "")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	if !s.cfg.CORS {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, If-None-Match")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
