package sonicanalysis

import (
	"errors"
	"testing"
)

func TestTextSearcherCloseIsTerminal(t *testing.T) {
	searcher := NewTextSearcher(Config{})
	searcher.Close()
	searcher.Close()

	if searcher.Ready() {
		t.Fatal("closed text searcher reported ready")
	}
	if _, err := searcher.Embed("late request"); !errors.Is(err, ErrTextSearcherClosed) {
		t.Fatalf("Embed after Close error = %v, want ErrTextSearcherClosed", err)
	}

	searcher.Reconfigure(Config{Accelerator: AccelCPU})
	if _, err := searcher.Embed("after reconfigure"); !errors.Is(err, ErrTextSearcherClosed) {
		t.Fatalf("Reconfigure revived closed searcher: %v", err)
	}
}
