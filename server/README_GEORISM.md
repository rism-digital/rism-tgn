# georism

Simple Go HTTP service for TGN lookups.

## Run

```bash
export DATABASE_URL='postgresql://user:pass@localhost:5432/mydb'
export GEORISM_ADDR=':8080' # optional

cd server
go run ./cmd/georism
```

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

Uses:
- `tgn.match_place_name($1, NULL, NULL, 10)`

### ID lookup

```bash
curl 'http://localhost:8080/places/7003746'
```

Returns one place object or `404`.

Uses:
- `tgn.get_place_by_id($1)`
