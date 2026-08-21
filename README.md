# georism

`georism` is the HTTP search API for the Getty Thesaurus of Geographic Names
(TGN). It queries a local Solr core populated by `tgn-indexer`.

## Prerequisites

- Go 1.25 or newer.
- A local Solr 9 instance, available at `http://localhost:8983/solr`.
- A Getty Linked Art TGN JSON-LD archive, or an extracted directory containing
  the JSON files. The relational `*.out` source dump is not accepted directly
  by the indexer.

The examples below assume the repository root as the starting directory and
the default values in [`server/config.toml`](server/config.toml).

## Start Solr and create the cores

Start Solr using its installation's `bin/solr` command, then create both cores
from this repository's configset. Set `SOLR_BIN` to the directory containing
Solr's `bin` directory if necessary.

```bash
export SOLR_BIN=/path/to/solr
$SOLR_BIN/bin/solr start -p 8983

$SOLR_BIN/bin/solr create -c tgn_live -d ./solr/configsets/tgn/conf
$SOLR_BIN/bin/solr create -c tgn_indexing -d ./solr/configsets/tgn/conf
```

The indexer requires both cores. It clears `tgn_indexing`, builds the new
documents there, and swaps it with `tgn_live` only after a successful commit.
This keeps the API available while a rebuild is running.

Verify the Solr service and cores before indexing:

```bash
curl -s 'http://localhost:8983/solr/admin/cores?action=STATUS&wt=json'
```

If the schema changes, reload or recreate both cores from
`solr/configsets/tgn/conf` before rebuilding the index.

## Configure the service

[`server/config.toml`](server/config.toml) controls the HTTP address, page size,
indexing batch size, and Solr connection:

```toml
[server]
addr = ":8080"

[solr]
url = "http://localhost:8983/solr"
live_core = "tgn_live"
indexing_core = "tgn_indexing"
```

To use another configuration file, set `GEORISM_CONFIG` to its path. The path
is resolved from the process working directory.

## Build the Solr index

Run the indexer from `server/`. Use exactly one input option:

```bash
cd server

# Recommended: index a Getty Linked Art archive.
go run ./cmd/tgn-indexer --input-archive ../tgn_linkedart_0426.tar.gz

# Or index an extracted Linked Art JSON-LD directory.
go run ./cmd/tgn-indexer --input-dir /path/to/extracted/tgn
```

The command logs progress, commits the indexing core, swaps it with the live
core, and exits. Re-running it is safe: it performs a complete replacement of
the live index.

## Run the API

With Solr indexed, start the server in a separate terminal:

```bash
cd server
go run ./cmd/georism
```

For a non-default configuration:

```bash
cd server
GEORISM_CONFIG=/path/to/config.toml go run ./cmd/georism
```

## API

Endpoint: `GET /places`

- Search: `GET /places?q=<place-name>[&page=<positive-integer>]`
- ID lookup: `GET /places/{tgn-id}`

Examples:

```bash
curl 'http://localhost:8080/places?q=Genf'
curl 'http://localhost:8080/places/7003746'
```

Search uses boosted preferred and alternate name fields. An ID lookup returns
one place object or `404`; a search without `q` returns `400`.

## Name languages

`label_lang` is a two-item `[name, language]` array for the primary label; the
language is `null` when Getty does not provide one. Each `ancestor_pairs` item
also includes `label_lang` in the same form.

`alternate_names` remains a plain list for backwards compatibility.
`alternate_names_languages` contains the canonical Getty name records used to
derive it:

```json
{
  "alternate_names": ["Geneva", "Genf"],
  "alternate_names_languages": [
    {"name": "Geneva", "language": "en"},
    {"name": "Genf", "language": "de"}
  ]
}
```
