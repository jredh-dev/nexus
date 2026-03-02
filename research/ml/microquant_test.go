//go:build research

package ml

import (
	"math"
	"math/rand/v2"
	"testing"
)

// === QUANTIZATION / DEQUANTIZATION ROUNDTRIP ===

func TestQuantizeAbsmaxInt8Roundtrip(t *testing.T) {
	weights := [][]float64{
		{0.1, -0.2, 0.3},
		{-0.05, 0.15, -0.25},
	}
	qm := QuantizeAbsmaxInt8(weights)
	deq := DequantizeAbsmax(qm)

	// Max error should be small (scale = 0.3/127 ≈ 0.00236)
	for i := range weights {
		for j := range weights[i] {
			err := math.Abs(weights[i][j] - deq[i][j])
			if err > 0.003 {
				t.Errorf("[%d][%d]: error %.6f > 0.003 (orig=%.4f, deq=%.4f)",
					i, j, err, weights[i][j], deq[i][j])
			}
		}
	}
}

func TestQuantizeAbsmaxInt4Roundtrip(t *testing.T) {
	weights := [][]float64{
		{0.1, -0.2, 0.3},
		{-0.05, 0.15, -0.25},
	}
	qm := QuantizeAbsmaxInt4(weights)
	deq := DequantizeAbsmax(qm)

	// INT4 has coarser grid (scale = 0.3/7 ≈ 0.0429), max error can be larger
	for i := range weights {
		for j := range weights[i] {
			err := math.Abs(weights[i][j] - deq[i][j])
			if err > 0.05 {
				t.Errorf("[%d][%d]: error %.6f > 0.05 (orig=%.4f, deq=%.4f)",
					i, j, err, weights[i][j], deq[i][j])
			}
		}
	}
}

func TestQuantizeZeropointInt8Roundtrip(t *testing.T) {
	weights := [][]float64{
		{0.1, 0.5, 0.9},
		{0.2, 0.6, 0.8},
	}
	qm := QuantizeZeropointInt8(weights)
	deq := DequantizeZeropoint(qm)

	for i := range weights {
		for j := range weights[i] {
			err := math.Abs(weights[i][j] - deq[i][j])
			if err > 0.005 {
				t.Errorf("[%d][%d]: error %.6f > 0.005", i, j, err)
			}
		}
	}
}

func TestQuantizePerChannelInt8Roundtrip(t *testing.T) {
	// Two rows with very different scales -- per-channel should handle this well
	weights := [][]float64{
		{0.001, -0.002, 0.003}, // tiny values
		{1.0, -2.0, 3.0},       // large values
	}
	qm := QuantizePerChannelInt8(weights)
	deq := DequantizePerChannel(qm)

	// Per-channel should have much lower error on the small row than per-tensor
	for j := range weights[0] {
		err := math.Abs(weights[0][j] - deq[0][j])
		if err > 0.00005 {
			t.Errorf("small row [0][%d]: error %.8f > 0.00005", j, err)
		}
	}
}

func TestQuantizeAbsmaxInt8ClampsSaturated(t *testing.T) {
	// All values are the same magnitude -- should map to exactly ±127
	weights := [][]float64{{1.0, -1.0}}
	qm := QuantizeAbsmaxInt8(weights)
	if qm.Data[0][0] != 127 || qm.Data[0][1] != -127 {
		t.Errorf("Expected [127, -127], got %v", qm.Data[0])
	}
}

func TestQuantizeZeroMatrix(t *testing.T) {
	weights := [][]float64{{0, 0}, {0, 0}}

	qm8 := QuantizeAbsmaxInt8(weights)
	if qm8.Scale != 1.0 {
		t.Errorf("INT8 zero matrix: scale = %f, want 1.0", qm8.Scale)
	}

	qm4 := QuantizeAbsmaxInt4(weights)
	if qm4.Scale != 1.0 {
		t.Errorf("INT4 zero matrix: scale = %f, want 1.0", qm4.Scale)
	}
}

// === INT8 VS INT4 ERROR COMPARISON ===

func TestInt8LowerErrorThanInt4(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 0))
	weights := make([][]float64, 10)
	for i := range weights {
		weights[i] = make([]float64, 10)
		for j := range weights[i] {
			weights[i][j] = rng.NormFloat64() * 0.1
		}
	}

	deq8 := DequantizeAbsmax(QuantizeAbsmaxInt8(weights))
	deq4 := DequantizeAbsmax(QuantizeAbsmaxInt4(weights))

	err8, err4 := 0.0, 0.0
	for i := range weights {
		for j := range weights[i] {
			err8 += math.Abs(weights[i][j] - deq8[i][j])
			err4 += math.Abs(weights[i][j] - deq4[i][j])
		}
	}

	if err8 >= err4 {
		t.Errorf("INT8 total error (%.6f) should be less than INT4 (%.6f)", err8, err4)
	}
}

