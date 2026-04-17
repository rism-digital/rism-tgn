package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/rs/zerolog"

	"georism/internal/georism"
)

func main() {
	output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	logger := zerolog.New(output).With().Timestamp().Logger()

	dbURL := stringsOr(os.Getenv("DATABASE_URL"), "")
	if dbURL == "" {
		logger.Error().Msg("DATABASE_URL is required")
		os.Exit(1)
	}
	dbURL = withDefaultSSLModeDisable(dbURL)

	addr := stringsOr(os.Getenv("GEORISM_ADDR"), ":8080")

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		logger.Error().Err(err).Msg("open database")
		os.Exit(1)
	}
	defer db.Close()

	configureDBPool(db)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		logger.Error().Err(err).Msg("ping database")
		os.Exit(1)
	}

	handler := georism.NewHandler(georism.NewSQLRepository(db))
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info().Str("addr", addr).Msg("starting georism")
		errCh <- server.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info().Str("signal", sig.String()).Msg("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error().Err(err).Msg("server shutdown")
			os.Exit(1)
		}
		logger.Info().Msg("server stopped")
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error().Err(err).Msg("server failed")
			os.Exit(1)
		}
	}
}

func configureDBPool(db *sql.DB) {
	maxOpen := envInt("GEORISM_DB_MAX_OPEN", 20)
	maxIdle := envInt("GEORISM_DB_MAX_IDLE", 10)
	maxLifetimeSec := envInt("GEORISM_DB_MAX_LIFETIME_SEC", 300)

	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(time.Duration(maxLifetimeSec) * time.Second)
}

func envInt(name string, def int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return def
	}
	return v
}

func stringsOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func withDefaultSSLModeDisable(dsn string) string {
	if strings.Contains(dsn, "://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return dsn
		}
		q := u.Query()
		if q.Get("sslmode") == "" {
			q.Set("sslmode", "disable")
			u.RawQuery = q.Encode()
		}
		return u.String()
	}

	lower := strings.ToLower(dsn)
	if strings.Contains(lower, "sslmode=") {
		return dsn
	}
	return strings.TrimSpace(dsn) + " sslmode=disable"
}
