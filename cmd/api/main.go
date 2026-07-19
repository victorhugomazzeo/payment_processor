package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	dsn := os.Getenv("DATABASE_URL")

	if dsn == "" {
		slog.Error("DATABASE_URL is empty")
		os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, dsn)

	if err != nil {
		slog.Error("database pool failed", "err", err)
		os.Exit(1)
	}

	defer pool.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {

		w.Header().Add("Content-Type", "application/json")

		if err := pool.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"degraded"}`))

			slog.Error("database degraded", "err", err)
			return
		}

		w.Write([]byte(`{"status":"ok"}`))

		slog.Info("request", "method", r.Method, "path", r.URL.Path)

	})

	slog.Info("server started", "addr", ":8082")

	srv := &http.Server{Addr: ":8082", Handler: mux}

	go func() {
		err := srv.ListenAndServe()

		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("serve failed", "err", err)
			os.Exit(1)
		}

	}()

	<-ctx.Done()

	ctxTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	defer cancel()

	if err := srv.Shutdown(ctxTimeout); err != nil {
		slog.Error("shutdown failed", "err", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}
