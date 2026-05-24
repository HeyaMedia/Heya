package sonicanalysis

import (
	"context"
	"fmt"
)

// Patch-cutter and batch-runner that turns a mel-spectrogram into a
// single 512-dim track embedding via the Discogs-EffNet session.

const (
	patchSize    = 128 // frames per model patch (~2.06s @ 16 kHz, hop 256)
	patchHopSize = 62  // 50% overlap; ~1.008 patches/sec
)

// slicePatches turns a flat (nFrames, melNumBands) mel-spec into
// (nPatches, patchSize, melNumBands) laid out as one flat float32
// slice ready for batching. Drops the last partial patch — same as
// Essentia's lastPatchMode=discard.
func slicePatches(melSpec []float32, nFrames int) (patches []float32, nPatches int) {
	if nFrames < patchSize {
		return nil, 0
	}
	nPatches = 1 + (nFrames-patchSize)/patchHopSize
	out := make([]float32, nPatches*patchSize*melNumBands)
	for p := 0; p < nPatches; p++ {
		srcFrame := p * patchHopSize
		srcOff := srcFrame * melNumBands
		dstOff := p * patchSize * melNumBands
		copy(out[dstOff:dstOff+patchSize*melNumBands],
			melSpec[srcOff:srcOff+patchSize*melNumBands])
	}
	return out, nPatches
}

// runBatched runs the discogsSession over `nPatches` patches in
// batches of `discogsBatchSize`, zero-padding the last batch when
// needed and discarding the padding-derived predictions.
func runBatched(sess *discogsSession, patches []float32, nPatches int) ([]float32, error) {
	if nPatches == 0 {
		return nil, fmt.Errorf("no patches (audio too short)")
	}
	patchFloats := patchSize * melNumBands
	batchFloats := discogsBatchSize * patchFloats

	out := make([]float32, nPatches*discogsEmbedDim)
	batchBuf := make([]float32, batchFloats)

	for batchStart := 0; batchStart < nPatches; batchStart += discogsBatchSize {
		remaining := nPatches - batchStart
		batchN := discogsBatchSize
		if remaining < discogsBatchSize {
			batchN = remaining
			for i := range batchBuf {
				batchBuf[i] = 0
			}
		}
		copy(batchBuf, patches[batchStart*patchFloats:(batchStart+batchN)*patchFloats])

		raw, err := sess.InferBatch(batchBuf)
		if err != nil {
			return nil, fmt.Errorf("batch %d: %w", batchStart/discogsBatchSize, err)
		}
		copy(out[batchStart*discogsEmbedDim:(batchStart+batchN)*discogsEmbedDim],
			raw[:batchN*discogsEmbedDim])
	}
	return out, nil
}

// meanPool collapses a (nPatches, embedDim) flat float32 slice into a
// single embedDim vector by averaging across the patch dimension.
func meanPool(patchEmbeds []float32, nPatches, embedDim int) []float32 {
	out := make([]float32, embedDim)
	for p := 0; p < nPatches; p++ {
		off := p * embedDim
		for d := 0; d < embedDim; d++ {
			out[d] += patchEmbeds[off+d]
		}
	}
	inv := float32(1.0) / float32(nPatches)
	for d := range out {
		out[d] *= inv
	}
	return out
}

// extractAllHeads runs every loaded Discogs specialized head on the
// same mel-spec patches, returning {head: 512-dim mean-pooled vector}.
//
//nolint:unused // staged: single-file CLI variant; pipeline path uses extractAllHeadsFromPCM
func extractAllHeads(
	ctx context.Context,
	bank *discogsHeadBank,
	audioPath string,
) (vectors map[string][]float32, patches []float32, nPatches int, err error) {
	pcm, err := decodePCM(ctx, audioPath, melSampleRate)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("decode: %w", err)
	}
	spec, nFrames := melSpec(pcm)
	patches, nPatches = slicePatches(spec, nFrames)
	if nPatches == 0 {
		return nil, nil, 0, fmt.Errorf("audio shorter than one patch (~2s)")
	}

	vectors = make(map[string][]float32, len(bank.sessions))
	for _, h := range bank.Heads() {
		sess := bank.sessions[h]
		patchEmbeds, runErr := runBatched(sess, patches, nPatches)
		if runErr != nil {
			return nil, nil, 0, fmt.Errorf("%s head: %w", h, runErr)
		}
		vectors[h] = meanPool(patchEmbeds, nPatches, discogsEmbedDim)
	}
	return vectors, patches, nPatches, nil
}
