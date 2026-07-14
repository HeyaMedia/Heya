package scanner

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediatype"
	"github.com/karbowiak/heya/internal/metadata"
)

type Options struct {
	Apply              bool
	ApplyDB            *pgxpool.Pool
	JSONL              bool
	PersistenceDB      *pgxpool.Pool
	PersistScan        bool
	Report             bool
	FetchPreview       bool
	MaterializePreview bool
	RemoteSearch       bool
	MovieFetcher       MovieDetailProvider
	MovieMaterializer  MovieMaterializeStore
	MovieSearcher      MovieSearchProvider
	BookFetcher        BookDetailProvider
	BookMaterializer   BookMaterializeStore
	BookSearcher       BookSearchProvider
	MusicFetcher       MusicDetailProvider
	MusicMaterializer  MusicMaterializeStore
	MusicProbe         MusicProbeFunc
	MusicSearcher      MusicSearchProvider
	EventWriters       []EventWriter
	ScopePaths         []string
	TVFetcher          TVDetailProvider
	TVMaterializer     TVMaterializeStore
	TVSearcher         TVSearchProvider
}

type Result struct {
	Inventory        Inventory                 `json:"-"`
	Movies           []MoviePlan               `json:"movies,omitempty"`
	MovieMatches     []MovieMatch              `json:"movie_matches,omitempty"`
	MovieSearch      []MovieSearchMatch        `json:"movie_search,omitempty"`
	MovieMetadata    []MovieFetchPreview       `json:"movie_metadata,omitempty"`
	MovieMaterialize []MovieMaterializePreview `json:"movie_materialize,omitempty"`
	MovieApply       []MovieApplyResult        `json:"movie_apply,omitempty"`
	BookPlans        []BookPlan                `json:"book_plans,omitempty"`
	BookSearch       []BookSearchMatch         `json:"book_search,omitempty"`
	BookMetadata     []BookFetchPreview        `json:"book_metadata,omitempty"`
	BookMaterialize  []BookMaterializePreview  `json:"book_materialize,omitempty"`
	BookApply        []BookApplyResult         `json:"book_apply,omitempty"`
	TVPlans          []TVPlan                  `json:"tv_plans,omitempty"`
	TVMatches        []TVMatch                 `json:"tv_matches,omitempty"`
	TVSearch         []TVSearchMatch           `json:"tv_search,omitempty"`
	TVMetadata       []TVFetchPreview          `json:"tv_metadata,omitempty"`
	TVMaterialize    []TVMaterializePreview    `json:"tv_materialize,omitempty"`
	TVApply          []TVApplyResult           `json:"tv_apply,omitempty"`
	MusicTracks      []MusicTrackPlan          `json:"music_tracks,omitempty"`
	MusicAlbums      []MusicAlbumPlan          `json:"music_albums,omitempty"`
	MusicArtists     []MusicArtistPlan         `json:"music_artists,omitempty"`
	MusicSearch      []MusicSearchMatch        `json:"music_search,omitempty"`
	MusicMetadata    []MusicFetchPreview       `json:"music_metadata,omitempty"`
	MusicMaterialize []MusicMaterializePreview `json:"music_materialize,omitempty"`
	MusicApply       []MusicApplyResult        `json:"music_apply,omitempty"`
}

type Phase string

const (
	PhaseAnalyze     Phase = "analyze"
	PhaseSearch      Phase = "search"
	PhaseFetch       Phase = "fetch"
	PhaseMaterialize Phase = "materialize"
	PhaseApply       Phase = "apply"
)

type LibraryRun struct {
	lib             sqlc.Library
	opts            Options
	out             io.Writer
	recorder        *EventRecorder
	sink            *EventSink
	result          Result
	scanRunID       int64
	analyzed        bool
	searchLoaded    bool
	searchDecisions SearchDecisions
}

func RunLibrary(ctx context.Context, lib sqlc.Library, opts Options, out io.Writer) (Result, error) {
	opts = NormalizeOptions(opts)
	run := NewLibraryRun(lib, opts, out)
	if err := run.Run(ctx, PhasesForOptions(opts)...); err != nil {
		return run.Result(), err
	}
	return run.Finish(ctx)
}

