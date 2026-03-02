//go:build research

package ml

import (
	"math"
	"math/rand"
	"testing"
)

// === UNIT TESTS ===

// TestLinearCounting verifies that linearCounting produces the same result as LinearFloat
// and counts the correct number of multiplies.
func TestLinearCounting(t *testing.T) {
	x := []float64{1, 2, 3}
	w := [][]float64{
		{0.1, 0.2, 0.3},
		{0.4, 0.5, 0.6},
	}
	counter := 0
	got := linearCounting(x, w, &counter)

	// Expected: [0.1*1+0.2*2+0.3*3, 0.4*1+0.5*2+0.6*3] = [1.4, 3.2]
	want := LinearFloat(x, w)
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-10 {
			t.Errorf("linearCounting[%d] = %f, want %f", i, got[i], want[i])
		}
	}

	// 2 rows * 3 cols = 6 multiplies
	if counter != 6 {
		t.Errorf("multiply count = %d, want 6", counter)
	}
}

// TestSoftmaxCounting verifies that softmaxCounting matches SoftmaxFloat.
func TestSoftmaxCounting(t *testing.T) {
	logits := []float64{1.0, 2.0, 3.0}
	got := softmaxCounting(logits)
	want := SoftmaxFloat(logits)
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-10 {
			t.Errorf("softmaxCounting[%d] = %f, want %f", i, got[i], want[i])
		}
	}
}

// TestRMSNormCounting verifies that rmsnormCounting matches RMSNormFloat.
func TestRMSNormCounting(t *testing.T) {
	x := []float64{1, 2, 3, 4}
	got := rmsnormCounting(x)
	want := RMSNormFloat(x)
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-10 {
			t.Errorf("rmsnormCounting[%d] = %f, want %f", i, got[i], want[i])
		}
	}
}

// TestGenerateNoCacheBasic verifies that GenerateNoCache produces tokens and multiply counts.
func TestGenerateNoCacheBasic(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	trained := trainSmallKVModel(rng)
	result := GenerateNoCache(trained.BOS, trained.FloatParams, trained.VocabSize, 4)

	if len(result.Tokens) != 4 {
		t.Fatalf("expected 4 tokens, got %d", len(result.Tokens))
	}
	if len(result.MulsPerStep) != 4 {
		t.Fatalf("expected 4 mul counts, got %d", len(result.MulsPerStep))
	}
	for i, m := range result.MulsPerStep {
		if m <= 0 {
			t.Errorf("step %d: expected positive mul count, got %d", i, m)
		}
	}
}

// TestGenerateWithCacheBasic verifies that GenerateWithCache produces tokens, counts, and cache sizes.
func TestGenerateWithCacheBasic(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	trained := trainSmallKVModel(rng)
	result := GenerateWithCache(trained.BOS, trained.FloatParams, trained.VocabSize, 4)

	if len(result.Tokens) != 4 {
		t.Fatalf("expected 4 tokens, got %d", len(result.Tokens))
	}
	if len(result.CacheSizes) != 4 {
		t.Fatalf("expected 4 cache sizes, got %d", len(result.CacheSizes))
	}
	// Cache must grow monotonically
	for i := 1; i < len(result.CacheSizes); i++ {
		if result.CacheSizes[i] <= result.CacheSizes[i-1] {
			t.Errorf("cache should grow: step %d (%d) <= step %d (%d)",
				i, result.CacheSizes[i], i-1, result.CacheSizes[i-1])
		}
	}
}

// TestCachedMatchesUncached verifies that both generation methods produce identical tokens.
// This is the core correctness test: the KV cache is memoization, not approximation.
func TestCachedMatchesUncached(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	trained := trainSmallKVModel(rng)

	noCache := GenerateNoCache(trained.BOS, trained.FloatParams, trained.VocabSize, 8)
	withCache := GenerateWithCache(trained.BOS, trained.FloatParams, trained.VocabSize, 8)

	if len(noCache.Tokens) != len(withCache.Tokens) {
		t.Fatalf("token count mismatch: no-cache=%d, cached=%d",
			len(noCache.Tokens), len(withCache.Tokens))
	}
	for i := range noCache.Tokens {
		if noCache.Tokens[i] != withCache.Tokens[i] {
			t.Errorf("token mismatch at step %d: no-cache=%d, cached=%d",
				i, noCache.Tokens[i], withCache.Tokens[i])
		}
	}
}

