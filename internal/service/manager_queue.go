package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/manager/decision"
	"github.com/karbowiak/heya/internal/manager/formats"
	"github.com/karbowiak/heya/internal/manager/sabnzbd"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/parser/video"
)

// ManagerQueueItemView is one in-flight (or recently finished) download,
// annotated with Heya's shadow verdict: what THIS pipeline would have done
// with the release the live arrs grabbed.
type ManagerQueueItemView struct {
	Client      string  `json:"client"`
	ClientID    int64   `json:"client_id"`
	NzoID       string  `json:"nzo_id"`
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	Status      string  `json:"status"`
	SizeMB      float64 `json:"size_mb"`
	SizeLeftMB  float64 `json:"size_left_mb"`
	Percentage  int     `json:"percentage"`
	TimeLeft    string  `json:"time_left,omitempty"`
	History     bool    `json:"history"` // finished item from SAB history
	CompletedAt int64   `json:"completed_at,omitempty"`
	FailMessage string  `json:"fail_message,omitempty"`

	// What the auto-tagger made of the release name, and where it landed.
	Parsed         string `json:"parsed,omitempty" doc:"Parsed identity label, e.g. \"the ark S03E01\" or \"heat (1995)\""`
	MatchedItemID  *int64 `json:"matched_item_id,omitempty"`
	MatchedTitle   string `json:"matched_title,omitempty"`
	MatchedLibrary int64  `json:"matched_library,omitempty"`
	// LibraryState says what the covered units look like on disk right now:
	// missing (nothing), partial (some), have (all) — with the verdict this
	// answers "fills a gap" vs "upgrade for something we have".
	LibraryState  string `json:"library_state,omitempty"`
	Verdict       string `json:"verdict"`
	VerdictDetail string `json:"verdict_detail,omitempty"`
	// Rejections carries every reason, not just the headline detail.
	Rejections []decision.Rejection `json:"rejections,omitempty"`

	// What the release parses to — quality always (parse is profile-free),
	// score + matched format labels only when a profile evaluation ran.
	Quality         string               `json:"quality,omitempty"`
	Score           int32                `json:"score,omitempty"`
	FormatBreakdown []decision.FormatHit `json:"format_breakdown,omitempty"`
}

type ManagerQueueView struct {
	Items  []ManagerQueueItemView `json:"items"`
	Errors []string               `json:"errors,omitempty"`
}

// ManagerQueue merges every enabled download client's queue + recent
// history, best-effort per client, and persists a shadow verdict per item.
func (a *App) ManagerQueue(ctx context.Context) (*ManagerQueueView, error) {
	q := sqlc.New(a.db)
	clients, err := q.ListManagerDownloadClients(ctx)
	if err != nil {
		return nil, err
	}
	// The queue recognizes against the WHOLE library — foreign downloads
	// should resolve to their item even when it isn't monitored.
	index, _, err := a.buildIdentityIndex(ctx, false)
	if err != nil {
		return nil, err
	}

	view := &ManagerQueueView{Items: []ManagerQueueItemView{}}
	for _, client := range clients {
		if !client.Enabled || client.Kind != "sabnzbd" {
			continue
		}
		sab := sabnzbd.New(client.BaseUrl, client.ApiKey)
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		queue, qerr := sab.Queue(cctx)
		history, herr := sab.History(cctx, 20)
		cancel()
		if qerr != nil {
			view.Errors = append(view.Errors, fmt.Sprintf("%s: %s", client.Name, qerr.Error()))
		}
		if queue != nil {
			for _, slot := range queue.Slots {
				item := ManagerQueueItemView{
					Client: client.Name, ClientID: client.ID, NzoID: slot.NzoID, Name: slot.Filename,
					Category: slot.Category, Status: slot.Status,
					SizeMB: parseSABFloat(slot.SizeMB), SizeLeftMB: parseSABFloat(slot.SizeLeftMB),
					Percentage: int(parseSABFloat(slot.Percentage)), TimeLeft: slot.TimeLeft,
				}
				a.annotateQueueItem(ctx, q, client, index, &item)
				view.Items = append(view.Items, item)
			}
		}
		if herr == nil {
			for _, slot := range history {
				item := ManagerQueueItemView{
					Client: client.Name, ClientID: client.ID, NzoID: slot.NzoID, Name: slot.Name,
					Category: slot.Category, Status: slot.Status,
					SizeMB: float64(slot.Bytes) / (1024 * 1024), Percentage: 100,
					History: true, CompletedAt: slot.Completed, FailMessage: slot.FailMessage,
				}
				a.annotateQueueItem(ctx, q, client, index, &item)
				view.Items = append(view.Items, item)
			}
		}
	}
	return view, nil
}

