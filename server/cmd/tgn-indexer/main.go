package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"georism/internal/georism"
)

func main() {
	var (
		inputDir     = flag.String("input-dir", "", "Path to the extracted Getty TGN JSON-LD directory")
		inputArchive = flag.String("input-archive", "", "Path to the Getty TGN JSON-LD tar.gz archive")
	)
	flag.Parse()

	hasDir := *inputDir != ""
	hasArchive := *inputArchive != ""
	if hasDir == hasArchive {
		fmt.Fprintln(os.Stderr, "usage: tgn-indexer (--input-dir <dir> | --input-archive <path.tar.gz>)")
		os.Exit(2)
	}

	output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	logger := zerolog.New(output).With().Timestamp().Logger()
	log.Logger = logger

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg, err := georism.LoadConfig(georism.ConfigPathFromEnv())
	if err != nil {
		logger.Error().Err(err).Msg("load config")
		os.Exit(1)
	}

	if hasDir {
		logger.Info().Str("input_dir", *inputDir).Msg("building TGN index")
		if err := georism.BuildSolrIndex(ctx, cfg, *inputDir); err != nil {
			logger.Error().Err(err).Msg("index build failed")
			os.Exit(1)
		}
	} else {
		logger.Info().Str("input_archive", *inputArchive).Msg("building TGN index")
		if err := georism.BuildSolrIndexFromArchive(ctx, cfg, *inputArchive); err != nil {
			logger.Error().Err(err).Msg("index build failed")
			os.Exit(1)
		}
	}
	logger.Info().Msg("index build complete")
}
