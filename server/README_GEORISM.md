# georism

Simple Go HTTP service for TGN place search.

## Run

First, build the Solr index:

```bash
./server/cmd/tgn-indexer --input-dir ./tgn_rel_0126
# or
./server/cmd/tgn-indexer --input-archive tgn_linkedart_0426.tar.gz
```

Then run the server:

```bash
export GEORISM_ADDR=':8080' # optional, defaults to :8080
export GEORISM_CONFIG='config.toml' # optional

cd server
go run ./cmd/georism
```

Configure Solr connection in `config.toml`:
- `solr.url`: Solr base URL (required)
- `solr.live_core`: Active search core name
- `solr.indexing_core`: Indexing core name

## API

Endpoint: `GET /places`

Rules:
- search uses query parameter `q`
- ID lookup uses path parameter `/places/{id}`
- `/places` without `q` returns `400`

### Search

```bash
curl 'http://localhost:8080/places?q=Genf'
```

Uses Solr search with field boosting on `preferred_term`, `alternate_names`, and full-text fields.

### ID lookup

```bash
curl 'http://localhost:8080/places/7003746'
```

Returns one place object or `404`.