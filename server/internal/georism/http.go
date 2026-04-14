package georism

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

type Handler struct {
	repo Repository
}

func NewHandler(repo Repository) *Handler {
	return &Handler{repo: repo}
}

type errorPayload struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type searchResponse struct {
	Query   string       `json:"query"`
	Count   int          `json:"count"`
	Results []PlaceMatch `json:"results"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
		return
	}

	path := strings.TrimSpace(r.URL.Path)
	switch {
	case path == "/places" || path == "/places/":
		h.handlePlacesSearch(w, r)
		return
	case strings.HasPrefix(path, "/places/"):
		idRaw := strings.TrimPrefix(path, "/places/")
		if idRaw == "" || strings.Contains(idRaw, "/") {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return
		}
		h.handleIDLookup(w, idRaw)
		return
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}
}

func (h *Handler) handlePlacesSearch(w http.ResponseWriter, r *http.Request) {
	if _, hasID := r.URL.Query()["id"]; hasID {
		writeError(w, http.StatusBadRequest, "invalid_params", "use /places/{id} for id lookups")
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "invalid_params", "provide q for /places search")
		return
	}
	h.handleSearch(w, q)
}

func (h *Handler) handleSearch(w http.ResponseWriter, q string) {
	results, err := h.repo.SearchPlaces(q, 10)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "search query failed")
		return
	}

	writeJSON(w, http.StatusOK, searchResponse{
		Query:   q,
		Count:   len(results),
		Results: results,
	})
}

func (h *Handler) handleIDLookup(w http.ResponseWriter, idRaw string) {
	id, err := strconv.ParseInt(idRaw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be a positive integer")
		return
	}

	item, err := h.repo.GetPlaceByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "id lookup failed")
		return
	}
	if item == nil {
		writeError(w, http.StatusNotFound, "not_found", "place id not found")
		return
	}

	writeJSON(w, http.StatusOK, item)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	payload := errorPayload{}
	payload.Error.Code = code
	payload.Error.Message = message
	writeJSON(w, status, payload)
}

var ErrNotFound = errors.New("not found")
