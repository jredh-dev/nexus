//go:build research

// Flash Attention computes exact attention identical to standard attention, but processes
// Q, K, V in tiles with an online softmax that never materializes the full N*N score matrix.
//
// Reference: Dao et al., "FlashAttention: Fast and Memory-Efficient Exact Attention
// with IO-Awareness" (2022). https://arxiv.org/abs/2205.14135
// Also: Milakov & Gimelshein, "Online normalizer calculation for softmax" (2018).
//
// Port of mathews-tom/no-magic microflash.py to Go.
// AGPL-3.0 -- see LICENSE in repo root.
package ml

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"strings"
	"time"
)

// === CONSTANTS ===

const (
	// Head dimension (d_k = d_v). Production transformers use d=128; we use 16
	// for instant execution and readable output. The algorithm is identical.
	flashDHead = 16
)

// FlashVerifyConfig holds a (sequence_length, block_size) pair for verification.
type FlashVerifyConfig struct {
	N         int
	BlockSize int
}

// === MATRIX HELPERS ===
// Plain Go matrix operations. No external deps -- explicit loops so every
// memory allocation is visible and countable.

// Mat is a row-major dense matrix: Mat[row][col].
type Mat [][]float64

// RandMatrix creates a random matrix with 1/sqrt(cols) scaling to keep dot
// products O(1). Without scaling, QK^T products grow proportional to d,
// pushing softmax into saturation (near-one-hot). Xavier-like init prevents this.
func RandMatrix(rng *rand.Rand, rows, cols int) Mat {
	s := 1.0 / math.Sqrt(float64(cols))
	m := make(Mat, rows)
	for i := range m {
		m[i] = make([]float64, cols)
		for j := range m[i] {
			m[i][j] = rng.NormFloat64() * s
		}
	}
	return m
}

// Matmul computes A[m,k] @ B[k,n] -> C[m,n].
func Matmul(a, b Mat) Mat {
	m := len(a)
	k := len(a[0])
	n := len(b[0])
	// Transpose B so inner loop accesses contiguous rows (cache-friendly in C;
	// less relevant in Go, but mirrors the pattern Flash Attention exploits on GPU).
	bt := make(Mat, n)
	for j := 0; j < n; j++ {
		bt[j] = make([]float64, k)
		for r := 0; r < k; r++ {
			bt[j][r] = b[r][j]
		}
	}
	c := make(Mat, m)
	for i := 0; i < m; i++ {
		c[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			var dot float64
			for p := 0; p < k; p++ {
				dot += a[i][p] * bt[j][p]
			}
			c[i][j] = dot
		}
	}
	return c
}

// Transpose returns M^T.
func Transpose(m Mat) Mat {
	rows := len(m)
	cols := len(m[0])
	t := make(Mat, cols)
	for c := 0; c < cols; c++ {
		t[c] = make([]float64, rows)
		for r := 0; r < rows; r++ {
			t[c][r] = m[r][c]
		}
	}
	return t
}

// SoftmaxRows applies row-wise softmax with numerical stability (subtract row max).
//
// softmax(x_i) = exp(x_i - max(x)) / sum_j(exp(x_j - max(x)))
// Subtracting max(x) prevents exp() overflow while preserving the distribution.
// This is the "two-pass" softmax: pass 1 finds max, pass 2 computes exp and sum.
func SoftmaxRows(m Mat) Mat {
	out := make(Mat, len(m))
	for i, row := range m {
		mx := row[0]
		for _, v := range row[1:] {
			if v > mx {
				mx = v
			}
		}
		exps := make([]float64, len(row))
		var s float64
		for j, v := range row {
			e := math.Exp(v - mx)
			exps[j] = e
			s += e
		}
		out[i] = make([]float64, len(row))
		for j, e := range exps {
			out[i][j] = e / s
		}
	}
	return out
}

// MaxAbsDiff returns the element-wise maximum absolute difference between two matrices.
func MaxAbsDiff(a, b Mat) float64 {
	var maxDiff float64
	for i := range a {
		for j := range a[i] {
			d := math.Abs(a[i][j] - b[i][j])
			if d > maxDiff {
				maxDiff = d
			}
		}
	}
	return maxDiff
}

// === STANDARD ATTENTION ===
// The textbook formulation that Flash Attention replaces. Computing this requires
// materializing the full N*N score matrix in memory -- the bottleneck.

// AttentionResult holds the output matrix and peak memory usage.
type AttentionResult struct {
	Output     Mat
	PeakMemory int // peak floats allocated for the score matrix
}

