//go:build research

// Package ml provides educational ML algorithm implementations.
//
// microkv.go — KV Cache: Why autoregressive generation recomputes redundant work at every
// step, and how the KV cache eliminates that redundancy by memoizing key/value projections
// across the sequence.
//
// Reference: Pope et al., "Efficiently Scaling Transformer Inference" (2022) for KV cache
// analysis. Kwon et al., "Efficient Memory Management for Large Language Model Serving
// with PagedAttention" (2023) for paged allocation. Architecture follows the microgpt
// pattern (Radford et al., 2019) with pedagogical simplifications.
package ml

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"
)

// === CONSTANTS AND HYPERPARAMETERS ===

// KV cache demonstration constants. Model architecture is intentionally tiny — just
// enough to produce non-random outputs so the cached vs uncached comparison is meaningful.
const (
	kvNEmbd    = 16
	kvNHead    = 2
	kvNLayer   = 1
	kvBlockSz  = 32
	kvHeadDim  = kvNEmbd / kvNHead // 8
	kvLR       = 0.01
	kvBeta1    = 0.85
	kvBeta2    = 0.99
	kvEpsAdam  = 1e-8
	kvNumSteps = 300
	kvGenLen   = 16 // characters to generate for the comparison
	kvPageBlk  = 4  // positions per block in paged attention simulation
)

// Signpost: production KV caches store thousands of positions across dozens of layers
// with 128-dimensional heads. Our toy dimensions (1 layer, 8-dim heads) preserve the
// algorithmic structure while keeping runtime under a minute.

// === MULTIPLY-COUNTING HELPERS ===
// These mirror LinearFloat/SoftmaxFloat/RMSNormFloat but count every scalar multiply
// so we can compare computational cost between cached and uncached inference.

// linearCounting computes y = W @ x on plain floats and counts every scalar multiply.
func linearCounting(x []float64, w [][]float64, counter *int) []float64 {
	*counter += len(w) * len(x)
	out := make([]float64, len(w))
	for i, row := range w {
		s := 0.0
		for j, xj := range x {
			s += row[j] * xj
		}
		out[i] = s
	}
	return out
}

// softmaxCounting computes stable softmax on plain floats (no multiply counting — only exp/add).
func softmaxCounting(logits []float64) []float64 {
	mx := logits[0]
	for _, v := range logits[1:] {
		if v > mx {
			mx = v
		}
	}
	exps := make([]float64, len(logits))
	sum := 0.0
	for i, v := range logits {
		e := math.Exp(v - mx)
		exps[i] = e
		sum += e
	}
	for i := range exps {
		exps[i] /= sum
	}
	return exps
}

// rmsnormCounting applies RMSNorm on plain floats (no multiply counting — normalizations are cheap).
func rmsnormCounting(x []float64) []float64 {
	meanSq := 0.0
	for _, xi := range x {
		meanSq += xi * xi
	}
	meanSq /= float64(len(x))
	scale := 1.0 / math.Sqrt(meanSq+1e-5)
	out := make([]float64, len(x))
	for i, xi := range x {
		out[i] = xi * scale
	}
	return out
}

// === KV CACHE FLOAT PARAMS ===
// We use a local param struct with fixed architecture dimensions to match the Python
// original's direct indexing style. This avoids coupling to GPTConfig/FloatGPTParams
// from microquant.go, which have different field layouts and conventions.

// KVFloatParams holds extracted float weights for the KV cache demonstration model.
type KVFloatParams struct {
	Wte    [][]float64 // [vocabSize][kvNEmbd]
	Wpe    [][]float64 // [kvBlockSz][kvNEmbd]
	Layers []KVLayerParams
	LMHead [][]float64 // [vocabSize][kvNEmbd]
}

// KVLayerParams holds per-layer float weights.
type KVLayerParams struct {
	WQ, WK, WV, WO [][]float64 // [kvNEmbd][kvNEmbd]
	FC1            [][]float64 // [4*kvNEmbd][kvNEmbd]
	FC2            [][]float64 // [kvNEmbd][4*kvNEmbd]
}

