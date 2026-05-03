package georism

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type SolrRepository struct {
	baseURL string
	core    string
	client  *http.Client
}

type solrStoredPlace struct {
	TGNID         int64
	PreferredTerm string
	PreferredNorm string
	MatchedTerms  []string
	AncestorTerms []string
}

func OpenSolrRepository(cfg Config) (*SolrRepository, error) {
	if cfg.Solr.URL == "" || cfg.Solr.LiveCore == "" {
		return nil, fmt.Errorf("Solr config is incomplete")
	}
	return &SolrRepository{
		baseURL: strings.TrimRight(cfg.Solr.URL, "/"),
		core:    cfg.Solr.LiveCore,
		client:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (r *SolrRepository) Close() error {
	return nil
}

func (r *SolrRepository) SearchPlaces(query string, page int, pageSize int) (SearchPage, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 25
	}
	norm := normalizeText(query)

	requestBody := map[string]any{
		"query":  query,
		"offset": (page - 1) * pageSize,
		"limit":  pageSize,
		"fields": []string{
			"id", "tgn_id", "preferred_term", "preferred_term_norm", "matched_terms",
			"alternate_names", "ancestor_pairs_json", "place_type_id", "place_type_label",
			"parent_subject_id", "lat", "lon", "score",
		},
		"filter": []string{"type:place"},
		"sort":   "score desc, preferred_term asc, tgn_id asc",
		"params": map[string]any{
			"defType":    "edismax",
			"q.op":       "AND",
			"sow":        true,
			"qf":         "preferred_term_text^12 alternate_names_text^8 text^1",
			"pf":         "preferred_term_text^40 alternate_names_text^20",
			"omitHeader": true,
		},
	}
	if norm != "" {
		requestBody["params"].(map[string]any)["bq"] = fmt.Sprintf(`preferred_term_norm:"%s"^100`, solrPhraseEscape(norm))
	}

	body, err := r.postQuery(ctxBackground(), requestBody)
	if err != nil {
		return SearchPage{}, fmt.Errorf("search Solr index: %w", err)
	}

	var resp struct {
		Response struct {
			NumFound int              `json:"numFound"`
			Docs     []map[string]any `json:"docs"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return SearchPage{}, fmt.Errorf("decode Solr search response: %w", err)
	}

	results := make([]PlaceMatch, 0, len(resp.Response.Docs))
	for _, doc := range resp.Response.Docs {
		match, _, err := placeMatchFromSolrDoc(doc, norm, true)
		if err != nil {
			return SearchPage{}, err
		}
		results = append(results, match)
	}
	return SearchPage{Results: results, Total: resp.Response.NumFound}, nil
}

func (r *SolrRepository) GetPlaceByID(id int64) (*PlaceMatch, error) {
	requestBody := map[string]any{
		"query": "*:*",
		"limit": 1,
		"fields": []string{
			"id", "tgn_id", "preferred_term", "preferred_term_norm", "matched_terms",
			"alternate_names", "ancestor_pairs_json", "place_type_id", "place_type_label",
			"parent_subject_id", "lat", "lon",
		},
		"filter": []string{fmt.Sprintf("id:%s", strconv.FormatInt(id, 10))},
		"params": map[string]any{
			"omitHeader": true,
		},
	}
	body, err := r.postQuery(ctxBackground(), requestBody)
	if err != nil {
		return nil, fmt.Errorf("lookup Solr document by id: %w", err)
	}
	var resp struct {
		Response struct {
			Docs []map[string]any `json:"docs"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode Solr id response: %w", err)
	}
	if len(resp.Response.Docs) == 0 {
		return nil, nil
	}
	match, _, err := placeMatchFromSolrDoc(resp.Response.Docs[0], "", false)
	if err != nil {
		return nil, err
	}
	return &match, nil
}

func (r *SolrRepository) postQuery(ctx context.Context, requestBody map[string]any) ([]byte, error) {
	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("encode Solr query request: %w", err)
	}
	endpoint := fmt.Sprintf("%s/%s/select", r.baseURL, r.core)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create Solr request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute Solr request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	return respBody, nil
}

func placeMatchFromSolrDoc(doc map[string]any, normQuery string, includeScore bool) (PlaceMatch, *solrStoredPlace, error) {
	preferredTerm := stringFromDoc(doc, "preferred_term")
	matchedTerms := stringSliceFromDoc(doc, "matched_terms")
	if preferredTerm == "" {
		preferredTerm = stringFromDoc(doc, "id")
	}

	item := PlaceMatch{
		TGNID:         int64FromDoc(doc, "tgn_id"),
		TGNURI:        tgnPageURI(int64FromDoc(doc, "tgn_id")),
		PreferredTerm: preferredTerm,
	}
	item.MatchedTerm = bestMatchedTerm(normQuery, preferredTerm, matchedTerms)
	item.AlternateNames = rawJSONFromStrings(stringSliceFromDoc(doc, "alternate_names"))
	item.AncestorPairs = rawJSONFromDoc(doc, "ancestor_pairs_json", "[]")

	if v := strings.TrimSpace(stringFromDoc(doc, "place_type_id")); v != "" {
		item.PlaceTypeID = &v
	}
	if v := strings.TrimSpace(stringFromDoc(doc, "place_type_label")); v != "" {
		item.PlaceTypeLabel = &v
	}
	if v, ok := optionalInt64FromDoc(doc, "parent_subject_id"); ok {
		item.ParentSubject = &v
	}
	if v, ok := optionalFloat64FromDoc(doc, "lat"); ok {
		item.Lat = &v
	}
	if v, ok := optionalFloat64FromDoc(doc, "lon"); ok {
		item.Lon = &v
	}
	if includeScore {
		score := float64FromDoc(doc, "score")
		item.Score = &score
	}

	stored := &solrStoredPlace{
		TGNID:         item.TGNID,
		PreferredTerm: preferredTerm,
		PreferredNorm: stringFromDoc(doc, "preferred_term_norm"),
		MatchedTerms:  matchedTerms,
		AncestorTerms: ancestorLabelsFromRaw(item.AncestorPairs),
	}
	return item, stored, nil
}

func alternateNamesSlice(preferredTerm string, terms []string) []string {
	if len(terms) == 0 {
		return nil
	}
	alternates := make([]string, 0, len(terms))
	seen := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		trimmed := strings.TrimSpace(term)
		if trimmed == "" || trimmed == preferredTerm {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		alternates = append(alternates, trimmed)
	}
	return alternates
}

func ancestorLabelsFromRaw(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var objectPairs []map[string]any
	if err := json.Unmarshal(raw, &objectPairs); err == nil {
		labels := make([]string, 0, len(objectPairs))
		for _, pair := range objectPairs {
			label, ok := pair["label"].(string)
			if !ok {
				continue
			}
			label = strings.TrimSpace(label)
			if label == "" {
				continue
			}
			labels = append(labels, label)
		}
		return labels
	}

	var legacyPairs [][]any
	if err := json.Unmarshal(raw, &legacyPairs); err != nil {
		return nil
	}
	labels := make([]string, 0, len(legacyPairs))
	for _, pair := range legacyPairs {
		if len(pair) < 2 {
			continue
		}
		label, ok := pair[1].(string)
		if !ok {
			continue
		}
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		labels = append(labels, label)
	}
	return labels
}

func rawJSONFromStrings(values []string) json.RawMessage {
	if len(values) == 0 {
		return json.RawMessage("[]")
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return json.RawMessage("[]")
	}
	return json.RawMessage(raw)
}

func bestMatchedTerm(normQuery, preferredTerm string, terms []string) string {
	if len(terms) == 0 || normQuery == "" {
		return preferredTerm
	}
	bestTerm := preferredTerm
	bestScore := -1
	queryTokens := strings.Fields(normQuery)
	for _, term := range terms {
		score := matchScore(normQuery, queryTokens, term, preferredTerm)
		if score > bestScore {
			bestScore = score
			bestTerm = term
		}
	}
	if bestTerm == "" {
		return preferredTerm
	}
	return bestTerm
}

func matchScore(normQuery string, queryTokens []string, term string, preferredTerm string) int {
	normTerm := normalizeText(term)
	score := 0
	switch {
	case normTerm == normQuery:
		score += 1000
	case strings.HasPrefix(normTerm, normQuery):
		score += 700
	case strings.Contains(normTerm, normQuery):
		score += 500
	}
	for _, token := range queryTokens {
		if strings.Contains(normTerm, token) {
			score += 100
		}
	}
	if term == preferredTerm {
		score += 25
	}
	return score
}

func rerankScore(normQuery string, queryTokens []string, baseScore float64, match PlaceMatch, stored *solrStoredPlace) float64 {
	score := baseScore
	if stored == nil {
		return score
	}
	matchedNorm := normalizeText(match.MatchedTerm)
	preferredNorm := strings.TrimSpace(stored.PreferredNorm)
	aliasText := " " + normalizeText(strings.Join(stored.MatchedTerms, " ")) + " "
	ancestorText := " " + normalizeText(strings.Join(stored.AncestorTerms, " ")) + " "
	firstToken := ""
	if len(queryTokens) > 0 {
		firstToken = queryTokens[0]
	}

	switch {
	case matchedNorm == normQuery:
		score += 20
	case preferredNorm == normQuery:
		score += 18
	case strings.Contains(" "+preferredNorm+" ", " "+normQuery+" "):
		score += 10
	case strings.Contains(" "+matchedNorm+" ", " "+normQuery+" "):
		score += 8
	}

	termHits := 0
	ancestorHits := 0
	coverageHits := 0
	for _, token := range queryTokens {
		inTerm := strings.Contains(" "+matchedNorm+" ", " "+token+" ") ||
			strings.Contains(" "+preferredNorm+" ", " "+token+" ") ||
			strings.Contains(aliasText, " "+token+" ")
		inAncestor := strings.Contains(ancestorText, " "+token+" ")
		if inTerm {
			termHits++
		}
		if inAncestor {
			ancestorHits++
		}
		if inTerm || inAncestor {
			coverageHits++
		}
	}

	if len(queryTokens) > 0 {
		score += float64(termHits) * 3.5
		score += float64(ancestorHits) * 5.0
		score += float64(coverageHits) * 9.0
		if firstToken != "" {
			firstTokenInTerm := strings.Contains(" "+matchedNorm+" ", " "+firstToken+" ") ||
				strings.Contains(" "+preferredNorm+" ", " "+firstToken+" ") ||
				strings.Contains(aliasText, " "+firstToken+" ")
			if firstTokenInTerm {
				score += 9
			} else {
				score -= 9
			}
		}
		if coverageHits == len(queryTokens) {
			score += 15
		}
		if termHits == len(queryTokens) {
			score += 8
		}
		score -= float64(len(queryTokens)-coverageHits) * 7.5
	}

	return score
}

func ctxBackground() context.Context {
	return context.Background()
}

func solrPhraseEscape(input string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return replacer.Replace(input)
}

func stringFromDoc(doc map[string]any, key string) string {
	switch value := doc[key].(type) {
	case string:
		return value
	case []any:
		if len(value) == 0 {
			return ""
		}
		if s, ok := value[0].(string); ok {
			return s
		}
	}
	return ""
}

func stringSliceFromDoc(doc map[string]any, key string) []string {
	value, ok := doc[key]
	if !ok {
		return nil
	}
	switch vv := value.(type) {
	case []any:
		out := make([]string, 0, len(vv))
		for _, item := range vv {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.TrimSpace(vv) == "" {
			return nil
		}
		return []string{vv}
	default:
		return nil
	}
}

func rawJSONFromDoc(doc map[string]any, key, fallback string) json.RawMessage {
	value := stringFromDoc(doc, key)
	if strings.TrimSpace(value) == "" {
		return json.RawMessage(fallback)
	}
	return json.RawMessage(value)
}

func int64FromDoc(doc map[string]any, key string) int64 {
	v, _ := optionalInt64FromDoc(doc, key)
	return v
}

func optionalInt64FromDoc(doc map[string]any, key string) (int64, bool) {
	value, ok := doc[key]
	if !ok {
		return 0, false
	}
	switch vv := value.(type) {
	case float64:
		return int64(vv), true
	case json.Number:
		i, err := vv.Int64()
		return i, err == nil
	case string:
		if strings.TrimSpace(vv) == "" {
			return 0, false
		}
		i, err := strconv.ParseInt(vv, 10, 64)
		return i, err == nil
	default:
		return 0, false
	}
}

func float64FromDoc(doc map[string]any, key string) float64 {
	v, _ := optionalFloat64FromDoc(doc, key)
	return v
}

func optionalFloat64FromDoc(doc map[string]any, key string) (float64, bool) {
	value, ok := doc[key]
	if !ok {
		return 0, false
	}
	switch vv := value.(type) {
	case float64:
		return vv, true
	case json.Number:
		f, err := vv.Float64()
		return f, err == nil
	case string:
		if strings.TrimSpace(vv) == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(vv, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func derefFloat(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
