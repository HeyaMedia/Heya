package service

import (
	"testing"

	"github.com/karbowiak/heya/internal/textembed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResetRecEmbedderDefersCloseUntilLastLease(t *testing.T) {
	app := &App{}
	embedder := &textembed.Embedder{}
	generation := &recEmbedderGeneration{embedder: embedder, refs: 1}
	app.recEmbedder = generation
	lease := &recEmbedderLease{app: app, generation: generation, embedder: embedder}

	app.resetRecEmbedder()

	app.recEmbedderMu.Lock()
	assert.Nil(t, app.recEmbedder)
	assert.True(t, generation.retired)
	assert.False(t, generation.closed, "native model must remain alive while inference holds a lease")
	app.recEmbedderMu.Unlock()

	lease.Close()
	lease.Close()

	app.recEmbedderMu.Lock()
	defer app.recEmbedderMu.Unlock()
	assert.True(t, generation.closed)
	assert.Zero(t, generation.refs)
	assert.Nil(t, generation.embedder)
}

func TestRetiredRecEmbedderReleaseDoesNotRetireReplacement(t *testing.T) {
	app := &App{}
	oldEmbedder := &textembed.Embedder{}
	oldGeneration := &recEmbedderGeneration{embedder: oldEmbedder, refs: 1}
	app.recEmbedder = oldGeneration
	oldLease := &recEmbedderLease{app: app, generation: oldGeneration, embedder: oldEmbedder}

	app.resetRecEmbedder()
	replacement := &recEmbedderGeneration{embedder: &textembed.Embedder{}, refs: 1}
	app.recEmbedderMu.Lock()
	app.recEmbedder = replacement
	app.recEmbedderMu.Unlock()

	oldLease.Close()

	app.recEmbedderMu.Lock()
	defer app.recEmbedderMu.Unlock()
	require.Same(t, replacement, app.recEmbedder)
	assert.True(t, oldGeneration.closed)
	assert.False(t, replacement.retired)
}