// KVGenResult holds the output of a generation run.
type KVGenResult struct {
	Tokens      []int
	MulsPerStep []int
	CacheSizes  []int // only populated for cached generation
}

// === INFERENCE WITHOUT KV CACHE ===
// At each generation step, recompute Q/K/V projections for ALL positions from scratch.
// This is how attention would work if we treated every step as independent: feed the
// entire sequence, attend over everything, discard intermediate state, repeat.
// Total work for T steps: sum(t * C_proj + t^2 * C_attn for t in 1..T) ~ O(T^3)

// GenerateNoCache generates tokens WITHOUT KV cache. At each step, every position's
// K and V projections are recomputed from scratch — the redundant work that caching eliminates.
func GenerateNoCache(promptTok int, wf *KVFloatParams, vocabSize, genLen int) *KVGenResult {
	result := &KVGenResult{
		Tokens:      make([]int, 0, genLen),
		MulsPerStep: make([]int, 0, genLen),
	}

	tokens := []int{promptTok}

	for step := 0; step < genLen; step++ {
		counter := 0
		seqLen := len(tokens)

		// Embed all positions
		embeddings := make([][]float64, seqLen)
		for pos := 0; pos < seqLen; pos++ {
			x := make([]float64, kvNEmbd)
			for j := 0; j < kvNEmbd; j++ {
				x[j] = wf.Wte[tokens[pos]][j] + wf.Wpe[pos][j]
			}
			embeddings[pos] = rmsnormCounting(x)
		}

		// Transformer layers — recompute Q, K, V for EVERY position
		hiddens := make([][]float64, seqLen)
		for i, emb := range embeddings {
			hiddens[i] = make([]float64, len(emb))
			copy(hiddens[i], emb)
		}

		for li := 0; li < kvNLayer; li++ {
			layer := &wf.Layers[li]
			residuals := make([][]float64, seqLen)
			for i := range hiddens {
				residuals[i] = make([]float64, len(hiddens[i]))
				copy(residuals[i], hiddens[i])
			}
			normed := make([][]float64, seqLen)
			for i := range hiddens {
				normed[i] = rmsnormCounting(hiddens[i])
			}

			// Project all positions to Q, K, V — this is the redundant work.
			// Positions 0..(t-1) were already projected on previous steps.
			allQ := make([][]float64, seqLen)
			allK := make([][]float64, seqLen)
			allV := make([][]float64, seqLen)
			for p := 0; p < seqLen; p++ {
				allQ[p] = linearCounting(normed[p], layer.WQ, &counter)
				allK[p] = linearCounting(normed[p], layer.WK, &counter)
				allV[p] = linearCounting(normed[p], layer.WV, &counter)
			}

			// Causal multi-head attention over the full sequence
			attnOut := make([][]float64, seqLen)
			for pos := 0; pos < seqLen; pos++ {
				headCat := make([]float64, 0, kvNEmbd)
				for h := 0; h < kvNHead; h++ {
					hs := h * kvHeadDim
					qH := allQ[pos][hs : hs+kvHeadDim]
					// Causal: only attend to positions 0..pos
					scores := make([]float64, pos+1)
					for t := 0; t <= pos; t++ {
						dot := 0.0
						for j := 0; j < kvHeadDim; j++ {
							dot += qH[j] * allK[t][hs+j]
						}
						counter += kvHeadDim
						scores[t] = dot / math.Sqrt(float64(kvHeadDim))
					}
					weights := softmaxCounting(scores)
					headOut := make([]float64, kvHeadDim)
					for j := 0; j < kvHeadDim; j++ {
						val := 0.0
						for t := 0; t <= pos; t++ {
							val += weights[t] * allV[t][hs+j]
							counter++
						}
						headOut[j] = val
					}
					headCat = append(headCat, headOut...)
				}
				attnOut[pos] = headCat
			}

			// Output projection + residual
			for pos := 0; pos < seqLen; pos++ {
				projected := linearCounting(attnOut[pos], layer.WO, &counter)
				for j := range projected {
					hiddens[pos][j] = projected[j] + residuals[pos][j]
				}
			}

			// MLP + residual
			for pos := 0; pos < seqLen; pos++ {
				res2 := make([]float64, len(hiddens[pos]))
				copy(res2, hiddens[pos])
				h := rmsnormCounting(hiddens[pos])
				h = linearCounting(h, layer.FC1, &counter)
				for i := range h {
					if h[i] < 0 {
						h[i] = 0 // ReLU
					}
				}
				h = linearCounting(h, layer.FC2, &counter)
				for i := range h {
					hiddens[pos][i] = h[i] + res2[i]
				}
			}
		}

		// Logits from last position only
		logits := linearCounting(hiddens[seqLen-1], wf.LMHead, &counter)
		probs := softmaxCounting(logits)
		nextTok := 0
		maxP := probs[0]
		for i := 1; i < vocabSize; i++ {
			if probs[i] > maxP {
				maxP = probs[i]
				nextTok = i
			}
		}
		tokens = append(tokens, nextTok)
		result.Tokens = append(result.Tokens, nextTok)
		result.MulsPerStep = append(result.MulsPerStep, counter)
	}

	return result
}

