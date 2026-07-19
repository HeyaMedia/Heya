package service

import (
	"errors"
	"testing"

	"github.com/karbowiak/heya/internal/generatedwrite"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/stretchr/testify/require"
)

type generatedWriteTestWatcher struct {
	err error
}

func (*generatedWriteTestWatcher) Pause(int64)  {}
func (*generatedWriteTestWatcher) Resume(int64) {}
func (w *generatedWriteTestWatcher) SuppressGeneratedWrite(generatedwrite.Output) error {
	return w.err
}

func TestLazyWatcherGeneratedWriteAcknowledgementIsStrictlyWired(t *testing.T) {
	var target worker.WatcherPauser
	lazy := lazyWatcher{ptr: &target}
	output := generatedwrite.FromBytes("/library/artist.nfo", []byte("generated"))

	require.ErrorContains(t, lazy.SuppressGeneratedWrite(output), "not initialized")

	wantErr := errors.New("durable acknowledgement failed")
	target = &generatedWriteTestWatcher{err: wantErr}
	require.ErrorIs(t, lazy.SuppressGeneratedWrite(output), wantErr)
}
