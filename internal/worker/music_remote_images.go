package worker

import (
	"context"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// remoteArtistImages is the classified+sorted view of heya.media's artist
// artwork pool, ready for the gap-fill orchestrator to consume. Each
// singular slot carries the single best candidate (or "" when none); the
// backdrop slot is an ordered list (largest first, URL-deduped).
//
// The classifier deliberately uses string-URL dedup rather than perceptual
// hashing — upstream often returns the same Discogs / Wikimedia URL via
// multiple `artwork` records, and the URL is enough to catch that without
// having to fetch a single byte.
type remoteArtistImages struct {
	Poster    string
	Backdrops []string
	Logo      string
	Banner    string
	Clearart  string
	Thumb     string
}

// rankRemoteArtistImages walks the artwork pool plus the top-level
// poster/backdrop URLs, classifies each by asset_type+aspect, dedupes by
// URL, and orders the result by size desc.
//
// Aspect rules:
//   - height > width → poster pool
//   - width > height*1.2 → backdrop pool (the 1.2 hysteresis keeps
//     near-square images from drifting into "backdrop" — they're rarely
//     usable as wide hero shots)
//   - otherwise → fallback poster pool (square art is fine as a poster
//     and there's no other slot for it)
//
// asset_type on the upstream record wins when it's specific (logo / banner
// / clearart / thumb) — those slots are typed at the source and don't get
// reclassified by aspect.
//
// Taking the pool by value (not *MediaDetail) keeps this trivially unit-
// testable against fixture slices.
func rankRemoteArtistImages(images []metadata.ArtworkResult, posterURL, backdropURL string) remoteArtistImages {
	type ranked struct {
		url      string
		score    int // sort key (height for portrait, width for landscape, fallback to score*1000)
		fallback bool
	}

	var (
		posterPool   []ranked
		backdropPool []ranked
		logoBest     ranked
		bannerBest   ranked
		clearartBest ranked
		thumbBest    ranked
	)

	considerTyped := func(target *ranked, r ranked) {
		// Largest size wins; ties broken by upstream score via the fallback
		// path below. Empty url == unset, anything beats it.
		if target.url == "" || r.score > target.score {
			*target = r
		}
	}

	for _, img := range images {
		if img.URL == "" {
			continue
		}
		size := img.Width
		if img.Height > size {
			size = img.Height
		}
		if size == 0 {
			// Some upstream records have no dimensions. Use score*1000 as
			// a deterministic fallback so they sort behind anything with a
			// real size.
			size = int(img.Score * 1000)
		}

		r := ranked{url: img.URL, score: size}

		switch img.AssetType {
		case "logo":
			considerTyped(&logoBest, r)
			continue
		case "banner":
			considerTyped(&bannerBest, r)
			continue
		case "clearart":
			considerTyped(&clearartBest, r)
			continue
		case "thumb":
			considerTyped(&thumbBest, r)
			continue
		}

		// Aspect-based classification for the unstructured "artwork" /
		// "photo" / "poster" / "backdrop" buckets.
		w, h := img.Width, img.Height
		switch {
		case w > 0 && h > 0 && h > w:
			posterPool = append(posterPool, ranked{url: img.URL, score: h})
		case w > 0 && h > 0 && float64(w) > float64(h)*1.2:
			backdropPool = append(backdropPool, ranked{url: img.URL, score: w})
		default:
			// Square or unknown — usable as a poster fallback.
			posterPool = append(posterPool, ranked{url: img.URL, score: size, fallback: true})
		}
	}

	// Top-level poster / backdrop URLs are extra candidates. They lack
	// dimensions so they ride the fallback path — but they're often the
	// hand-picked "primary" upstream and worth keeping in the pool.
	if posterURL != "" {
		posterPool = append(posterPool, ranked{url: posterURL, score: 0, fallback: true})
	}
	if backdropURL != "" {
		backdropPool = append(backdropPool, ranked{url: backdropURL, score: 0, fallback: true})
	}

	// Sort: real-size entries first (largest desc), then fallbacks.
	sort.SliceStable(posterPool, func(i, j int) bool {
		if posterPool[i].fallback != posterPool[j].fallback {
			return !posterPool[i].fallback
		}
		return posterPool[i].score > posterPool[j].score
	})
	sort.SliceStable(backdropPool, func(i, j int) bool {
		if backdropPool[i].fallback != backdropPool[j].fallback {
			return !backdropPool[i].fallback
		}
		return backdropPool[i].score > backdropPool[j].score
	})

	out := remoteArtistImages{
		Logo:     logoBest.url,
		Banner:   bannerBest.url,
		Clearart: clearartBest.url,
		Thumb:    thumbBest.url,
	}

	if len(posterPool) > 0 {
		out.Poster = posterPool[0].url
	}

	seen := map[string]bool{}
	if out.Poster != "" {
		// Posters never count as backdrops, but a remote that surfaces the
		// same URL under both slots shouldn't be queued twice — the unique
		// index on media_assets would reject the second insert anyway.
		seen[out.Poster] = true
	}
	for _, r := range backdropPool {
		if seen[r.url] {
			continue
		}
		seen[r.url] = true
		out.Backdrops = append(out.Backdrops, r.url)
		if len(out.Backdrops) >= maxArtistBackdrops {
			break
		}
	}

	return out
}

// queueArtistArtworkGaps walks the singular slots + the backdrop list and
// enqueues a DownloadImageArgs for whatever the local detector didn't
// already fill. Locals always win — for a slot where local.Count > 0 the
// remote URL is dropped on the floor (no fallback queueing).
//
// `mediaType` is the EnrichMediaItemArgs.MediaType the worker passes down;
// it lands as the directory name under data/images/<media_type>/<slug>/.
// For artist art that's "music".
func queueArtistArtworkGaps(
	ctx context.Context,
	client *river.Client[pgx.Tx],
	item sqlc.MediaItemCard,
	mediaType string,
	local musicLocalAssets,
	remote remoteArtistImages,
) {
	queue := func(url, assetType string, sortOrder int) {
		if url == "" || local.UsedURLs[url] {
			return
		}
		if _, err := client.Insert(ctx, DownloadImageArgs{
			MediaItemID: item.ID,
			EntityType:  "media",
			URL:         url,
			AssetType:   assetType,
			MediaType:   mediaType,
			SortOrder:   sortOrder,
		}, &river.InsertOpts{Priority: PriorityEnrichment}); err != nil {
			log.Warn().Err(err).
				Int64("item_id", item.ID).
				Str("asset_type", assetType).
				Str("url", url).
				Msg("enqueue artist artwork gap fill failed")
			return
		}
		// Mark the URL as used immediately so a logic mistake further down
		// (or a future caller that re-queries) can't double-queue.
		local.UsedURLs[url] = true
	}

	// Singular slots — queue at sort_order=0 only when local has nothing.
	if local.Poster == 0 {
		queue(remote.Poster, "poster", 0)
	}
	if local.Logo == 0 {
		queue(remote.Logo, "logo", 0)
	}
	if local.Banner == 0 {
		queue(remote.Banner, "banner", 0)
	}
	if local.Clearart == 0 {
		queue(remote.Clearart, "clearart", 0)
	}
	if local.Thumb == 0 {
		queue(remote.Thumb, "thumb", 0)
	}

	// Backdrops: fill up to maxArtistBackdrops total. The first remote
	// candidate goes to sort_order = local.Backdrop (the next free slot)
	// — if local already populated sort 0 + 1, remote starts at 2.
	need := maxArtistBackdrops - local.Backdrop
	if need <= 0 {
		log.Info().
			Int64("item_id", item.ID).
			Int("local_backdrop", local.Backdrop).
			Int("remote_backdrops_avail", len(remote.Backdrops)).
			Int("queued", 0).
			Msg("artist artwork gap fill complete")
		return
	}

	queued := 0
	for _, url := range remote.Backdrops {
		if queued >= need {
			break
		}
		if url == "" || local.UsedURLs[url] {
			continue
		}
		queue(url, "backdrop", local.Backdrop+queued)
		queued++
	}

	log.Info().
		Int64("item_id", item.ID).
		Int("local_poster", local.Poster).
		Int("local_backdrop", local.Backdrop).
		Int("local_logo", local.Logo).
		Int("local_banner", local.Banner).
		Int("local_clearart", local.Clearart).
		Int("local_thumb", local.Thumb).
		Bool("remote_poster", remote.Poster != "").
		Bool("remote_logo", remote.Logo != "").
		Bool("remote_banner", remote.Banner != "").
		Bool("remote_clearart", remote.Clearart != "").
		Bool("remote_thumb", remote.Thumb != "").
		Int("remote_backdrops", len(remote.Backdrops)).
		Int("backdrops_queued", queued).
		Msg("artist artwork gap fill complete")
}
