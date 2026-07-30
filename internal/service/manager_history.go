package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// ── History (decisions ledger) ───────────────────────────────────────────

type ManagerDecisionView struct {
	ID           int64     `json:"id"`
	RunID        int64     `json:"run_id"`
	RunKind      string    `json:"run_kind"`
	DecidedAt    time.Time `json:"decided_at"`
	TargetKind   string    `json:"target_kind"`
	TargetKey    string    `json:"target_key"`
	MediaItemID  *int64    `json:"media_item_id,omitempty"`
	LibraryID    int64     `json:"library_id"`
	Domain       string    `json:"domain"`
	TargetTitle  string    `json:"target_title"`
	TargetYear   int       `json:"target_year,omitempty"`
	SeasonNumber *int      `json:"season_number,omitempty"`
	EpisodeNum   *int      `json:"episode_number,omitempty"`
	ProfileName  string    `json:"profile_name,omitempty"`
	Verdict      string    `json:"verdict"`
	ChosenTitle  string    `json:"chosen_title,omitempty"`
	// The chosen release's evaluation facts — why it won, legibly.
	ChosenQuality   string `json:"chosen_quality,omitempty"`
	ChosenScore     int32  `json:"chosen_score,omitempty"`
	ChosenSizeBytes int64  `json:"chosen_size_bytes,omitempty"`
	ChosenBreakdown any    `json:"chosen_breakdown,omitempty"`
	Context         any    `json:"context,omitempty"`
}

type ManagerHistoryPage struct {
	Decisions []ManagerDecisionView `json:"decisions"`
	// Cursor for the next page: pass back as before_decided_at/before_id.
	NextBefore *time.Time `json:"next_before,omitempty"`
	NextID     int64      `json:"next_id,omitempty"`
}

type ManagerHistoryParams struct {
	Verdicts  []string
	Domains   []string
	LibraryID int64
	Before    *time.Time
	BeforeID  int64
	Limit     int
}

// ManagerHistory returns the decision ledger, newest first, keyset-paged.
func (a *App) ManagerHistory(ctx context.Context, p ManagerHistoryParams) (ManagerHistoryPage, error) {
	q := sqlc.New(a.db)
	limit := p.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var before pgtype.Timestamptz
	if p.Before != nil {
		before = pgtype.Timestamptz{Time: *p.Before, Valid: true}
	}
	rows, err := q.ListManagerDecisions(ctx, sqlc.ListManagerDecisionsParams{
		Verdicts: orEmptyStrings(p.Verdicts), Domains: orEmptyStrings(p.Domains),
		LibraryID: p.LibraryID, BeforeDecidedAt: before, BeforeID: p.BeforeID,
		PageLimit: int32(limit + 1),
	})
	if err != nil {
		return ManagerHistoryPage{}, fmt.Errorf("listing decisions: %w", err)
	}

	page := ManagerHistoryPage{Decisions: []ManagerDecisionView{}}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	models := make([]sqlc.ManagerDecision, 0, len(rows))
	kinds := make([]string, 0, len(rows))
	for _, row := range rows {
		models = append(models, decisionsRowModel(row))
		kinds = append(kinds, row.RunKind)
	}
	chosen, err := a.chosenTitlesFor(ctx, chosenDecisionIDs(models))
	if err != nil {
		return ManagerHistoryPage{}, err
	}
	for i, model := range models {
		page.Decisions = append(page.Decisions, decisionView(model, kinds[i], chosen[model.ID]))
	}
	if more && len(models) > 0 {
		last := models[len(models)-1]
		t := last.DecidedAt.Time
		page.NextBefore = &t
		page.NextID = last.ID
	}
	return page, nil
}

