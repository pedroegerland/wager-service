package httpapi

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type Config struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
	RateLimitRPS    float64
	RateLimitBurst  int
}

type Server struct {
	srv *http.Server
	cfg Config
	log *slog.Logger
}

func NewServer(cfg Config, handler http.Handler, log *slog.Logger) *Server {
	return &Server{
		srv: &http.Server{
			Addr:              cfg.Addr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       60 * time.Second,
		},
		cfg: cfg,
		log: log,
	}
}

func (s *Server) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	s.log.Info("http listening", "addr", ln.Addr().String())
	go func() {
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.log.Error("http server stopped", "err", err)
		}
	}()
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.ShutdownTimeout)
	defer cancel()
	return s.srv.Shutdown(ctx)
}