// === INFERENCE WITH KV CACHE ===
// At each step, compute Q/K/V for ONLY the new token. Append K and V to the cache.
// Attention: Q_new attends to all cached K (0..t), V (0..t).
// Work per step: C_proj + t * C_attn ~ O(t). Total for T steps: O(T^2) — one order better.
// The insight: K and V projections for past tokens never change in autoregressive decoding.
// Recomputing them is pure waste — the KV cache is memoization of linear projections.

// GenerateWithCache generates tokens WITH KV cache. Only the new token is projected at
// each step; cached K/V from previous positions are reused without recomputation.
func GenerateWithCache(promptTok int, wf *KVFloatParams, vocabSize, genLen int) *KVGenResult {
	result := &KVGenResult{
		Tokens:      make([]int, 0, genLen),
		MulsPerStep: make([]int, 0, genLen),
		CacheSizes:  make([]int, 0, genLen),
	}

	// KV cache: stores projected K and V vectors for each layer and position.
	// Shape: kvCache[layer][position] = vector of length kvNEmbd
	kvCacheK := make([][]float64Slice, kvNLayer)
	kvCacheV := make([][]float64Slice, kvNLayer)
	for li := range kvCacheK {
		kvCacheK[li] = make([]float64Slice, 0)
		kvCacheV[li] = make([]float64Slice, 0)
	}

	currentTok := promptTok
	for step := 0; step < genLen; step++ {
		counter := 0
		pos := step

		// Embed only the NEW token — previous embeddings don't need recomputation
		x := make([]float64, kvNEmbd)
		for j := 0; j < kvNEmbd; j++ {
			x[j] = wf.Wte[currentTok][j] + wf.Wpe[pos][j]
		}
		x = rmsnormCounting(x)

		for li := 0; li < kvNLayer; li++ {
			layer := &wf.Layers[li]
			xRes := make([]float64, len(x))
			copy(xRes, x)

			x = rmsnormCounting(x)

			// Project ONLY the new token — this is where the cache saves work.
			// Without cache: project all t tokens. With cache: project 1 token.
			q := linearCounting(x, layer.WQ, &counter)
			k := linearCounting(x, layer.WK, &counter)
			v := linearCounting(x, layer.WV, &counter)

			// Append new K, V to cache (the cache grows by one entry per step)
			kvCacheK[li] = append(kvCacheK[li], k)
			kvCacheV[li] = append(kvCacheV[li], v)

			// Attention: Q from new token attends to ALL cached K/V
			headCat := make([]float64, 0, kvNEmbd)
			cachedLen := len(kvCacheK[li])
			for h := 0; h < kvNHead; h++ {
				hs := h * kvHeadDim
				qH := q[hs : hs+kvHeadDim]
				scores := make([]float64, cachedLen)
				for t := 0; t < cachedLen; t++ {
					dot := 0.0
					for j := 0; j < kvHeadDim; j++ {
						dot += qH[j] * kvCacheK[li][t][hs+j]
					}
					counter += kvHeadDim
					scores[t] = dot / math.Sqrt(float64(kvHeadDim))
				}
				weights := softmaxCounting(scores)
				headOut := make([]float64, kvHeadDim)
				for j := 0; j < kvHeadDim; j++ {
					val := 0.0
					for t := 0; t < cachedLen; t++ {
						val += weights[t] * kvCacheV[li][t][hs+j]
						counter++
					}
					headOut[j] = val
				}
				headCat = append(headCat, headOut...)
			}

			x = linearCounting(headCat, layer.WO, &counter)
			for i := range x {
				x[i] += xRes[i]
			}
			xRes = make([]float64, len(x))
			copy(xRes, x)

			x = rmsnormCounting(x)
			x = linearCounting(x, layer.FC1, &counter)
			for i := range x {
				if x[i] < 0 {
					x[i] = 0 // ReLU
				}
			}
			x = linearCounting(x, layer.FC2, &counter)
			for i := range x {
				x[i] += xRes[i]
			}
		}

		logits := linearCounting(x, wf.LMHead, &counter)
		probs := softmaxCounting(logits)
		nextTok := 0
		maxP := probs[0]
		for i := 1; i < vocabSize; i++ {
			if probs[i] > maxP {
				maxP = probs[i]
				nextTok = i
			}
		}
		result.Tokens = append(result.Tokens, nextTok)
		currentTok = nextTok
		result.MulsPerStep = append(result.MulsPerStep, counter)

		// Cache memory: 2 (K+V) * nLayer * nEmbd floats per cached position
		totalCachedFloats := 2 * kvNLayer * kvNEmbd * len(kvCacheK[0])
		result.CacheSizes = append(result.CacheSizes, totalCachedFloats)
	}

	return result
}