// === PER-CHANNEL VS PER-TENSOR ===

func TestPerChannelBeatsPerTensorOnMixedScales(t *testing.T) {
	// Matrix with rows at very different scales -- per-channel should win
	weights := [][]float64{
		{0.001, -0.002, 0.001, -0.001},
		{10.0, -20.0, 15.0, -5.0},
	}

	deqTensor := DequantizeAbsmax(QuantizeAbsmaxInt8(weights))
	deqChannel := DequantizePerChannel(QuantizePerChannelInt8(weights))

	// Error on the small row
	errTensor, errChannel := 0.0, 0.0
	for j := range weights[0] {
		errTensor += math.Abs(weights[0][j] - deqTensor[0][j])
		errChannel += math.Abs(weights[0][j] - deqChannel[0][j])
	}

	if errChannel >= errTensor {
		t.Errorf("Per-channel error (%.8f) should be less than per-tensor (%.8f) on mixed-scale matrix",
			errChannel, errTensor)
	}
}

// === FLOAT OPERATIONS ===

func TestLinearFloat(t *testing.T) {
	w := [][]float64{{1, 2}, {3, 4}}
	x := []float64{1, 1}
	y := LinearFloat(x, w)
	if math.Abs(y[0]-3) > 1e-10 || math.Abs(y[1]-7) > 1e-10 {
		t.Errorf("LinearFloat([1,1], [[1,2],[3,4]]) = %v, want [3, 7]", y)
	}
}

func TestSoftmaxFloat(t *testing.T) {
	logits := []float64{1, 2, 3}
	probs := SoftmaxFloat(logits)

	sum := 0.0
	for _, p := range probs {
		sum += p
		if p < 0 || p > 1 {
			t.Errorf("Probability out of range: %f", p)
		}
	}
	if math.Abs(sum-1.0) > 1e-10 {
		t.Errorf("Softmax sum = %f, want 1.0", sum)
	}
	// Probabilities should be monotonically increasing
	if probs[0] >= probs[1] || probs[1] >= probs[2] {
		t.Errorf("Softmax not monotonic: %v", probs)
	}
}

func TestRMSNormFloat(t *testing.T) {
	x := []float64{3, 4}
	normed := RMSNormFloat(x)

	// RMS = sqrt((9+16)/2) = sqrt(12.5) ≈ 3.536
	// scale = 1/sqrt(12.5 + 1e-5) ≈ 0.2828
	rms := math.Sqrt((9 + 16) / 2.0)
	expected0 := 3.0 / rms
	expected1 := 4.0 / rms
	if math.Abs(normed[0]-expected0) > 1e-4 || math.Abs(normed[1]-expected1) > 1e-4 {
		t.Errorf("RMSNormFloat([3,4]) = %v, want [%.4f, %.4f]", normed, expected0, expected1)
	}
}

// === FLOAT GPT FORWARD ===

func TestGPTForwardFloatOutputShape(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 0))
	vocabSize := 10
	cfg := DefaultGPTConfig(vocabSize)
	params := InitGPTParams(rng, cfg)
	fp := ExtractFloatParams(params)

	keys := make([][]float64Slice, cfg.NLayer)
	values := make([][]float64Slice, cfg.NLayer)

	logits := GPTForwardFloat(0, 0, &keys, &values, fp)
	if len(logits) != vocabSize {
		t.Errorf("logits length = %d, want %d", len(logits), vocabSize)
	}
}

func TestGPTForwardFloatMatchesAutograd(t *testing.T) {
	// Float forward should produce the same logits as Value-based forward
	rng := rand.New(rand.NewPCG(42, 0))
	vocabSize := 5
	cfg := DefaultGPTConfig(vocabSize)
	params := InitGPTParams(rng, cfg)
	fp := ExtractFloatParams(params)

	// Value-based forward
	vKeys := make([][]*[]*Value, cfg.NLayer)
	vValues := make([][]*[]*Value, cfg.NLayer)
	vLogits := GPTForward(0, 0, &vKeys, &vValues, params)

	// Float-based forward
	fKeys := make([][]float64Slice, cfg.NLayer)
	fValues := make([][]float64Slice, cfg.NLayer)
	fLogits := GPTForwardFloat(0, 0, &fKeys, &fValues, fp)

	for i := range vLogits {
		if math.Abs(vLogits[i].Data-fLogits[i]) > 1e-10 {
			t.Errorf("logit[%d]: Value=%.10f, Float=%.10f", i, vLogits[i].Data, fLogits[i])
		}
	}
}

