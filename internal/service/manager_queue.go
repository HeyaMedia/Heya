package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

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

	MatchedItemID  *int64 `json:"matched_item_id,omitempty"`
	MatchedTitle   string `json:"matched_title,omitempty"`
	MatchedLibrary int64  `json:"matched_library,omitempty"`
	Verdict        string `json:"verdict"`
	VerdictDetail  string `json:"verdict_detail,omitempty"`
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
	index, _, err := a.buildRSSIdentityIndex(ctx)
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
					Client: client.Name, NzoID: slot.NzoID, Name: slot.Filename,
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
					Client: client.Name, NzoID: slot.NzoID, Name: slot.Name,
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
	if !isTV {
		parsedTitle = video.FilenameParseMovie(item.Name).Title
	}
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
			for _, cand := range result.Candidates {
				if len(cand.RunRejections) > 0 {
					rejections = cand.RunRejections
					detail = cand.RunRejections[0].Message
				}
				for _, eval := range cand.PerUnit {
					if eval.Acceptable {
						verdict = "would_accept"
						detail = ""
					} else if len(eval.Rejections) > 0 && detail == "" {
						rejections = eval.Rejections
						detail = eval.Rejections[0].Message
					}
				}
			}
		}
	}
	item.Verdict = verdict
	item.VerdictDetail = detail

	a.persistQueueVerdict(ctx, q, client, item, policyHash, rejections)
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
