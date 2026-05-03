package georism

import "encoding/json"

// PlaceMatch is the API payload for both search and id lookups.
type PlaceMatch struct {
	TGNID          int64           `json:"tgn_id"`
	TGNURI         string          `json:"tgn_uri"`
	MatchedTerm    string          `json:"matched_term"`
	PreferredTerm  string          `json:"label"`
	PlaceTypeID    *string         `json:"place_type_uri,omitempty"`
	PlaceTypeLabel *string         `json:"place_type_label,omitempty"`
	Score          *float64        `json:"score,omitempty"`
	ParentSubject  *int64          `json:"-"`
	Lat            *float64        `json:"lat,omitempty"`
	Lon            *float64        `json:"lon,omitempty"`
	AncestorPairs  json.RawMessage `json:"ancestor_pairs"`
	AlternateNames json.RawMessage `json:"alternate_names"`
}

type SearchPage struct {
	Results []PlaceMatch
	Total   int
}

// Repository provides read-only place lookup operations.
type Repository interface {
	SearchPlaces(query string, page int, pageSize int) (SearchPage, error)
	GetPlaceByID(id int64) (*PlaceMatch, error)
}
