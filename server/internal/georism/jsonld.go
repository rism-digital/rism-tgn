package georism

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	preferredParentTypeID       = "http://vocab.getty.edu/aat/300449152"
	preferredTermTypeID         = "http://vocab.getty.edu/aat/300404670"
	placeTypeClassID            = "http://vocab.getty.edu/aat/300435109"
	ancestorStopID        int64 = 7029392
)

type rawPlace struct {
	ID           string              `json:"id"`
	Type         string              `json:"type"`
	IdentifiedBy []rawIdentifier     `json:"identified_by"`
	ClassifiedAs []rawClassification `json:"classified_as"`
	PartOf       rawRelationList     `json:"part_of"`
	Label        string              `json:"_label"`
}

type rawIdentifier struct {
	ID           string              `json:"id"`
	Type         string              `json:"type"`
	Content      string              `json:"content"`
	Value        string              `json:"value"`
	ClassifiedAs []rawClassification `json:"classified_as"`
}

type rawClassification struct {
	ID           string              `json:"id"`
	Type         string              `json:"type"`
	Label        string              `json:"_label"`
	ClassifiedAs []rawClassification `json:"classified_as"`
}

type rawRelation struct {
	ID           string              `json:"id"`
	Type         string              `json:"type"`
	Label        string              `json:"_label"`
	ClassifiedAs []rawClassification `json:"classified_as"`
}

type rawRelationList []rawRelation

func (r *rawRelationList) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*r = nil
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var list []rawRelation
		if err := json.Unmarshal(data, &list); err != nil {
			return err
		}
		*r = list
		return nil
	}

	var single rawRelation
	if err := json.Unmarshal(data, &single); err != nil {
		return err
	}
	*r = []rawRelation{single}
	return nil
}

type parsedPlace struct {
	TGNID           int64
	PreferredTerm   string
	Terms           []string
	PlaceTypeID     *string
	PlaceTypeLabel  *string
	ParentSubjectID *int64
	ParentLabel     *string
	Lat             *float64
	Lon             *float64
}

type placeSummary struct {
	TGNID           int64
	PreferredTerm   string
	Terms           []string
	PlaceTypeID     *string
	PlaceTypeLabel  *string
	ParentSubjectID *int64
	ParentLabel     *string
}

func parsePlaceFile(path string) (*parsedPlace, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read place file %s: %w", path, err)
	}
	defer file.Close()
	return parsePlace(file, path)
}

func parsePlace(r io.Reader, sourceName string) (*parsedPlace, error) {
	var raw rawPlace
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode place file %s: %w", sourceName, err)
	}
	if raw.Type != "Place" {
		return nil, fmt.Errorf("unexpected record type %q in %s", raw.Type, sourceName)
	}

	id, err := parseGettyID(raw.ID)
	if err != nil {
		return nil, fmt.Errorf("parse place id from %s: %w", sourceName, err)
	}

	place := &parsedPlace{TGNID: id}
	place.Terms, place.PreferredTerm = extractTerms(raw)
	place.PlaceTypeID, place.PlaceTypeLabel = extractPlaceType(raw.ClassifiedAs)
	place.ParentSubjectID, place.ParentLabel = extractPreferredParent(raw.PartOf)
	place.Lat, place.Lon = extractCoordinates(raw.IdentifiedBy)

	return place, nil
}

func extractTerms(raw rawPlace) ([]string, string) {
	seen := make(map[string]struct{})
	terms := make([]string, 0, len(raw.IdentifiedBy)+1)
	preferred := ""

	addTerm := func(term string) {
		term = strings.TrimSpace(term)
		if term == "" {
			return
		}
		if _, ok := seen[term]; ok {
			return
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}

	for _, ident := range raw.IdentifiedBy {
		if ident.Type != "Name" || strings.TrimSpace(ident.Content) == "" {
			continue
		}
		addTerm(ident.Content)
		if preferred == "" && hasClassification(ident.ClassifiedAs, preferredTermTypeID) {
			preferred = strings.TrimSpace(ident.Content)
		}
	}

	if preferred == "" {
		switch {
		case len(terms) > 0:
			preferred = terms[0]
		case strings.TrimSpace(raw.Label) != "":
			preferred = strings.TrimSpace(raw.Label)
			addTerm(preferred)
		default:
			preferred = strconv.FormatInt(mustPlaceID(raw.ID), 10)
			addTerm(preferred)
		}
	}

	if len(terms) == 0 {
		addTerm(preferred)
	}

	return terms, preferred
}

func extractPlaceType(classes []rawClassification) (*string, *string) {
	for _, class := range classes {
		if !hasClassification(class.ClassifiedAs, placeTypeClassID) {
			continue
		}
		id := strings.TrimSpace(class.ID)
		if id == "" {
			continue
		}
		label := strings.TrimSpace(class.Label)
		if label == "" {
			return &id, nil
		}
		return &id, &label
	}
	return nil, nil
}

func extractPreferredParent(relations []rawRelation) (*int64, *string) {
	if len(relations) == 1 {
		id, err := parseGettyID(relations[0].ID)
		if err == nil {
			label := strings.TrimSpace(relations[0].Label)
			if label == "" {
				return &id, nil
			}
			return &id, &label
		}
	}

	for _, rel := range relations {
		if !hasClassification(rel.ClassifiedAs, preferredParentTypeID) {
			continue
		}
		id, err := parseGettyID(rel.ID)
		if err != nil {
			continue
		}
		label := strings.TrimSpace(rel.Label)
		if label == "" {
			return &id, nil
		}
		return &id, &label
	}
	return nil, nil
}

func extractCoordinates(identifiers []rawIdentifier) (*float64, *float64) {
	for _, ident := range identifiers {
		if ident.Type != "crm:E47_Spatial_Coordinates" || strings.TrimSpace(ident.Value) == "" {
			continue
		}
		trimmed := strings.Trim(strings.TrimSpace(ident.Value), "[]")
		parts := strings.Split(trimmed, ",")
		if len(parts) != 2 {
			continue
		}
		lon, errLon := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		lat, errLat := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if errLon != nil || errLat != nil {
			continue
		}
		return &lat, &lon
	}
	return nil, nil
}

func normalizeText(input string) string {
	decomposed := norm.NFD.String(strings.ToLower(strings.TrimSpace(input)))
	var b strings.Builder
	b.Grow(len(decomposed))
	lastSpace := true
	for _, r := range decomposed {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastSpace = false
		default:
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func parseGettyID(uri string) (int64, error) {
	trimmed := strings.TrimSpace(uri)
	if trimmed == "" {
		return 0, fmt.Errorf("empty Getty id")
	}
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 {
		trimmed = trimmed[idx+1:]
	}
	trimmed = strings.TrimSpace(trimmed)
	return strconv.ParseInt(trimmed, 10, 64)
}

func mustPlaceID(uri string) int64 {
	id, _ := parseGettyID(uri)
	return id
}

func hasClassification(classes []rawClassification, wantID string) bool {
	for _, class := range classes {
		if class.ID == wantID {
			return true
		}
	}
	return false
}

func pairtreePath(root string, id int64) (string, error) {
	if id <= 0 {
		return "", fmt.Errorf("invalid id %d", id)
	}
	digits := strconv.FormatInt(id, 10)
	if len(digits) < 6 {
		return "", fmt.Errorf("id %d is too short for pairtree layout", id)
	}
	return filepath.Join(root, digits[:3], digits[3:6], digits+".json"), nil
}
