package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/manager/decision"
	"github.com/karbowiak/heya/internal/manager/formats"
	"github.com/karbowiak/heya/internal/matcher"
)

// buildBookTarget assembles the decision target for one book: title +
// aliases as primary identity, the author as secondary corroboration
// (containment semantics — book release names don't segment), and existing
// files with format-derived quality. Ebooks only, per the agreed plan.
func (a *App) buildBookTarget(ctx context.Context, itemID int64) (decision.Target, searchTargetMeta, error) {
	var (
		target decision.Target
		meta   searchTargetMeta
	)
	var (
		title, authorName string
		year              int
		monitored         bool
		publishDate       pgtype.Date
		profileID         pgtype.Int8
		libraryID         int64
	)
	err := a.db.QueryRow(ctx, `
		SELECT c.title,
		       COALESCE(CASE WHEN c.year ~ '^\d{4}' THEN left(c.year, 4)::int END, 0),
		       mi.monitored, mi.quality_profile_id, c.library_id,
		       COALESCE(au.name, ''), b.publish_date
		FROM media_item_cards c
		JOIN media_items mi ON mi.id = c.id
		LEFT JOIN books b ON b.media_item_id = c.id
		LEFT JOIN authors au ON au.id = b.author_id
		WHERE c.id = $1 AND c.media_type = 'book'`, itemID).Scan(
		&title, &year, &monitored, &profileID, &libraryID, &authorName, &publishDate)
	if err != nil {
		return target, meta, fmt.Errorf("book %d: %w", itemID, err)
	}

	meta = searchTargetMeta{
		LibraryID: libraryID, Domain: "book", Title: title, Year: year,
		ArtistName: authorName,
	}
	if profileID.Valid {
		meta.ProfileID = profileID.Int64
	}

	target = decision.Target{
		Domain:      "book",
		MediaItemID: itemID,
		Year:        year,
		IDs:         map[string]string{},
	}
	if n := matcher.NormalizeTitle(title); n != "" {
		target.NormalizedTitles = append(target.NormalizedTitles, n)
	}
	aliasRows, err := a.db.Query(ctx, `SELECT title FROM media_titles WHERE media_item_id = $1`, itemID)
	if err == nil {
		for aliasRows.Next() {
			var alias string
			if aliasRows.Scan(&alias) == nil {
				if n := matcher.NormalizeTitle(alias); n != "" {
					target.NormalizedTitles = append(target.NormalizedTitles, n)
				}
			}
		}
		aliasRows.Close()
	}
	if n := matcher.NormalizeTitle(authorName); n != "" {
		target.AlbumTitles = append(target.AlbumTitles, n)
	}

	released := true
	if publishDate.Valid {
		released = !publishDate.Time.After(time.Now())
	}
	unit := decision.Unit{
		Key:       fmt.Sprintf("book:%d", itemID),
		Monitored: monitored,
		Released:  released,
	}
	fileRows, err := a.db.Query(ctx, `
		SELECT lf.id, lf.path
		FROM library_files lf
		WHERE lf.media_item_id = $1 AND lf.deleted_at IS NULL`, itemID)
	if err != nil {
		return target, meta, fmt.Errorf("book files: %w", err)
	}
	defer fileRows.Close()
	for fileRows.Next() {
		var fileID int64
		var path string
		if err := fileRows.Scan(&fileID, &path); err != nil {
			return target, meta, err
		}
		base := filepath.Base(path)
		quality := formats.BookQualityKey(base)
		unit.Existing = append(unit.Existing, decision.ExistingFile{
			FileID: fileID, Basename: base, Quality: quality,
			Provenance: "parsed_name", Uncertain: quality == "",
		})
	}
	if err := fileRows.Err(); err != nil {
		return target, meta, err
	}
	target.Units = []decision.Unit{unit}
	return target, meta, nil
}

// bookQueueRef is one book in the containment index.
type bookQueueRef struct {
	itemID    int64
	libraryID int64
	title     string
	monitored bool
	compact   []string // book title + aliases
	author    string   // compact author, "" when unknown
}

// bookIdentityIndex recognizes book release names by containment: the name
// must carry the book title (or an alias); when the author is known it must
// appear too. Longest title match wins; ties across items are ambiguous.
type bookIdentityIndex struct {
	books []*bookQueueRef
}

func (a *App) buildBookIdentityIndex(ctx context.Context, monitoredOnly bool) (*bookIdentityIndex, error) {
	rows, err := a.db.Query(ctx, `
		SELECT mi.id, mi.library_id, c.title, mi.monitored, COALESCE(au.name, ''),
		       COALESCE(array_agg(DISTINCT mt.title) FILTER (WHERE mt.title IS NOT NULL), '{}')
		FROM media_items mi
		JOIN media_item_cards c ON c.id = mi.id
		LEFT JOIN books b ON b.media_item_id = mi.id
		LEFT JOIN authors au ON au.id = b.author_id
		LEFT JOIN media_titles mt ON mt.media_item_id = mi.id
		WHERE mi.media_type = 'book' AND (mi.monitored OR NOT $1)
		GROUP BY mi.id, mi.library_id, c.title, mi.monitored, au.name`, monitoredOnly)
	if err != nil {
		return nil, fmt.Errorf("building book identity index: %w", err)
	}
	defer rows.Close()

	index := &bookIdentityIndex{}
	for rows.Next() {
		var (
			ref     bookQueueRef
			author  string
			aliases []string
		)
		if err := rows.Scan(&ref.itemID, &ref.libraryID, &ref.title, &ref.monitored, &author, &aliases); err != nil {
			return nil, err
		}
		for _, t := range append([]string{ref.title}, aliases...) {
			if c := compactQueueTitle(t); len(c) >= 4 {
				ref.compact = append(ref.compact, c)
			}
		}
		if len(ref.compact) == 0 {
			continue
		}
		ref.author = compactQueueTitle(author)
		index.books = append(index.books, &ref)
	}
	return index, rows.Err()
}

func (idx *bookIdentityIndex) match(name string) *bookQueueRef {
	compact := compactQueueTitle(name)
	if compact == "" {
		return nil
	}
	var best *bookQueueRef
	bestLen := 0
	ambiguous := false
	for _, ref := range idx.books {
		if ref.author != "" && !containsCompact(compact, ref.author) {
			continue
		}
		for _, title := range ref.compact {
			if !containsCompact(compact, title) {
				continue
			}
			switch {
			case len(title) > bestLen:
				best, bestLen, ambiguous = ref, len(title), false
			case len(title) == bestLen && best != nil && ref != best:
				ambiguous = true
			}
		}
	}
	if ambiguous {
		return nil
	}
	return best
}

func containsCompact(haystack, needle string) bool {
	return needle != "" && strings.Contains(haystack, needle)
}