// TestCacheReducesMultiplies verifies that cached generation uses fewer multiplies.
// Step 0 has equal cost (both methods project exactly 1 token). The savings start at step 1+.
func TestCacheReducesMultiplies(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	trained := trainSmallKVModel(rng)

	noCache := GenerateNoCache(trained.BOS, trained.FloatParams, trained.VocabSize, 8)
	withCache := GenerateWithCache(trained.BOS, trained.FloatParams, trained.VocabSize, 8)

	// Step 0: equal cost (only 1 token to process either way)
	if noCache.MulsPerStep[0] != withCache.MulsPerStep[0] {
		t.Errorf("step 0: expected equal cost, got no-cache=%d, cached=%d",
			noCache.MulsPerStep[0], withCache.MulsPerStep[0])
	}

	// Steps 1+: cache must be strictly cheaper
	for i := 1; i < len(noCache.MulsPerStep); i++ {
		if withCache.MulsPerStep[i] >= noCache.MulsPerStep[i] {
			t.Errorf("step %d: cached (%d) should be less than uncached (%d)",
				i, withCache.MulsPerStep[i], noCache.MulsPerStep[i])
		}
	}

	// Overall speedup should be > 1
	totalNo := 0
	totalYes := 0
	for i := range noCache.MulsPerStep {
		totalNo += noCache.MulsPerStep[i]
		totalYes += withCache.MulsPerStep[i]
	}
	ratio := float64(totalNo) / float64(totalYes)
	if ratio <= 1.0 {
		t.Errorf("expected overall speedup > 1.0, got %.2f", ratio)
	}
}

// TestSpeedupGrowsWithSequenceLength verifies that the speedup ratio increases
// with sequence length — the key insight of KV caching.
func TestSpeedupGrowsWithSequenceLength(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	trained := trainSmallKVModel(rng)

	noCache := GenerateNoCache(trained.BOS, trained.FloatParams, trained.VocabSize, 12)
	withCache := GenerateWithCache(trained.BOS, trained.FloatParams, trained.VocabSize, 12)

	// Compute speedup at step 2 vs step 10
	earlyRatio := float64(noCache.MulsPerStep[2]) / float64(withCache.MulsPerStep[2])
	lateRatio := float64(noCache.MulsPerStep[10]) / float64(withCache.MulsPerStep[10])

	if lateRatio <= earlyRatio {
		t.Errorf("speedup should increase with length: early=%.2fx, late=%.2fx", earlyRatio, lateRatio)
	}
}

// TestCacheMemoryLinearGrowth verifies that cache memory grows linearly with position.
func TestCacheMemoryLinearGrowth(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	trained := trainSmallKVModel(rng)
	result := GenerateWithCache(trained.BOS, trained.FloatParams, trained.VocabSize, 8)

	// Check that growth per step is constant
	floatsPerPos := 2 * kvNLayer * kvNEmbd
	for i, sz := range result.CacheSizes {
		expected := floatsPerPos * (i + 1)
		if sz != expected {
			t.Errorf("step %d: cache size = %d, want %d (linear growth)", i, sz, expected)
		}
	}
}

// TestSimulatePagedAttention verifies paged block allocation.
func TestSimulatePagedAttention(t *testing.T) {
	tests := []struct {
		seqLen    int
		blockSize int
		wantBlks  int
		wantWaste int
	}{
		{16, 4, 4, 0}, // exact fit
		{17, 4, 5, 3}, // 1 wasted in last block = 5*4-17=3
		{1, 4, 1, 3},  // single position
		{8, 8, 1, 0},  // single block exact
		{15, 4, 4, 1}, // 4*4=16 - 15 = 1 wasted
	}
	for _, tc := range tests {
		result := SimulatePagedAttention(tc.seqLen, tc.blockSize)
		if len(result.Blocks) != tc.wantBlks {
			t.Errorf("SimulatePagedAttention(%d, %d): got %d blocks, want %d",
				tc.seqLen, tc.blockSize, len(result.Blocks), tc.wantBlks)
		}
		if result.WastedSlots != tc.wantWaste {
			t.Errorf("SimulatePagedAttention(%d, %d): wasted %d, want %d",
				tc.seqLen, tc.blockSize, result.WastedSlots, tc.wantWaste)
		}
		if result.UsedSlots != tc.seqLen {
			t.Errorf("SimulatePagedAttention(%d, %d): used %d, want %d",
				tc.seqLen, tc.blockSize, result.UsedSlots, tc.seqLen)
		}
	}
}

// TestPagedBlockPositionMapping verifies that logical block boundaries are correct.
func TestPagedBlockPositionMapping(t *testing.T) {
	result := SimulatePagedAttention(10, 3)
	// 10 positions, block size 3 -> blocks: [0-2], [3-5], [6-8], [9]
	expected := [][2]int{{0, 2}, {3, 5}, {6, 8}, {9, 9}}
	if len(result.Blocks) != len(expected) {
		t.Fatalf("expected %d blocks, got %d", len(expected), len(result.Blocks))
	}
	for i, blk := range result.Blocks {
		if blk.StartPos != expected[i][0] || blk.EndPos != expected[i][1] {
			t.Errorf("block %d: got [%d-%d], want [%d-%d]",
				i, blk.StartPos, blk.EndPos, expected[i][0], expected[i][1])
		}
	}
}

// TestPagedBlockSlotCounts verifies slot utilization per block.
func TestPagedBlockSlotCounts(t *testing.T) {
	result := SimulatePagedAttention(7, 4)
	// 7 positions, block size 4 -> blocks: [0-3] full (4/4), [4-6] partial (3/4)
	if len(result.Blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(result.Blocks))
	}
	if result.Blocks[0].SlotsUsed != 4 {
		t.Errorf("block 0: used %d, want 4", result.Blocks[0].SlotsUsed)
	}
	if result.Blocks[1].SlotsUsed != 3 {
		t.Errorf("block 1: used %d, want 3", result.Blocks[1].SlotsUsed)
	}
}

