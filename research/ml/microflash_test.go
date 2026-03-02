//go:build research

package ml

import (
	"math"
	"math/rand/v2"
	"testing"
)

// === MATRIX HELPER TESTS ===

func TestMatmul(t *testing.T) {
	// 2x3 @ 3x2 = 2x2
	a := Mat{{1, 2, 3}, {4, 5, 6}}
	b := Mat{{7, 8}, {9, 10}, {11, 12}}
	got := Matmul(a, b)

	// Row 0: [1*7+2*9+3*11, 1*8+2*10+3*12] = [58, 64]
	// Row 1: [4*7+5*9+6*11, 4*8+5*10+6*12] = [139, 154]
	want := Mat{{58, 64}, {139, 154}}

	if len(got) != 2 || len(got[0]) != 2 {
		t.Fatalf("wrong shape: got %dx%d", len(got), len(got[0]))
	}
	for i := range want {
		for j := range want[i] {
			if math.Abs(got[i][j]-want[i][j]) > 1e-10 {
				t.Errorf("got[%d][%d] = %f, want %f", i, j, got[i][j], want[i][j])
			}
		}
	}
}

func TestTranspose(t *testing.T) {
	m := Mat{{1, 2, 3}, {4, 5, 6}}
	got := Transpose(m)
	want := Mat{{1, 4}, {2, 5}, {3, 6}}

	if len(got) != 3 || len(got[0]) != 2 {
		t.Fatalf("wrong shape: got %dx%d", len(got), len(got[0]))
	}
	for i := range want {
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Errorf("got[%d][%d] = %f, want %f", i, j, got[i][j], want[i][j])
			}
		}
	}
}

func TestSoftmaxRows(t *testing.T) {
	m := Mat{{1, 2, 3}, {0, 0, 0}}
	got := SoftmaxRows(m)

	// Row 0: softmax([1,2,3]) = [0.0900, 0.2447, 0.6652]
	// Row 1: softmax([0,0,0]) = [1/3, 1/3, 1/3]
	for i, row := range got {
		var sum float64
		for _, v := range row {
			sum += v
		}
		if math.Abs(sum-1.0) > 1e-10 {
			t.Errorf("row %d sum = %f, want 1.0", i, sum)
		}
	}

	// Uniform input -> uniform output
	for j := range got[1] {
		if math.Abs(got[1][j]-1.0/3.0) > 1e-10 {
			t.Errorf("uniform row: got[1][%d] = %f, want %f", j, got[1][j], 1.0/3.0)
		}
	}

	// Monotonicity: larger input -> larger output
	if got[0][0] >= got[0][1] || got[0][1] >= got[0][2] {
		t.Errorf("softmax not monotonic: %v", got[0])
	}
}

// === ATTENTION TESTS ===

func TestStandardAttentionIdentity(t *testing.T) {
	// When Q=K, attention weights should be roughly uniform (all queries equally
	// similar to all keys), producing output close to mean(V).
	rng := rand.New(rand.NewPCG(99, 0))
	n, d := 8, 4
	q := RandMatrix(rng, n, d)
	v := RandMatrix(rng, n, d)

	result := StandardAttention(q, q, v) // Q=K

	if result.PeakMemory != n*n {
		t.Errorf("peak memory: got %d, want %d", result.PeakMemory, n*n)
	}
	if len(result.Output) != n || len(result.Output[0]) != d {
		t.Fatalf("output shape: got %dx%d, want %dx%d", len(result.Output), len(result.Output[0]), n, d)
	}
}