// annotateQueueItem parses, matches, evaluates, and persists the shadow
// verdict for one queue/history item. Re-evaluation happens only when the
// evaluation input (policy + inventory + title) actually changed.
func (a *App) annotateQueueItem(
	ctx context.Context,
	q *sqlc.Queries,
	client sqlc.ManagerDownloadClient,
	index *rssIdentityIndex,
	item *ManagerQueueItemView,
) {
	// Identity: try tv first (SxxExx markers are unambiguous), then movie.
	verdict := "unknown_identity"
	detail := ""
	var matchedRef *rssTargetRef
	var rejections []decision.Rejection

	show := video.FilenameParseShow(item.Name)
	isTV := len(show.Seasons) > 0 || len(show.EpisodeNumbers) > 0 || show.FullSeason
	parsedTitle := show.Title
	if isTV {
		item.Parsed = parsedTitle + " " + tvUnitLabel(show.Seasons, show.EpisodeNumbers, show.FullSeason)
	} else {
		parsed := video.FilenameParseMovie(item.Name)
		parsedTitle = parsed.Title
		item.Parsed = parsedTitle
		if parsed.Year != "" {
			item.Parsed = fmt.Sprintf("%s (%s)", parsedTitle, parsed.Year)
		}
	}
	item.Quality = formats.QualityKey(formats.ParseVideoRelease(item.Name, int64(item.SizeMB*1024*1024), isTV))
	normalized := matcher.NormalizeTitle(parsedTitle)
	if refs := index.byTitle[normalized]; len(refs) >= 1 {
		matchedRef = &refs[0]
	}

	var policyHash string
	if matchedRef != nil {
		item.MatchedItemID = &matchedRef.ItemID
		var (
			target decision.Target
			meta   searchTargetMeta
			err    error
		)
		if matchedRef.MediaType == "movie" {
			target, meta, err = a.buildMovieTarget(ctx, matchedRef.ItemID)
		} else if len(show.Seasons) > 0 {
			season := show.Seasons[0]
			target, meta, err = a.buildTVTarget(ctx, matchedRef.ItemID, matchedRef.MediaType == "anime", ManagerSearchScope{Season: &season})
		} else {
			err = fmt.Errorf("unresolvable episode numbering")
		}
		switch {
		case err != nil:
			verdict, detail = "unknown_identity", err.Error()
		case meta.ProfileID == 0:
			verdict = "no_profile"
			item.MatchedTitle = meta.Title
			item.MatchedLibrary = meta.LibraryID
		default:
			item.MatchedTitle = meta.Title
			item.MatchedLibrary = meta.LibraryID
			profile, hash, perr := a.buildDecisionPolicy(ctx, q, meta.ProfileID)
			if perr != nil || profile.Domain != meta.Domain {
				verdict, detail = "no_profile", "profile could not be resolved"
				break
			}
			policyHash = hash
			target.Profile = profile
			decision.ResolveUnits(&target)
			result := decision.Evaluate(target, []decision.Candidate{{
				Index: 0, Title: item.Name, SizeBytes: int64(item.SizeMB * 1024 * 1024),
				IndexerName: "queue", IndexerPriority: 25,
			}})
			verdict = "would_reject"
			existingByUnit := map[string]bool{}
			for _, unit := range target.Units {
				existingByUnit[unit.Key] = len(unit.Existing) > 0
			}
			for _, cand := range result.Candidates {
				if cand.QualityKey != "" {
					item.Quality = cand.QualityKey
				}
				item.Score = cand.FormatScore
				item.FormatBreakdown = cand.FormatBreakdown
				if len(cand.RunRejections) > 0 {
					rejections = cand.RunRejections
					detail = cand.RunRejections[0].Message
				}
				covered, have := 0, 0
				for key, eval := range cand.PerUnit {
					covered++
					if existingByUnit[key] {
						have++
					}
					if eval.Acceptable {
						verdict = "would_accept"
						detail = ""
					} else if len(eval.Rejections) > 0 && detail == "" {
						rejections = eval.Rejections
						detail = eval.Rejections[0].Message
					}
				}
				switch {
				case covered == 0:
				case have == 0:
					item.LibraryState = "missing"
				case have == covered:
					item.LibraryState = "have"
				default:
					item.LibraryState = "partial"
				}
			}
		}
	}
	// A matched-but-unmonitored item keeps its evaluation facts (quality,
	// score, library state — the modal shows them for reference), but the
	// verdict is honest: Heya wouldn't act on an unmonitored item.
	if matchedRef != nil && !matchedRef.Monitored {
		verdict = "unmonitored"
		if detail == "" {
			detail = "matched, but the item is not monitored — Heya wouldn't act on it"
		}
	}
	item.Verdict = verdict
	item.VerdictDetail = detail
	item.Rejections = rejections

	a.persistQueueVerdict(ctx, q, client, item, policyHash, rejections)
}