// StandardAttention computes attention by materializing the full N*N score matrix.
//
//	S = Q @ K^T / sqrt(d)    -- score matrix [N, N]
//	P = softmax(S, axis=-1)  -- attention weights [N, N], rows sum to 1
//	O = P @ V                -- output [N, d]
//
// Peak memory: N*N floats for S (or P -- same shape, can overwrite S in-place).
// This O(N^2) memory is the reason standard attention breaks on long sequences.
// At N=128K with float16, the score matrix alone is 32 GB.
func StandardAttention(q, k, v Mat) AttentionResult {
	n := len(q)
	d := len(q[0])
	scale := 1.0 / math.Sqrt(float64(d))

	// S = Q @ K^T -- the N*N matrix we want to avoid materializing
	scores := Matmul(q, Transpose(k))

	// Scale before softmax
	for i := range scores {
		for j := range scores[i] {
			scores[i][j] *= scale
		}
	}

	// P = softmax(S) -- still N*N
	weights := SoftmaxRows(scores)

	// O = P @ V -- back to [N, d]
	output := Matmul(weights, v)

	return AttentionResult{Output: output, PeakMemory: n * n}
}

// === FLASH ATTENTION ===
//
// The key insight from Dao et al.: attention can be computed in tiles without ever
// storing the full N*N score matrix. The trick is an "online softmax" that maintains
// running statistics (max and denominator sum) across tiles.
//
// Why tiling matters on GPU (but not in this simulation):
//   GPU memory has two levels: HBM (large, slow) and SRAM (small, fast).
//   Standard attention reads Q,K from HBM, writes N*N scores to HBM, reads them
//   back for softmax, writes weights to HBM, reads them back for P@V.
//   Flash Attention loads tiles of Q,K,V into SRAM, computes attention within SRAM,
//   and writes only the final output to HBM. Total HBM reads drop from O(N^2) to O(N).
//
// This simulation shows the ALGORITHM (tiling + online softmax) that makes this possible.
// It does not show the SPEEDUP, which comes from the GPU memory hierarchy.
//
// === ONLINE SOFTMAX: THE CORE INSIGHT ===
//
// Standard softmax needs ALL scores to compute the denominator:
//   softmax(x_i) = exp(x_i - max(x)) / sum_j(exp(x_j - max(x)))
//
// Online softmax processes scores in blocks, maintaining running statistics:
//   m = running maximum (for numerical stability)
//   l = running sum of exp(score - m) (the softmax denominator)
//
// When a new block arrives with local max m_new:
//   1. m_combined = max(m_old, m_new)
//   2. Rescale old sum:    l_old' = l_old * exp(m_old - m_combined)
//   3. Compute new block:  l_new  = sum(exp(scores - m_combined))
//   4. l_combined = l_old' + l_new
//   5. Rescale old output:  O' = O * (l_old / l_combined) * exp(m_old - m_combined)
//   6. Add new contribution: O' += (1/l_combined) * exp(scores - m_combined) @ V_block
//
// After processing all blocks, O holds the exact same result as standard attention.