func TestFlashMatchesStandard(t *testing.T) {
	// Core correctness: flash attention must produce the same output as standard.
	configs := []FlashVerifyConfig{
		{32, 8},
		{64, 8},
		{64, 16},
		{48, 12},
		{37, 8}, // N not divisible by block_size
	}

	rng := rand.New(rand.NewPCG(42, 0))

	for _, cfg := range configs {
		t.Run(
			"N="+itoa(cfg.N)+"_B="+itoa(cfg.BlockSize),
			func(t *testing.T) {
				result := VerifyFlash(rng, cfg.N, flashDHead, cfg.BlockSize, 1e-6)
				if !result.Passed {
					t.Errorf("flash != standard: max diff = %.2e", result.MaxDiff)
				}
				if result.FlashMemory != cfg.BlockSize*cfg.BlockSize {
					t.Errorf("flash peak memory: got %d, want %d",
						result.FlashMemory, cfg.BlockSize*cfg.BlockSize)
				}
				if result.StdMemory != cfg.N*cfg.N {
					t.Errorf("standard peak memory: got %d, want %d",
						result.StdMemory, cfg.N*cfg.N)
				}
			},
		)
	}
}

func TestFlashBlockSizeOne(t *testing.T) {
	// Edge case: block_size=1 means every tile is 1x1 -- maximum tiling granularity.
	rng := rand.New(rand.NewPCG(7, 0))
	result := VerifyFlash(rng, 8, 4, 1, 1e-6)
	if !result.Passed {
		t.Errorf("block_size=1 failed: max diff = %.2e", result.MaxDiff)
	}
}

func TestFlashBlockSizeEqualsN(t *testing.T) {
	// Edge case: block_size=N means one tile covers everything -- should degenerate
	// to standard attention behavior.
	rng := rand.New(rand.NewPCG(13, 0))
	result := VerifyFlash(rng, 16, 8, 16, 1e-6)
	if !result.Passed {
		t.Errorf("block_size=N failed: max diff = %.2e", result.MaxDiff)
	}
	// Peak memory should equal N*N when block covers all
	if result.FlashMemory != 16*16 {
		t.Errorf("flash peak memory with B=N: got %d, want %d", result.FlashMemory, 16*16)
	}
}

func TestFlashMemoryBound(t *testing.T) {
	// Flash attention peak memory must be <= blockSize^2, regardless of N.
	rng := rand.New(rand.NewPCG(42, 0))
	n, d, bs := 100, 8, 10
	q := RandMatrix(rng, n, d)
	k := RandMatrix(rng, n, d)
	v := RandMatrix(rng, n, d)

	result := FlashAttention(q, k, v, bs)
	if result.PeakMemory > bs*bs {
		t.Errorf("flash peak memory %d > block_size^2 %d", result.PeakMemory, bs*bs)
	}
}

// === BENCHMARKS ===

func BenchmarkStandardAttention(b *testing.B) {
	rng := rand.New(rand.NewPCG(42, 0))
	q := RandMatrix(rng, 64, 16)
	k := RandMatrix(rng, 64, 16)
	v := RandMatrix(rng, 64, 16)
	b.ResetTimer()
	for b.Loop() {
		StandardAttention(q, k, v)
	}
}

func BenchmarkFlashAttention_B8(b *testing.B) {
	rng := rand.New(rand.NewPCG(42, 0))
	q := RandMatrix(rng, 64, 16)
	k := RandMatrix(rng, 64, 16)
	v := RandMatrix(rng, 64, 16)
	b.ResetTimer()
	for b.Loop() {
		FlashAttention(q, k, v, 8)
	}
}

func BenchmarkFlashAttention_B16(b *testing.B) {
	rng := rand.New(rand.NewPCG(42, 0))
	q := RandMatrix(rng, 64, 16)
	k := RandMatrix(rng, 64, 16)
	v := RandMatrix(rng, 64, 16)
	b.ResetTimer()
	for b.Loop() {
		FlashAttention(q, k, v, 16)
	}
}

func BenchmarkMatmul(b *testing.B) {
	rng := rand.New(rand.NewPCG(42, 0))
	a := RandMatrix(rng, 64, 64)
	m := RandMatrix(rng, 64, 64)
	b.ResetTimer()
	for b.Loop() {
		Matmul(a, m)
	}
}

// itoa avoids importing strconv for a test file.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
