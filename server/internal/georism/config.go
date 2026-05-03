package georism

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server struct {
		Addr string `toml:"addr"`
	} `toml:"server"`
	Search struct {
		ResultsPerPage int `toml:"results_per_page"`
	} `toml:"search"`
	Indexer struct {
		SkipPlaceTypeLabels []string `toml:"skip_place_type_labels"`
		SolrBatchSize       int      `toml:"solr_batch_size"`
	} `toml:"indexer"`
	Solr struct {
		URL          string `toml:"url"`
		LiveCore     string `toml:"live_core"`
		IndexingCore string `toml:"indexing_core"`
	} `toml:"solr"`
}

func LoadConfig(path string) (Config, error) {
	var cfg Config
	if strings.TrimSpace(path) == "" {
		path = "config.toml"
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, fmt.Errorf("load config %s: %w", path, err)
	}

	cfg.Server.Addr = strings.TrimSpace(cfg.Server.Addr)
	if cfg.Server.Addr == "" {
		cfg.Server.Addr = ":8080"
	}
	if cfg.Search.ResultsPerPage <= 0 {
		cfg.Search.ResultsPerPage = 25
	}
	cfg.Indexer.SkipPlaceTypeLabels = normalizeLabelList(cfg.Indexer.SkipPlaceTypeLabels)
	if cfg.Indexer.SolrBatchSize <= 0 {
		cfg.Indexer.SolrBatchSize = 5000
	}
	cfg.Solr.URL = strings.TrimRight(strings.TrimSpace(cfg.Solr.URL), "/")
	cfg.Solr.LiveCore = strings.TrimSpace(cfg.Solr.LiveCore)
	cfg.Solr.IndexingCore = strings.TrimSpace(cfg.Solr.IndexingCore)

	switch {
	case cfg.Solr.URL == "":
		return Config{}, fmt.Errorf("solr.url is required")
	case cfg.Solr.LiveCore == "":
		return Config{}, fmt.Errorf("solr.live_core is required")
	case cfg.Solr.IndexingCore == "":
		return Config{}, fmt.Errorf("solr.indexing_core is required")
	default:
		return cfg, nil
	}
}

func normalizeLabelList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		label := normalizePlaceTypeLabel(value)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		normalized = append(normalized, label)
	}
	return normalized
}

func normalizePlaceTypeLabel(value string) string {
	return normalizeText(strings.TrimSpace(value))
}

func ConfigPathFromEnv() string {
	if path := strings.TrimSpace(os.Getenv("GEORISM_CONFIG")); path != "" {
		return path
	}
	return "config.toml"
}