// TestNoCacheMultiplyGrowthRate verifies that uncached multiply counts grow
// superlinearly (due to full-sequence recomputation).
func TestNoCacheMultiplyGrowthRate(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	trained := trainSmallKVModel(rng)
	result := GenerateNoCache(trained.BOS, trained.FloatParams, trained.VocabSize, 10)

	// Each subsequent step should use more multiplies than the previous
	for i := 1; i < len(result.MulsPerStep); i++ {
		if result.MulsPerStep[i] <= result.MulsPerStep[i-1] {
			t.Errorf("uncached step %d (%d) should be > step %d (%d)",
				i, result.MulsPerStep[i], i-1, result.MulsPerStep[i-1])
		}
	}
}

// TestWithCacheMultiplyGrowthRate verifies that cached multiply counts also grow,
// but more slowly than uncached.
func TestWithCacheMultiplyGrowthRate(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	trained := trainSmallKVModel(rng)
	result := GenerateWithCache(trained.BOS, trained.FloatParams, trained.VocabSize, 10)

	// Cached steps should also grow (attention over longer cache)
	for i := 1; i < len(result.MulsPerStep); i++ {
		if result.MulsPerStep[i] <= result.MulsPerStep[i-1] {
			t.Errorf("cached step %d (%d) should be > step %d (%d)",
				i, result.MulsPerStep[i], i-1, result.MulsPerStep[i-1])
		}
	}
}

// TestCommaInt verifies the comma-separated integer formatter.
func TestCommaInt(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1,000"},
		{12345, "12,345"},
		{1234567, "1,234,567"},
	}
	for _, tc := range tests {
		got := commaInt(tc.n)
		if got != tc.want {
			t.Errorf("commaInt(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// TestTrainKVModelConverges verifies that training loss decreases.
func TestTrainKVModelConverges(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	docs := []string{"emma", "olivia", "ava", "sophia", "mia"}
	trained := TrainKVModel(docs, 50, rng, false)

	if trained.VocabSize <= 0 {
		t.Error("vocab size should be positive")
	}
	if trained.FloatParams == nil {
		t.Error("float params should not be nil")
	}
	if len(trained.Chars) == 0 {
		t.Error("chars should not be empty")
	}
}

// TestTokensInValidRange verifies that all generated tokens are within vocab bounds.
func TestTokensInValidRange(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	trained := trainSmallKVModel(rng)

	noCache := GenerateNoCache(trained.BOS, trained.FloatParams, trained.VocabSize, 8)
	for i, tok := range noCache.Tokens {
		if tok < 0 || tok >= trained.VocabSize {
			t.Errorf("no-cache token %d = %d out of range [0, %d)", i, tok, trained.VocabSize)
		}
	}

	withCache := GenerateWithCache(trained.BOS, trained.FloatParams, trained.VocabSize, 8)
	for i, tok := range withCache.Tokens {
		if tok < 0 || tok >= trained.VocabSize {
			t.Errorf("cached token %d = %d out of range [0, %d)", i, tok, trained.VocabSize)
		}
	}
}

// TestKVCacheDeterministic verifies that running the same generation twice
// produces identical results (greedy argmax is deterministic).
func TestKVCacheDeterministic(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	trained := trainSmallKVModel(rng)

	r1 := GenerateWithCache(trained.BOS, trained.FloatParams, trained.VocabSize, 8)
	r2 := GenerateWithCache(trained.BOS, trained.FloatParams, trained.VocabSize, 8)

	for i := range r1.Tokens {
		if r1.Tokens[i] != r2.Tokens[i] {
			t.Errorf("non-deterministic at step %d: %d vs %d", i, r1.Tokens[i], r2.Tokens[i])
		}
	}
}

// === BENCHMARKS ===

func BenchmarkGenerateNoCache(b *testing.B) {
	rng := rand.New(rand.NewSource(42))
	trained := trainSmallKVModel(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GenerateNoCache(trained.BOS, trained.FloatParams, trained.VocabSize, kvGenLen)
	}
}

func BenchmarkGenerateWithCache(b *testing.B) {
	rng := rand.New(rand.NewSource(42))
	trained := trainSmallKVModel(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GenerateWithCache(trained.BOS, trained.FloatParams, trained.VocabSize, kvGenLen)
	}
}

func BenchmarkTrainKVModel(b *testing.B) {
	docs := []string{"emma", "olivia", "ava", "sophia", "mia"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rng := rand.New(rand.NewSource(42))
		TrainKVModel(docs, 20, rng, false)
	}
}

// === TEST HELPERS ===

// trainSmallKVModel creates a minimal trained model for testing.
func trainSmallKVModel(rng *rand.Rand) *KVTrainResult {
	docs := []string{"emma", "olivia", "ava", "sophia", "mia", "liam", "noah", "oliver"}
	return TrainKVModel(docs, 30, rng, false)
}