// ManagerItemDecisions is the entity-page accountability slice.
func (a *App) ManagerItemDecisions(ctx context.Context, mediaItemID int64, page, perPage int) ([]ManagerDecisionView, int64, error) {
	q := sqlc.New(a.db)
	if perPage <= 0 || perPage > 100 {
		perPage = 25
	}
	if page < 1 {
		page = 1
	}
	rows, err := q.ListManagerDecisionsByItem(ctx, sqlc.ListManagerDecisionsByItemParams{
		MediaItemID: pgtype.Int8{Int64: mediaItemID, Valid: true},
		PageLimit:   int32(perPage), PageOffset: int32((page - 1) * perPage),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("listing item decisions: %w", err)
	}
	total, err := q.CountManagerDecisionsByItem(ctx, pgtype.Int8{Int64: mediaItemID, Valid: true})
	if err != nil {
		return nil, 0, err
	}
	views := []ManagerDecisionView{}
	models := make([]sqlc.ManagerDecision, 0, len(rows))
	kinds := make([]string, 0, len(rows))
	for _, row := range rows {
		models = append(models, decisionsRowModel(sqlc.ListManagerDecisionsRow(row)))
		kinds = append(kinds, row.RunKind)
	}
	chosen, err := a.chosenTitlesFor(ctx, chosenDecisionIDs(models))
	if err != nil {
		return nil, 0, err
	}
	for i, model := range models {
		views = append(views, decisionView(model, kinds[i], chosen[model.ID]))
	}
	return views, total, nil
}

// decisionsRowModel narrows the flattened joined row back to the base
// decision model (sqlc flattens d.* + run fields into one struct).
func decisionsRowModel(row sqlc.ListManagerDecisionsRow) sqlc.ManagerDecision {
	return sqlc.ManagerDecision{
		ID: row.ID, RunID: row.RunID, DecidedAt: row.DecidedAt,
		TargetKind: row.TargetKind, TargetKey: row.TargetKey,
		MediaItemID: row.MediaItemID, TvEpisodeID: row.TvEpisodeID, MusicTargetID: row.MusicTargetID,
		LibraryID: row.LibraryID, Domain: row.Domain,
		TargetTitle: row.TargetTitle, TargetYear: row.TargetYear,
		SeasonNumber: row.SeasonNumber, EpisodeNumber: row.EpisodeNumber, AbsoluteNumber: row.AbsoluteNumber,
		ArtistName: row.ArtistName, AlbumType: row.AlbumType, EditionKey: row.EditionKey, AlbumTitle: row.AlbumTitle,
		ProfileID: row.ProfileID, ProfileName: row.ProfileName, PolicyHash: row.PolicyHash,
		EvaluatorVersion: row.EvaluatorVersion, ParserVersion: row.ParserVersion,
		Verdict: row.Verdict, ChosenTargetRow: row.ChosenTargetRow, Context: row.Context,
	}
}

func chosenDecisionIDs(models []sqlc.ManagerDecision) []int64 {
	ids := make([]int64, 0, len(models))
	for _, model := range models {
		if model.ChosenTargetRow.Valid {
			ids = append(ids, model.ID)
		}
	}
	return ids
}

// chosenFact is the chosen candidate's evaluation record for one decision.
type chosenFact struct {
	Title     string
	Quality   string
	Score     int32
	SizeBytes int64
	Breakdown []byte
}

// chosenTitlesFor resolves the chosen candidates' facts for a page of
// decisions in one query — the "why it won" data every surface renders.
func (a *App) chosenTitlesFor(ctx context.Context, ids []int64) (map[int64]chosenFact, error) {
	facts := map[int64]chosenFact{}
	if len(ids) == 0 {
		return facts, nil
	}
	dbRows, err := a.db.Query(ctx, `
		SELECT d.id, c.title, COALESCE(c.quality, ''), c.format_score, c.size_bytes, c.format_breakdown
		FROM manager_decisions d
		JOIN manager_candidate_targets ct ON ct.id = d.chosen_target_row
		JOIN manager_candidates c ON c.id = ct.candidate_id
		WHERE d.id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("resolving chosen candidates: %w", err)
	}
	defer dbRows.Close()
	for dbRows.Next() {
		var id int64
		var fact chosenFact
		if err := dbRows.Scan(&id, &fact.Title, &fact.Quality, &fact.Score, &fact.SizeBytes, &fact.Breakdown); err != nil {
			return nil, err
		}
		facts[id] = fact
	}
	return facts, dbRows.Err()
}

func decisionView(row sqlc.ManagerDecision, runKind string, chosen chosenFact) ManagerDecisionView {
	view := ManagerDecisionView{
		ID: row.ID, RunID: row.RunID, RunKind: runKind,
		DecidedAt: row.DecidedAt.Time, TargetKind: row.TargetKind, TargetKey: row.TargetKey,
		LibraryID: row.LibraryID, Domain: row.Domain,
		TargetTitle: row.TargetTitle, TargetYear: int(row.TargetYear),
		ProfileName: row.ProfileName, Verdict: row.Verdict,
		ChosenTitle: chosen.Title, ChosenQuality: chosen.Quality,
		ChosenScore: chosen.Score, ChosenSizeBytes: chosen.SizeBytes,
	}
	if len(chosen.Breakdown) > 0 {
		var breakdown any
		if json.Unmarshal(chosen.Breakdown, &breakdown) == nil {
			view.ChosenBreakdown = breakdown
		}
	}
	if row.MediaItemID.Valid {
		id := row.MediaItemID.Int64
		view.MediaItemID = &id
	}
	if row.SeasonNumber.Valid {
		n := int(row.SeasonNumber.Int32)
		view.SeasonNumber = &n
	}
	if row.EpisodeNumber.Valid {
		n := int(row.EpisodeNumber.Int32)
		view.EpisodeNum = &n
	}
	if len(row.Context) > 0 {
		var doc any
		if json.Unmarshal(row.Context, &doc) == nil {
			view.Context = doc
		}
	}
	return view
}

// ── Run detail ───────────────────────────────────────────────────────────

type ManagerRunCandidateView struct {
	ID              int64                  `json:"id"`
	Title           string                 `json:"title"`
	Indexer         string                 `json:"indexer"`
	SizeBytes       int64                  `json:"size_bytes"`
	PublishDate     *time.Time             `json:"publish_date,omitempty"`
	Quality         string                 `json:"quality,omitempty"`
	FormatScore     int32                  `json:"format_score"`
	FormatBreakdown any                    `json:"format_breakdown,omitempty"`
	Parsed          any                    `json:"parsed,omitempty"`
	Rejections      []ManagerRejectionView `json:"rejections"`
	PerTarget       []ManagerRunTargetEval `json:"per_target,omitempty"`
}

type ManagerRunTargetEval struct {
	DecisionID    int64                  `json:"decision_id"`
	Verdict       string                 `json:"verdict"`
	SelectionRank int                    `json:"selection_rank,omitempty"`
	Chosen        bool                   `json:"chosen"`
	Rejections    []ManagerRejectionView `json:"rejections,omitempty"`
}

type ManagerRunDetailView struct {
	ID         int64                     `json:"id"`
	Kind       string                    `json:"kind"`
	Source     string                    `json:"source"`
	Status     string                    `json:"status"`
	Partial    bool                      `json:"partial"`
	Truncated  bool                      `json:"truncated"`
	Scope      any                       `json:"scope,omitempty"`
	Stats      any                       `json:"stats,omitempty"`
	StartedAt  time.Time                 `json:"started_at"`
	FinishedAt *time.Time                `json:"finished_at,omitempty"`
	Indexers   []ManagerRunIndexerView   `json:"indexers"`
	Decisions  []ManagerDecisionView     `json:"decisions"`
	Candidates []ManagerRunCandidateView `json:"candidates"`
}

// ManagerRunDetail returns one run with its full accountability record.
func (a *App) ManagerRunDetail(ctx context.Context, runID int64) (*ManagerRunDetailView, error) {
	q := sqlc.New(a.db)
	run, err := q.GetManagerRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("run %d: %w", runID, err)
	}
	view := &ManagerRunDetailView{
		ID: run.ID, Kind: run.Kind, Source: run.Source, Status: run.Status,
		Partial: run.Partial, Truncated: run.Truncated,
		StartedAt: run.StartedAt.Time,
		Indexers:  []ManagerRunIndexerView{}, Decisions: []ManagerDecisionView{},
		Candidates: []ManagerRunCandidateView{},
	}
	if run.FinishedAt.Valid {
		t := run.FinishedAt.Time
		view.FinishedAt = &t
	}
	_ = json.Unmarshal(run.Scope, &view.Scope)
	_ = json.Unmarshal(run.Stats, &view.Stats)

	indexers, err := q.ListManagerRunIndexers(ctx, runID)
	if err != nil {
		return nil, err
	}
	for _, idx := range indexers {
		view.Indexers = append(view.Indexers, ManagerRunIndexerView{
			Indexer: idx.IndexerName, Domain: idx.Domain, Status: idx.Status,
			Fetched: int(idx.Fetched), DurationMs: idx.DurationMs, Error: idx.Error,
		})
	}

	decisions, err := q.ListManagerDecisionsByRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	chosenRows := map[int64]int64{} // chosen candidate_target row id → decision id
	chosen, err := a.chosenTitlesFor(ctx, chosenDecisionIDs(decisions))
	if err != nil {
		return nil, err
	}
	for _, d := range decisions {
		view.Decisions = append(view.Decisions, decisionView(d, run.Kind, chosen[d.ID]))
		if d.ChosenTargetRow.Valid {
			chosenRows[d.ChosenTargetRow.Int64] = d.ID
		}
	}

	candidates, err := q.ListManagerCandidatesByRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	targets, err := q.ListManagerCandidateTargetsByRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	targetsByCandidate := map[int64][]sqlc.ManagerCandidateTarget{}
	for _, ct := range targets {
		targetsByCandidate[ct.CandidateID] = append(targetsByCandidate[ct.CandidateID], ct)
	}

	for _, cand := range candidates {
		cv := ManagerRunCandidateView{
			ID: cand.ID, Title: cand.Title, Indexer: cand.IndexerName,
			SizeBytes: cand.SizeBytes, FormatScore: cand.FormatScore,
			Rejections: []ManagerRejectionView{},
		}
		if cand.PublishDate.Valid {
			t := cand.PublishDate.Time
			cv.PublishDate = &t
		}
		if cand.Quality.Valid {
			cv.Quality = cand.Quality.String
		}
		_ = json.Unmarshal(cand.FormatBreakdown, &cv.FormatBreakdown)
		_ = json.Unmarshal(cand.Parsed, &cv.Parsed)
		_ = json.Unmarshal(cand.Rejections, &cv.Rejections)
		for _, ct := range targetsByCandidate[cand.ID] {
			eval := ManagerRunTargetEval{
				DecisionID: ct.DecisionID, Verdict: ct.Verdict,
				Chosen: chosenRows[ct.ID] == ct.DecisionID && chosenRows[ct.ID] != 0,
			}
			if ct.SelectionRank.Valid {
				eval.SelectionRank = int(ct.SelectionRank.Int32)
			}
			_ = json.Unmarshal(ct.Rejections, &eval.Rejections)
			cv.PerTarget = append(cv.PerTarget, eval)
		}
		view.Candidates = append(view.Candidates, cv)
	}
	return view, nil
}

func orEmptyStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// ── Activity feed ────────────────────────────────────────────────────────

type ManagerActivityRun struct {
	ID         int64                   `json:"id"`
	Kind       string                  `json:"kind"`
	Source     string                  `json:"source"`
	Status     string                  `json:"status"`
	Partial    bool                    `json:"partial"`
	Truncated  bool                    `json:"truncated"`
	Scope      any                     `json:"scope,omitempty"`
	Stats      any                     `json:"stats,omitempty"`
	StartedAt  time.Time               `json:"started_at"`
	FinishedAt *time.Time              `json:"finished_at,omitempty"`
	Indexers   []ManagerRunIndexerView `json:"indexers"`
}

type ManagerActivityPage struct {
	Runs  []ManagerActivityRun `json:"runs"`
	Total int64                `json:"total"`
}

// ManagerActivity lists pipeline runs newest-first with their per-indexer
// accounting — the operational feed.
func (a *App) ManagerActivity(ctx context.Context, page, perPage int) (ManagerActivityPage, error) {
	q := sqlc.New(a.db)
	if perPage <= 0 || perPage > 100 {
		perPage = 30
	}
	if page < 1 {
		page = 1
	}
	rows, err := q.ListManagerRuns(ctx, sqlc.ListManagerRunsParams{
		Kinds: []string{}, PageLimit: int32(perPage), PageOffset: int32((page - 1) * perPage),
	})
	if err != nil {
		return ManagerActivityPage{}, err
	}
	total, err := q.CountManagerRuns(ctx, []string{})
	if err != nil {
		return ManagerActivityPage{}, err
	}
	out := ManagerActivityPage{Runs: []ManagerActivityRun{}, Total: total}
	for _, run := range rows {
		view := ManagerActivityRun{
			ID: run.ID, Kind: run.Kind, Source: run.Source, Status: run.Status,
			Partial: run.Partial, Truncated: run.Truncated,
			StartedAt: run.StartedAt.Time, Indexers: []ManagerRunIndexerView{},
		}
		if run.FinishedAt.Valid {
			t := run.FinishedAt.Time
			view.FinishedAt = &t
		}
		_ = json.Unmarshal(run.Scope, &view.Scope)
		_ = json.Unmarshal(run.Stats, &view.Stats)
		indexers, err := q.ListManagerRunIndexers(ctx, run.ID)
		if err == nil {
			for _, idx := range indexers {
				view.Indexers = append(view.Indexers, ManagerRunIndexerView{
					Indexer: idx.IndexerName, Domain: idx.Domain, Status: idx.Status,
					Fetched: int(idx.Fetched), DurationMs: idx.DurationMs, Error: idx.Error,
				})
			}
		}
		out.Runs = append(out.Runs, view)
	}
	return out, nil
}
