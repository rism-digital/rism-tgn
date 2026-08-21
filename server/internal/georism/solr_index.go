package georism

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	progressLogEvery = 50000
)

type solrPlaceDocument struct {
	ID                      string   `json:"id"`
	IndexedAt               string   `json:"indexed,omitempty"`
	Type                    string   `json:"type"`
	TGNID                   int64    `json:"tgn_id"`
	PreferredTerm           string   `json:"preferred_term"`
	LabelLang               string   `json:"label_lang"`
	PreferredTermText       []string `json:"preferred_term_text,omitempty"`
	MatchedTerms            []string `json:"matched_terms"`
	AlternateNamesLanguages string   `json:"alternate_names_languages,omitempty"`
	AlternateNamesText      []string `json:"alternate_names_text,omitempty"`
	AncestorPairs           string   `json:"ancestor_pairs_json"`
	PlaceTypeID             *string  `json:"place_type_id,omitempty"`
	PlaceTypeLabel          *string  `json:"place_type_label,omitempty"`
	ParentSubjectID         *int64   `json:"parent_subject_id,omitempty"`
	Lat                     *float64 `json:"lat,omitempty"`
	Lon                     *float64 `json:"lon,omitempty"`
	Location                *string  `json:"location,omitempty"`
	Text                    []string `json:"text,omitempty"`
}

type indexBuilder struct {
	summaryCache        map[int64]placeSummary
	pathByID            map[int64]string
	skippedPlaceTypeSet map[string]struct{}
}

type ancestorInfo struct {
	ID             int64
	Label          string
	LabelLanguage  *string
	Terms          []string
	PlaceTypeID    *string
	PlaceTypeLabel *string
}

type builtDocument struct {
	placeID int64
	doc     *solrPlaceDocument
}

type preloadedPlace struct {
	id      int64
	path    string
	place   *parsedPlace
	summary placeSummary
}

type ancestorPairDocument struct {
	TGNID          int64   `json:"tgn_id"`
	TGNURI         string  `json:"tgn_uri"`
	Label          string  `json:"label"`
	LabelLang      []any   `json:"label_lang"`
	PlaceTypeID    *string `json:"place_type_id,omitempty"`
	PlaceTypeLabel *string `json:"place_type_label,omitempty"`
}

func BuildSolrIndex(ctx context.Context, cfg Config, inputDir string) error {
	if strings.TrimSpace(inputDir) == "" {
		return fmt.Errorf("input dir is required")
	}
	builder := &indexBuilder{
		summaryCache:        make(map[int64]placeSummary),
		pathByID:            make(map[int64]string),
		skippedPlaceTypeSet: makeSkipPlaceTypeSet(cfg.Indexer.SkipPlaceTypeLabels),
	}
	paths, err := listJSONFiles(ctx, inputDir)
	if err != nil {
		return err
	}
	log.Info().Int("json_files", len(paths)).Msg("discovered json files")

	uniquePlaces, err := builder.preloadSummaries(ctx, paths)
	if err != nil {
		return err
	}
	return buildSolrIndexFromPlaces(ctx, cfg, builder, uniquePlaces)
}

func BuildSolrIndexFromArchive(ctx context.Context, cfg Config, archivePath string) error {
	if strings.TrimSpace(archivePath) == "" {
		return fmt.Errorf("input archive is required")
	}
	builder := &indexBuilder{
		summaryCache:        make(map[int64]placeSummary),
		pathByID:            make(map[int64]string),
		skippedPlaceTypeSet: makeSkipPlaceTypeSet(cfg.Indexer.SkipPlaceTypeLabels),
	}
	selection, err := builder.preloadArchiveSummaries(ctx, archivePath)
	if err != nil {
		return err
	}
	return buildSolrIndexFromArchiveSelection(ctx, cfg, builder, archivePath, selection)
}

