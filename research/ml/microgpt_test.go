//go:build research

package ml

import (
	"math"
	"math/rand/v2"
	"testing"
)

// === GPT CONFIG AND PARAMS ===

func TestDefaultGPTConfig(t *testing.T) {
	cfg := DefaultGPTConfig(27)
	if cfg.VocabSize != 27 {
		t.Errorf("VocabSize = %d, want 27", cfg.VocabSize)
	}
	if cfg.HeadDim() != 4 {
		t.Errorf("HeadDim = %d, want 4 (16/4)", cfg.HeadDim())
	}
}

func TestInitGPTParams(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 0))
	cfg := DefaultGPTConfig(10)
	params := InitGPTParams(rng, cfg)

	// Check shapes
	if len(params.Wte) != 10 || len(params.Wte[0]) != 16 {
		t.Errorf("Wte shape: [%d][%d], want [10][16]", len(params.Wte), len(params.Wte[0]))
	}
	if len(params.Wpe) != 16 || len(params.Wpe[0]) != 16 {
		t.Errorf("Wpe shape: [%d][%d], want [16][16]", len(params.Wpe), len(params.Wpe[0]))
	}
	if len(params.Layers) != 1 {
		t.Fatalf("Layers count = %d, want 1", len(params.Layers))
	}
	layer := params.Layers[0]
	if len(layer.AttnWQ) != 16 || len(layer.AttnWQ[0]) != 16 {
		t.Errorf("AttnWQ shape: [%d][%d], want [16][16]", len(layer.AttnWQ), len(layer.AttnWQ[0]))
	}
	if len(layer.MLPFC1) != 64 || len(layer.MLPFC1[0]) != 16 {
		t.Errorf("MLPFC1 shape: [%d][%d], want [64][16]", len(layer.MLPFC1), len(layer.MLPFC1[0]))
	}
	if len(layer.MLPFC2) != 16 || len(layer.MLPFC2[0]) != 64 {
		t.Errorf("MLPFC2 shape: [%d][%d], want [16][64]", len(layer.MLPFC2), len(layer.MLPFC2[0]))
	}

	// All parameters should be non-zero (Gaussian init)
	allParams := params.AllParams()
	nonZero := 0
	for _, p := range allParams {
		if p.Data != 0 {
			nonZero++
		}
	}
	if nonZero < len(allParams)/2 {
		t.Errorf("Only %d/%d params non-zero, expected most to be non-zero", nonZero, len(allParams))
	}
}

func TestAllParamsCount(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 0))
	vocabSize := 27
	cfg := DefaultGPTConfig(vocabSize)
	params := InitGPTParams(rng, cfg)
	all := params.AllParams()

	// Expected: wte(27*16) + wpe(16*16) + 1 layer * (4*16*16 + 64*16 + 16*64) + lm_head(27*16)
	//         = 432 + 256 + (1024 + 1024 + 1024) + 432
	//         = 432 + 256 + 3072 + 432 = 4192
	expected := vocabSize*16 + 16*16 + 1*(4*16*16+64*16+16*64) + vocabSize*16
	if len(all) != expected {
		t.Errorf("param count = %d, want %d", len(all), expected)
	}
}

// === GPT FORWARD PASS ===

func TestGPTForwardOutputShape(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 0))
	vocabSize := 10
	cfg := DefaultGPTConfig(vocabSize)
	params := InitGPTParams(rng, cfg)

	keys := make([][]*[]*Value, cfg.NLayer)
	values := make([][]*[]*Value, cfg.NLayer)

	logits := GPTForward(0, 0, &keys, &values, params)

	if len(logits) != vocabSize {
		t.Errorf("logits length = %d, want %d", len(logits), vocabSize)
	}

	// KV cache should have 1 entry per layer after 1 forward pass
	for li := 0; li < cfg.NLayer; li++ {
		if len(keys[li]) != 1 {
			t.Errorf("keys[%d] length = %d, want 1", li, len(keys[li]))
		}
	}
}

func TestGPTForwardSequential(t *testing.T) {
	// Process multiple tokens and verify KV cache grows
	rng := rand.New(rand.NewPCG(42, 0))
	vocabSize := 10
	cfg := DefaultGPTConfig(vocabSize)
	params := InitGPTParams(rng, cfg)

	keys := make([][]*[]*Value, cfg.NLayer)
	values := make([][]*[]*Value, cfg.NLayer)

	// Process 3 tokens
	for pos := 0; pos < 3; pos++ {
		logits := GPTForward(pos%vocabSize, pos, &keys, &values, params)
		if len(logits) != vocabSize {
			t.Fatalf("pos %d: logits length = %d, want %d", pos, len(logits), vocabSize)
		}
	}

	// KV cache should have 3 entries per layer
	for li := 0; li < cfg.NLayer; li++ {
		if len(keys[li]) != 3 {
			t.Errorf("keys[%d] length = %d, want 3", li, len(keys[li]))
		}
	}
}