func NormalizeOptions(opts Options) Options {
	if opts.Apply {
		opts.MaterializePreview = true
	}
	if opts.MaterializePreview {
		opts.FetchPreview = true
	}
	if opts.FetchPreview {
		opts.RemoteSearch = true
	}
	return opts
}

func PhasesForOptions(opts Options) []Phase {
	phases := []Phase{PhaseAnalyze}
	if opts.RemoteSearch {
		phases = append(phases, PhaseSearch)
	}
	if opts.FetchPreview {
		phases = append(phases, PhaseFetch)
	}
	if opts.MaterializePreview {
		phases = append(phases, PhaseMaterialize)
	}
	if opts.Apply {
		phases = append(phases, PhaseApply)
	}
	return phases
}

func NewLibraryRun(lib sqlc.Library, opts Options, out io.Writer) *LibraryRun {
	if out == nil {
		out = os.Stdout
	}
	writer := EventWriter(NewHumanWriter(out))
	if opts.JSONL {
		writer = NewJSONLWriter(out)
	}
	recorder := &EventRecorder{}
	writers := []EventWriter{writer}
	if opts.Report {
		writers = []EventWriter{recorder}
	} else {
		writers = append(writers, recorder)
	}
	for _, extra := range opts.EventWriters {
		if extra != nil {
			writers = append(writers, extra)
		}
	}
	domain := string(lib.MediaType)
	sink := NewEventSink(Event{
		LibraryID:   lib.ID,
		LibraryName: lib.Name,
		LibraryType: string(lib.MediaType),
		Domain:      domain,
	}, writers...)

	return &LibraryRun{
		lib:      lib,
		opts:     opts,
		out:      out,
		recorder: recorder,
		sink:     sink,
	}
}

func (r *LibraryRun) Result() Result {
	return r.result
}

func (r *LibraryRun) ScanRunID() int64 {
	return r.scanRunID
}

func (r *LibraryRun) ResumeSearchResult(ctx context.Context, result Result, artifactID int64) error {
	r.result = result
	r.analyzed = true
	r.sink.Emit(Event{Event: "scan.artifact_loaded", Data: map[string]any{
		"kind":        scanArtifactKindSearch,
		"artifact_id": artifactID,
	}})
	if err := r.refreshSearchDecisions(ctx); err != nil {
		return err
	}
	return nil
}

func (r *LibraryRun) ResumeFetchResult(ctx context.Context, result Result, artifactID int64) (bool, error) {
	if err := r.refreshSearchDecisions(ctx); err != nil {
		return true, err
	}
	applySearchDecisionsToResult(&result, r.lib, r.searchDecisions, r.sink)
	if !fetchMetadataCoversAcceptedSearch(result, r.lib) {
		r.sink.Emit(Event{Event: "scan.artifact_stale", Severity: SeverityInfo, Data: map[string]any{
			"kind":        scanArtifactKindFetch,
			"artifact_id": artifactID,
			"reason":      "metadata_missing_or_incomplete_for_current_search_decision",
		}})
		return false, nil
	}
	r.result = result
	r.analyzed = true
	r.opts.RemoteSearch = true
	r.opts.FetchPreview = true
	r.sink.Emit(Event{Event: "scan.artifact_loaded", Data: map[string]any{
		"kind":        scanArtifactKindFetch,
		"artifact_id": artifactID,
	}})
	return true, nil
}

func (r *LibraryRun) Run(ctx context.Context, phases ...Phase) error {
	for _, phase := range phases {
		var err error
		r.sink.Emit(Event{Event: "scan.phase.start", Data: map[string]any{"phase": string(phase)}})
		switch phase {
		case PhaseAnalyze:
			err = r.runAnalyze(ctx)
		case PhaseSearch:
			err = r.runSearch(ctx)
		case PhaseFetch:
			err = r.runFetch(ctx)
		case PhaseMaterialize:
			err = r.runMaterialize(ctx)
		case PhaseApply:
			err = r.runApply(ctx)
		default:
			err = fmt.Errorf("unknown scanner phase %q", phase)
		}
		if err != nil {
			return err
		}
		r.sink.Emit(Event{Event: "scan.phase.complete", Data: map[string]any{"phase": string(phase)}})
	}
	return nil
}

