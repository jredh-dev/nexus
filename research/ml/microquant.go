//go:build research

// How to shrink a model by 4x with minimal quality loss -- the math behind INT8 and INT4
// weight quantization, demonstrated end-to-end: train, quantize, dequantize, compare.
//
// Reference: Dettmers et al., "LLM.int8(): 8-bit Matrix Multiplication for Transformers
// at Scale" (2022). https://arxiv.org/abs/2208.07339
// Also: Frantar et al., "GPTQ: Accurate Post-Training Quantization for Generative
// Pre-trained Transformers" (2022). https://arxiv.org/abs/2210.17323
//
// Port of microquant.py from mathews-tom/no-magic to Go.
// AGPL-3.0 -- see LICENSE in repo root.
package ml

import (
	"fmt"
	"math"
	"math/rand/v2"
)

// === QUANTIZATION FUNCTIONS ===
//
// Quantization maps continuous float weights to a discrete integer grid.
// The core insight: neural network weights are approximately normally distributed
// with small magnitude. Most values cluster near zero, so mapping to [-127, +127]
// (INT8) or [-8, +7] (INT4) loses surprisingly little information. The network's
// nonlinearities and redundancy absorb the rounding error.

// QuantizedMatrix holds integer weights and their quantization parameters.
type QuantizedMatrix struct {
	Data      [][]int
	Scale     float64
	ZeroPoint int       // only used by zero-point quantization
	Scales    []float64 // only used by per-channel quantization (one scale per row)
}

// QuantizeAbsmaxInt8 scales the float range [-max|W|, +max|W|] to [-127, +127].
// Symmetric around zero -- assumes weight distribution is roughly centered.
// This is the simplest quantization scheme and the baseline for everything else.
//
// Math: q_i = clamp(round(w_i / s), -127, 127)  where s = max(|W|) / 127
func QuantizeAbsmaxInt8(weights [][]float64) QuantizedMatrix {
	maxAbs := matMaxAbs(weights)
	if maxAbs == 0 {
		return QuantizedMatrix{Data: zeroIntMatrix(weights), Scale: 1.0}
	}
	scale := maxAbs / 127.0
	data := make([][]int, len(weights))
	for i, row := range weights {
		data[i] = make([]int, len(row))
		for j, w := range row {
			data[i][j] = clampInt(int(math.Round(w/scale)), -127, 127)
		}
	}
	return QuantizedMatrix{Data: data, Scale: scale}
}

// QuantizeAbsmaxInt4 maps to [-8, +7] (4-bit signed integer range).
// 8x compression vs float32. The quantization grid is 16x coarser than INT8,
// so rounding errors are substantially larger. Neural nets tolerate this because
// individual weight precision matters less than the collective statistical
// properties of weight matrices.
//
// Signpost: production INT4 (GPTQ, AWQ) uses calibration data to minimize
// output error rather than naive round-to-nearest.
func QuantizeAbsmaxInt4(weights [][]float64) QuantizedMatrix {
	maxAbs := matMaxAbs(weights)
	if maxAbs == 0 {
		return QuantizedMatrix{Data: zeroIntMatrix(weights), Scale: 1.0}
	}
	scale := maxAbs / 7.0
	data := make([][]int, len(weights))
	for i, row := range weights {
		data[i] = make([]int, len(row))
		for j, w := range row {
			data[i][j] = clampInt(int(math.Round(w/scale)), -8, 7)
		}
	}
	return QuantizedMatrix{Data: data, Scale: scale}
}