// FlashAttention computes exact attention WITHOUT materializing the N*N matrix.
//
// Process Q, K, V in tiles of size blockSize. For each query block, iterate over
// all key/value blocks, accumulating the output using online softmax to maintain
// correct normalization without storing all scores.
//
// Peak memory: blockSize * blockSize floats (one tile of scores at a time).
// Compare to N*N for standard attention.
func FlashAttention(q, k, v Mat, blockSize int) AttentionResult {
	n := len(q)
	d := len(q[0])
	scale := 1.0 / math.Sqrt(float64(d))

	// Per-query running statistics. Each query row gets its own max and sum because
	// softmax is applied independently per row (each query attends separately).
	output := make(Mat, n)
	for i := range output {
		output[i] = make([]float64, d)
	}
	rowMax := make([]float64, n) // m_i: running max of scores for query i
	rowSum := make([]float64, n) // l_i: running sum of exp(score - m_i) for query i
	for i := range rowMax {
		rowMax[i] = math.Inf(-1)
	}

	peakMemory := 0

	// Outer loop: iterate over blocks of queries
	for qStart := 0; qStart < n; qStart += blockSize {
		qEnd := qStart + blockSize
		if qEnd > n {
			qEnd = n
		}
		qBlock := q[qStart:qEnd]
		bq := qEnd - qStart

		// Inner loop: for each query block, sweep over ALL key/value blocks.
		// This is the "tiling" -- only one bq * bk tile of scores exists at a time.
		for kStart := 0; kStart < n; kStart += blockSize {
			kEnd := kStart + blockSize
			if kEnd > n {
				kEnd = n
			}
			kBlock := k[kStart:kEnd]
			vBlock := v[kStart:kEnd]
			bk := kEnd - kStart

			// Track simulated memory: the score tile is the largest temporary
			if bq*bk > peakMemory {
				peakMemory = bq * bk
			}

			// Step 1: Compute partial scores S_ij = Q_block @ K_block^T / sqrt(d)
			// This is a bq * bk matrix -- NOT N*N.
			scoresTile := make(Mat, bq)
			for qi := 0; qi < bq; qi++ {
				scoresTile[qi] = make([]float64, bk)
				for ki := 0; ki < bk; ki++ {
					var dot float64
					for c := 0; c < d; c++ {
						dot += qBlock[qi][c] * kBlock[ki][c]
					}
					scoresTile[qi][ki] = dot * scale
				}
			}

			// Step 2: For each query row in this block, apply the online softmax update
			for qi := 0; qi < bq; qi++ {
				gi := qStart + qi // global index into the full output

				// Local max for this tile row
				mTile := scoresTile[qi][0]
				for _, s := range scoresTile[qi][1:] {
					if s > mTile {
						mTile = s
					}
				}

				// Combined max: max of running max and this tile's max
				mOld := rowMax[gi]
				mNew := math.Max(mOld, mTile)

				// Rescale factor for old accumulator
				var oldScale float64
				if math.IsInf(mOld, -1) {
					oldScale = 0.0
				} else {
					oldScale = math.Exp(mOld - mNew)
				}

				// Compute exp(score - m_new) for each score in this tile row
				expScores := make([]float64, bk)
				var newSum float64
				for ki := 0; ki < bk; ki++ {
					e := math.Exp(scoresTile[qi][ki] - mNew)
					expScores[ki] = e
					newSum += e
				}

				// Update running denominator
				lOld := rowSum[gi]
				lNew := lOld*oldScale + newSum

				// Update output accumulator
				if lNew > 0 {
					// Rescale previous accumulator
					rescale := (lOld * oldScale) / lNew
					for c := 0; c < d; c++ {
						output[gi][c] *= rescale
					}

					// Add new contribution: (1/l_new) * sum_ki(exp_scores[ki] * V[ki, c])
					invL := 1.0 / lNew
					for ki := 0; ki < bk; ki++ {
						w := expScores[ki] * invL
						for c := 0; c < d; c++ {
							output[gi][c] += w * vBlock[ki][c]
						}
					}
				}

				rowMax[gi] = mNew
				rowSum[gi] = lNew
			}
		}
	}

	// No final normalization needed -- output is already correctly normalized at each
	// step because we divide by l (the running denominator) incrementally.
	return AttentionResult{Output: output, PeakMemory: peakMemory}
}

// === VERIFICATION ===

// FlashVerifyResult holds the result of comparing standard vs flash attention.
type FlashVerifyResult struct {
	Passed      bool
	MaxDiff     float64
	StdMemory   int
	FlashMemory int
}

// VerifyFlash runs standard and flash attention on identical inputs and checks outputs match.
func VerifyFlash(rng *rand.Rand, n, d, blockSize int, tolerance float64) FlashVerifyResult {
	q := RandMatrix(rng, n, d)
	k := RandMatrix(rng, n, d)
	v := RandMatrix(rng, n, d)

	std := StandardAttention(q, k, v)
	flash := FlashAttention(q, k, v, blockSize)

	diff := MaxAbsDiff(std.Output, flash.Output)
	return FlashVerifyResult{
		Passed:      diff < tolerance,
		MaxDiff:     diff,
		StdMemory:   std.PeakMemory,
		FlashMemory: flash.PeakMemory,
	}
}

// === DEMO ===

