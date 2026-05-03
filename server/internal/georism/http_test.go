package georism

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

type fakeRepo struct {
	searchPage     SearchPage
	searchQuery    string
	searchPageNum  int
	searchPageSize int
	searchErr      error
	idResult       *PlaceMatch
	idErr          error
}

func (f *fakeRepo) SearchPlaces(query string, page int, pageSize int) (SearchPage, error) {
	f.searchQuery = query
	f.searchPageNum = page
	f.searchPageSize = pageSize
	if f.searchErr != nil {
		return SearchPage{}, f.searchErr
	}
	return f.searchPage, nil
}

func (f *fakeRepo) GetPlaceByID(id int64) (*PlaceMatch, error) {
	if f.idErr != nil {
		return nil, f.idErr
	}
	return f.idResult, nil
}

func TestHandlerSearchSuccess(t *testing.T) {
	h := NewHandler(&fakeRepo{searchPage: SearchPage{
		Results: []PlaceMatch{{TGNID: 7003746, MatchedTerm: "Genf", PreferredTerm: "Genf"}},
		Total:   1,
	}}, 25)
	req := newTestRequest(t, http.MethodGet, "/places?q=Genf")
	w := newResponseRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["count"].(float64) != 1 {
		t.Fatalf("expected count 1, got %v", got["count"])
	}
	page := got["page"].(map[string]any)
	if page["next"] != nil {
		t.Fatalf("expected next nil, got %v", page["next"])
	}
	if page["previous"] != nil {
		t.Fatalf("expected previous nil, got %v", page["previous"])
	}
}

func TestHandlerSearchSuccessWithTrailingSlash(t *testing.T) {
	h := NewHandler(&fakeRepo{searchPage: SearchPage{
		Results: []PlaceMatch{{TGNID: 7003746, MatchedTerm: "Genf", PreferredTerm: "Genf"}},
		Total:   1,
	}}, 25)
	req := newTestRequest(t, http.MethodGet, "/places/?q=Genf")
	w := newResponseRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandlerIDSuccess(t *testing.T) {
	id := int64(7003746)
	h := NewHandler(&fakeRepo{idResult: &PlaceMatch{TGNID: id, TGNURI: "http://vocab.getty.edu/page/tgn/7003746", MatchedTerm: "Genf", PreferredTerm: "Genf"}}, 25)
	req := newTestRequest(t, http.MethodGet, "/places/7003746")
	w := newResponseRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, exists := got["result"]; exists {
		t.Fatalf("id response should be a plain object, got wrapper field \"result\"")
	}
	if got["tgn_id"].(float64) != float64(id) {
		t.Fatalf("expected tgn_id %d, got %v", id, got["tgn_id"])
	}
	if got["tgn_uri"] != "http://vocab.getty.edu/page/tgn/7003746" {
		t.Fatalf("unexpected tgn_uri %v", got["tgn_uri"])
	}
}

func TestHandlerRejectsBothParams(t *testing.T) {
	h := NewHandler(&fakeRepo{}, 25)
	req := newTestRequest(t, http.MethodGet, "/places?q=Genf&id=7003746")
	w := newResponseRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandlerRejectsNoParams(t *testing.T) {
	h := NewHandler(&fakeRepo{}, 25)
	req := newTestRequest(t, http.MethodGet, "/places")
	w := newResponseRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandlerRejectsNoParamsWithTrailingSlash(t *testing.T) {
	h := NewHandler(&fakeRepo{}, 25)
	req := newTestRequest(t, http.MethodGet, "/places/")
	w := newResponseRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandlerRejectsInvalidID(t *testing.T) {
	h := NewHandler(&fakeRepo{}, 25)
	req := newTestRequest(t, http.MethodGet, "/places/abc")
	w := newResponseRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandlerReturns404ForUnknownID(t *testing.T) {
	h := NewHandler(&fakeRepo{idResult: nil}, 25)
	req := newTestRequest(t, http.MethodGet, "/places/999")
	w := newResponseRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandlerReturns500OnRepoError(t *testing.T) {
	h := NewHandler(&fakeRepo{searchErr: errors.New("boom")}, 25)
	req := newTestRequest(t, http.MethodGet, "/places?q=Basel")
	w := newResponseRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestHandlerRejectsIDQueryParamOnPlaces(t *testing.T) {
	h := NewHandler(&fakeRepo{}, 25)
	req := newTestRequest(t, http.MethodGet, "/places?id=7003746")
	w := newResponseRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandlerPassesConfiguredPageToRepository(t *testing.T) {
	repo := &fakeRepo{searchPage: SearchPage{
		Results: []PlaceMatch{{TGNID: 7003746, MatchedTerm: "Genf", PreferredTerm: "Genf"}},
		Total:   52,
	}}
	h := NewHandler(repo, 25)
	req := newTestRequest(t, http.MethodGet, "/places?q=Genf&page=2")
	req.Host = "localhost:8080"
	w := newResponseRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if repo.searchQuery != "Genf" || repo.searchPageNum != 2 || repo.searchPageSize != 25 {
		t.Fatalf("got search inputs query=%q page=%d pageSize=%d", repo.searchQuery, repo.searchPageNum, repo.searchPageSize)
	}

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	page := got["page"].(map[string]any)
	if page["previous"] != "http://localhost:8080/places?page=1&q=Genf" && page["previous"] != "http://localhost:8080/places?q=Genf&page=1" {
		t.Fatalf("unexpected previous link %v", page["previous"])
	}
	if page["next"] != "http://localhost:8080/places?page=3&q=Genf" && page["next"] != "http://localhost:8080/places?q=Genf&page=3" {
		t.Fatalf("unexpected next link %v", page["next"])
	}
}

func TestHandlerRejectsInvalidPage(t *testing.T) {
	h := NewHandler(&fakeRepo{}, 25)
	req := newTestRequest(t, http.MethodGet, "/places?q=Genf&page=0")
	w := newResponseRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

type responseRecorder struct {
	header http.Header
	Body   bytes.Buffer
	Code   int
}

func newResponseRecorder() *responseRecorder {
	return &responseRecorder{header: make(http.Header)}
}

func (r *responseRecorder) Header() http.Header {
	return r.header
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.Code == 0 {
		r.Code = http.StatusOK
	}
	return r.Body.Write(b)
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.Code = statusCode
}

func newTestRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()

	req, err := http.NewRequest(method, target, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return req
}