func buildSolrIndexFromPlaces(ctx context.Context, cfg Config, builder *indexBuilder, places []preloadedPlace) error {
	startedAt := time.Now()
	client := &http.Client{Timeout: 60 * time.Second}
	admin := newSolrAdmin(client, cfg.Solr.URL)

	if err := admin.requireCore(ctx, cfg.Solr.LiveCore); err != nil {
		return err
	}
	if err := admin.requireCore(ctx, cfg.Solr.IndexingCore); err != nil {
		return err
	}
	if err := admin.clearCore(ctx, cfg.Solr.IndexingCore); err != nil {
		return err
	}

	indexedCount := 0
	batchSize := cfg.Indexer.SolrBatchSize
	batch := make([]solrPlaceDocument, 0, batchSize)
	docs, errCh := builder.buildDocuments(ctx, places)
	for result := range docs {
		batch = append(batch, *result.doc)
		indexedCount++
		if indexedCount%progressLogEvery == 0 {
			log.Info().
				Str("elapsed", formatElapsedHHMMSS(time.Since(startedAt))).
				Int("indexed_places", indexedCount).
				Int("summary_places", len(builder.summaryCache)).
				Str("last_id", strconv.FormatInt(result.placeID, 10)).
				Msg("indexing progress")
		}
		if len(batch) >= batchSize {
			if err := admin.postDocuments(ctx, cfg.Solr.IndexingCore, batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	if err := <-errCh; err != nil {
		return err
	}
	if len(batch) > 0 {
		if err := admin.postDocuments(ctx, cfg.Solr.IndexingCore, batch); err != nil {
			return err
		}
	}
	if err := admin.commit(ctx, cfg.Solr.IndexingCore); err != nil {
		return err
	}
	if err := admin.swapCores(ctx, cfg.Solr.IndexingCore, cfg.Solr.LiveCore); err != nil {
		return err
	}
	if err := admin.commit(ctx, cfg.Solr.LiveCore); err != nil {
		return err
	}

	log.Info().
		Str("elapsed", formatElapsedHHMMSS(time.Since(startedAt))).
		Int("indexed_places", indexedCount).
		Int("summary_places", len(builder.summaryCache)).
		Msg("index build complete")
	return nil
}

type archiveSelection struct {
	selectedEntryNames map[string]struct{}
}

func buildSolrIndexFromArchiveSelection(ctx context.Context, cfg Config, builder *indexBuilder, archivePath string, selection archiveSelection) error {
	startedAt := time.Now()
	client := &http.Client{Timeout: 60 * time.Second}
	admin := newSolrAdmin(client, cfg.Solr.URL)

	if err := admin.requireCore(ctx, cfg.Solr.LiveCore); err != nil {
		return err
	}
	if err := admin.requireCore(ctx, cfg.Solr.IndexingCore); err != nil {
		return err
	}
	if err := admin.clearCore(ctx, cfg.Solr.IndexingCore); err != nil {
		return err
	}

	indexedCount := 0
	batchSize := cfg.Indexer.SolrBatchSize
	batch := make([]solrPlaceDocument, 0, batchSize)
	docs, errCh := builder.buildDocumentsFromArchive(ctx, archivePath, selection)
	for result := range docs {
		batch = append(batch, *result.doc)
		indexedCount++
		if indexedCount%progressLogEvery == 0 {
			log.Info().
				Str("elapsed", formatElapsedHHMMSS(time.Since(startedAt))).
				Int("indexed_places", indexedCount).
				Int("summary_places", len(builder.summaryCache)).
				Str("last_id", strconv.FormatInt(result.placeID, 10)).
				Msg("indexing progress")
		}
		if len(batch) >= batchSize {
			if err := admin.postDocuments(ctx, cfg.Solr.IndexingCore, batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	if err := <-errCh; err != nil {
		return err
	}
	if len(batch) > 0 {
		if err := admin.postDocuments(ctx, cfg.Solr.IndexingCore, batch); err != nil {
			return err
		}
	}
	if err := admin.commit(ctx, cfg.Solr.IndexingCore); err != nil {
		return err
	}
	if err := admin.swapCores(ctx, cfg.Solr.IndexingCore, cfg.Solr.LiveCore); err != nil {
		return err
	}
	if err := admin.commit(ctx, cfg.Solr.LiveCore); err != nil {
		return err
	}

	log.Info().
		Str("elapsed", formatElapsedHHMMSS(time.Since(startedAt))).
		Int("indexed_places", indexedCount).
		Int("summary_places", len(builder.summaryCache)).
		Msg("index build complete")
	return nil
}

type solrAdmin struct {
	client  *http.Client
	baseURL string
}

func newSolrAdmin(client *http.Client, baseURL string) *solrAdmin {
	return &solrAdmin{client: client, baseURL: strings.TrimRight(baseURL, "/")}
}

func (s *solrAdmin) requireCore(ctx context.Context, core string) error {
	exists, err := s.coreExists(ctx, core)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("required Solr core %s does not exist", core)
	}
	return nil
}

func (s *solrAdmin) coreExists(ctx context.Context, core string) (bool, error) {
	params := url.Values{
		"action": {"STATUS"},
		"core":   {core},
		"wt":     {"json"},
	}
	body, err := s.getAdmin(ctx, params)
	if err != nil {
		return false, err
	}
	var resp struct {
		Status map[string]json.RawMessage `json:"status"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return false, fmt.Errorf("decode Solr core status: %w", err)
	}
	_, ok := resp.Status[core]
	return ok, nil
}

func (s *solrAdmin) clearCore(ctx context.Context, core string) error {
	body := []byte(`{"delete":{"query":"*:*"}}`)
	if err := s.postUpdateJSON(ctx, core, body, false); err != nil {
		return fmt.Errorf("clear Solr core %s: %w", core, err)
	}
	return s.commit(ctx, core)
}

func (s *solrAdmin) postDocuments(ctx context.Context, core string, docs []solrPlaceDocument) error {
	body, err := json.Marshal(docs)
	if err != nil {
		return fmt.Errorf("encode Solr documents: %w", err)
	}
	endpoint := fmt.Sprintf("%s/%s/update/json/docs?overwrite=true&commit=false", s.baseURL, url.PathEscape(core))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Solr document request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("post Solr documents: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("post Solr documents: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return nil
}

func (s *solrAdmin) commit(ctx context.Context, core string) error {
	return s.postUpdateJSON(ctx, core, []byte(`{"commit":{}}`), true)
}

func (s *solrAdmin) postUpdateJSON(ctx context.Context, core string, body []byte, commit bool) error {
	endpoint := fmt.Sprintf("%s/%s/update", s.baseURL, url.PathEscape(core))
	if commit {
		endpoint += "?commit=true"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Solr update request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("post Solr update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("post Solr update: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return nil
}

func (s *solrAdmin) swapCores(ctx context.Context, a, b string) error {
	params := url.Values{
		"action": {"SWAP"},
		"core":   {a},
		"other":  {b},
		"wt":     {"json"},
	}
	if _, err := s.getAdmin(ctx, params); err != nil {
		return fmt.Errorf("swap Solr cores %s and %s: %w", a, b, err)
	}
	return nil
}

func (s *solrAdmin) getAdmin(ctx context.Context, params url.Values) ([]byte, error) {
	endpoint := fmt.Sprintf("%s/admin/cores?%s", s.baseURL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create Solr admin request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute Solr admin request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (b *indexBuilder) preloadSummaries(ctx context.Context, paths []string) ([]preloadedPlace, error) {
	preloadStartedAt := time.Now()
	pathCh := make(chan string, workerCount())
	resultCh := make(chan preloadedPlace, workerCount())
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var sendErr sync.Once

	var workers sync.WaitGroup
	for i := 0; i < workerCount(); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for path := range pathCh {
				if ctx.Err() != nil {
					return
				}
				place, err := parsePlaceFile(path)
				if err != nil {
					sendErr.Do(func() { errCh <- err })
					cancel()
					return
				}
				result := preloadedPlace{
					id:    place.TGNID,
					path:  path,
					place: place,
					summary: placeSummary{
						TGNID:                 place.TGNID,
						PreferredTerm:         place.PreferredTerm,
						PreferredTermLanguage: place.PreferredTermLanguage,
						Terms:                 uniqueTerms(place.Terms),
						PlaceTypeID:           place.PlaceTypeID,
						PlaceTypeLabel:        place.PlaceTypeLabel,
						ParentSubjectID:       place.ParentSubjectID,
						ParentLabel:           place.ParentLabel,
					},
				}
				select {
				case resultCh <- result:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		defer close(pathCh)
		for _, path := range paths {
			select {
			case pathCh <- path:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		workers.Wait()
		close(resultCh)
	}()

	preloadedCount := 0
	uniquePlaces := make([]preloadedPlace, 0, len(paths))
	for result := range resultCh {
		if existingPath, ok := b.pathByID[result.id]; ok {
			if result.path < existingPath {
				log.Warn().
					Int64("tgn_id", result.id).
					Str("kept_path", result.path).
					Str("skipped_path", existingPath).
					Msg("duplicate place id in source tree; replacing duplicate file choice")
				b.summaryCache[result.id] = result.summary
				b.pathByID[result.id] = result.path
				for i := range uniquePlaces {
					if uniquePlaces[i].path == existingPath {
						uniquePlaces[i] = result
						break
					}
				}
				continue
			}
			log.Warn().
				Int64("tgn_id", result.id).
				Str("kept_path", existingPath).
				Str("skipped_path", result.path).
				Msg("duplicate place id in source tree; skipping duplicate file")
			continue
		}
		b.summaryCache[result.id] = result.summary
		b.pathByID[result.id] = result.path
		uniquePlaces = append(uniquePlaces, result)
		preloadedCount++
		if preloadedCount%progressLogEvery == 0 {
			log.Info().
				Str("elapsed", formatElapsedHHMMSS(time.Since(preloadStartedAt))).
				Int("preloaded_places", preloadedCount).
				Str("last_id", strconv.FormatInt(result.id, 10)).
				Msg("summary preload progress")
		}
	}

	select {
	case err := <-errCh:
		return nil, err
	default:
		return uniquePlaces, nil
	}
}

func (b *indexBuilder) preloadArchiveSummaries(ctx context.Context, archivePath string) (archiveSelection, error) {
	preloadStartedAt := time.Now()
	resultCh := make(chan preloadedPlace, workerCount())
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		defer close(resultCh)
		errCh <- scanArchiveJSONEntries(ctx, archivePath, func(entryName string, r io.Reader) error {
			place, err := parsePlace(r, entryName)
			if err != nil {
				return err
			}
			result := preloadedPlace{
				id:    place.TGNID,
				path:  entryName,
				place: place,
				summary: placeSummary{
					TGNID:                 place.TGNID,
					PreferredTerm:         place.PreferredTerm,
					PreferredTermLanguage: place.PreferredTermLanguage,
					Terms:                 uniqueTerms(place.Terms),
					PlaceTypeID:           place.PlaceTypeID,
					PlaceTypeLabel:        place.PlaceTypeLabel,
					ParentSubjectID:       place.ParentSubjectID,
					ParentLabel:           place.ParentLabel,
				},
			}
			select {
			case resultCh <- result:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()

	preloadedCount := 0
	selectedNames := make(map[string]struct{})
	for result := range resultCh {
		if existingPath, ok := b.pathByID[result.id]; ok {
			if result.path < existingPath {
				log.Warn().
					Int64("tgn_id", result.id).
					Str("kept_path", result.path).
					Str("skipped_path", existingPath).
					Msg("duplicate place id in source archive; replacing duplicate file choice")
				b.summaryCache[result.id] = result.summary
				b.pathByID[result.id] = result.path
				delete(selectedNames, existingPath)
				selectedNames[result.path] = struct{}{}
				continue
			}
			log.Warn().
				Int64("tgn_id", result.id).
				Str("kept_path", existingPath).
				Str("skipped_path", result.path).
				Msg("duplicate place id in source archive; skipping duplicate file")
			continue
		}
		b.summaryCache[result.id] = result.summary
		b.pathByID[result.id] = result.path
		selectedNames[result.path] = struct{}{}
		preloadedCount++
		if preloadedCount%progressLogEvery == 0 {
			log.Info().
				Str("elapsed", formatElapsedHHMMSS(time.Since(preloadStartedAt))).
				Int("preloaded_places", preloadedCount).
				Str("last_id", strconv.FormatInt(result.id, 10)).
				Msg("summary preload progress")
		}
	}
	if err := <-errCh; err != nil {
		return archiveSelection{}, err
	}
	return archiveSelection{selectedEntryNames: selectedNames}, nil
}

func (b *indexBuilder) buildDocuments(ctx context.Context, places []preloadedPlace) (<-chan builtDocument, <-chan error) {
	docCh := make(chan builtDocument, workerCount())
	errCh := make(chan error, 1)
	placeCh := make(chan preloadedPlace, workerCount())
	ctx, cancel := context.WithCancel(ctx)
	var sendErr sync.Once

	var workers sync.WaitGroup
	for i := 0; i < workerCount(); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for candidate := range placeCh {
				if ctx.Err() != nil {
					return
				}
				if b.shouldSkipPlace(candidate.place) {
					continue
				}
				ancestors, err := b.ancestorInfoFor(candidate.place.TGNID, map[int64]struct{}{candidate.place.TGNID: {}})
				if err != nil {
					sendErr.Do(func() { errCh <- err })
					cancel()
					return
				}
				doc, err := newSolrPlaceDocument(candidate.place, ancestors)
				if err != nil {
					sendErr.Do(func() { errCh <- err })
					cancel()
					return
				}
				select {
				case docCh <- builtDocument{placeID: candidate.place.TGNID, doc: doc}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		defer close(placeCh)
		for _, place := range places {
			select {
			case placeCh <- place:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		workers.Wait()
		close(docCh)
		sendErr.Do(func() { errCh <- nil })
		close(errCh)
		cancel()
	}()

	return docCh, errCh
}

func (b *indexBuilder) buildDocumentsFromArchive(ctx context.Context, archivePath string, selection archiveSelection) (<-chan builtDocument, <-chan error) {
	docCh := make(chan builtDocument, workerCount())
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(ctx)

	go func() {
		defer close(docCh)
		defer close(errCh)
		defer cancel()
		errCh <- scanArchiveJSONEntries(ctx, archivePath, func(entryName string, r io.Reader) error {
			if _, ok := selection.selectedEntryNames[entryName]; !ok {
				return nil
			}
			place, err := parsePlace(r, entryName)
			if err != nil {
				return err
			}
			if b.shouldSkipPlace(place) {
				return nil
			}
			ancestors, err := b.ancestorInfoFor(place.TGNID, map[int64]struct{}{place.TGNID: {}})
			if err != nil {
				return err
			}
			doc, err := newSolrPlaceDocument(place, ancestors)
			if err != nil {
				return err
			}
			select {
			case docCh <- builtDocument{placeID: place.TGNID, doc: doc}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()

	return docCh, errCh
}

func makeSkipPlaceTypeSet(labels []string) map[string]struct{} {
	if len(labels) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		normalized := normalizePlaceTypeLabel(label)
		if normalized == "" {
			continue
		}
		set[normalized] = struct{}{}
	}
	return set
}

func (b *indexBuilder) shouldSkipPlace(place *parsedPlace) bool {
	if len(b.skippedPlaceTypeSet) == 0 || place == nil || place.PlaceTypeLabel == nil {
		return false
	}
	_, ok := b.skippedPlaceTypeSet[normalizePlaceTypeLabel(*place.PlaceTypeLabel)]
	return ok
}

func formatElapsedHHMMSS(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	totalSeconds := int64(elapsed / time.Second)
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func newSolrPlaceDocument(place *parsedPlace, ancestors []ancestorInfo) (*solrPlaceDocument, error) {
	terms := uniqueTerms(place.Terms)
	alternateNames := alternateLocalizedNames(place.PreferredTerm, place.Names)
	ancestorPairs := make([]ancestorPairDocument, 0, len(ancestors))
	ancestorTerms := make([]string, 0, len(ancestors)*2)
	for _, ancestor := range ancestors {
		ancestorPairs = append(ancestorPairs, ancestorPairDocument{
			TGNID:          ancestor.ID,
			TGNURI:         tgnPageURI(ancestor.ID),
			Label:          ancestor.Label,
			LabelLang:      nameLanguagePair(ancestor.Label, ancestor.LabelLanguage),
			PlaceTypeID:    ancestor.PlaceTypeID,
			PlaceTypeLabel: ancestor.PlaceTypeLabel,
		})
		ancestorTerms = append(ancestorTerms, uniqueTerms(ancestor.Terms)...)
	}
	ancestorPairsJSON, err := json.Marshal(ancestorPairs)
	if err != nil {
		return nil, fmt.Errorf("encode ancestors for %d: %w", place.TGNID, err)
	}
	labelLangJSON, err := json.Marshal(nameLanguagePair(place.PreferredTerm, place.PreferredTermLanguage))
	if err != nil {
		return nil, fmt.Errorf("encode label language for %d: %w", place.TGNID, err)
	}
	alternateNamesJSON, err := json.Marshal(alternateNames)
	if err != nil {
		return nil, fmt.Errorf("encode alternate names for %d: %w", place.TGNID, err)
	}

	textTerms := uniqueTerms(append(append([]string{}, terms...), ancestorTerms...))
	var location *string
	if place.Lat != nil && place.Lon != nil {
		value := fmt.Sprintf("%f,%f", *place.Lat, *place.Lon)
		location = &value
	}

	return &solrPlaceDocument{
		ID:                      strconv.FormatInt(place.TGNID, 10),
		Type:                    "place",
		TGNID:                   place.TGNID,
		PreferredTerm:           place.PreferredTerm,
		LabelLang:               string(labelLangJSON),
		PreferredTermText:       []string{place.PreferredTerm},
		MatchedTerms:            terms,
		AlternateNamesLanguages: string(alternateNamesJSON),
		AlternateNamesText:      localizedNameStrings(alternateNames),
		AncestorPairs:           string(ancestorPairsJSON),
		PlaceTypeID:             place.PlaceTypeID,
		PlaceTypeLabel:          place.PlaceTypeLabel,
		ParentSubjectID:         place.ParentSubjectID,
		Lat:                     place.Lat,
		Lon:                     place.Lon,
		Location:                location,
		Text:                    textTerms,
	}, nil
}

func nameLanguagePair(name string, language *string) []any {
	return []any{name, language}
}

func alternateLocalizedNames(preferredTerm string, names []localizedName) []localizedName {
	alternates := make([]localizedName, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name.Name)
		if trimmed == "" || trimmed == preferredTerm {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		name.Name = trimmed
		alternates = append(alternates, name)
	}
	return alternates
}

func tgnPageURI(id int64) string {
	return fmt.Sprintf("http://vocab.getty.edu/page/tgn/%d", id)
}

func listJSONFiles(ctx context.Context, inputDir string) ([]string, error) {
	paths := make([]string, 0, 1024)
	err := filepath.WalkDir(inputDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if filepath.Ext(path) == ".json" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return paths, nil
}

func scanArchiveJSONEntries(ctx context.Context, archivePath string, fn func(entryName string, r io.Reader) error) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive %s: %w", archivePath, err)
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gzip reader for %s: %w", archivePath, err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read archive entry from %s: %w", archivePath, err)
		}
		if header == nil {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		if filepath.Ext(header.Name) != ".json" {
			continue
		}
		if err := fn(header.Name, tr); err != nil {
			return err
		}
	}
}

func workerCount() int {
	n := runtime.GOMAXPROCS(0)
	if n < 2 {
		return 2
	}
	return n
}

func uniqueTerms(terms []string) []string {
	seen := make(map[string]struct{}, len(terms))
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		trimmed := strings.TrimSpace(term)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func (b *indexBuilder) ancestorInfoFor(id int64, path map[int64]struct{}) ([]ancestorInfo, error) {
	summary, err := b.summaryForID(id)
	if err != nil {
		return nil, err
	}
	if summary.ParentSubjectID == nil {
		return nil, nil
	}
	parentID := *summary.ParentSubjectID
	if parentID == ancestorStopID {
		return nil, nil
	}
	if _, exists := path[parentID]; exists {
		return nil, nil
	}

	parentSummary, err := b.summaryForID(parentID)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			label := strconv.FormatInt(parentID, 10)
			if summary.ParentLabel != nil && strings.TrimSpace(*summary.ParentLabel) != "" {
				label = *summary.ParentLabel
			}
			log.Warn().
				Int64("child_id", id).
				Int64("parent_id", parentID).
				Msg("missing parent file; using inline parent label")
			return []ancestorInfo{{
				ID:    parentID,
				Label: label,
				Terms: []string{label},
			}}, nil
		}
		return nil, err
	}

	nextPath := clonePath(path)
	nextPath[parentID] = struct{}{}

	ancestors := []ancestorInfo{{
		ID:             parentID,
		Label:          parentSummary.PreferredTerm,
		LabelLanguage:  parentSummary.PreferredTermLanguage,
		Terms:          uniqueTerms(parentSummary.Terms),
		PlaceTypeID:    parentSummary.PlaceTypeID,
		PlaceTypeLabel: parentSummary.PlaceTypeLabel,
	}}
	rest, err := b.ancestorInfoFor(parentID, nextPath)
	if err != nil {
		return nil, err
	}
	return append(ancestors, rest...), nil
}

func (b *indexBuilder) summaryForID(id int64) (placeSummary, error) {
	if summary, ok := b.summaryCache[id]; ok {
		return summary, nil
	}
	return placeSummary{}, fs.ErrNotExist
}

func clonePath(in map[int64]struct{}) map[int64]struct{} {
	out := make(map[int64]struct{}, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}