// RunMicroflash demonstrates Flash Attention by verifying correctness against
// standard attention across multiple configurations and printing memory analysis.
func RunMicroflash() {
	fmt.Println("=== Flash Attention: Algorithmic Simulation ===")
	fmt.Println()
	fmt.Println("Signpost: This is an algorithmic simulation, not a performance benchmark.")
	fmt.Println("Pure Go is slower than standard attention here. The point is showing WHAT")
	fmt.Println("Flash Attention does (tiled computation, online softmax), not achieving speedup.")
	fmt.Println("On GPU, the speedup comes from keeping tiles in SRAM (fast, small) instead of")
	fmt.Println("reading/writing the N*N matrix from HBM (large, slow).")
	fmt.Println()

	rng := rand.New(rand.NewPCG(42, 0))

	configs := []FlashVerifyConfig{
		{32, 8},
		{64, 8},
		{64, 16},
		{48, 12}, // non-power-of-2 to test remainder handling
		{37, 8},  // N not divisible by block_size -- the general case
	}

	// --- Verification ---
	fmt.Println("--- Verification ---")
	allPassed := true

	for _, cfg := range configs {
		fmt.Printf("\nConfig: N=%d, d=%d, block_size=%d\n", cfg.N, flashDHead, cfg.BlockSize)
		t0 := time.Now()
		result := VerifyFlash(rng, cfg.N, flashDHead, cfg.BlockSize, 1e-6)
		elapsed := time.Since(t0)

		fmt.Printf("  Standard attention: computed (peak memory: %s floats)\n", formatInt(result.StdMemory))
		fmt.Printf("  Flash attention:    computed (peak memory: %s floats)\n", formatInt(result.FlashMemory))
		fmt.Printf("  Max element difference: %.2e\n", result.MaxDiff)
		fmt.Printf("  Time: %.1f ms\n", float64(elapsed.Milliseconds()))

		if result.Passed {
			fmt.Println("  PASS: outputs match within 1e-6 tolerance")
		} else {
			fmt.Println("  FAIL: outputs diverge beyond 1e-6 tolerance")
			allPassed = false
		}
	}

	if allPassed {
		fmt.Println("\nOverall: all configurations passed")
	} else {
		fmt.Println("\nOverall: SOME CONFIGURATIONS FAILED")
		os.Exit(1)
	}

	// --- Memory Comparison ---
	fmt.Println("\n--- Memory Comparison ---")
	fmt.Println("Peak floats allocated for the score matrix (standard) vs one tile (flash):")
	fmt.Println()

	seqLens := []int{16, 32, 64, 128, 256}
	blockSizes := []int{4, 8, 16}
	printMemoryTable(seqLens, blockSizes)

	fmt.Println("\nStandard attention memory grows as O(N^2) -- doubling N quadruples memory.")
	fmt.Println("Flash attention memory is O(B^2), independent of sequence length N.")
	fmt.Println("At N=128K with B=128, standard needs 16 billion floats; flash needs 16,384.")

	// --- Block Size Effect ---
	fmt.Printf("\n--- Block Size Effect ---\n")
	fmt.Printf("For N=%d, d=%d:\n\n", 64, flashDHead)
	printBlockEffectTable(64, []int{4, 8, 16, 32})

	fmt.Println("\nSmaller blocks use less memory but require more tiles (iterations).")
	fmt.Println("On GPU, the optimal block size fills SRAM: A100 has 192KB SRAM,")
	fmt.Println("fitting B~128 for d=128 in float16. Pure Go has no SRAM,")
	fmt.Println("so block size affects only iteration count here.")
}

// === FORMATTING HELPERS ===

func formatInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, ",")
}

func printMemoryTable(seqLens, blockSizes []int) {
	header := fmt.Sprintf("%14s   %18s", "Seq Length (N)", "Standard (floats)")
	for _, b := range blockSizes {
		header += fmt.Sprintf("   %12s", fmt.Sprintf("Flash B=%d", b))
	}
	fmt.Println(header)

	sep := strings.Repeat("\u2500", 14) + "   " + strings.Repeat("\u2500", 18)
	for range blockSizes {
		sep += "   " + strings.Repeat("\u2500", 12)
	}
	fmt.Println(sep)

	for _, n := range seqLens {
		stdMem := n * n
		row := fmt.Sprintf("%14d   %18s", n, formatInt(stdMem))
		for _, b := range blockSizes {
			flashMem := b * b
			row += fmt.Sprintf("   %12s", formatInt(flashMem))
		}
		fmt.Println(row)
	}
}

func printBlockEffectTable(n int, blockSizes []int) {
	fmt.Printf("%10s   %15s   %9s\n", "Block Size", "Memory (floats)", "Num Tiles")
	sep := strings.Repeat("\u2500", 10) + "   " + strings.Repeat("\u2500", 15) + "   " + strings.Repeat("\u2500", 9)
	fmt.Println(sep)

	for _, b := range blockSizes {
		mem := b * b
		numQ := (n + b - 1) / b
		numK := (n + b - 1) / b
		numTiles := numQ * numK
		fmt.Printf("%10d   %15s   %9d\n", b, formatInt(mem), numTiles)
	}
}