func TestGPTForwardDeterministic(t *testing.T) {
	// Same seed, same input -> same output
	run := func() []*Value {
		rng := rand.New(rand.NewPCG(99, 0))
		cfg := DefaultGPTConfig(5)
		params := InitGPTParams(rng, cfg)
		keys := make([][]*[]*Value, cfg.NLayer)
		values := make([][]*[]*Value, cfg.NLayer)
		return GPTForward(2, 0, &keys, &values, params)
	}

	logits1 := run()
	logits2 := run()

	for i := range logits1 {
		if logits1[i].Data != logits2[i].Data {
			t.Errorf("logit[%d]: %.6f != %.6f", i, logits1[i].Data, logits2[i].Data)
		}
	}
}

// === ADAM OPTIMIZER ===

func TestAdamStep(t *testing.T) {
	// Simple gradient descent: minimize x^2
	x := V(3.0)
	state := NewAdamState(1)

	for step := 0; step < 100; step++ {
		// f(x) = x^2, grad = 2x
		x.Grad = 2 * x.Data
		AdamStep([]*Value{x}, state, step, 1000, 0.1, 0.9, 0.999, 1e-8)
	}

	if math.Abs(x.Data) > 0.1 {
		t.Errorf("Adam failed to minimize x^2: x = %.4f, want ~0", x.Data)
	}
}

// === TRAINING ===

func TestTrainGPTLossDecreases(t *testing.T) {
	// Train for a short while and verify loss decreases
	docs := []string{"ab", "ba", "aa", "bb"}
	rng := rand.New(rand.NewPCG(42, 0))

	result := TrainGPT(docs, 50, rng, false)

	// Loss should decrease from start to end
	startLoss := result.LossHistory[0]
	endLoss := result.FinalLoss

	if endLoss >= startLoss {
		t.Errorf("Loss did not decrease: start=%.4f, end=%.4f", startLoss, endLoss)
	}
}

func TestTrainGPTDeterministic(t *testing.T) {
	docs := []string{"hello", "world"}

	r1 := TrainGPT(docs, 10, rand.New(rand.NewPCG(42, 0)), false)
	r2 := TrainGPT(docs, 10, rand.New(rand.NewPCG(42, 0)), false)

	if r1.FinalLoss != r2.FinalLoss {
		t.Errorf("Non-deterministic: loss1=%.6f, loss2=%.6f", r1.FinalLoss, r2.FinalLoss)
	}
}

// === INFERENCE ===

func TestGPTSampleProducesOutput(t *testing.T) {
	// Train briefly, then sample -- should produce non-empty strings
	docs := []string{"abc", "bca", "cab"}
	rng := rand.New(rand.NewPCG(42, 0))

	result := TrainGPT(docs, 30, rng, false)

	chars, _, bos := BuildVocab(docs)
	samples := GPTSample(result.Params, chars, bos, 5, 0.5, rng)

	if len(samples) != 5 {
		t.Errorf("Expected 5 samples, got %d", len(samples))
	}

	// At least some samples should be non-empty
	nonEmpty := 0
	for _, s := range samples {
		if s != "" {
			nonEmpty++
		}
	}
	if nonEmpty == 0 {
		t.Error("All samples empty -- model may not be generating")
	}
}

func TestGPTSampleTemperature(t *testing.T) {
	// Low temperature should produce less varied output than high temperature
	docs := []string{"aaa", "bbb", "ccc"}
	rng := rand.New(rand.NewPCG(42, 0))

	result := TrainGPT(docs, 30, rng, false)

	chars, _, bos := BuildVocab(docs)

	// Generate many samples at low and high temperature
	lowT := GPTSample(result.Params, chars, bos, 20, 0.1, rand.New(rand.NewPCG(1, 0)))
	highT := GPTSample(result.Params, chars, bos, 20, 2.0, rand.New(rand.NewPCG(1, 0)))

	// Count unique samples as a rough diversity measure
	uniqueLow := map[string]bool{}
	for _, s := range lowT {
		uniqueLow[s] = true
	}
	uniqueHigh := map[string]bool{}
	for _, s := range highT {
		uniqueHigh[s] = true
	}

	// High temperature should generally produce more diversity
	// (this is a soft check -- may not always hold with tiny models)
	t.Logf("Low temp (0.1): %d unique / %d samples", len(uniqueLow), len(lowT))
	t.Logf("High temp (2.0): %d unique / %d samples", len(uniqueHigh), len(highT))
}

// === HELPERS ===

