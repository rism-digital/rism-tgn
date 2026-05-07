# AGENTS.md

## Purpose
This repository contains Getty TGN data and a Solr-based indexing pipeline for full-text search of place names, with support for alternate names, hierarchies, and geospatial data.

## Repo Layout
- `*.out`: Getty TGN REL source dump files (archived reference data).
- `tgn_rel_0126/`: Extracted TGN JSON-LD files for indexing.
- `server/cmd/tgn-indexer/`: Solr index builder CLI.
- `server/cmd/georism/`: Go HTTP server for TGN search API.
- `server/internal/georism/`: Solr repository and HTTP handler.
- `solr/configsets/tgn/`: Solr configuration (schema and core config).

## Indexing Flow
1. Run `tgn-indexer` with either:
    - `--input-dir <path>` for extracted JSON-LD files
    - `--input-archive <path.tar.gz>` for compressed archive
2. Indexer parses TGN JSON-LD, extracts place metadata (IDs, terms, parents, coordinates)
3. Builds ancestor chains recursively using in-memory cache
4. Posts documents to Solr indexing core
5. Swaps indexing core with live core for zero-downtime updates

`./server/cmd/tgn-indexer --input-dir ./tgn_rel_0126` builds the Solr index.

## Data Model
Each indexed place document contains:
- `tgn_id`: Getty TGN numeric ID
- `preferred_term`: Primary place name
- `matched_terms`: All alternate names
- `alternate_names`: Secondary alternate names
- `place_type_id`, `place_type_label`: Getty AAT place type
- `parent_subject_id`: Parent place in hierarchy
- `lat`, `lon`: Geographic coordinates
- `ancestor_pairs_json`: Full ancestor chain as JSON
- `text`: Combined searchable text field

## Search Configuration
Solr uses Dismax query parser with field boosting:
- `preferred_term_text^100`: Primary field for queries
- `alternate_names_text^60`: Alternate names
- `text^1`: Fallback to all text fields

Phrase boosts prioritize exact matches:
- `preferred_term_text^200`, `alternate_names_text^120`

## Safe Edit Rules For Future Agents
- Indexer is idempotent: each run clears and rebuilds the index
- If changing document schema, update `solrPlaceDocument` struct in `solr_index.go`
- If changing search behavior, update query parameters in `solr_repository.go`
- If adding new source files, update indexer configuration in `config.toml`

## Quick Ops Commands
- Build Solr index from directory:
    - `./server/cmd/tgn-indexer --input-dir ./tgn_rel_0126`
- Build Solr index from archive:
    - `./server/cmd/tgn-indexer --input-archive tgn_linkedart_0426.tar.gz`
- Run search API:
    - `cd server && go run ./cmd/georism`
- Query API:
    - `curl 'http://localhost:8080/places?q=Genf'`