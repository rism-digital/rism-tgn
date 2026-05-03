package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"georism/internal/georism"
)

func main() {
	output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	logger := zerolog.New(output).With().Timestamp().Logger()

	cfg, err := georism.LoadConfig(georism.ConfigPathFromEnv())
	if err != nil {
		logger.Error().Err(err).Msg("load config")
		os.Exit(1)
	}

	repo, err := georism.OpenSolrRepository(cfg)
	if err != nil {
		logger.Error().Err(err).Msg("open Solr repository")
		os.Exit(1)
	}
	defer repo.Close()

	var repository georism.Repository = repo
	handler := georism.NewHandler(repository, cfg.Search.ResultsPerPage)
	server := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info().Str("addr", cfg.Server.Addr).Msg("starting georism")
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