// === PAGED ATTENTION SIMULATION ===
// Production systems (vLLM) can't pre-allocate contiguous memory for every sequence's
// KV cache because sequence lengths are unknown and variable. Paged attention borrows
// the OS virtual memory idea: allocate fixed-size blocks on demand, map logical positions
// to physical blocks through a page table. This eliminates fragmentation from over-
// allocation and enables sharing physical blocks across sequences (e.g., shared prefixes).

// PagedBlock represents a physical memory block in the paged attention simulation.
type PagedBlock struct {
	LogicalIdx  int
	PhysicalIdx int
	StartPos    int
	EndPos      int
	SlotsUsed   int
	SlotsTotal  int
}

// PagedResult holds the output of a paged attention simulation.
type PagedResult struct {
	Blocks      []PagedBlock
	BlockSize   int
	SeqLen      int
	TotalSlots  int
	UsedSlots   int
	WastedSlots int
}

// SimulatePagedAttention demonstrates how paged attention allocates and maps cache blocks.
// It returns the allocation result for inspection; the demo function prints the trace.
func SimulatePagedAttention(seqLen, blockSize int) *PagedResult {
	numBlocks := (seqLen + blockSize - 1) / blockSize
	blocks := make([]PagedBlock, numBlocks)
	for i := 0; i < numBlocks; i++ {
		start := i * blockSize
		end := start + blockSize - 1
		if end >= seqLen {
			end = seqLen - 1
		}
		used := end - start + 1
		blocks[i] = PagedBlock{
			LogicalIdx:  i,
			PhysicalIdx: i, // 1:1 mapping for single-sequence simulation
			StartPos:    start,
			EndPos:      end,
			SlotsUsed:   used,
			SlotsTotal:  blockSize,
		}
	}

	totalSlots := numBlocks * blockSize
	return &PagedResult{
		Blocks:      blocks,
		BlockSize:   blockSize,
		SeqLen:      seqLen,
		TotalSlots:  totalSlots,
		UsedSlots:   seqLen,
		WastedSlots: totalSlots - seqLen,
	}
}

// === TRAINING ===
// Training uses the same autograd engine from value.go and GPT forward pass from microgpt.go.
// After training, weights are extracted to plain floats for the inference comparison.
// This isolates the KV cache comparison from autograd overhead.

