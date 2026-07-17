package textembed

import (
	"math"
	"os"
	"testing"

	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/sugarme/tokenizer/pretrained"
)

func TestBGEVocabularyTokenizesUnicode(t *testing.T) {
	path := os.Getenv("BGE_TOKENIZER")
	if path == "" {
		t.Skip("BGE_TOKENIZER not set — needs the BGE-M3 tokenizer.json")
	}
	tokenizer, err := pretrained.FromFile(path)
	if err != nil {
		t.Fatalf("load tokenizer: %v", err)
	}
	for _, text := range []string{
		"激しい女性ボーカルと反抗的なムードの日本のロック",
		"Énergique, mélancolique et atmosphérique",
		"어두운 분위기의 신시사이저와 강렬한 보컬",
	} {
		encoded, err := tokenizer.EncodeSingle(text, true)
		if err != nil {
			t.Fatalf("tokenize %q: %v", text, err)
		}
		if len(encoded.Ids) < 3 {
			t.Fatalf("tokenize %q produced only %d tokens", text, len(encoded.Ids))
		}
	}
}

// TestEmbed is an integration test guarded on BGE_DIR (the model isn't in CI).
// Run with: BGE_DIR=/path/to/models/recommendations go test ./internal/textembed/ -run TestEmbed -v
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

	// Regression for the former ASCII sanitizer: Japanese text must reach the
	// multilingual tokenizer and produce a finite, normalized vector.
	jp, err := e.Embed("激しい女性ボーカルと反抗的なムードの日本のロック")
	if err != nil {
		t.Fatalf("embed Japanese: %v", err)
	}
	if len(jp) != Dim {
		t.Fatalf("Japanese embedding dim = %d, want %d", len(jp), Dim)
	}
}

func dotf(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

func TestL2NormRejectsInvalidOutput(t *testing.T) {
	for name, vector := range map[string][]float32{
		"nan":  {1, float32(math.NaN())},
		"inf":  {1, float32(math.Inf(1))},
		"zero": {0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			if err := l2norm(vector); err == nil {
				t.Fatal("l2norm accepted invalid model output")
			}
		})
	}
}

func TestL2NormNormalizesFiniteOutput(t *testing.T) {
	vector := []float32{3, 4}
	if err := l2norm(vector); err != nil {
		t.Fatalf("l2norm: %v", err)
	}
	if math.Abs(float64(vector[0])-0.6) > 1e-6 || math.Abs(float64(vector[1])-0.8) > 1e-6 {
		t.Fatalf("normalized vector = %v, want [0.6 0.8]", vector)
	}
}
