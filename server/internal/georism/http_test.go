package georism

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeRepo struct {
	searchResults []PlaceMatch
	searchErr     error
	idResult      *PlaceMatch
	idErr         error
}

func (f *fakeRepo) SearchPlaces(query string, limit int) ([]PlaceMatch, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.searchResults, nil
}

func (f *fakeRepo) GetPlaceByID(id int64) (*PlaceMatch, error) {
	if f.idErr != nil {
		return nil, f.idErr
	}
	return f.idResult, nil
}

func TestHandlerSearchSuccess(t *testing.T) {
	h := NewHandler(&fakeRepo{searchResults: []PlaceMatch{{TGNID: 7003746, MatchedTerm: "Genf", PreferredTerm: "Genf"}}})
	req := httptest.NewRequest(http.MethodGet, "/places?q=Genf", nil)
	w := httptest.NewRecorder()

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
}

func TestHandlerSearchSuccessWithTrailingSlash(t *testing.T) {
	h := NewHandler(&fakeRepo{searchResults: []PlaceMatch{{TGNID: 7003746, MatchedTerm: "Genf", PreferredTerm: "Genf"}}})
	req := httptest.NewRequest(http.MethodGet, "/places/?q=Genf", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandlerIDSuccess(t *testing.T) {
	id := int64(7003746)
	h := NewHandler(&fakeRepo{idResult: &PlaceMatch{
		TGNID:          id,
		MatchedTerm:    "Genf",
		PreferredTerm:  "Genf",
		AlternateNames: json.RawMessage(`["Genf","Geneva","Genève"]`),
	}})
	req := httptest.NewRequest(http.MethodGet, "/places/7003746", nil)
	w := httptest.NewRecorder()

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
	altNames, ok := got["alternate_names"].([]any)
	if !ok || len(altNames) != 3 {
		t.Fatalf("expected alternate_names array, got %v", got["alternate_names"])
	}
}

func TestHandlerRejectsBothParams(t *testing.T) {
	h := NewHandler(&fakeRepo{})
	req := httptest.NewRequest(http.MethodGet, "/places?q=Genf&id=7003746", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandlerRejectsNoParams(t *testing.T) {
	h := NewHandler(&fakeRepo{})
	req := httptest.NewRequest(http.MethodGet, "/places", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandlerRejectsNoParamsWithTrailingSlash(t *testing.T) {
	h := NewHandler(&fakeRepo{})
	req := httptest.NewRequest(http.MethodGet, "/places/", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandlerRejectsInvalidID(t *testing.T) {
	h := NewHandler(&fakeRepo{})
	req := httptest.NewRequest(http.MethodGet, "/places/abc", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandlerReturns404ForUnknownID(t *testing.T) {
	h := NewHandler(&fakeRepo{idResult: nil})
	req := httptest.NewRequest(http.MethodGet, "/places/999", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandlerReturns500OnRepoError(t *testing.T) {
	h := NewHandler(&fakeRepo{searchErr: errors.New("boom")})
	req := httptest.NewRequest(http.MethodGet, "/places?q=Basel", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestHandlerRejectsIDQueryParamOnPlaces(t *testing.T) {
	h := NewHandler(&fakeRepo{})
	req := httptest.NewRequest(http.MethodGet, "/places?id=7003746", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