// === EXTRACT AND EVAL ===

func TestExtractFloatParams(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 0))
	cfg := DefaultGPTConfig(5)
	params := InitGPTParams(rng, cfg)
	fp := ExtractFloatParams(params)

	if len(fp.Wte) != 5 || len(fp.Wte[0]) != 16 {
		t.Errorf("Wte shape: [%d][%d], want [5][16]", len(fp.Wte), len(fp.Wte[0]))
	}
	// Values should match
	for i := range params.Wte {
		for j := range params.Wte[i] {
			if params.Wte[i][j].Data != fp.Wte[i][j] {
				t.Fatalf("Wte[%d][%d] mismatch", i, j)
			}
		}
	}
}

func TestComputeModelSize(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 0))
	cfg := DefaultGPTConfig(27)
	params := InitGPTParams(rng, cfg)
	fp := ExtractFloatParams(params)

	size32 := ComputeModelSize(fp, 32)
	size8 := ComputeModelSize(fp, 8)
	size4 := ComputeModelSize(fp, 4)

	if size32 != size8*4 {
		t.Errorf("size32 (%d) should be 4x size8 (%d)", size32, size8)
	}
	if size32 != size4*8 {
		t.Errorf("size32 (%d) should be 8x size4 (%d)", size32, size4)
	}
}

// === QUANTIZED MODEL EVALUATION ===

func TestQuantizedModelLossFinite(t *testing.T) {
	// Train a tiny model, quantize, verify loss is finite
	docs := []string{"ab", "ba", "aa", "bb"}
	rng := rand.New(rand.NewPCG(42, 0))
	result := TrainGPT(docs, 20, rng, false)
	fp := ExtractFloatParams(result.Params)

	chars, charToIdx, bos := BuildVocab(docs)

	for _, method := range []string{"int8-absmax", "int4-absmax", "int8-zeropoint", "int8-perchannel"} {
		qfp := QuantizeAllParams(fp, method)
		loss := EvalQuantLoss(qfp, docs, chars, charToIdx, bos)
		if math.IsNaN(loss) || math.IsInf(loss, 0) {
			t.Errorf("%s: loss is %f, expected finite", method, loss)
		}
	}
}

func TestQuantizedLossCloseToBaseline(t *testing.T) {
	// INT8 quantized loss should be within 50% of baseline for a trained model
	docs := []string{"abc", "bca", "cab", "abc", "bca"}
	rng := rand.New(rand.NewPCG(42, 0))
	result := TrainGPT(docs, 50, rng, false)
	fp := ExtractFloatParams(result.Params)

	chars, charToIdx, bos := BuildVocab(docs)
	baselineLoss := EvalQuantLoss(fp, docs, chars, charToIdx, bos)

	qfp := QuantizeAllParams(fp, "int8-absmax")
	qLoss := EvalQuantLoss(qfp, docs, chars, charToIdx, bos)

	ratio := qLoss / baselineLoss
	if ratio > 1.5 || ratio < 0.5 {
		t.Errorf("INT8 loss (%.4f) diverges too far from baseline (%.4f), ratio=%.2f",
			qLoss, baselineLoss, ratio)
	}
	t.Logf("Baseline: %.4f, INT8: %.4f, ratio: %.2f", baselineLoss, qLoss, ratio)
}

// === BENCHMARKS ===

func BenchmarkQuantizeAbsmaxInt8(b *testing.B) {
	rng := rand.New(rand.NewPCG(42, 0))
	weights := make([][]float64, 64)
	for i := range weights {
		weights[i] = make([]float64, 16)
		for j := range weights[i] {
			weights[i][j] = rng.NormFloat64() * 0.1
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		QuantizeAbsmaxInt8(weights)
	}
}

func BenchmarkGPTForwardFloat(b *testing.B) {
	rng := rand.New(rand.NewPCG(42, 0))
	cfg := DefaultGPTConfig(10)
	params := InitGPTParams(rng, cfg)
	fp := ExtractFloatParams(params)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		keys := make([][]float64Slice, cfg.NLayer)
		values := make([][]float64Slice, cfg.NLayer)
		GPTForwardFloat(0, 0, &keys, &values, fp)
	}
}
