package worker

import (
	"context"
	"errors"
	"math"
	"math/bits"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/images"
)

var structuralImageLabel = regexp.MustCompile(`^(season-[0-9]+|s[0-9]+e[0-9]+)$`)

// MaterializeMediaAsset records the bytes that landed for one pending asset
// and collapses exact or conservatively-matched visual duplicates. It returns
// the representative row that remains after deduplication and whether rows
// were removed.
func MaterializeMediaAsset(ctx context.Context, db *pgxpool.Pool, pending sqlc.MediaAsset, localPath string) (sqlc.MediaAsset, bool, error) {
	fingerprint, err := images.FingerprintFile(localPath)
	if err != nil {
		return sqlc.MediaAsset{}, false, err
	}
	q := sqlc.New(db)
	tx, err := db.Begin(ctx)
	if err != nil {
		return sqlc.MediaAsset{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Multiple image workers may finish the same title's artwork together. The
	// transaction lock prevents both workers from choosing each other as the
	// duplicate winner and deleting both rows. It also serializes backdrop
	// order normalization for that title.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", pending.MediaItemID); err != nil {
		return sqlc.MediaAsset{}, false, err
	}
	txq := q.WithTx(tx)
	asset, err := txq.UpdateMediaAssetMaterialization(ctx, sqlc.UpdateMediaAssetMaterializationParams{
		ID: pending.ID, LocalPath: localPath,
		ContentHash: fingerprint.ContentHash, VisualHash: fingerprint.VisualHash,
		Width: int32(fingerprint.Width), Height: int32(fingerprint.Height), FileSize: fingerprint.ByteSize,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// A concurrent materialization may have already collapsed this row. Return
		// its surviving visual equivalent so the caller still completes cleanly.
		candidates, listErr := txq.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
			MediaItemID: pending.MediaItemID, AssetType: pending.AssetType,
		})
		if listErr != nil {
			return sqlc.MediaAsset{}, false, listErr
		}
		materialized := pending
		materialized.LocalPath = localPath
		materialized.ContentHash = fingerprint.ContentHash
		materialized.VisualHash = fingerprint.VisualHash
		materialized.Width = int32(fingerprint.Width)
		materialized.Height = int32(fingerprint.Height)
		materialized.FileSize = fingerprint.ByteSize
		for _, candidate := range candidates {
			if imageDedupScope(candidate) == imageDedupScope(materialized) && sameMaterializedImage(materialized, candidate) {
				if err := tx.Commit(ctx); err != nil {
					return sqlc.MediaAsset{}, false, err
				}
				return candidate, true, nil
			}
		}
	}
	if err != nil {
		return sqlc.MediaAsset{}, false, err
	}

	candidates, err := txq.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
		MediaItemID: asset.MediaItemID, AssetType: asset.AssetType,
	})
	if err != nil {
		return asset, false, err
	}
	scope := imageDedupScope(asset)
	duplicates := make([]sqlc.MediaAsset, 0, 2)
	for _, candidate := range candidates {
		if candidate.LocalPath == "" || candidate.ContentHash == "" || imageDedupScope(candidate) != scope {
			continue
		}
		if sameMaterializedImage(asset, candidate) {
			duplicates = append(duplicates, candidate)
		}
	}
	if len(duplicates) <= 1 {
		if err := tx.Commit(ctx); err != nil {
			return asset, false, err
		}
		return asset, false, nil
	}

	winner := duplicates[0]
	desiredOrder := winner.SortOrder
	for _, candidate := range duplicates[1:] {
		if candidate.SortOrder < desiredOrder {
			desiredOrder = candidate.SortOrder
		}
		if betterImageCandidate(candidate, winner) {
			winner = candidate
		}
	}

	loserPaths := make([]string, 0, len(duplicates)-1)
	for _, candidate := range duplicates {
		if candidate.ID == winner.ID {
			continue
		}
		if err := txq.DeleteMediaAsset(ctx, candidate.ID); err != nil {
			return asset, false, err
		}
		if candidate.Source == "remote" && candidate.LocalPath != winner.LocalPath {
			loserPaths = append(loserPaths, candidate.LocalPath)
		}
	}
	if asset.AssetType == sqlc.AssetTypeBackdrop {
		if err := txq.StageMediaAssetsAfterDedup(ctx, sqlc.StageMediaAssetsAfterDedupParams{
			MediaItemID: asset.MediaItemID, AssetType: asset.AssetType,
			WinnerID: winner.ID, DesiredOrder: int64(desiredOrder),
		}); err != nil {
			return asset, false, err
		}
		if err := txq.FinalizeStagedMediaAssetOrder(ctx, sqlc.FinalizeStagedMediaAssetOrderParams{
			MediaItemID: asset.MediaItemID, AssetType: asset.AssetType,
		}); err != nil {
			return asset, false, err
		}
		if desiredOrder == 0 {
			if err := txq.UpdateMediaItemBackdropPath(ctx, sqlc.UpdateMediaItemBackdropPathParams{
				ID: asset.MediaItemID, BackdropPath: winner.LocalPath,
			}); err != nil {
				return asset, false, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return asset, false, err
	}
	for _, path := range loserPaths {
		_ = os.Remove(path)
	}
	winner, err = q.GetMediaAssetByID(ctx, winner.ID)
	return winner, true, err
}

func imageDedupScope(asset sqlc.MediaAsset) string {
	if asset.AssetType == sqlc.AssetTypeStill || structuralImageLabel.MatchString(asset.Label) {
		return asset.Label
	}
	return ""
}

func sameMaterializedImage(left, right sqlc.MediaAsset) bool {
	if left.ContentHash != "" && left.ContentHash == right.ContentHash {
		return true
	}
	if left.VisualHash == "" || right.VisualHash == "" || left.Width <= 0 || left.Height <= 0 || right.Width <= 0 || right.Height <= 0 {
		return false
	}
	leftAspect := float64(left.Width) / float64(left.Height)
	rightAspect := float64(right.Width) / float64(right.Height)
	if math.Abs(leftAspect-rightAspect)/math.Max(leftAspect, rightAspect) > 0.02 {
		return false
	}
	leftHash, leftRGB, ok := parseVisualHash(left.VisualHash)
	if !ok {
		return false
	}
	rightHash, rightRGB, ok := parseVisualHash(right.VisualHash)
	if !ok || bits.OnesCount64(leftHash^rightHash) > 4 {
		return false
	}
	for i := range leftRGB {
		if absInt(int(leftRGB[i])-int(rightRGB[i])) > 12 {
			return false
		}
	}
	return true
}

func parseVisualHash(value string) (uint64, [3]uint8, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 || len(parts[1]) != 6 {
		return 0, [3]uint8{}, false
	}
	hash, err := strconv.ParseUint(parts[0], 16, 64)
	if err != nil {
		return 0, [3]uint8{}, false
	}
	var rgb [3]uint8
	for i := range rgb {
		channel, err := strconv.ParseUint(parts[1][i*2:i*2+2], 16, 8)
		if err != nil {
			return 0, [3]uint8{}, false
		}
		rgb[i] = uint8(channel)
	}
	return hash, rgb, true
}

func betterImageCandidate(candidate, current sqlc.MediaAsset) bool {
	if candidateSourceRank(candidate.Source) != candidateSourceRank(current.Source) {
		return candidateSourceRank(candidate.Source) > candidateSourceRank(current.Source)
	}
	candidatePixels := int64(candidate.Width) * int64(candidate.Height)
	currentPixels := int64(current.Width) * int64(current.Height)
	if candidatePixels != currentPixels {
		return candidatePixels > currentPixels
	}
	if candidate.FileSize != current.FileSize {
		return candidate.FileSize > current.FileSize
	}
	if candidate.SortOrder != current.SortOrder {
		return candidate.SortOrder < current.SortOrder
	}
	return candidate.ID < current.ID
}

func candidateSourceRank(source string) int {
	switch source {
	case "custom":
		return 3
	case "local":
		return 2
	default:
		return 1
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