// kvExtractMatrix converts [][]*Value to [][]float64.
func kvExtractMatrix(m [][]*Value) [][]float64 {
	out := make([][]float64, len(m))
	for i, row := range m {
		out[i] = make([]float64, len(row))
		for j, v := range row {
			out[i][j] = v.Data
		}
	}
	return out
}

// KVTrainResult holds the trained model in both Value and float forms.
type KVTrainResult struct {
	FloatParams *KVFloatParams
	Chars       []rune
	VocabSize   int
	BOS         int
}

// TrainKVModel trains a tiny GPT model and extracts float weights for the KV cache demo.
func TrainKVModel(docs []string, steps int, rng *rand.Rand, verbose bool) *KVTrainResult {
	// Build vocabulary
	charSet := make(map[rune]bool)
	for _, doc := range docs {
		for _, ch := range doc {
			charSet[ch] = true
		}
	}
	chars := make([]rune, 0, len(charSet))
	for ch := range charSet {
		chars = append(chars, ch)
	}
	// Sort for determinism
	for i := 0; i < len(chars); i++ {
		for j := i + 1; j < len(chars); j++ {
			if chars[j] < chars[i] {
				chars[i], chars[j] = chars[j], chars[i]
			}
		}
	}
	charToIdx := make(map[rune]int, len(chars))
	for i, ch := range chars {
		charToIdx[ch] = i
	}
	bos := len(chars)
	vocabSize := len(chars) + 1

	// Initialize parameters using MakeVMatrix from value.go
	wte := MakeVMatrix(rng, vocabSize, kvNEmbd, 0.08)
	wpe := MakeVMatrix(rng, kvBlockSz, kvNEmbd, 0.08)

	type layerParams struct {
		wq, wk, wv, wo [][]*Value
		fc1, fc2       [][]*Value
	}
	layers := make([]layerParams, kvNLayer)
	for li := range layers {
		layers[li] = layerParams{
			wq:  MakeVMatrix(rng, kvNEmbd, kvNEmbd, 0.08),
			wk:  MakeVMatrix(rng, kvNEmbd, kvNEmbd, 0.08),
			wv:  MakeVMatrix(rng, kvNEmbd, kvNEmbd, 0.08),
			wo:  MakeVMatrix(rng, kvNEmbd, kvNEmbd, 0.08),
			fc1: MakeVMatrix(rng, 4*kvNEmbd, kvNEmbd, 0.08),
			fc2: MakeVMatrix(rng, kvNEmbd, 4*kvNEmbd, 0.08),
		}
	}
	lmHead := MakeVMatrix(rng, vocabSize, kvNEmbd, 0.08)

	// Collect all parameters
	var allParams []*Value
	collectMatrix := func(m [][]*Value) {
		for _, row := range m {
			allParams = append(allParams, row...)
		}
	}
	collectMatrix(wte)
	collectMatrix(wpe)
	for li := range layers {
		collectMatrix(layers[li].wq)
		collectMatrix(layers[li].wk)
		collectMatrix(layers[li].wv)
		collectMatrix(layers[li].wo)
		collectMatrix(layers[li].fc1)
		collectMatrix(layers[li].fc2)
	}
	collectMatrix(lmHead)

	mState := make([]float64, len(allParams))
	vState := make([]float64, len(allParams))

	// Forward pass (single token, Value-based)
	kvForwardTrain := func(tokenID, posID int, keysT, valsT [][][]*Value) []*Value {
		x := make([]*Value, kvNEmbd)
		for j := 0; j < kvNEmbd; j++ {
			x[j] = wte[tokenID][j].Add(wpe[posID][j])
		}
		x = VRMSNorm(x)

		for li := 0; li < kvNLayer; li++ {
			xRes := make([]*Value, len(x))
			copy(xRes, x)
			x = VRMSNorm(x)
			q := VLinear(x, layers[li].wq)
			k := VLinear(x, layers[li].wk)
			v := VLinear(x, layers[li].wv)
			keysT[li] = append(keysT[li], k)
			valsT[li] = append(valsT[li], v)

			xAttn := make([]*Value, 0, kvNEmbd)
			for h := 0; h < kvNHead; h++ {
				hs := h * kvHeadDim
				qH := q[hs : hs+kvHeadDim]
				kH := make([][]*Value, len(keysT[li]))
				vH := make([][]*Value, len(valsT[li]))
				for t := range keysT[li] {
					kH[t] = keysT[li][t][hs : hs+kvHeadDim]
					vH[t] = valsT[li][t][hs : hs+kvHeadDim]
				}
				scale := 1.0 / math.Sqrt(float64(kvHeadDim))
				scores := make([]*Value, len(kH))
				for t := range kH {
					dot := qH[0].Mul(kH[t][0])
					for j := 1; j < kvHeadDim; j++ {
						dot = dot.Add(qH[j].Mul(kH[t][j]))
					}
					scores[t] = dot.MulScalar(scale)
				}
				weights := VSoftmax(scores)
				for j := 0; j < kvHeadDim; j++ {
					s := weights[0].Mul(vH[0][j])
					for t := 1; t < len(vH); t++ {
						s = s.Add(weights[t].Mul(vH[t][j]))
					}
					xAttn = append(xAttn, s)
				}
			}
			x = VLinear(xAttn, layers[li].wo)
			for j := range x {
				x[j] = x[j].Add(xRes[j])
			}
			xRes = make([]*Value, len(x))
			copy(xRes, x)
			x = VRMSNorm(x)
			x = VLinear(x, layers[li].fc1)
			for j := range x {
				x[j] = x[j].ReLU()
			}
			x = VLinear(x, layers[li].fc2)
			for j := range x {
				x[j] = x[j].Add(xRes[j])
			}
		}
		return VLinear(x, lmHead)
	}

	// Training loop
	for step := 0; step < steps; step++ {
		doc := docs[step%len(docs)]
		tokens := make([]int, 0)
		tokens = append(tokens, bos)
		for _, ch := range doc {
			tokens = append(tokens, charToIdx[ch])
		}
		tokens = append(tokens, bos)

		seqLen := kvBlockSz
		if len(tokens)-1 < seqLen {
			seqLen = len(tokens) - 1
		}

		keysT := make([][][]*Value, kvNLayer)
		valsT := make([][][]*Value, kvNLayer)

		var losses []*Value
		for pos := 0; pos < seqLen; pos++ {
			logits := kvForwardTrain(tokens[pos], pos, keysT, valsT)
			probs := VSoftmax(logits)
			target := tokens[pos+1]
			negLogProb := probs[target].SafeLog().MulScalar(-1.0)
			losses = append(losses, negLogProb)
		}

		// Mean loss
		loss := VSum(losses).MulScalar(1.0 / float64(seqLen))
		loss.Backward()

		// Adam update
		lrT := kvLR * (1.0 - float64(step)/float64(steps))
		for i, p := range allParams {
			mState[i] = kvBeta1*mState[i] + (1-kvBeta1)*p.Grad
			vState[i] = kvBeta2*vState[i] + (1-kvBeta2)*p.Grad*p.Grad
			mHat := mState[i] / (1 - math.Pow(kvBeta1, float64(step+1)))
			vHat := vState[i] / (1 - math.Pow(kvBeta2, float64(step+1)))
			p.Data -= lrT * mHat / (math.Sqrt(vHat) + kvEpsAdam)
			p.Grad = 0
		}

		if verbose && ((step+1)%100 == 0 || step == 0) {
			fmt.Printf("  step %4d/%d | loss: %.4f\n", step+1, steps, loss.Data)
		}
	}

	// Extract to float params
	fp := &KVFloatParams{
		Wte:    kvExtractMatrix(wte),
		Wpe:    kvExtractMatrix(wpe),
		Layers: make([]KVLayerParams, kvNLayer),
		LMHead: kvExtractMatrix(lmHead),
	}
	for li := range layers {
		fp.Layers[li] = KVLayerParams{
			WQ:  kvExtractMatrix(layers[li].wq),
			WK:  kvExtractMatrix(layers[li].wk),
			WV:  kvExtractMatrix(layers[li].wv),
			WO:  kvExtractMatrix(layers[li].wo),
			FC1: kvExtractMatrix(layers[li].fc1),
			FC2: kvExtractMatrix(layers[li].fc2),
		}
	}

	return &KVTrainResult{
		FloatParams: fp,
		Chars:       chars,
		VocabSize:   vocabSize,
		BOS:         bos,
	}
}