// QuantizeZeropointInt8 maps [min_W, max_W] to [0, 255] (asymmetric).
// Unlike absmax which centers on zero, zero-point shifts the mapping so the
// full 8-bit range covers the actual weight range. More accurate when weights
// are not symmetric around zero.
//
// Math: scale = (w_max - w_min) / 255
//
//	zero_point = round(-w_min / scale)
//	q_i = clamp(round(w_i / scale) + zero_point, 0, 255)
func QuantizeZeropointInt8(weights [][]float64) QuantizedMatrix {
	wMin, wMax := matMinMax(weights)
	if wMax == wMin {
		return QuantizedMatrix{Data: zeroIntMatrix(weights), Scale: 1.0}
	}
	scale := (wMax - wMin) / 255.0
	zeroPoint := int(math.Round(-wMin / scale))
	data := make([][]int, len(weights))
	for i, row := range weights {
		data[i] = make([]int, len(row))
		for j, w := range row {
			data[i][j] = clampInt(int(math.Round(w/scale))+zeroPoint, 0, 255)
		}
	}
	return QuantizedMatrix{Data: data, Scale: scale, ZeroPoint: zeroPoint}
}

// QuantizePerChannelInt8 gives each output row its own scale factor.
// Per-tensor quantization uses one scale for the entire matrix, so a single
// outlier weight forces the entire grid to be coarse. Per-channel (per-row)
// quantization lets each output neuron use its own range, dramatically reducing
// error for matrices with non-uniform row magnitudes.
//
// Signpost: LLM.int8() (Dettmers 2022) goes further with mixed-precision
// decomposition -- outlier channels stay in fp16 while the rest quantize to INT8.
func QuantizePerChannelInt8(weights [][]float64) QuantizedMatrix {
	data := make([][]int, len(weights))
	scales := make([]float64, len(weights))
	for i, row := range weights {
		maxAbs := 0.0
		for _, w := range row {
			if a := math.Abs(w); a > maxAbs {
				maxAbs = a
			}
		}
		if maxAbs == 0 {
			scales[i] = 1.0
		} else {
			scales[i] = maxAbs / 127.0
		}
		data[i] = make([]int, len(row))
		for j, w := range row {
			data[i][j] = clampInt(int(math.Round(w/scales[i])), -127, 127)
		}
	}
	return QuantizedMatrix{Data: data, Scales: scales}
}

// === DEQUANTIZATION FUNCTIONS ===
// Reverse the quantization mapping to recover approximate float weights.

// DequantizeAbsmax returns w_hat = q * scale.
func DequantizeAbsmax(qm QuantizedMatrix) [][]float64 {
	out := make([][]float64, len(qm.Data))
	for i, row := range qm.Data {
		out[i] = make([]float64, len(row))
		for j, q := range row {
			out[i][j] = float64(q) * qm.Scale
		}
	}
	return out
}

// DequantizeZeropoint returns w_hat = (q - zeroPoint) * scale.
func DequantizeZeropoint(qm QuantizedMatrix) [][]float64 {
	out := make([][]float64, len(qm.Data))
	for i, row := range qm.Data {
		out[i] = make([]float64, len(row))
		for j, q := range row {
			out[i][j] = float64(q-qm.ZeroPoint) * qm.Scale
		}
	}
	return out
}

// DequantizePerChannel returns w_hat[i] = q[i] * scales[row].
func DequantizePerChannel(qm QuantizedMatrix) [][]float64 {
	out := make([][]float64, len(qm.Data))
	for i, row := range qm.Data {
		out[i] = make([]float64, len(row))
		for j, q := range row {
			out[i][j] = float64(q) * qm.Scales[i]
		}
	}
	return out
}

// === FLOAT-BASED OPERATIONS (no autograd, for quantized inference) ===
// After quantization, weights are dequantized back to floats. These functions
// mirror the Value-based versions but operate on raw floats -- no gradient tracking
// because quantized models are inference-only.

// LinearFloat computes y = W @ x with plain floats.
func LinearFloat(x []float64, w [][]float64) []float64 {
	out := make([]float64, len(w))
	for i, wRow := range w {
		s := 0.0
		for j, xj := range x {
			s += wRow[j] * xj
		}
		out[i] = s
	}
	return out
}

