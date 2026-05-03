package georism

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Handler struct {
	repo           Repository
	resultsPerPage int
}

func NewHandler(repo Repository, resultsPerPage int) *Handler {
	if resultsPerPage <= 0 {
		resultsPerPage = 25
	}
	return &Handler{repo: repo, resultsPerPage: resultsPerPage}
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
	Page    pageLinks    `json:"page"`
	Results []PlaceMatch `json:"results"`
}

type pageLinks struct {
	Next     *string `json:"next"`
	Previous *string `json:"previous"`
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
	page, err := parsePage(r.URL.Query().Get("page"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_params", "page must be a positive integer")
		return
	}
	h.handleSearch(w, r, q, page)
}

func (h *Handler) handleSearch(w http.ResponseWriter, r *http.Request, q string, page int) {
	resultPage, err := h.repo.SearchPlaces(q, page, h.resultsPerPage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "search query failed")
		return
	}

	writeJSON(w, http.StatusOK, searchResponse{
		Query: q,
		Count: resultPage.Total,
		Page: pageLinks{
			Next:     pageURL(r, page+1, h.resultsPerPage, resultPage.Total),
			Previous: previousPageURL(r, page),
		},
		Results: resultPage.Results,
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

func parsePage(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 1, nil
	}
	page, err := strconv.Atoi(raw)
	if err != nil || page <= 0 {
		return 0, strconv.ErrSyntax
	}
	return page, nil
}

func pageURL(r *http.Request, page int, pageSize int, total int) *string {
	if page <= 0 || pageSize <= 0 || (page-1)*pageSize >= total {
		return nil
	}
	return absolutePageURL(r, page)
}

func previousPageURL(r *http.Request, page int) *string {
	if page <= 1 {
		return nil
	}
	return absolutePageURL(r, page-1)
}

func absolutePageURL(r *http.Request, page int) *string {
	if page <= 0 {
		return nil
	}
	scheme := requestScheme(r)
	host := r.Host
	if host == "" {
		host = "localhost"
	}
	u := url.URL{
		Scheme:   scheme,
		Host:     host,
		Path:     r.URL.Path,
		RawQuery: cloneQueryWithPage(r.URL.Query(), page).Encode(),
	}
	value := u.String()
	return &value
}

func cloneQueryWithPage(values url.Values, page int) url.Values {
	cloned := make(url.Values, len(values))
	for key, current := range values {
		copied := make([]string, len(current))
		copy(copied, current)
		cloned[key] = copied
	}
	cloned.Set("page", strconv.Itoa(page))
	return cloned
}

func requestScheme(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		if idx := strings.IndexByte(forwarded, ','); idx >= 0 {
			forwarded = forwarded[:idx]
		}
		forwarded = strings.TrimSpace(forwarded)
		if forwarded != "" {
			return forwarded
		}
	}
	if r.TLS != nil {
		return "https"
	}
	if host, _, err := net.SplitHostPort(r.Host); err == nil {
		if host != "" {
			return "http"
		}
	}
	return "http"
}
