package georism

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// SQLRepository reads place data from PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

func NewSQLRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func (r *SQLRepository) SearchPlaces(query string, limit int) ([]PlaceMatch, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := r.db.QueryContext(context.Background(), `
SELECT
	tgn_id,
	matched_term,
	preferred_term,
	place_type_id,
	place_type_label,
	score,
	parent_subject_id,
	lat,
	lon,
	ancestor_pairs,
	alternate_names
FROM tgn.match_place_name($1, NULL, NULL, $2)
ORDER BY score DESC, preferred_term, tgn_id
`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("query search places: %w", err)
	}
	defer rows.Close()

	results := make([]PlaceMatch, 0, limit)
	for rows.Next() {
		item, err := scanPlaceMatch(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search rows: %w", err)
	}
	return results, nil
}

func (r *SQLRepository) GetPlaceByID(id int64) (*PlaceMatch, error) {
	row := r.db.QueryRowContext(context.Background(), `
SELECT
	tgn_id,
	matched_term,
	preferred_term,
	place_type_id,
	place_type_label,
	score,
	parent_subject_id,
	lat,
	lon,
	ancestor_pairs,
	alternate_names
FROM tgn.get_place_by_id($1)
`, id)

	item, err := scanPlaceMatch(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanPlaceMatch(s scanner) (PlaceMatch, error) {
	var item PlaceMatch
	var placeTypeID sql.NullInt64
	var placeTypeLabel sql.NullString
	var score sql.NullFloat64
	var parentSubjectID sql.NullInt64
	var lat sql.NullFloat64
	var lon sql.NullFloat64
	var ancestorPairs []byte
	var alternateNames []byte

	err := s.Scan(
		&item.TGNID,
		&item.MatchedTerm,
		&item.PreferredTerm,
		&placeTypeID,
		&placeTypeLabel,
		&score,
		&parentSubjectID,
		&lat,
		&lon,
		&ancestorPairs,
		&alternateNames,
	)
	if err != nil {
		return PlaceMatch{}, err
	}

	item.PlaceTypeID = nullInt64Ptr(placeTypeID)
	item.PlaceTypeLabel = nullStringPtr(placeTypeLabel)
	item.Score = nullFloat64Ptr(score)
	item.ParentSubject = nullInt64Ptr(parentSubjectID)
	item.Lat = nullFloat64Ptr(lat)
	item.Lon = nullFloat64Ptr(lon)

	if len(ancestorPairs) == 0 {
		item.AncestorPairs = json.RawMessage("[]")
	} else {
		item.AncestorPairs = ancestorPairs
	}
	if len(alternateNames) == 0 {
		item.AlternateNames = json.RawMessage("[]")
	} else {
		item.AlternateNames = alternateNames
	}

	return item, nil
}

func nullInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	out := v.Int64
	return &out
}

func nullFloat64Ptr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	out := v.Float64
	return &out
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	out := v.String
	return &out
}
