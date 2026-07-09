package textembed

import (
	"math"
	"os"
	"testing"

	"github.com/karbowiak/heya/internal/sonicanalysis"
)

// TestEmbed is an integration test guarded on BGE_DIR (the model isn't in CI).
// Run with: BGE_DIR=/path/to/bge go test ./internal/textembed/ -run TestEmbed -v
func TestEmbed(t *testing.T) {
	dir := os.Getenv("BGE_DIR")
	if dir == "" {
		t.Skip("BGE_DIR not set — needs the BGE model files")
	}
	e, err := New(dir, sonicanalysis.AccelCPU)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer e.Close()

	a, err := e.Embed("A melancholic fantasy about an elf reflecting on mortality and lost friends.")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(a) != Dim {
		t.Fatalf("dim = %d, want %d", len(a), Dim)
	}
	var norm float64
	for _, x := range a {
		norm += float64(x) * float64(x)
	}
	if math.Abs(math.Sqrt(norm)-1) > 1e-3 {
		t.Fatalf("embedding not L2-normalized: |v| = %v", math.Sqrt(norm))
	}

	b, _ := e.Embed("A slow, emotional story about grief, memory, and saying goodbye.")
	c, _ := e.Embed("Explosive mecha war with giant robots and massive space battles.")
	simAB, simAC := dotf(a, b), dotf(a, c)
	t.Logf("cosine(melancholy, grief) = %.3f   cosine(melancholy, mecha) = %.3f", simAB, simAC)
	if simAB <= simAC {
		t.Errorf("expected melancholy closer to grief than to mecha, got %.3f vs %.3f", simAB, simAC)
	}
}

func dotf(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}
