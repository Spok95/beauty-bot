package http

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	srv *http.Server
	mux *http.ServeMux
}

func New(addr string, exposeMetrics bool) *Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	if exposeMetrics {
		mux.Handle("/metrics", promhttp.Handler())
	}

	return &Server{
		srv: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
		mux: mux,
	}
}

// Handle позволяет в main регистрировать свои ручки.
func (s *Server) Handle(pattern string, handler http.Handler) {
	s.mux.Handle(pattern, handler)
}

func (s *Server) Start() error {
	return s.srv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