// SoftmaxFloat computes stable softmax on plain floats.
func SoftmaxFloat(logits []float64) []float64 {
	maxVal := logits[0]
	for _, v := range logits[1:] {
		if v > maxVal {
			maxVal = v
		}
	}
	expVals := make([]float64, len(logits))
	total := 0.0
	for i, v := range logits {
		expVals[i] = math.Exp(v - maxVal)
		total += expVals[i]
	}
	out := make([]float64, len(logits))
	for i, e := range expVals {
		out[i] = e / total
	}
	return out
}

// RMSNormFloat applies RMSNorm on plain floats.
func RMSNormFloat(x []float64) []float64 {
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

// === FLOAT GPT FORWARD PASS (for quantized inference) ===
// Structurally identical to GPTForward but operates on dequantized float weights.
// This separation keeps autograd overhead out of the quantization evaluation path.

// FloatGPTParams holds float64 weight matrices keyed by name (matching GPTParams layout).
type FloatGPTParams struct {
	Config GPTConfig
	Wte    [][]float64
	Wpe    [][]float64
	Layers []FloatGPTLayerParams
	LMHead [][]float64
}

// FloatGPTLayerParams holds float weights for one transformer layer.
type FloatGPTLayerParams struct {
	AttnWQ, AttnWK, AttnWV, AttnWO [][]float64
	MLPFC1, MLPFC2                 [][]float64
}

// ExtractFloatParams converts Value-based GPTParams to plain float64 matrices.
func ExtractFloatParams(p *GPTParams) *FloatGPTParams {
	fp := &FloatGPTParams{Config: p.Config}
	fp.Wte = extractMatrix(p.Wte)
	fp.Wpe = extractMatrix(p.Wpe)
	fp.Layers = make([]FloatGPTLayerParams, len(p.Layers))
	for i := range p.Layers {
		fp.Layers[i] = FloatGPTLayerParams{
			AttnWQ: extractMatrix(p.Layers[i].AttnWQ),
			AttnWK: extractMatrix(p.Layers[i].AttnWK),
			AttnWV: extractMatrix(p.Layers[i].AttnWV),
			AttnWO: extractMatrix(p.Layers[i].AttnWO),
			MLPFC1: extractMatrix(p.Layers[i].MLPFC1),
			MLPFC2: extractMatrix(p.Layers[i].MLPFC2),
		}
	}
	fp.LMHead = extractMatrix(p.LMHead)
	return fp
}

// GPTForwardFloat runs a single-token forward pass with plain float weights. No gradient tracking.
func GPTForwardFloat(tokenID, posID int, keys, values *[][]float64Slice, fp *FloatGPTParams) []float64 {
	cfg := fp.Config
	headDim := cfg.HeadDim()

	tokEmb := fp.Wte[tokenID]
	posEmb := fp.Wpe[posID]
	x := make([]float64, cfg.NEmbd)
	for i := range x {
		x[i] = tokEmb[i] + posEmb[i]
	}
	x = RMSNormFloat(x)

	for li := range fp.Layers {
		layer := &fp.Layers[li]
		xResidual := make([]float64, len(x))
		copy(xResidual, x)

		x = RMSNormFloat(x)
		q := LinearFloat(x, layer.AttnWQ)
		k := LinearFloat(x, layer.AttnWK)
		v := LinearFloat(x, layer.AttnWV)

		(*keys)[li] = append((*keys)[li], k)
		(*values)[li] = append((*values)[li], v)

		xAttn := make([]float64, 0, cfg.NEmbd)
		for head := 0; head < cfg.NHead; head++ {
			hs := head * headDim

			qHead := q[hs : hs+headDim]
			cachedK := (*keys)[li]
			cachedV := (*values)[li]
			seqLen := len(cachedK)

			scale := 1.0 / math.Sqrt(float64(headDim))
			attnLogits := make([]float64, seqLen)
			for t := 0; t < seqLen; t++ {
				kT := cachedK[t]
				dot := 0.0
				for j := 0; j < headDim; j++ {
					dot += qHead[j] * kT[hs+j]
				}
				attnLogits[t] = dot * scale
			}

			attnWeights := SoftmaxFloat(attnLogits)

			headOutput := make([]float64, headDim)
			for j := 0; j < headDim; j++ {
				s := 0.0
				for t := 0; t < seqLen; t++ {
					s += attnWeights[t] * cachedV[t][hs+j]
				}
				headOutput[j] = s
			}
			xAttn = append(xAttn, headOutput...)
		}

		x = LinearFloat(xAttn, layer.AttnWO)
		for i := range x {
			x[i] += xResidual[i]
		}
		xResidual = make([]float64, len(x))
		copy(xResidual, x)

		x = RMSNormFloat(x)
		x = LinearFloat(x, layer.MLPFC1)
		for i := range x {
			if x[i] < 0 {
				x[i] = 0 // ReLU
			}
		}
		x = LinearFloat(x, layer.MLPFC2)
		for i := range x {
			x[i] += xResidual[i]
		}
	}

	return LinearFloat(x, fp.LMHead)
}

// float64Slice is an alias to enable KV cache as [][]float64Slice (per-layer list of vectors).
type float64Slice = []float64

// === EVALUATION HELPERS ===

// EvalQuantLoss computes average cross-entropy loss on evaluation documents using float forward pass.
func EvalQuantLoss(fp *FloatGPTParams, docs []string, chars []rune, charToIdx map[rune]int, bos int) float64 {
	cfg := fp.Config
	totalLoss := 0.0
	totalTokens := 0

	for _, doc := range docs {
		tokens := Tokenize(doc, charToIdx, bos)
		seqLen := cfg.BlockSize
		if len(tokens)-1 < seqLen {
			seqLen = len(tokens) - 1
		}

		keys := make([][]float64Slice, cfg.NLayer)
		values := make([][]float64Slice, cfg.NLayer)

		for pos := 0; pos < seqLen; pos++ {
			logits := GPTForwardFloat(tokens[pos], pos, &keys, &values, fp)
			probs := SoftmaxFloat(logits)
			pTarget := math.Max(probs[tokens[pos+1]], 1e-10)
			totalLoss += -math.Log(pTarget)
			totalTokens++
		}
	}
	if totalTokens == 0 {
		return math.Inf(1)
	}
	return totalLoss / float64(totalTokens)
}

// GenerateQuantSample generates a single sample from float GPT params.
func GenerateQuantSample(fp *FloatGPTParams, chars []rune, bos int, temperature float64, rng *rand.Rand) string {
	cfg := fp.Config
	keys := make([][]float64Slice, cfg.NLayer)
	values := make([][]float64Slice, cfg.NLayer)

	tokenID := bos
	var generated []rune

	for pos := 0; pos < cfg.BlockSize; pos++ {
		logits := GPTForwardFloat(tokenID, pos, &keys, &values, fp)
		scaled := make([]float64, len(logits))
		for i, l := range logits {
			scaled[i] = l / temperature
		}
		probs := SoftmaxFloat(scaled)
		tokenID = weightedSampleFloat(probs, rng)
		if tokenID == bos {
			break
		}
		generated = append(generated, chars[tokenID])
	}
	return string(generated)
}

// QuantizeAllParams applies a quantization scheme to all float weight matrices and returns
// dequantized float params. The method string selects the scheme.
func QuantizeAllParams(fp *FloatGPTParams, method string) *FloatGPTParams {
	qfp := &FloatGPTParams{Config: fp.Config}
	q := func(m [][]float64) [][]float64 {
		switch method {
		case "int8-absmax":
			return DequantizeAbsmax(QuantizeAbsmaxInt8(m))
		case "int4-absmax":
			return DequantizeAbsmax(QuantizeAbsmaxInt4(m))
		case "int8-zeropoint":
			return DequantizeZeropoint(QuantizeZeropointInt8(m))
		case "int8-perchannel":
			return DequantizePerChannel(QuantizePerChannelInt8(m))
		default:
			panic("unknown quantization method: " + method)
		}
	}
	qfp.Wte = q(fp.Wte)
	qfp.Wpe = q(fp.Wpe)
	qfp.Layers = make([]FloatGPTLayerParams, len(fp.Layers))
	for i := range fp.Layers {
		qfp.Layers[i] = FloatGPTLayerParams{
			AttnWQ: q(fp.Layers[i].AttnWQ),
			AttnWK: q(fp.Layers[i].AttnWK),
			AttnWV: q(fp.Layers[i].AttnWV),
			AttnWO: q(fp.Layers[i].AttnWO),
			MLPFC1: q(fp.Layers[i].MLPFC1),
			MLPFC2: q(fp.Layers[i].MLPFC2),
		}
	}
	qfp.LMHead = q(fp.LMHead)
	return qfp
}

// ComputeRoundtripError returns max |w - dequant(quant(w))| across all weights.
func ComputeRoundtripError(original, dequantized *FloatGPTParams) float64 {
	maxErr := 0.0
	check := func(a, b [][]float64) {
		for i := range a {
			for j := range a[i] {
				if e := math.Abs(a[i][j] - b[i][j]); e > maxErr {
					maxErr = e
				}
			}
		}
	}
	check(original.Wte, dequantized.Wte)
	check(original.Wpe, dequantized.Wpe)
	for i := range original.Layers {
		check(original.Layers[i].AttnWQ, dequantized.Layers[i].AttnWQ)
		check(original.Layers[i].AttnWK, dequantized.Layers[i].AttnWK)
		check(original.Layers[i].AttnWV, dequantized.Layers[i].AttnWV)
		check(original.Layers[i].AttnWO, dequantized.Layers[i].AttnWO)
		check(original.Layers[i].MLPFC1, dequantized.Layers[i].MLPFC1)
		check(original.Layers[i].MLPFC2, dequantized.Layers[i].MLPFC2)
	}
	check(original.LMHead, dequantized.LMHead)
	return maxErr
}

// ComputeModelSize returns model size in bytes at the given bit width.
func ComputeModelSize(fp *FloatGPTParams, bits int) int {
	n := 0
	count := func(m [][]float64) {
		for _, row := range m {
			n += len(row)
		}
	}
	count(fp.Wte)
	count(fp.Wpe)
	for i := range fp.Layers {
		count(fp.Layers[i].AttnWQ)
		count(fp.Layers[i].AttnWK)
		count(fp.Layers[i].AttnWV)
		count(fp.Layers[i].AttnWO)
		count(fp.Layers[i].MLPFC1)
		count(fp.Layers[i].MLPFC2)
	}
	count(fp.LMHead)
	return n * bits / 8
}

// === DEMO ===

// RunMicroquant trains a tiny GPT, applies 4 quantization methods, and compares results.
func RunMicroquant() {
	fmt.Println("=== MicroQuant: Weight Quantization Demo ===")
	fmt.Println()

	// Small built-in corpus
	docs := []string{
		"emma", "olivia", "ava", "sophia", "isabella",
		"mia", "charlotte", "amelia", "harper", "evelyn",
		"abigail", "emily", "elizabeth", "mila", "ella",
		"avery", "sofia", "camila", "aria", "scarlett",
		"victoria", "madison", "luna", "grace", "chloe",
		"penelope", "layla", "riley", "zoey", "nora",
		"lily", "eleanor", "hannah", "lillian", "addison",
		"aubrey", "ellie", "stella", "natalie", "zoe",
		"leah", "hazel", "violet", "aurora", "savannah",
		"audrey", "brooklyn", "bella", "claire", "skylar",
	}

	rng := rand.New(rand.NewPCG(42, 0))

	// Phase 1: Train base model (800 steps, matching Python)
	fmt.Println("Phase 1: Training base model...")
	result := TrainGPT(docs, 800, rng, true)

	// Phase 2: Extract float weights
	fmt.Println("\n=== Extracting Float32 Weights ===")
	fp := ExtractFloatParams(result.Params)

	chars, charToIdx, bos := BuildVocab(docs)
	evalDocs := docs[:20] // smaller eval set for demo speed

	baselineLoss := EvalQuantLoss(fp, evalDocs, chars, charToIdx, bos)
	fmt.Printf("Float32 baseline loss: %.4f\n", baselineLoss)

	// Phase 3-6: Apply quantization methods
	methods := []struct {
		name   string
		method string
		bits   int
	}{
		{"INT8 absmax", "int8-absmax", 8},
		{"INT4 absmax", "int4-absmax", 4},
		{"INT8 zero-point", "int8-zeropoint", 8},
		{"INT8 per-channel", "int8-perchannel", 8},
	}

	type quantResult struct {
		name string
		bits int
		loss float64
		err  float64
	}
	var results []quantResult

	for _, m := range methods {
		fmt.Printf("\n=== %s Quantization ===\n", m.name)
		qfp := QuantizeAllParams(fp, m.method)
		loss := EvalQuantLoss(qfp, evalDocs, chars, charToIdx, bos)
		err := ComputeRoundtripError(fp, qfp)
		delta := (loss - baselineLoss) / baselineLoss * 100
		fmt.Printf("%s loss: %.4f (delta: %+.1f%%)\n", m.name, loss, delta)
		results = append(results, quantResult{m.name, m.bits, loss, err})
	}

	// Phase 7: Comparison table
	size32 := ComputeModelSize(fp, 32)
	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("=== Quantization Results ===")
	fmt.Println("========================================")
	fmt.Printf("%-24s %4s %9s %8s %8s %10s\n", "Method", "Bits", "Size", "Loss", "Delta", "Max Err")
	fmt.Printf("%-24s %4s %9s %8s %8s %10s\n", "------", "----", "----", "----", "-----", "-------")
	fmt.Printf("%-24s %4d %7d B %8.4f %8s %10s\n", "Float32 (baseline)", 32, size32, baselineLoss, "---", "---")
	for _, r := range results {
		size := ComputeModelSize(fp, r.bits)
		delta := (r.loss - baselineLoss) / baselineLoss * 100
		fmt.Printf("%-24s %4d %7d B %8.4f %+7.1f%% %10.6f\n", r.name, r.bits, size, r.loss, delta, r.err)
	}
	fmt.Printf("\nCompression: float32->INT8 = %.1fx, float32->INT4 = %.1fx\n",
		float64(size32)/float64(ComputeModelSize(fp, 8)),
		float64(size32)/float64(ComputeModelSize(fp, 4)))
}

// === INTERNAL HELPERS ===

func extractMatrix(m [][]*Value) [][]float64 {
	out := make([][]float64, len(m))
	for i, row := range m {
		out[i] = make([]float64, len(row))
		for j, v := range row {
			out[i][j] = v.Data
		}
	}
	return out
}

func matMaxAbs(m [][]float64) float64 {
	maxAbs := 0.0
	for _, row := range m {
		for _, w := range row {
			if a := math.Abs(w); a > maxAbs {
				maxAbs = a
			}
		}
	}
	return maxAbs
}

func matMinMax(m [][]float64) (float64, float64) {
	wMin, wMax := m[0][0], m[0][0]
	for _, row := range m {
		for _, w := range row {
			if w < wMin {
				wMin = w
			}
			if w > wMax {
				wMax = w
			}
		}
	}
	return wMin, wMax
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func zeroIntMatrix(ref [][]float64) [][]int {
	out := make([][]int, len(ref))
	for i, row := range ref {
		out[i] = make([]int, len(row))
	}
	return out
}

func weightedSampleFloat(weights []float64, rng *rand.Rand) int {
	total := 0.0
	for _, w := range weights {
		total += w
	}
	r := rng.Float64() * total
	cum := 0.0
	for i, w := range weights {
		cum += w
		if r <= cum {
			return i
		}
	}
	return len(weights) - 1
}