// tvUnitLabel renders the parsed episode scope: "S03", "S03E01", "S03E01-E03".
func tvUnitLabel(seasons, episodes []int, fullSeason bool) string {
	if len(seasons) == 0 {
		if len(episodes) > 0 {
			return fmt.Sprintf("E%02d", episodes[0])
		}
		return ""
	}
	label := fmt.Sprintf("S%02d", seasons[0])
	if fullSeason || len(episodes) == 0 {
		return label
	}
	label += fmt.Sprintf("E%02d", episodes[0])
	if len(episodes) > 1 {
		label += fmt.Sprintf("-E%02d", episodes[len(episodes)-1])
	}
	return label
}

// ManagerQueueFilesView lists what a finished download actually produced on
// disk — the client's storage path mapped into this server's filesystem.
type ManagerQueueFilesView struct {
	Path  string                 `json:"path" doc:"Mapped local folder (or file) of the completed download"`
	Files []ManagerQueueFileView `json:"files"`
	Error string                 `json:"error,omitempty" doc:"Set when the storage path can't be reached from this server"`
}

type ManagerQueueFileView struct {
	Name      string `json:"name" doc:"Path relative to the download folder"`
	SizeBytes int64  `json:"size_bytes"`
}

// ManagerQueueFiles resolves a history entry's storage path through the
// client's path mappings and lists its files. The walk runs behind a
// timeout: a stale network mount hangs on stat, and that must degrade to an
// honest error instead of wedging the request.
func (a *App) ManagerQueueFiles(ctx context.Context, clientID int64, nzoID string) (*ManagerQueueFilesView, error) {
	q := sqlc.New(a.db)
	client, err := q.GetManagerDownloadClient(ctx, clientID)
	if err != nil {
		return nil, fmt.Errorf("download client %d: %w", clientID, err)
	}
	if client.Kind != "sabnzbd" {
		return nil, fmt.Errorf("download client %q: file listing not supported for kind %q", client.Name, client.Kind)
	}
	sab := sabnzbd.New(client.BaseUrl, client.ApiKey)
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	history, err := sab.History(cctx, 200)
	cancel()
	if err != nil {
		return nil, err
	}
	var storage string
	for _, slot := range history {
		if slot.NzoID == nzoID {
			storage = slot.Storage
			break
		}
	}
	if storage == "" {
		return nil, fmt.Errorf("history entry %q: %w", nzoID, pgx.ErrNoRows)
	}

	var mappings []ManagerPathMapping
	_ = json.Unmarshal(client.PathMappings, &mappings)
	local := mapClientPath(storage, mappings)
	view := &ManagerQueueFilesView{Path: local, Files: []ManagerQueueFileView{}}

	type listResult struct {
		files []ManagerQueueFileView
		err   error
	}
	done := make(chan listResult, 1)
	go func() { // the goroutine may leak on a truly wedged mount — better than the request
		files, lerr := listDownloadFiles(local)
		done <- listResult{files, lerr}
	}()
	select {
	case res := <-done:
		if res.err != nil {
			view.Error = res.err.Error()
			return view, nil
		}
		view.Files = res.files
	case <-time.After(4 * time.Second):
		view.Error = "storage path not reachable from this server (timed out)"
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return view, nil
}

// mapClientPath rewrites a download-client path into this server's
// filesystem via the client's configured mappings (longest prefix wins).
func mapClientPath(remote string, mappings []ManagerPathMapping) string {
	best := ""
	local := remote
	for _, m := range mappings {
		if m.Remote != "" && strings.HasPrefix(remote, m.Remote) && len(m.Remote) > len(best) {
			best = m.Remote
			local = m.Local + strings.TrimPrefix(remote, m.Remote)
		}
	}
	return local
}

func listDownloadFiles(root string) ([]ManagerQueueFileView, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []ManagerQueueFileView{{Name: filepath.Base(root), SizeBytes: info.Size()}}, nil
	}
	const maxEntries = 500
	files := []ManagerQueueFileView{}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return nil //nolint:nilerr // unreadable subpaths are skipped, not fatal
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = d.Name()
		}
		var size int64
		if fi, ierr := d.Info(); ierr == nil {
			size = fi.Size()
		}
		files = append(files, ManagerQueueFileView{Name: rel, SizeBytes: size})
		if len(files) >= maxEntries {
			return filepath.SkipAll
		}
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, err
}