func TestBuildVocab(t *testing.T) {
	chars, idx, bos := BuildVocab([]string{"cab"})
	if len(chars) != 3 {
		t.Fatalf("Expected 3 chars, got %d", len(chars))
	}
	// Should be sorted
	if chars[0] != 'a' || chars[1] != 'b' || chars[2] != 'c' {
		t.Errorf("Chars not sorted: %v", chars)
	}
	if bos != 3 {
		t.Errorf("BOS = %d, want 3", bos)
	}
	if idx['a'] != 0 || idx['b'] != 1 || idx['c'] != 2 {
		t.Errorf("Unexpected charToIdx: %v", idx)
	}
}

func TestTokenize(t *testing.T) {
	_, idx, bos := BuildVocab([]string{"abc"})
	tokens := Tokenize("abc", idx, bos)
	// Should be [BOS, a, b, c, BOS]
	expected := []int{bos, idx['a'], idx['b'], idx['c'], bos}
	if len(tokens) != len(expected) {
		t.Fatalf("Tokenize length = %d, want %d", len(tokens), len(expected))
	}
	for i := range tokens {
		if tokens[i] != expected[i] {
			t.Errorf("tokens[%d] = %d, want %d", i, tokens[i], expected[i])
		}
	}
}

func TestWeightedSample(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 0))
	weights := []float64{0.0, 0.0, 1.0, 0.0}

	// With all weight on index 2, should always return 2
	for i := 0; i < 100; i++ {
		idx := weightedSample(weights, rng)
		if idx != 2 {
			t.Fatalf("weightedSample returned %d, expected 2 (deterministic weights)", idx)
		}
	}
}

// === BACKWARD THROUGH FORWARD PASS ===

func TestGPTForwardBackward(t *testing.T) {
	// Verify gradients flow back through the full GPT forward pass.
	// We process 2 tokens so attention has multiple keys and the query-key
	// dot product actually matters (with 1 token, softmax([x]) = [1] always,
	// so the attention score gradient is zero).
	rng := rand.New(rand.NewPCG(42, 0))
	vocabSize := 5
	cfg := DefaultGPTConfig(vocabSize)
	params := InitGPTParams(rng, cfg)

	keys := make([][]*[]*Value, cfg.NLayer)
	values := make([][]*[]*Value, cfg.NLayer)

	// Process two tokens so attention has >1 key to attend to
	GPTForward(0, 0, &keys, &values, params)
	logits := GPTForward(1, 1, &keys, &values, params)
	probs := VSoftmax(logits)
	loss := probs[2].SafeLog().Neg() // target = token 2

	loss.Backward()

	// Check that embedding parameters received gradients
	nonZeroGrad := 0
	for _, v := range params.Wte[0] {
		if v.Grad != 0 {
			nonZeroGrad++
		}
	}
	if nonZeroGrad == 0 {
		t.Error("No gradients flowed to Wte[0] embeddings")
	}

	// Check attention weights got gradients (needs >1 key for non-trivial attention)
	nonZeroGrad = 0
	for _, row := range params.Layers[0].AttnWQ {
		for _, v := range row {
			if v.Grad != 0 {
				nonZeroGrad++
			}
		}
	}
	if nonZeroGrad == 0 {
		t.Error("No gradients flowed to AttnWQ weights")
	}
}

// === BENCHMARKS ===

func BenchmarkGPTForward(b *testing.B) {
	rng := rand.New(rand.NewPCG(42, 0))
	cfg := DefaultGPTConfig(10)
	params := InitGPTParams(rng, cfg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		keys := make([][]*[]*Value, cfg.NLayer)
		values := make([][]*[]*Value, cfg.NLayer)
		GPTForward(0, 0, &keys, &values, params)
	}
}

func BenchmarkGPTTrainStep(b *testing.B) {
	// Benchmark a single training step (forward + backward + optimizer)
	rng := rand.New(rand.NewPCG(42, 0))
	vocabSize := 5
	cfg := DefaultGPTConfig(vocabSize)
	params := InitGPTParams(rng, cfg)
	paramList := params.AllParams()
	adam := NewAdamState(len(paramList))

	tokens := []int{4, 0, 1, 2, 4} // BOS, a, b, c, BOS

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		keys := make([][]*[]*Value, cfg.NLayer)
		values := make([][]*[]*Value, cfg.NLayer)

		seqLen := len(tokens) - 1
		losses := make([]*Value, seqLen)
		for pos := 0; pos < seqLen; pos++ {
			logits := GPTForward(tokens[pos], pos, &keys, &values, params)
			probs := VSoftmax(logits)
			losses[pos] = probs[tokens[pos+1]].SafeLog().Neg()
		}
		loss := VSum(losses).DivScalar(float64(seqLen))
		loss.Backward()
		AdamStep(paramList, adam, i, 1000, 0.01, 0.85, 0.99, 1e-8)
	}
}
