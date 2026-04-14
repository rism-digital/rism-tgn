package georism

import "encoding/json"

// PlaceMatch is the API payload for both search and id lookups.
type PlaceMatch struct {
	TGNID          int64           `json:"tgn_id"`
	MatchedTerm    string          `json:"matched_term"`
	PreferredTerm  string          `json:"preferred_term"`
	PlaceTypeID    *int64          `json:"place_type_id,omitempty"`
	PlaceTypeLabel *string         `json:"place_type_label,omitempty"`
	Score          *float64        `json:"score,omitempty"`
	ParentSubject  *int64          `json:"parent_subject_id,omitempty"`
	Lat            *float64        `json:"lat,omitempty"`
	Lon            *float64        `json:"lon,omitempty"`
	AncestorPairs  json.RawMessage `json:"ancestor_pairs"`
}

// Repository provides read-only place lookup operations.
type Repository interface {
	SearchPlaces(query string, limit int) ([]PlaceMatch, error)
	GetPlaceByID(id int64) (*PlaceMatch, error)
}
