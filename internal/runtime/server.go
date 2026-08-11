package runtime

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

type HTTPServerConfig struct {
	ServiceName     string
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

func RunHTTPServer(ctx context.Context, cfg HTTPServerConfig, handler http.Handler, logger *slog.Logger) error {
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = 10 * time.Second
	}

	server := &http.Server{
		Addr:         cfg.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	errc := make(chan error, 1)
	go func() {
		logger.Info("http server starting", slog.String("addr", cfg.Addr), slog.String("name", cfg.ServiceName))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
			return
		}
		errc <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		logger.Info("http server shutting down", slog.String("name", cfg.ServiceName))
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return <-errc
	case err := <-errc:
		return err
	}
}