// === MAIN DEMO ===

// RunMicrokv trains a tiny GPT, then compares generation with and without KV cache,
// showing multiply counts per step, speedup ratios, memory growth, and paged attention.
func RunMicrokv() {
	rng := rand.New(rand.NewSource(42))

	// Training data — short names (same source as microgpt/microquant)
	docs := []string{
		"emma", "olivia", "ava", "sophia", "isabella",
		"mia", "charlotte", "amelia", "harper", "evelyn",
		"liam", "noah", "oliver", "james", "benjamin",
		"elijah", "lucas", "mason", "logan", "alexander",
	}
	rng.Shuffle(len(docs), func(i, j int) { docs[i], docs[j] = docs[j], docs[i] })

	fmt.Println("Loading data...")
	fmt.Printf("Loaded %d documents, vocab size will be determined during training\n", len(docs))

	fmt.Printf("\nTraining tiny model (%d steps)...\n", kvNumSteps)
	trained := TrainKVModel(docs, kvNumSteps, rng, true)
	fmt.Printf("Training complete.\n")

	// Run both inference methods on the same prompt
	promptTok := trained.BOS
	fmt.Printf("\n=== KV-Cache Comparison ===\n")
	fmt.Printf("Generating %d-character sequence from BOS token\n\n", kvGenLen)

	t0 := time.Now()
	noCache := GenerateNoCache(promptTok, trained.FloatParams, trained.VocabSize, kvGenLen)
	timeNo := time.Since(t0)

	t1 := time.Now()
	withCache := GenerateWithCache(promptTok, trained.FloatParams, trained.VocabSize, kvGenLen)
	timeCached := time.Since(t1)

	// Verify identical outputs — both methods compute the exact same function.
	// The KV cache is a computational shortcut, not an approximation.
	match := true
	for i := range noCache.Tokens {
		if noCache.Tokens[i] != withCache.Tokens[i] {
			match = false
			break
		}
	}
	if !match {
		fmt.Println("WARNING: Output mismatch between cached and uncached generation!")
	}

	// Step-by-step comparison
	header := fmt.Sprintf("%4s  %16s  %18s  %8s  %5s", "Step", "No Cache (muls)", "With Cache (muls)", "Speedup", "Match")
	fmt.Println(header)
	fmt.Println(strings.Repeat("-", len(header)))
	for i := 0; i < kvGenLen; i++ {
		ratio := 0.0
		if withCache.MulsPerStep[i] > 0 {
			ratio = float64(noCache.MulsPerStep[i]) / float64(withCache.MulsPerStep[i])
		}
		stepMatch := "yes"
		if noCache.Tokens[i] != withCache.Tokens[i] {
			stepMatch = "NO"
		}
		fmt.Printf("%4d  %16s  %18s  %7.1fx  %5s\n",
			i+1,
			commaInt(noCache.MulsPerStep[i]),
			commaInt(withCache.MulsPerStep[i]),
			ratio,
			stepMatch)
	}

	totalNo := 0
	totalYes := 0
	for i := 0; i < kvGenLen; i++ {
		totalNo += noCache.MulsPerStep[i]
		totalYes += withCache.MulsPerStep[i]
	}
	overallRatio := 0.0
	if totalYes > 0 {
		overallRatio = float64(totalNo) / float64(totalYes)
	}
	fmt.Printf("\nTotal multiplies -- No cache: %s | With cache: %s | Ratio: %.1fx\n",
		commaInt(totalNo), commaInt(totalYes), overallRatio)
	fmt.Printf("Wall time -- No cache: %.3fs | With cache: %.3fs\n",
		timeNo.Seconds(), timeCached.Seconds())

	// Generated text
	chars := trained.Chars
	bos := trained.BOS
	var sb strings.Builder
	for _, tok := range withCache.Tokens {
		if tok == bos {
			sb.WriteByte('.')
		} else if tok < len(chars) {
			sb.WriteRune(chars[tok])
		}
	}
	fmt.Printf("\nGenerated: %q (both methods identical)\n", sb.String())

	// Memory growth analysis
	// KV cache stores 2 vectors (K and V) per layer per position, each of size nEmbd.
	// Memory growth is strictly linear in sequence length — no quadratic blowup.
	// This linear growth is WHY long-context models (100K+ tokens) are memory-bound:
	// at d_model=4096, 40 layers, 100K tokens, the cache is ~32GB in float16.
	floatsPerPos := 2 * kvNLayer * kvNEmbd
	fmt.Printf("\n=== Memory Growth ===\n")
	fmt.Printf("%8s   %20s   %28s\n", "Position", "Cache Size (floats)", "Cache Size (bytes, float32)")
	fmt.Println(strings.Repeat("-", 62))
	for i := 0; i < kvGenLen; i++ {
		nFloats := withCache.CacheSizes[i]
		nBytes := nFloats * 4
		fmt.Printf("%8d   %20s   %28s\n", i+1, commaInt(nFloats), commaInt(nBytes))
	}
	fmt.Printf("\nGrowth: linear O(n) -- %d floats per position (2 * %d layer * %d embd)\n",
		floatsPerPos, kvNLayer, kvNEmbd)
	fmt.Println("Signpost: LLaMA-2 70B with 80 layers, 8192 embd, 4K context = ~5.2 GB KV cache in float16.")
	fmt.Println("This is why KV cache memory, not compute, is the bottleneck for long sequences.")

	// Paged attention
	fmt.Printf("\n=== Paged Attention Simulation ===\n")
	fmt.Printf("Block size: %d positions | Sequence length: %d\n", kvPageBlk, kvGenLen)
	fmt.Printf("Each block holds %d positions of KV data\n\n", kvPageBlk)

	paged := SimulatePagedAttention(kvGenLen, kvPageBlk)

	fmt.Println("Allocation trace:")
	for pos := 0; pos < kvGenLen; pos++ {
		logicalBlock := pos / kvPageBlk
		slotInBlock := pos % kvPageBlk
		if slotInBlock == 0 {
			fmt.Printf("  Position %2d: new block needed -> logical block %d -> physical block %d\n",
				pos, logicalBlock, paged.Blocks[logicalBlock].PhysicalIdx)
		} else {
			fmt.Printf("  Position %2d: slot %d in logical block %d (physical %d)\n",
				pos, slotInBlock, logicalBlock, paged.Blocks[logicalBlock].PhysicalIdx)
		}
	}

	fmt.Println("\nPage table (logical -> physical):")
	for _, blk := range paged.Blocks {
		status := "FULL"
		if blk.SlotsUsed < blk.SlotsTotal {
			status = fmt.Sprintf("%d/%d", blk.SlotsUsed, blk.SlotsTotal)
		}
		fmt.Printf("  Logical block %d -> Physical block %d [positions %d-%d] %s\n",
			blk.LogicalIdx, blk.PhysicalIdx, blk.StartPos, blk.EndPos, status)
	}

	// Signpost: in production, physical blocks are shared across sequences. Two prompts
	// starting with the same system message reuse the same physical blocks for the shared
	// prefix — vLLM's copy-on-write avoids duplicating KV data. We simulate single-
	// sequence allocation; the multi-sequence sharing is the real memory win at scale.
	fragPct := 0.0
	if paged.TotalSlots > 0 {
		fragPct = 100 * float64(paged.WastedSlots) / float64(paged.TotalSlots)
	}
	fmt.Printf("\nBlocks allocated: %d (%d slots)\n", len(paged.Blocks), paged.TotalSlots)
	fmt.Printf("Slots used: %d | Wasted: %d (%.0f%% internal fragmentation)\n",
		paged.UsedSlots, paged.WastedSlots, fragPct)
}

// commaInt formats an integer with comma separators (e.g., 1234567 -> "1,234,567").
func commaInt(n int) string {
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
