//go:build research

package ml

import (
	"testing"
)

// === CORRECTNESS TESTS ===

func TestBPEPairCounts(t *testing.T) {
	// [1, 2, 3, 2, 3] should produce {(1,2):1, (2,3):2, (3,2):1}
	ids := []int{1, 2, 3, 2, 3}
	counts := BPEPairCounts(ids)

	expected := map[BPEPair]int{
		{1, 2}: 1,
		{2, 3}: 2,
		{3, 2}: 1,
	}

	if len(counts) != len(expected) {
		t.Fatalf("expected %d pairs, got %d", len(expected), len(counts))
	}

	for p, want := range expected {
		got, ok := counts[p]
		if !ok {
			t.Errorf("missing pair %v", p)
		} else if got != want {
			t.Errorf("pair %v: want %d, got %d", p, want, got)
		}
	}
}

func TestBPEPairCountsEmpty(t *testing.T) {
	if len(BPEPairCounts(nil)) != 0 {
		t.Error("nil input should produce empty counts")
	}
	if len(BPEPairCounts([]int{})) != 0 {
		t.Error("empty input should produce empty counts")
	}
	if len(BPEPairCounts([]int{42})) != 0 {
		t.Error("single-element input should produce empty counts")
	}
}

func TestBPEApplyMerge(t *testing.T) {
	tests := []struct {
		name  string
		ids   []int
		pair  BPEPair
		newID int
		want  []int
	}{
		{
			name:  "basic merge",
			ids:   []int{1, 2, 3, 4},
			pair:  BPEPair{2, 3},
			newID: 99,
			want:  []int{1, 99, 4},
		},
		{
			name:  "no match",
			ids:   []int{1, 2, 3},
			pair:  BPEPair{5, 6},
			newID: 99,
			want:  []int{1, 2, 3},
		},
		{
			name:  "multiple occurrences",
			ids:   []int{1, 2, 1, 2},
			pair:  BPEPair{1, 2},
			newID: 99,
			want:  []int{99, 99},
		},
		{
			name:  "overlapping pairs resolve left-to-right",
			ids:   []int{1, 1, 1},
			pair:  BPEPair{1, 1},
			newID: 99,
			want:  []int{99, 1},
		},
		{
			name:  "empty input",
			ids:   []int{},
			pair:  BPEPair{1, 2},
			newID: 99,
			want:  []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BPEApplyMerge(tt.ids, tt.pair, tt.newID)
			if len(got) != len(tt.want) {
				t.Fatalf("len: got %d, want %d\n  got:  %v\n  want: %v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("index %d: got %d, want %d\n  got:  %v\n  want: %v", i, got[i], tt.want[i], got, tt.want)
					break
				}
			}
		})
	}
}

func TestBPETrainSmall(t *testing.T) {
	// "aab aab aab" as bytes
	corpus := []int{97, 97, 98, 32, 97, 97, 98, 32, 97, 97, 98}
	merges := BPETrain(corpus, 3)

	if len(merges) == 0 {
		t.Fatal("expected at least one merge")
	}

	// First merge should be (97, 97) since "aa" appears 3 times.
	if merges[0].Pair != (BPEPair{97, 97}) {
		t.Errorf("first merge pair: got %v, want (97, 97)", merges[0].Pair)
	}
	if merges[0].NewID != 256 {
		t.Errorf("first merge NewID: got %d, want 256", merges[0].NewID)
	}
}

func TestBPERoundTrip(t *testing.T) {
	corpus := "hello world hello world goodbye world hello"
	corpusIDs := make([]int, len(corpus))
	for i, b := range []byte(corpus) {
		corpusIDs[i] = int(b)
	}
	merges := BPETrain(corpusIDs, 20)
	vocab := BPEBuildVocab(merges)

	tests := []string{
		"hello",
		"world",
		"goodbye",
		"hello world",
		"",
		"x",
		"helloworld",
	}

	for _, s := range tests {
		encoded := BPEEncode(s, merges)
		decoded := BPEDecode(encoded, vocab)
		if decoded != s {
			t.Errorf("round-trip failed for %q: got %q", s, decoded)
		}
	}
}

func TestBPEBuildVocab(t *testing.T) {
	merges := []BPEMerge{
		{Pair: BPEPair{104, 105}, NewID: 256}, // h + i -> 256
		{Pair: BPEPair{256, 33}, NewID: 257},  // "hi" + ! -> 257
	}
	vocab := BPEBuildVocab(merges)

	if string(vocab[104]) != "h" {
		t.Errorf("vocab[104]: got %q, want %q", vocab[104], "h")
	}
	if string(vocab[256]) != "hi" {
		t.Errorf("vocab[256]: got %q, want %q", vocab[256], "hi")
	}
	if string(vocab[257]) != "hi!" {
		t.Errorf("vocab[257]: got %q, want %q", vocab[257], "hi!")
	}
}

// === BENCHMARKS ===

func BenchmarkBPEPairCounts(b *testing.B) {
	corpus := make([]int, 100_000)
	for i := range corpus {
		corpus[i] = i % 256
	}
	b.ResetTimer()
	for b.Loop() {
		BPEPairCounts(corpus)
	}
}

func BenchmarkBPEApplyMerge(b *testing.B) {
	corpus := make([]int, 100_000)
	for i := range corpus {
		corpus[i] = i % 256
	}
	pair := BPEPair{0, 1}
	b.ResetTimer()
	for b.Loop() {
		BPEApplyMerge(corpus, pair, 256)
	}
}

func BenchmarkBPETrain(b *testing.B) {
	corpus := make([]int, 50_000)
	for i := range corpus {
		corpus[i] = i % 128
	}
	b.ResetTimer()
	for b.Loop() {
		BPETrain(corpus, 64)
	}
}

func BenchmarkBPEEncode(b *testing.B) {
	corpus := "the quick brown fox jumps over the lazy dog "
	for i := 0; i < 6; i++ {
		corpus += corpus
	}
	corpusIDs := make([]int, len(corpus))
	for i, ch := range []byte(corpus) {
		corpusIDs[i] = int(ch)
	}
	merges := BPETrain(corpusIDs, 64)
	input := "the quick brown fox jumps over the lazy dog"

	b.ResetTimer()
	for b.Loop() {
		BPEEncode(input, merges)
	}
}
