package georism

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	body := `[server]
addr = ":9090"

[search]
results_per_page = 30

[indexer]
solr_batch_size = 7000
skip_place_type_labels = [" Streams ", "streams"]

[solr]
url = "http://localhost:8983/solr/"
live_core = "tgn_live"
indexing_core = "tgn_indexing"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Server.Addr != ":9090" {
		t.Fatalf("got addr %q", cfg.Server.Addr)
	}
	if cfg.Search.ResultsPerPage != 30 {
		t.Fatalf("got results per page %d", cfg.Search.ResultsPerPage)
	}
	if cfg.Indexer.SolrBatchSize != 7000 {
		t.Fatalf("got solr batch size %d", cfg.Indexer.SolrBatchSize)
	}
	if cfg.Solr.URL != "http://localhost:8983/solr" {
		t.Fatalf("got url %q", cfg.Solr.URL)
	}
	if !reflect.DeepEqual(cfg.Indexer.SkipPlaceTypeLabels, []string{"streams"}) {
		t.Fatalf("got skip labels %#v", cfg.Indexer.SkipPlaceTypeLabels)
	}
}

func TestPairtreePath(t *testing.T) {
	got, err := pairtreePath("/tmp/tgn", 2209866)
	if err != nil {
		t.Fatalf("pairtreePath returned error: %v", err)
	}
	want := filepath.Join("/tmp/tgn", "220", "986", "2209866.json")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestParsePlaceFile(t *testing.T) {
	root := t.TempDir()
	path := mustWritePairtreePlace(t, root, 3000001, `{
  "id": "https://data.getty.edu/vocab/tgn/3000001",
  "type": "Place",
  "identified_by": [
    {
      "id": "http://vocab.getty.edu/tgn/term/1",
      "type": "Name",
      "content": "Sainte-Geneviève",
      "classified_as": [
        {"id": "http://vocab.getty.edu/aat/300404650", "type": "Type", "_label": "names"},
        {"id": "http://vocab.getty.edu/aat/300404670", "type": "Type", "_label": "preferred term"}
      ]
    },
    {
      "id": "http://vocab.getty.edu/tgn/term/2",
      "type": "Name",
      "content": "Ste Genevieve",
      "classified_as": [
        {"id": "http://vocab.getty.edu/aat/300404650", "type": "Type", "_label": "names"}
      ]
    },
    {
      "id": "http://vocab.getty.edu/tgn/geometry/3000001",
      "type": "crm:E47_Spatial_Coordinates",
      "value": "[2.35,48.85]"
    }
  ],
  "classified_as": [
    {
      "id": "http://vocab.getty.edu/aat/300008347",
      "type": "Type",
      "_label": "inhabited places",
      "classified_as": [
        {"id": "http://vocab.getty.edu/aat/300435109", "type": "Type", "_label": "place types"}
      ]
    }
  ],
  "part_of": [
    {
      "id": "http://vocab.getty.edu/tgn/3000002",
      "type": "Place",
      "_label": "Test County",
      "classified_as": [
        {"id": "http://vocab.getty.edu/aat/300449152", "type": "Type", "_label": "preferred parent"}
      ]
    }
  ],
  "_label": "Sainte-Geneviève"
}`)

	place, err := parsePlaceFile(path)
	if err != nil {
		t.Fatalf("parsePlaceFile returned error: %v", err)
	}
	if place.TGNID != 3000001 {
		t.Fatalf("got id %d", place.TGNID)
	}
	if place.PreferredTerm != "Sainte-Geneviève" {
		t.Fatalf("got preferred term %q", place.PreferredTerm)
	}
	if !reflect.DeepEqual(place.Terms, []string{"Sainte-Geneviève", "Ste Genevieve"}) {
		t.Fatalf("got terms %#v", place.Terms)
	}
	if place.PlaceTypeID == nil || *place.PlaceTypeID != "http://vocab.getty.edu/aat/300008347" {
		t.Fatalf("got place type id %v", place.PlaceTypeID)
	}
}

func TestNewSolrPlaceDocumentIncludesAncestorMetadata(t *testing.T) {
	root := t.TempDir()
	path := mustWritePairtreePlace(t, root, 3000001, `{
  "id": "https://data.getty.edu/vocab/tgn/3000001",
  "type": "Place",
  "identified_by": [
    {"id": "http://vocab.getty.edu/tgn/term/1", "type": "Name", "content": "Sainte-Geneviève", "classified_as": [{"id": "http://vocab.getty.edu/aat/300404670", "type": "Type"}]},
    {"id": "http://vocab.getty.edu/tgn/term/2", "type": "Name", "content": "Ste Genevieve", "classified_as": [{"id": "http://vocab.getty.edu/aat/300404650", "type": "Type"}]},
    {"id": "http://vocab.getty.edu/tgn/geometry/3000001", "type": "crm:E47_Spatial_Coordinates", "value": "[2.35,48.85]"}
  ],
  "classified_as": [{"id": "http://vocab.getty.edu/aat/300008347", "type": "Type", "_label": "inhabited places", "classified_as": [{"id": "http://vocab.getty.edu/aat/300435109", "type": "Type"}]}],
  "part_of": [{"id": "http://vocab.getty.edu/tgn/3000002", "type": "Place", "_label": "Test County", "classified_as": [{"id": "http://vocab.getty.edu/aat/300449152", "type": "Type"}]}],
  "_label": "Sainte-Geneviève"
}`)

	builder := &indexBuilder{
		summaryCache: map[int64]placeSummary{
			7000001: {TGNID: 7000001, PreferredTerm: "Region One", Terms: []string{"Region One", "Region 1"}, PlaceTypeID: stringPtr("http://vocab.getty.edu/aat/300387064"), PlaceTypeLabel: stringPtr("regions"), ParentSubjectID: int64Ptr(ancestorStopID), ParentLabel: stringPtr("World")},
			3000002: {TGNID: 3000002, PreferredTerm: "Test County", Terms: []string{"Test County"}, PlaceTypeID: stringPtr("http://vocab.getty.edu/aat/300000776"), PlaceTypeLabel: stringPtr("counties"), ParentSubjectID: int64Ptr(7000001), ParentLabel: stringPtr("Region One")},
			3000001: {TGNID: 3000001, PreferredTerm: "Sainte-Geneviève", Terms: []string{"Sainte-Geneviève", "Ste Genevieve"}, PlaceTypeID: stringPtr("http://vocab.getty.edu/aat/300008347"), PlaceTypeLabel: stringPtr("inhabited places"), ParentSubjectID: int64Ptr(3000002), ParentLabel: stringPtr("Test County")},
		},
		pathByID: map[int64]string{},
	}

	place, err := parsePlaceFile(path)
	if err != nil {
		t.Fatalf("parsePlaceFile returned error: %v", err)
	}
	ancestors, err := builder.ancestorInfoFor(place.TGNID, map[int64]struct{}{place.TGNID: {}})
	if err != nil {
		t.Fatalf("ancestorInfoFor returned error: %v", err)
	}
	doc, err := newSolrPlaceDocument(place, ancestors)
	if err != nil {
		t.Fatalf("newSolrPlaceDocument returned error: %v", err)
	}

	if doc.Location == nil || !strings.Contains(*doc.Location, "48.850000,2.350000") {
		t.Fatalf("got location %v", doc.Location)
	}
	var pairs []map[string]any
	if err := json.Unmarshal([]byte(doc.AncestorPairs), &pairs); err != nil {
		t.Fatalf("decode ancestor pairs: %v", err)
	}
	wantPairs := []map[string]any{
		{
			"tgn_id":           float64(3000002),
			"tgn_uri":          "http://vocab.getty.edu/page/tgn/3000002",
			"label":            "Test County",
			"place_type_id":    "http://vocab.getty.edu/aat/300000776",
			"place_type_label": "counties",
		},
		{
			"tgn_id":           float64(7000001),
			"tgn_uri":          "http://vocab.getty.edu/page/tgn/7000001",
			"label":            "Region One",
			"place_type_id":    "http://vocab.getty.edu/aat/300387064",
			"place_type_label": "regions",
		},
	}
	if !reflect.DeepEqual(pairs, wantPairs) {
		t.Fatalf("got ancestor pairs %#v, want %#v", pairs, wantPairs)
	}
	if !contains(doc.Text, "Region 1") {
		t.Fatalf("expected ancestor alias in text: %#v", doc.Text)
	}
}

func TestPreloadSummariesSkipsDuplicateIDs(t *testing.T) {
	root := t.TempDir()
	first := mustWritePairtreePlace(t, root, 3000101, `{
  "id": "https://data.getty.edu/vocab/tgn/3000101",
  "type": "Place",
  "identified_by": [{"id": "http://vocab.getty.edu/tgn/term/1", "type": "Name", "content": "Duplicate One", "classified_as": [{"id": "http://vocab.getty.edu/aat/300404670", "type": "Type"}]}],
  "_label": "Duplicate One"
}`)
	dupeDir := filepath.Join(root, "dupe", "300", "010")
	if err := os.MkdirAll(dupeDir, 0o755); err != nil {
		t.Fatalf("mkdir duplicate dir: %v", err)
	}
	second := filepath.Join(dupeDir, "3000101.json")
	if err := os.WriteFile(second, []byte(`{
  "id": "https://data.getty.edu/vocab/tgn/3000101",
  "type": "Place",
  "identified_by": [{"id": "http://vocab.getty.edu/tgn/term/2", "type": "Name", "content": "Duplicate Two", "classified_as": [{"id": "http://vocab.getty.edu/aat/300404670", "type": "Type"}]}],
  "_label": "Duplicate Two"
}`), 0o644); err != nil {
		t.Fatalf("write duplicate file: %v", err)
	}

	builder := &indexBuilder{summaryCache: make(map[int64]placeSummary), pathByID: make(map[int64]string)}
	uniquePlaces, err := builder.preloadSummaries(context.Background(), []string{first, second})
	if err != nil {
		t.Fatalf("preloadSummaries returned error: %v", err)
	}
	if len(uniquePlaces) != 1 {
		t.Fatalf("got %d unique places, want 1", len(uniquePlaces))
	}
	if builder.summaryCache[3000101].PreferredTerm != "Duplicate One" {
		t.Fatalf("got preferred term %q", builder.summaryCache[3000101].PreferredTerm)
	}
	if uniquePlaces[0].place.PreferredTerm != "Duplicate One" {
		t.Fatalf("got preloaded place preferred term %q", uniquePlaces[0].place.PreferredTerm)
	}
}

func TestParsePlaceFileAcceptsSingletonPartOfObject(t *testing.T) {
	root := t.TempDir()
	path := mustWritePairtreePlace(t, root, 7568598, `{
  "id": "https://data.getty.edu/vocab/tgn/7568598",
  "type": "Place",
  "identified_by": [{"id": "http://vocab.getty.edu/tgn/term/1002135218-en", "type": "Name", "content": "Kotla Protected Forest", "classified_as": [{"id": "http://vocab.getty.edu/aat/300404650", "type": "Type"}]}],
  "classified_as": [{"id": "http://vocab.getty.edu/aat/300008863", "type": "Type", "_label": "forests (cultural landscapes)", "classified_as": [{"id": "http://vocab.getty.edu/aat/300435109", "type": "Type"}]}],
  "part_of": {"id": "http://vocab.getty.edu/tgn/1001996", "type": "Place", "_label": "Himāchal Pradesh"},
  "_label": "Kotla Protected Forest"
}`)

	place, err := parsePlaceFile(path)
	if err != nil {
		t.Fatalf("parsePlaceFile returned error: %v", err)
	}
	if place.ParentSubjectID == nil || *place.ParentSubjectID != 1001996 {
		t.Fatalf("got parent %v", place.ParentSubjectID)
	}
}

func TestBuildDocumentsSkipsConfiguredPlaceTypes(t *testing.T) {
	root := t.TempDir()
	streamPath := mustWritePairtreePlace(t, root, 3000201, `{
  "id": "https://data.getty.edu/vocab/tgn/3000201",
  "type": "Place",
  "identified_by": [{"id": "http://vocab.getty.edu/tgn/term/1", "type": "Name", "content": "Test Stream", "classified_as": [{"id": "http://vocab.getty.edu/aat/300404670", "type": "Type"}]}],
  "classified_as": [{"id": "http://vocab.getty.edu/aat/300008835", "type": "Type", "_label": "streams", "classified_as": [{"id": "http://vocab.getty.edu/aat/300435109", "type": "Type"}]}],
  "_label": "Test Stream"
}`)
	cityPath := mustWritePairtreePlace(t, root, 3000202, `{
  "id": "https://data.getty.edu/vocab/tgn/3000202",
  "type": "Place",
  "identified_by": [{"id": "http://vocab.getty.edu/tgn/term/2", "type": "Name", "content": "Test City", "classified_as": [{"id": "http://vocab.getty.edu/aat/300404670", "type": "Type"}]}],
  "classified_as": [{"id": "http://vocab.getty.edu/aat/300008347", "type": "Type", "_label": "inhabited places", "classified_as": [{"id": "http://vocab.getty.edu/aat/300435109", "type": "Type"}]}],
  "_label": "Test City"
}`)

	builder := &indexBuilder{
		summaryCache:        make(map[int64]placeSummary),
		pathByID:            make(map[int64]string),
		skippedPlaceTypeSet: makeSkipPlaceTypeSet([]string{"streams"}),
	}
	places, err := builder.preloadSummaries(context.Background(), []string{streamPath, cityPath})
	if err != nil {
		t.Fatalf("preloadSummaries returned error: %v", err)
	}

	docCh, errCh := builder.buildDocuments(context.Background(), places)
	var gotIDs []int64
	for doc := range docCh {
		gotIDs = append(gotIDs, doc.placeID)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("buildDocuments returned error: %v", err)
	}
	if !reflect.DeepEqual(gotIDs, []int64{3000202}) {
		t.Fatalf("got indexed ids %#v", gotIDs)
	}
}

func TestSolrRepositorySearchPlacesRanksAncestorContextHigher(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/tgn_live/select") {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if !strings.Contains(string(body), `"defType":"edismax"`) {
			t.Fatalf("expected JSON query body, got %s", string(body))
		}
		if !strings.Contains(string(body), `"qf":"preferred_term_text^12 alternate_names_text^8 text^1"`) {
			t.Fatalf("expected boosted qf fields in query body, got %s", string(body))
		}
		if !strings.Contains(string(body), `"offset":25`) {
			t.Fatalf("expected offset in query body, got %s", string(body))
		}
		if !strings.Contains(string(body), `"limit":25`) {
			t.Fatalf("expected limit in query body, got %s", string(body))
		}
		if !strings.Contains(string(body), `"sort":"score desc, preferred_term asc, tgn_id asc"`) {
			t.Fatalf("expected stable sort in query body, got %s", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "response": {
    "numFound": 284,
    "docs": [
      {
        "id": "3000202",
        "tgn_id": 3000202,
        "preferred_term": "Geneva",
        "preferred_term_norm": "geneva",
        "matched_terms": ["Geneva"],
        "alternate_names": [],
        "ancestor_pairs_json": "[{\"tgn_id\":7000003,\"tgn_uri\":\"http://vocab.getty.edu/page/tgn/7000003\",\"label\":\"United States\",\"place_type_id\":null,\"place_type_label\":null}]",
        "score": 12.0
      },
      {
        "id": "3000201",
        "tgn_id": 3000201,
        "preferred_term": "Geneva",
        "preferred_term_norm": "geneva",
        "matched_terms": ["Geneva"],
        "alternate_names": [],
        "ancestor_pairs_json": "[{\"tgn_id\":7000002,\"tgn_uri\":\"http://vocab.getty.edu/page/tgn/7000002\",\"label\":\"Switzerland\",\"place_type_id\":null,\"place_type_label\":null}]",
        "score": 10.0
      }
    ]
  }
}`))
	}))
	defer server.Close()

	repo, err := OpenSolrRepository(Config{Solr: struct {
		URL          string "toml:\"url\""
		LiveCore     string "toml:\"live_core\""
		IndexingCore string "toml:\"indexing_core\""
	}{URL: server.URL, LiveCore: "tgn_live"}})
	if err != nil {
		t.Fatalf("OpenSolrRepository returned error: %v", err)
	}

	page, err := repo.SearchPlaces("geneva switzerland", 2, 25)
	if err != nil {
		t.Fatalf("SearchPlaces returned error: %v", err)
	}
	if page.Total != 284 {
		t.Fatalf("expected total 284, got %d", page.Total)
	}
	if len(page.Results) < 2 {
		t.Fatalf("expected at least two results")
	}
	if page.Results[0].TGNID != 3000202 {
		t.Fatalf("expected Solr order preserved, got %d", page.Results[0].TGNID)
	}
}