// ManagerQueueDelete removes one entry from a download client's queue or
// history. Queue deletions also drop the partially-downloaded files;
// history deletions only remove the record. The shadow-verdict ledger keeps
// its rows — accounting survives the cleanup.
func (a *App) ManagerQueueDelete(ctx context.Context, clientID int64, nzoID string, history bool) error {
	q := sqlc.New(a.db)
	client, err := q.GetManagerDownloadClient(ctx, clientID)
	if err != nil {
		return fmt.Errorf("download client %d: %w", clientID, err)
	}
	if client.Kind != "sabnzbd" {
		return fmt.Errorf("download client %q: delete not supported for kind %q", client.Name, client.Kind)
	}
	sab := sabnzbd.New(client.BaseUrl, client.ApiKey)
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if history {
		return sab.DeleteHistoryItem(cctx, nzoID)
	}
	return sab.DeleteQueueItem(cctx, nzoID)
}

func (a *App) persistQueueVerdict(
	ctx context.Context,
	q *sqlc.Queries,
	client sqlc.ManagerDownloadClient,
	item *ManagerQueueItemView,
	policyHash string,
	rejections []decision.Rejection,
) {
	if item.NzoID == "" {
		return
	}
	inputSum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d|%d",
		policyHash, item.Name, item.Verdict, orZero(item.MatchedItemID), decision.EvaluatorVersion)))
	inputHash := hex.EncodeToString(inputSum[:16])

	prior, priorErr := q.GetManagerQueueVerdict(ctx, sqlc.GetManagerQueueVerdictParams{
		DownloadClientID: pgtype.Int8{Int64: client.ID, Valid: true}, NzoID: item.NzoID,
	})
	rejDoc, _ := json.Marshal(orEmptyRejections(rejections))
	parsedDoc, _ := json.Marshal(map[string]any{
		"attrs": formats.ParseVideoRelease(item.Name, 0, false),
	})
	var matched pgtype.Int8
	if item.MatchedItemID != nil {
		matched = pgtype.Int8{Int64: *item.MatchedItemID, Valid: true}
	}
	var hash pgtype.Text
	if policyHash != "" {
		hash = pgtype.Text{String: policyHash, Valid: true}
	}
	row, err := q.UpsertManagerQueueVerdict(ctx, sqlc.UpsertManagerQueueVerdictParams{
		DownloadClientID: pgtype.Int8{Int64: client.ID, Valid: true},
		ClientName:       client.Name, NzoID: item.NzoID,
		ReleaseTitle: item.Name, Category: item.Category, SabStatusLatest: item.Status,
		Parsed: parsedDoc, MatchedMediaItemID: matched, MatchedTitle: item.MatchedTitle,
		Verdict: item.Verdict, Rejections: rejDoc, PolicyHash: hash,
		EvaluationInputHash: inputHash,
	})
	if err != nil {
		return
	}
	// History rows append only when the evaluation input genuinely changed
	// (or on first sight).
	if priorErr != nil || prior.EvaluationInputHash != inputHash {
		_ = q.AppendManagerQueueVerdictHistory(ctx, sqlc.AppendManagerQueueVerdictHistoryParams{
			VerdictID: row.ID, Verdict: item.Verdict, Rejections: rejDoc, InputHash: inputHash,
		})
	}
}

func orZero(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