func (r *LibraryRun) Finish(ctx context.Context) (Result, error) {
	summary := scanSummaryData(r.lib, r.result)
	r.sink.Emit(Event{Event: "scan.summary", Data: summary})
	if r.opts.PersistScan && r.opts.PersistenceDB != nil && scanShouldPersist(r.opts) {
		scanRunID, err := PersistScanResult(ctx, r.lib, r.result, r.recorder.Events, r.opts, r.opts.PersistenceDB, summary)
		if err != nil {
			r.sink.Emit(Event{Event: "scan.persist_failed", Severity: SeverityWarn, Message: err.Error()})
			return r.result, err
		}
		r.scanRunID = scanRunID
		r.sink.Emit(Event{Event: "scan.persisted", Data: map[string]any{"mode": scanRunMode(r.opts)}})
	}
	if r.opts.Report {
		WriteReport(r.out, r.lib, r.result, r.recorder.Events)
	}
	return r.result, nil
}

func (r *LibraryRun) runAnalyze(ctx context.Context) error {
	if r.analyzed {
		return nil
	}
	lib := r.lib
	r.sink.Emit(Event{Event: "scan.start", Data: map[string]any{"paths": len(lib.Paths)}})
	var (
		inv Inventory
		err error
	)
	if len(r.opts.ScopePaths) > 0 {
		inv, err = WalkInventoryScoped(ctx, lib.Paths, r.opts.ScopePaths, r.sink)
	} else {
		inv, err = WalkInventory(ctx, lib.Paths, r.sink)
	}
	if err != nil {
		r.result.Inventory = inv
		return r.fail(err)
	}

	r.result = Result{Inventory: inv}
	switch {
	case lib.MediaType == sqlc.MediaTypeMovie:
		movies, err := AnalyzeMovies(ctx, inv, r.sink)
		if err != nil {
			return r.fail(err)
		}
		r.result.Movies = movies
		matches, err := AnalyzeMovieMatches(ctx, movies, r.sink)
		if err != nil {
			return r.fail(err)
		}
		r.result.MovieMatches = matches
	case lib.MediaType == sqlc.MediaTypeBook:
		books, err := AnalyzeBooks(ctx, inv, r.sink)
		if err != nil {
			return r.fail(err)
		}
		r.result.BookPlans = books
	case lib.MediaType == sqlc.MediaTypeMusic:
		tracks, albums, artists, err := AnalyzeMusicWithOptions(ctx, inv, r.sink, MusicAnalysisOptions{Probe: r.opts.MusicProbe})
		if err != nil {
			return r.fail(err)
		}
		r.result.MusicTracks = tracks
		r.result.MusicAlbums = albums
		r.result.MusicArtists = artists
	case mediatype.IsTVLike(lib.MediaType):
		var tvPlans []TVPlan
		var err error
		if lib.MediaType == sqlc.MediaTypeAnime {
			tvPlans, err = AnalyzeAnime(ctx, inv, r.sink)
		} else {
			tvPlans, err = AnalyzeTV(ctx, inv, r.sink)
		}
		if err != nil {
			return r.fail(err)
		}
		r.result.TVPlans = tvPlans
		var tvMatches []TVMatch
		if lib.MediaType == sqlc.MediaTypeAnime {
			tvMatches, err = AnalyzeAnimeMatches(ctx, tvPlans, r.sink)
		} else {
			tvMatches, err = AnalyzeTVMatches(ctx, tvPlans, r.sink)
		}
		if err != nil {
			return r.fail(err)
		}
		r.result.TVMatches = tvMatches
	default:
		err := fmt.Errorf("scanner currently supports movie, TV-like, music, and book libraries only (got %q)", lib.MediaType)
		r.sink.Emit(Event{Event: "domain.unsupported", Severity: SeverityWarn, Message: err.Error()})
		return err
	}

	r.analyzed = true
	return nil
}