func TestSolrRepositoryGetPlaceByID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/tgn_live/select") {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if !strings.Contains(string(body), `"query":"*:*"`) {
			t.Fatalf("expected match-all query body, got %s", string(body))
		}
		if !strings.Contains(string(body), `"filter":["id:3000001"]`) {
			t.Fatalf("expected id filter body, got %s", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "response": {
    "docs": [{
      "id": "3000001",
      "tgn_id": 3000001,
      "preferred_term": "Sainte-Geneviève",
      "preferred_term_norm": "sainte genevieve",
      "matched_terms": ["Sainte-Geneviève", "Ste Genevieve"],
      "alternate_names": ["Ste Genevieve"],
      "ancestor_pairs_json": "[{\"tgn_id\":3000002,\"tgn_uri\":\"http://vocab.getty.edu/page/tgn/3000002\",\"label\":\"Test County\",\"place_type_id\":\"http://vocab.getty.edu/aat/300000776\",\"place_type_label\":\"counties\"}]",
      "place_type_id": "http://vocab.getty.edu/aat/300008347",
      "place_type_label": "inhabited places",
      "parent_subject_id": 3000002,
      "lat": 48.85,
      "lon": 2.35
    }]
  }
}`))
	}))
	defer server.Close()

	repo, err := OpenSolrRepository(Config{Solr: struct {
		URL          string "toml:\"url\""
		LiveCore     string "toml:\"live_core\""
		IndexingCore string "toml:\"indexing_core\""
	}{URL: server.URL, LiveCore: "tgn_live"}})
	if err != nil {
		t.Fatalf("OpenSolrRepository returned error: %v", err)
	}

	item, err := repo.GetPlaceByID(3000001)
	if err != nil {
		t.Fatalf("GetPlaceByID returned error: %v", err)
	}
	if item == nil {
		t.Fatalf("expected place record")
	}
	if item.Score != nil {
		t.Fatalf("expected nil score")
	}
	if item.PlaceTypeID == nil || *item.PlaceTypeID != "http://vocab.getty.edu/aat/300008347" {
		t.Fatalf("got place type id %v", item.PlaceTypeID)
	}
	if item.TGNURI != "http://vocab.getty.edu/page/tgn/3000001" {
		t.Fatalf("got tgn uri %q", item.TGNURI)
	}
	var altNames []string
	if err := json.Unmarshal(item.AlternateNames, &altNames); err != nil {
		t.Fatalf("decode alternate names: %v", err)
	}
	if !reflect.DeepEqual(altNames, []string{"Ste Genevieve"}) {
		t.Fatalf("got alternate names %#v", altNames)
	}
}

func mustWritePairtreePlace(t *testing.T, root string, id int64, body string) string {
	t.Helper()
	path, err := pairtreePath(root, id)
	if err != nil {
		t.Fatalf("pairtreePath returned error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir pairtree dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write pairtree file: %v", err)
	}
	return path
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func stringPtr(v string) *string { return &v }
func int64Ptr(v int64) *int64    { return &v }
