package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
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
func MaterializeMediaAsset(ctx context.Context, db *pgxpool.Pool, pending sqlc.MediaAsset, localPath, managedImageRoot string) (sqlc.MediaAsset, bool, error) {
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
	materializedPending := pending
	materializedPending.LocalPath = localPath
	materializedPending.ContentHash = fingerprint.ContentHash
	materializedPending.VisualHash = fingerprint.VisualHash
	materializedPending.Width = int32(fingerprint.Width)
	materializedPending.Height = int32(fingerprint.Height)
	materializedPending.FileSize = fingerprint.ByteSize

	// A scanner row can already own this exact cache path while a pending
	// remote row is materializing. Resolve that identity collision before the
	// UPDATE so the local-path constraint never rejects the write.
	identityDeduplicated := false
	var asset sqlc.MediaAsset
	pathCandidates, err := txq.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
		MediaItemID: pending.MediaItemID, AssetType: pending.AssetType,
	})
	if err != nil {
		return sqlc.MediaAsset{}, false, err
	}
	for _, candidate := range pathCandidates {
		if candidate.ID == pending.ID || candidate.LocalPath != localPath || imageDedupScope(candidate) != imageDedupScope(materializedPending) {
			continue
		}
		materializedCandidate := candidate
		materializedCandidate.ContentHash = fingerprint.ContentHash
		materializedCandidate.VisualHash = fingerprint.VisualHash
		materializedCandidate.Width = int32(fingerprint.Width)
		materializedCandidate.Height = int32(fingerprint.Height)
		materializedCandidate.FileSize = fingerprint.ByteSize
		if betterImageCandidate(materializedPending, materializedCandidate) {
			if err := txq.DeleteMediaAsset(ctx, candidate.ID); err != nil {
				return sqlc.MediaAsset{}, false, err
			}
			identityDeduplicated = true
			break
		}
		asset, err = txq.UpdateMediaAssetMaterialization(ctx, sqlc.UpdateMediaAssetMaterializationParams{
			ID: candidate.ID, LocalPath: localPath,
			ContentHash: fingerprint.ContentHash, VisualHash: fingerprint.VisualHash,
			Width: int32(fingerprint.Width), Height: int32(fingerprint.Height), FileSize: fingerprint.ByteSize,
		})
		if err != nil {
			return sqlc.MediaAsset{}, false, err
		}
		if err := txq.DeleteMediaAsset(ctx, pending.ID); err != nil {
			return sqlc.MediaAsset{}, false, err
		}
		if pending.SortOrder < asset.SortOrder {
			if err := txq.SetAssetSortOrder(ctx, sqlc.SetAssetSortOrderParams{ID: asset.ID, SortOrder: pending.SortOrder}); err != nil {
				return sqlc.MediaAsset{}, false, err
			}
			asset.SortOrder = pending.SortOrder
		}
		identityDeduplicated = true
		break
	}
	if asset.ID == 0 {
		asset, err = txq.UpdateMediaAssetMaterialization(ctx, sqlc.UpdateMediaAssetMaterializationParams{
			ID: pending.ID, LocalPath: localPath,
			ContentHash: fingerprint.ContentHash, VisualHash: fingerprint.VisualHash,
			Width: int32(fingerprint.Width), Height: int32(fingerprint.Height), FileSize: fingerprint.ByteSize,
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		// A concurrent materialization may have already collapsed this row. Return
		// its surviving visual equivalent so the caller still completes cleanly.
		candidates, listErr := txq.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
			MediaItemID: pending.MediaItemID, AssetType: pending.AssetType,
		})
		if listErr != nil {
			return sqlc.MediaAsset{}, false, listErr
		}
		for _, candidate := range candidates {
			if imageDedupScope(candidate) == imageDedupScope(materializedPending) && sameMaterializedImage(materializedPending, candidate) {
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
		if identityDeduplicated {
			if err := normalizeMaterializedWinner(ctx, txq, asset, asset.SortOrder, scope); err != nil {
				return asset, false, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return asset, false, err
		}
		return asset, identityDeduplicated, nil
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
		if candidate.LocalPath != winner.LocalPath && managedMediaAssetPath(candidate.LocalPath, managedImageRoot) {
			loserPaths = append(loserPaths, candidate.LocalPath)
		}
	}
	if err := normalizeMaterializedWinner(ctx, txq, winner, desiredOrder, scope); err != nil {
		return asset, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return asset, false, err
	}
	for _, path := range loserPaths {
		removeUnreferencedManagedImage(ctx, db, path)
	}
	winner, err = q.GetMediaAssetByID(ctx, winner.ID)
	return winner, true, err
}

func normalizeMaterializedWinner(ctx context.Context, q *sqlc.Queries, winner sqlc.MediaAsset, desiredOrder int32, scope string) error {
	if winner.AssetType == sqlc.AssetTypeBackdrop {
		if err := q.StageMediaAssetsAfterDedup(ctx, sqlc.StageMediaAssetsAfterDedupParams{
			MediaItemID: winner.MediaItemID, AssetType: winner.AssetType,
			WinnerID: winner.ID, DesiredOrder: int64(desiredOrder),
		}); err != nil {
			return err
		}
		if err := q.FinalizeStagedMediaAssetOrder(ctx, sqlc.FinalizeStagedMediaAssetOrderParams{
			MediaItemID: winner.MediaItemID, AssetType: winner.AssetType,
		}); err != nil {
			return err
		}
		if desiredOrder == 0 {
			return q.UpdateMediaItemBackdropPath(ctx, sqlc.UpdateMediaItemBackdropPathParams{
				ID: winner.MediaItemID, BackdropPath: winner.LocalPath,
			})
		}
	} else if winner.AssetType == sqlc.AssetTypePoster && scope == "" && desiredOrder == 0 {
		return q.UpdateMediaItemPosterPath(ctx, sqlc.UpdateMediaItemPosterPathParams{
			ID: winner.MediaItemID, PosterPath: winner.LocalPath,
		})
	}
	return nil
}

// MediaAssetReconcileStats summarizes one scanner/backfill reconciliation.
// Failed files remain unhashed so a later scan can retry transient mounts.
type MediaAssetReconcileStats struct {
	Fingerprinted int
	Deduplicated  int
	Failed        int
}

// ReconcileMediaItemAssets fingerprints every materialized visual asset for a
// single title. Local scanner rows take this path immediately instead of
// waiting for every carousel image to be requested by a browser.
func ReconcileMediaItemAssets(ctx context.Context, db *pgxpool.Pool, mediaItemID int64, managedImageRoot string) (MediaAssetReconcileStats, error) {
	assets, err := sqlc.New(db).ListMediaAssets(ctx, mediaItemID)
	if err != nil {
		return MediaAssetReconcileStats{}, err
	}
	stats := MediaAssetReconcileStats{}
	for _, asset := range assets {
		if asset.LocalPath == "" || asset.ContentHash != "" || !fingerprintableAssetType(asset.AssetType) {
			continue
		}
		_, deduplicated, fingerprintErr := MaterializeMediaAsset(ctx, db, asset, asset.LocalPath, managedImageRoot)
		if fingerprintErr != nil {
			stats.Failed++
			continue
		}
		stats.Fingerprinted++
		if deduplicated {
			stats.Deduplicated++
		}
	}
	return stats, nil
}

func fingerprintableAssetType(assetType sqlc.AssetType) bool {
	switch assetType {
	case sqlc.AssetTypePoster, sqlc.AssetTypeBackdrop, sqlc.AssetTypeLogo,
		sqlc.AssetTypeArt, sqlc.AssetTypeBanner, sqlc.AssetTypeThumb,
		sqlc.AssetTypeDisc, sqlc.AssetTypeClearart, sqlc.AssetTypeStill:
		return true
	default:
		return false
	}
}

func managedMediaAssetPath(path, managedImageRoot string) bool {
	if path == "" || managedImageRoot == "" {
		return false
	}
	base, err := filepath.Abs(managedImageRoot)
	if err != nil {
		return false
	}
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(base, candidate)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, candidate)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func removeUnreferencedManagedImage(ctx context.Context, db *pgxpool.Pool, path string) {
	var referenced bool
	err := db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM media_assets WHERE local_path = $1)
		    OR EXISTS (SELECT 1 FROM media_item_profiles WHERE poster_path = $1 OR backdrop_path = $1)
	`, path).Scan(&referenced)
	if err == nil && !referenced {
		_ = os.Remove(path)
	}
}

func imageDedupScope(asset sqlc.MediaAsset) string {
	if asset.AssetType == sqlc.AssetTypeStill || structuralImageLabel.MatchString(asset.Label) {
		return asset.Label
	}
	return ""
}

func sameMaterializedImage(left, right sqlc.MediaAsset) bool {
	return images.VisuallyEquivalent(
		images.Fingerprint{
			ContentHash: left.ContentHash, VisualHash: left.VisualHash,
			Width: int(left.Width), Height: int(left.Height), ByteSize: left.FileSize,
		},
		images.Fingerprint{
			ContentHash: right.ContentHash, VisualHash: right.VisualHash,
			Width: int(right.Width), Height: int(right.Height), ByteSize: right.FileSize,
		},
	)
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