func (r *LibraryRun) runSearch(ctx context.Context) error {
	if err := r.requireAnalyzed(PhaseSearch); err != nil {
		return err
	}
	r.opts.RemoteSearch = true
	if err := r.loadSearchDecisions(ctx); err != nil {
		return r.fail(err)
	}
	lib := r.lib
	threshold := MatchThresholdForLibrary(lib)
	var err error
	switch {
	case lib.MediaType == sqlc.MediaTypeMovie:
		r.result.MovieSearch, err = SearchMovieMatches(ctx, r.result.MovieMatches, r.opts.MovieSearcher, r.sink, threshold, r.searchDecisions)
	case lib.MediaType == sqlc.MediaTypeBook:
		r.result.BookSearch, err = SearchBookPlans(ctx, r.result.BookPlans, r.opts.BookSearcher, r.sink, threshold, r.searchDecisions)
	case lib.MediaType == sqlc.MediaTypeMusic:
		r.result.MusicSearch, err = SearchMusicArtists(ctx, r.result.MusicArtists, r.opts.MusicSearcher, r.sink, threshold, r.searchDecisions)
	case mediatype.IsTVLike(lib.MediaType):
		searcher := r.opts.TVSearcher
		if searcher == nil {
			searcher = r.opts.MovieSearcher
		}
		if lib.MediaType == sqlc.MediaTypeAnime {
			r.result.TVSearch, err = SearchAnimeMatches(ctx, r.result.TVMatches, searcher, r.sink, threshold, r.searchDecisions)
		} else {
			r.result.TVSearch, err = SearchTVMatches(ctx, r.result.TVMatches, searcher, r.sink, threshold, r.searchDecisions)
		}
	default:
		err = fmt.Errorf("scanner currently supports movie, TV-like, music, and book libraries only (got %q)", lib.MediaType)
	}
	if err != nil {
		return r.fail(err)
	}
	return nil
}

func (r *LibraryRun) runFetch(ctx context.Context) error {
	if err := r.requireAnalyzed(PhaseFetch); err != nil {
		return err
	}
	r.opts.RemoteSearch = true
	r.opts.FetchPreview = true
	lib := r.lib
	var err error
	switch {
	case lib.MediaType == sqlc.MediaTypeMovie:
		fetcher := r.opts.MovieFetcher
		if fetcher == nil {
			if f, ok := r.opts.MovieSearcher.(MovieDetailProvider); ok {
				fetcher = f
			}
		}
		r.result.MovieMetadata, err = FetchMovieMetadataPreviews(ctx, r.result.MovieSearch, fetcher, r.sink)
	case lib.MediaType == sqlc.MediaTypeBook:
		fetcher := r.opts.BookFetcher
		if fetcher == nil {
			if f, ok := r.opts.BookSearcher.(BookDetailProvider); ok {
				fetcher = f
			}
		}
		r.result.BookMetadata, err = FetchBookMetadataPreviews(ctx, r.result.BookSearch, fetcher, r.sink)
	case lib.MediaType == sqlc.MediaTypeMusic:
		fetcher := r.opts.MusicFetcher
		if fetcher == nil {
			if f, ok := r.opts.MusicSearcher.(MusicDetailProvider); ok {
				fetcher = f
			}
		}
		r.result.MusicMetadata, err = FetchMusicMetadataPreviews(ctx, r.result.MusicSearch, r.result.MusicArtists, fetcher, r.sink)
	case mediatype.IsTVLike(lib.MediaType):
		fetcher := r.opts.TVFetcher
		if fetcher == nil {
			if f, ok := r.opts.TVSearcher.(TVDetailProvider); ok {
				fetcher = f
			}
		}
		if fetcher == nil {
			if f, ok := r.opts.MovieSearcher.(TVDetailProvider); ok {
				fetcher = f
			}
		}
		if lib.MediaType == sqlc.MediaTypeAnime {
			r.result.TVMetadata, err = FetchAnimeMetadataPreviews(ctx, r.result.TVSearch, r.result.TVMatches, fetcher, r.sink)
		} else {
			r.result.TVMetadata, err = FetchTVMetadataPreviews(ctx, r.result.TVSearch, r.result.TVMatches, fetcher, r.sink)
		}
	default:
		err = fmt.Errorf("scanner currently supports movie, TV-like, music, and book libraries only (got %q)", lib.MediaType)
	}
	if err != nil {
		return r.fail(err)
	}
	return nil
}

