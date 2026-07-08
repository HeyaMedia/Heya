package ingestv2

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediatype"
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
	TVPlans          []TVPlan                  `json:"tv_plans,omitempty"`
	TVMatches        []TVMatch                 `json:"tv_matches,omitempty"`
	TVSearch         []TVSearchMatch           `json:"tv_search,omitempty"`
	TVMetadata       []TVFetchPreview          `json:"tv_metadata,omitempty"`
	TVMaterialize    []TVMaterializePreview    `json:"tv_materialize,omitempty"`
	TVApply          []TVApplyResult           `json:"tv_apply,omitempty"`
}

func RunLibrary(ctx context.Context, lib sqlc.Library, opts Options, out io.Writer) (Result, error) {
	if opts.Apply {
		opts.MaterializePreview = true
	}
	if opts.MaterializePreview {
		opts.FetchPreview = true
	}
	if opts.FetchPreview {
		opts.RemoteSearch = true
	}
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
	domain := string(lib.MediaType)
	sink := NewEventSink(Event{
		LibraryID:   lib.ID,
		LibraryName: lib.Name,
		LibraryType: string(lib.MediaType),
		Domain:      domain,
	}, writers...)

	sink.Emit(Event{Event: "scan.start", Data: map[string]any{"paths": len(lib.Paths)}})
	inv, err := WalkInventory(ctx, lib.Paths, sink)
	if err != nil {
		sink.Emit(Event{Event: "scan.error", Severity: SeverityWarn, Message: err.Error()})
		return Result{Inventory: inv}, err
	}
	searchDecisions, err := loadScannerSearchDecisionsForRun(ctx, lib, opts, sink)
	if err != nil {
		sink.Emit(Event{Event: "scan.error", Severity: SeverityWarn, Message: err.Error()})
		return Result{Inventory: inv}, err
	}

	result := Result{Inventory: inv}
	switch {
	case lib.MediaType == sqlc.MediaTypeMovie:
		movies, err := AnalyzeMovies(ctx, inv, sink)
		if err != nil {
			sink.Emit(Event{Event: "scan.error", Severity: SeverityWarn, Message: err.Error()})
			return result, err
		}
		result.Movies = movies
		matches, err := AnalyzeMovieMatches(ctx, movies, sink)
		if err != nil {
			sink.Emit(Event{Event: "scan.error", Severity: SeverityWarn, Message: err.Error()})
			return result, err
		}
		result.MovieMatches = matches
		if opts.RemoteSearch {
			search, err := SearchMovieMatches(ctx, matches, opts.MovieSearcher, sink, searchDecisions)
			if err != nil {
				sink.Emit(Event{Event: "scan.error", Severity: SeverityWarn, Message: err.Error()})
				return result, err
			}
			result.MovieSearch = search
		}
		if opts.FetchPreview {
			fetcher := opts.MovieFetcher
			if fetcher == nil {
				if f, ok := opts.MovieSearcher.(MovieDetailProvider); ok {
					fetcher = f
				}
			}
			previews, err := FetchMovieMetadataPreviews(ctx, result.MovieSearch, fetcher, sink)
			if err != nil {
				sink.Emit(Event{Event: "scan.error", Severity: SeverityWarn, Message: err.Error()})
				return result, err
			}
			result.MovieMetadata = previews
		}
		if opts.MaterializePreview {
			materialize, err := PlanMovieMaterialization(ctx, lib, result, opts.MovieMaterializer, sink)
			if err != nil {
				sink.Emit(Event{Event: "scan.error", Severity: SeverityWarn, Message: err.Error()})
				return result, err
			}
			result.MovieMaterialize = materialize
		}
		if opts.Apply {
			applied, err := ApplyMovieMaterialization(ctx, lib, result, opts.ApplyDB, sink)
			if err != nil {
				sink.Emit(Event{Event: "scan.error", Severity: SeverityWarn, Message: err.Error()})
				return result, err
			}
			result.MovieApply = applied
		}
	case mediatype.IsTVLike(lib.MediaType):
		tvPlans, err := AnalyzeTV(ctx, inv, sink)
		if err != nil {
			sink.Emit(Event{Event: "scan.error", Severity: SeverityWarn, Message: err.Error()})
			return result, err
		}
		result.TVPlans = tvPlans
		tvMatches, err := AnalyzeTVMatches(ctx, tvPlans, sink)
		if err != nil {
			sink.Emit(Event{Event: "scan.error", Severity: SeverityWarn, Message: err.Error()})
			return result, err
		}
		result.TVMatches = tvMatches
		if opts.RemoteSearch {
			searcher := opts.TVSearcher
			if searcher == nil {
				searcher = opts.MovieSearcher
			}
			search, err := SearchTVMatches(ctx, tvMatches, searcher, sink, searchDecisions)
			if err != nil {
				sink.Emit(Event{Event: "scan.error", Severity: SeverityWarn, Message: err.Error()})
				return result, err
			}
			result.TVSearch = search
		}
		if opts.FetchPreview {
			fetcher := opts.TVFetcher
			if fetcher == nil {
				if f, ok := opts.TVSearcher.(TVDetailProvider); ok {
					fetcher = f
				}
			}
			if fetcher == nil {
				if f, ok := opts.MovieSearcher.(TVDetailProvider); ok {
					fetcher = f
				}
			}
			previews, err := FetchTVMetadataPreviews(ctx, result.TVSearch, result.TVMatches, fetcher, sink)
			if err != nil {
				sink.Emit(Event{Event: "scan.error", Severity: SeverityWarn, Message: err.Error()})
				return result, err
			}
			result.TVMetadata = previews
		}
		if opts.MaterializePreview {
			materializer := opts.TVMaterializer
			if materializer == nil {
				return result, fmt.Errorf("TV materialize store is required")
			}
			materialize, err := PlanTVMaterialization(ctx, lib, result, materializer, sink)
			if err != nil {
				sink.Emit(Event{Event: "scan.error", Severity: SeverityWarn, Message: err.Error()})
				return result, err
			}
			result.TVMaterialize = materialize
		}
		if opts.Apply {
			applied, err := ApplyTVMaterialization(ctx, lib, result, opts.ApplyDB, sink)
			if err != nil {
				sink.Emit(Event{Event: "scan.error", Severity: SeverityWarn, Message: err.Error()})
				return result, err
			}
			result.TVApply = applied
		}
	default:
		err := fmt.Errorf("ingest v2 currently supports movie and TV-like libraries only (got %q)", lib.MediaType)
		sink.Emit(Event{Event: "domain.unsupported", Severity: SeverityWarn, Message: err.Error()})
		return result, err
	}

	summary := scanSummaryData(lib, result)
	sink.Emit(Event{Event: "scan.summary", Data: summary})
	if opts.PersistScan && opts.PersistenceDB != nil && scanShouldPersist(opts) {
		if err := PersistScanResult(ctx, lib, result, recorder.Events, opts, opts.PersistenceDB, summary); err != nil {
			sink.Emit(Event{Event: "scan.persist_failed", Severity: SeverityWarn, Message: err.Error()})
			return result, err
		}
		sink.Emit(Event{Event: "scan.persisted", Data: map[string]any{"mode": scanRunMode(opts)}})
	}
	if opts.Report {
		WriteReport(out, lib, result, recorder.Events)
	}
	return result, nil
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
		"files":          countInventoryFiles(result.Inventory),
		"materialize":    len(result.MovieMaterialize),
		"movie_apply":    len(result.MovieApply),
		"movie_matches":  len(result.MovieMatches),
		"movie_metadata": countFetchedMovieMetadata(result.MovieMetadata),
		"movie_plans":    len(result.Movies),
		"movie_search":   countAcceptedMovieSearch(result.MovieSearch),
		"tv_matches":     len(result.TVMatches),
		"tv_apply":       len(result.TVApply),
		"tv_materialize": len(result.TVMaterialize),
		"tv_metadata":    countFetchedTVMetadata(result.TVMetadata),
		"tv_plans":       len(result.TVPlans),
		"tv_search":      countAcceptedTVSearch(result.TVSearch),
		"library_type":   string(lib.MediaType),
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