func (r *LibraryRun) runMaterialize(ctx context.Context) error {
	if err := r.requireAnalyzed(PhaseMaterialize); err != nil {
		return err
	}
	r.opts.RemoteSearch = true
	r.opts.FetchPreview = true
	r.opts.MaterializePreview = true
	lib := r.lib
	var err error
	switch {
	case lib.MediaType == sqlc.MediaTypeMovie:
		r.result.MovieMaterialize, err = PlanMovieMaterialization(ctx, lib, r.result, r.opts.MovieMaterializer, r.sink)
	case lib.MediaType == sqlc.MediaTypeBook:
		r.result.BookMaterialize, err = PlanBookMaterialization(ctx, lib, r.result, r.opts.BookMaterializer, r.sink)
	case lib.MediaType == sqlc.MediaTypeMusic:
		r.result.MusicMaterialize, err = PlanMusicMaterialization(ctx, lib, r.result, r.opts.MusicMaterializer, r.sink)
	case mediatype.IsTVLike(lib.MediaType):
		materializer := r.opts.TVMaterializer
		if materializer == nil {
			err = fmt.Errorf("TV materialize store is required")
		} else {
			r.result.TVMaterialize, err = PlanTVMaterialization(ctx, lib, r.result, materializer, r.sink)
		}
	default:
		err = fmt.Errorf("scanner currently supports movie, TV-like, music, and book libraries only (got %q)", lib.MediaType)
	}
	if err != nil {
		return r.fail(err)
	}
	return nil
}

func (r *LibraryRun) runApply(ctx context.Context) error {
	if err := r.requireAnalyzed(PhaseApply); err != nil {
		return err
	}
	r.opts.RemoteSearch = true
	r.opts.FetchPreview = true
	r.opts.MaterializePreview = true
	r.opts.Apply = true
	lib := r.lib
	var err error
	switch {
	case lib.MediaType == sqlc.MediaTypeMovie:
		r.result.MovieApply, err = ApplyMovieMaterialization(ctx, lib, r.result, r.opts.ApplyDB, r.sink)
	case lib.MediaType == sqlc.MediaTypeBook:
		r.result.BookApply, err = ApplyBookMaterialization(ctx, lib, r.result, r.opts.ApplyDB, r.sink)
	case lib.MediaType == sqlc.MediaTypeMusic:
		r.result.MusicApply, err = ApplyMusicMaterialization(ctx, lib, r.result, r.opts.ApplyDB, r.sink)
	case mediatype.IsTVLike(lib.MediaType):
		r.result.TVApply, err = ApplyTVMaterialization(ctx, lib, r.result, r.opts.ApplyDB, r.sink)
	default:
		err = fmt.Errorf("scanner currently supports movie, TV-like, music, and book libraries only (got %q)", lib.MediaType)
	}
	if err != nil {
		return r.fail(err)
	}
	return nil
}

func (r *LibraryRun) requireAnalyzed(phase Phase) error {
	if r.analyzed {
		return nil
	}
	err := fmt.Errorf("scanner phase %q requires %q to run first", phase, PhaseAnalyze)
	r.sink.Emit(Event{Event: "scan.error", Severity: SeverityWarn, Message: err.Error()})
	return err
}

func (r *LibraryRun) loadSearchDecisions(ctx context.Context) error {
	if r.searchLoaded || r.opts.PersistenceDB == nil {
		return nil
	}
	decisions, err := loadScannerSearchDecisionsForRun(ctx, r.lib, r.opts, r.sink)
	if err != nil {
		return err
	}
	r.searchDecisions = decisions
	r.searchLoaded = true
	return nil
}

func (r *LibraryRun) refreshSearchDecisions(ctx context.Context) error {
	r.searchLoaded = false
	if err := r.loadSearchDecisions(ctx); err != nil {
		return r.fail(err)
	}
	if len(r.searchDecisions) > 0 {
		applySearchDecisionsToResult(&r.result, r.lib, r.searchDecisions, r.sink)
	}
	return nil
}

func (r *LibraryRun) fail(err error) error {
	if err != nil {
		if retryAfter, deferred := metadata.DeferredWorkRetryAfter(err); deferred {
			r.sink.Emit(Event{
				Event:    "scan.deferred",
				Severity: SeverityInfo,
				Message:  "waiting for metadata",
				Data: map[string]any{
					"retry_after_ms": retryAfter.Milliseconds(),
				},
			})
			return err
		}
		r.sink.Emit(Event{Event: "scan.error", Severity: SeverityWarn, Message: err.Error()})
	}
	return err
}

func loadScannerSearchDecisionsForRun(ctx context.Context, lib sqlc.Library, opts Options, sink Emitter) (SearchDecisions, error) {
	if !opts.RemoteSearch || opts.PersistenceDB == nil {
		return nil, nil
	}
	decisions, err := LoadScannerSearchDecisions(ctx, opts.PersistenceDB, lib)
	if err != nil {
		return nil, err
	}
	if len(decisions) > 0 {
		sink.Emit(Event{
			Event: "match.decisions_loaded",
			Data: map[string]any{
				"count": len(decisions),
			},
		})
	}
	return decisions, nil
}

func scanSummaryData(lib sqlc.Library, result Result) map[string]any {
	return map[string]any{
		"files":             countInventoryFiles(result.Inventory),
		"book_apply":        len(result.BookApply),
		"book_materialize":  len(result.BookMaterialize),
		"book_metadata":     countFetchedBookMetadata(result.BookMetadata),
		"book_plans":        len(result.BookPlans),
		"book_search":       countAcceptedBookSearch(result.BookSearch),
		"materialize":       len(result.MovieMaterialize),
		"music_albums":      len(result.MusicAlbums),
		"music_artists":     len(result.MusicArtists),
		"music_metadata":    countFetchedMusicMetadata(result.MusicMetadata),
		"music_materialize": len(result.MusicMaterialize),
		"music_apply":       len(result.MusicApply),
		"music_tracks":      len(result.MusicTracks),
		"movie_apply":       len(result.MovieApply),
		"movie_matches":     len(result.MovieMatches),
		"movie_metadata":    countFetchedMovieMetadata(result.MovieMetadata),
		"movie_plans":       len(result.Movies),
		"movie_search":      countAcceptedMovieSearch(result.MovieSearch),
		"tv_matches":        len(result.TVMatches),
		"tv_apply":          len(result.TVApply),
		"tv_materialize":    len(result.TVMaterialize),
		"tv_metadata":       countFetchedTVMetadata(result.TVMetadata),
		"tv_plans":          len(result.TVPlans),
		"tv_search":         countAcceptedTVSearch(result.TVSearch),
		"library_type":      string(lib.MediaType),
	}
}

func scanShouldPersist(opts Options) bool {
	return opts.RemoteSearch || opts.FetchPreview || opts.MaterializePreview || opts.Apply
}

func scanRunMode(opts Options) string {
	switch {
	case opts.Apply:
		return "apply"
	case opts.MaterializePreview:
		return "materialize"
	case opts.FetchPreview:
		return "fetch"
	case opts.RemoteSearch:
		return "search"
	default:
		return "scan"
	}
}

func countInventoryFiles(inv Inventory) int {
	n := 0
	for _, root := range inv.Roots {
		n += len(root.Files)
	}
	return n
}

func countAcceptedMovieSearch(search []MovieSearchMatch) int {
	n := 0
	for _, result := range search {
		if result.Accepted {
			n++
		}
	}
	return n
}

func countFetchedMovieMetadata(previews []MovieFetchPreview) int {
	n := 0
	for _, preview := range previews {
		if preview.Error == "" {
			n++
		}
	}
	return n
}

func countAcceptedTVSearch(search []TVSearchMatch) int {
	n := 0
	for _, result := range search {
		if result.Accepted {
			n++
		}
	}
	return n
}

func countFetchedTVMetadata(previews []TVFetchPreview) int {
	n := 0
	for _, preview := range previews {
		if preview.Error == "" {
			n++
		}
	}
	return n
}

func countFetchedMusicMetadata(previews []MusicFetchPreview) int {
	n := 0
	for _, preview := range previews {
		if preview.Error == "" {
			n++
		}
	}
	return n
}

func countAcceptedBookSearch(search []BookSearchMatch) int {
	n := 0
	for _, result := range search {
		if result.Accepted {
			n++
		}
	}
	return n
}

func countFetchedBookMetadata(previews []BookFetchPreview) int {
	n := 0
	for _, preview := range previews {
		if preview.Error == "" {
			n++
		}
	}
	return n
}
