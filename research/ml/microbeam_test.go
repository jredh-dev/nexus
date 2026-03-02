//go:build research

package ml

import (
	"math"
	"math/rand/v2"
	"testing"
)

// === TEST HELPERS ===

// trainBeamTestModel trains a small model for beam search tests.
// Uses a tiny corpus and few steps for speed.
func trainBeamTestModel(t *testing.T, bc BeamConfig, steps int) (*FloatGPTParams, []rune, map[rune]int, int) {
	t.Helper()
	docs := []string{
		"emma", "olivia", "ava", "sophia", "isabella",
		"mia", "charlotte", "amelia", "harper", "evelyn",
	}
	chars, charToIdx, bos := BuildVocab(docs)
	rng := rand.New(rand.NewPCG(42, 0))
	result := TrainBeamModel(docs, chars, charToIdx, bos, bc, steps, rng, false)
	fp := ExtractFloatParams(result.Params)
	return fp, chars, charToIdx, bos
}

// === TRAINING TESTS ===

func TestTrainBeamModelTargetLossDecreases(t *testing.T) {
	docs := []string{"emma", "olivia", "ava", "sophia", "mia"}
	chars, charToIdx, bos := BuildVocab(docs)
	rng := rand.New(rand.NewPCG(42, 0))
	result := TrainBeamModel(docs, chars, charToIdx, bos, TargetBeamConfig(), 200, rng, false)

	if result.FinalLoss >= result.LossHistory[0] {
		t.Errorf("target model loss did not decrease: initial=%.4f final=%.4f",
			result.LossHistory[0], result.FinalLoss)
	}
}

func TestTrainBeamModelDraftLossDecreases(t *testing.T) {
	docs := []string{"emma", "olivia", "ava", "sophia", "mia"}
	chars, charToIdx, bos := BuildVocab(docs)
	rng := rand.New(rand.NewPCG(42, 0))
	result := TrainBeamModel(docs, chars, charToIdx, bos, DraftBeamConfig(), 200, rng, false)

	if result.FinalLoss >= result.LossHistory[0] {
		t.Errorf("draft model loss did not decrease: initial=%.4f final=%.4f",
			result.LossHistory[0], result.FinalLoss)
	}
}

func TestTrainBeamModelDifferentSizes(t *testing.T) {
	docs := []string{"emma", "olivia", "ava"}
	chars, charToIdx, bos := BuildVocab(docs)
	rng := rand.New(rand.NewPCG(42, 0))

	targetResult := TrainBeamModel(docs, chars, charToIdx, bos, TargetBeamConfig(), 50, rng, false)
	draftResult := TrainBeamModel(docs, chars, charToIdx, bos, DraftBeamConfig(), 50, rng, false)

	targetParams := len(targetResult.Params.AllParams())
	draftParams := len(draftResult.Params.AllParams())

	if targetParams <= draftParams {
		t.Errorf("target model should have more params than draft: target=%d draft=%d",
			targetParams, draftParams)
	}
}

// === FORWARD PASS TESTS ===

func TestBeamForwardProducesLogits(t *testing.T) {
	fp, _, _, bos := trainBeamTestModel(t, TargetBeamConfig(), 50)
	keys := MakeBeamKV(fp.Config.NLayer)
	values := MakeBeamKV(fp.Config.NLayer)

	logits := beamForward(bos, 0, &keys, &values, fp)

	if len(logits) != fp.Config.VocabSize {
		t.Errorf("expected %d logits, got %d", fp.Config.VocabSize, len(logits))
	}

	// KV cache should have 1 entry per layer
	for li := range keys {
		if len(keys[li]) != 1 {
			t.Errorf("layer %d: expected 1 key entry, got %d", li, len(keys[li]))
		}
	}
}

func TestFeedPromptBuildsKVCache(t *testing.T) {
	fp, _, charToIdx, bos := trainBeamTestModel(t, TargetBeamConfig(), 50)
	prompt := []int{bos, charToIdx['a']}
	keys, values, logits := feedPrompt(prompt, fp)

	if len(logits) != fp.Config.VocabSize {
		t.Errorf("expected %d logits, got %d", fp.Config.VocabSize, len(logits))
	}
	for li := range keys {
		if len(keys[li]) != len(prompt) {
			t.Errorf("layer %d: expected %d key entries, got %d", li, len(prompt), len(keys[li]))
		}
		if len(values[li]) != len(prompt) {
			t.Errorf("layer %d: expected %d value entries, got %d", li, len(prompt), len(values[li]))
		}
	}
}

// === CLONE KV TESTS ===

func TestCloneBeamKVIsDeepCopy(t *testing.T) {
	kv := MakeBeamKV(2)
	kv[0] = append(kv[0], []float64{1.0, 2.0, 3.0})
	kv[1] = append(kv[1], []float64{4.0, 5.0, 6.0})

	cloned := CloneBeamKV(kv)

	// Modify original
	kv[0][0][0] = 999.0

	// Clone should be unaffected
	if cloned[0][0][0] != 1.0 {
		t.Errorf("clone was affected by original mutation: got %f, want 1.0", cloned[0][0][0])
	}
}

// === DECODING STRATEGY TESTS ===

func TestDecodeGreedyDeterministic(t *testing.T) {
	fp, _, charToIdx, bos := trainBeamTestModel(t, TargetBeamConfig(), 100)
	prompt := []int{bos, charToIdx['a']}

	r1 := DecodeGreedy(prompt, fp, 12)
	r2 := DecodeGreedy(prompt, fp, 12)

	if len(r1.Tokens) != len(r2.Tokens) {
		t.Fatalf("greedy should be deterministic: got different lengths %d vs %d",
			len(r1.Tokens), len(r2.Tokens))
	}
	for i := range r1.Tokens {
		if r1.Tokens[i] != r2.Tokens[i] {
			t.Errorf("greedy token %d differs: %d vs %d", i, r1.Tokens[i], r2.Tokens[i])
		}
	}
	if r1.LogProb != r2.LogProb {
		t.Errorf("greedy log-prob differs: %f vs %f", r1.LogProb, r2.LogProb)
	}
}

func TestDecodeGreedyProducesTokens(t *testing.T) {
	fp, _, charToIdx, bos := trainBeamTestModel(t, TargetBeamConfig(), 100)
	prompt := []int{bos, charToIdx['a']}
	r := DecodeGreedy(prompt, fp, 12)

	if len(r.Tokens) == 0 {
		t.Error("greedy produced no tokens")
	}
	if r.LogProb >= 0 {
		t.Errorf("log-prob should be negative, got %f", r.LogProb)
	}
}

func TestDecodeTemperatureProducesTokens(t *testing.T) {
	fp, _, charToIdx, bos := trainBeamTestModel(t, TargetBeamConfig(), 100)
	prompt := []int{bos, charToIdx['a']}
	rng := rand.New(rand.NewPCG(99, 0))

	r := DecodeTemperature(prompt, fp, 12, 0.8, rng)
	if len(r.Tokens) == 0 {
		t.Error("temperature sampling produced no tokens")
	}
}

func TestDecodeTemperatureDiversityVsGreedy(t *testing.T) {
	fp, _, charToIdx, bos := trainBeamTestModel(t, TargetBeamConfig(), 200)

	// Generate multiple samples with temperature and greedy
	nSamples := 10
	greedyResults := make(map[string]bool)
	tempResults := make(map[string]bool)

	prompt := []int{bos, charToIdx['a']}
	for i := 0; i < nSamples; i++ {
		g := DecodeGreedy(prompt, fp, 12)
		greedyResults[tokensToKey(g.Tokens)] = true

		rng := rand.New(rand.NewPCG(uint64(i*31), 0))
		te := DecodeTemperature(prompt, fp, 12, 1.2, rng)
		tempResults[tokensToKey(te.Tokens)] = true
	}

	// Greedy should always produce the same output
	if len(greedyResults) != 1 {
		t.Errorf("greedy should produce 1 unique result, got %d", len(greedyResults))
	}
}

func TestDecodeTopKProducesTokens(t *testing.T) {
	fp, _, charToIdx, bos := trainBeamTestModel(t, TargetBeamConfig(), 100)
	prompt := []int{bos, charToIdx['a']}
	rng := rand.New(rand.NewPCG(99, 0))

	r := DecodeTopK(prompt, fp, 12, 5, rng)
	if len(r.Tokens) == 0 {
		t.Error("top-k produced no tokens")
	}
}

func TestDecodeTopPProducesTokens(t *testing.T) {
	fp, _, charToIdx, bos := trainBeamTestModel(t, TargetBeamConfig(), 100)
	prompt := []int{bos, charToIdx['a']}
	rng := rand.New(rand.NewPCG(99, 0))

	r := DecodeTopP(prompt, fp, 12, 0.9, rng)
	if len(r.Tokens) == 0 {
		t.Error("top-p produced no tokens")
	}
}

func TestDecodeBeamDeterministic(t *testing.T) {
	fp, _, charToIdx, bos := trainBeamTestModel(t, TargetBeamConfig(), 100)
	prompt := []int{bos, charToIdx['a']}

	r1 := DecodeBeam(prompt, fp, 12, 3)
	r2 := DecodeBeam(prompt, fp, 12, 3)

	if len(r1.Tokens) != len(r2.Tokens) {
		t.Fatalf("beam should be deterministic: got different lengths %d vs %d",
			len(r1.Tokens), len(r2.Tokens))
	}
	for i := range r1.Tokens {
		if r1.Tokens[i] != r2.Tokens[i] {
			t.Errorf("beam token %d differs: %d vs %d", i, r1.Tokens[i], r2.Tokens[i])
		}
	}
}

func TestDecodeBeamAtLeastAsGoodAsGreedy(t *testing.T) {
	fp, _, charToIdx, bos := trainBeamTestModel(t, TargetBeamConfig(), 200)
	prompt := []int{bos, charToIdx['a']}

	greedy := DecodeGreedy(prompt, fp, 12)
	beam := DecodeBeam(prompt, fp, 12, 3)

	// Beam search explores multiple paths, should find equal or better log-prob
	if beam.LogProb < greedy.LogProb-0.1 {
		t.Errorf("beam search (%.4f) significantly worse than greedy (%.4f)",
			beam.LogProb, greedy.LogProb)
	}
}

func TestDecodeBeamWidthEffect(t *testing.T) {
	fp, _, charToIdx, bos := trainBeamTestModel(t, TargetBeamConfig(), 200)
	prompt := []int{bos, charToIdx['a']}

	// Width 1 = greedy (in terms of search behavior)
	beam1 := DecodeBeam(prompt, fp, 12, 1)
	beam5 := DecodeBeam(prompt, fp, 12, 5)

	// Wider beam should find equal or better log-prob
	if beam5.LogProb < beam1.LogProb-0.1 {
		t.Errorf("wider beam (%.4f) significantly worse than beam-1 (%.4f)",
			beam5.LogProb, beam1.LogProb)
	}
}

func TestDecodeSpeculativeProducesTokens(t *testing.T) {
	targetFP, chars, charToIdx, bos := trainBeamTestModel(t, TargetBeamConfig(), 100)

	// Train draft model
	docs := []string{"emma", "olivia", "ava", "sophia", "isabella",
		"mia", "charlotte", "amelia", "harper", "evelyn"}
	rng := rand.New(rand.NewPCG(99, 0))
	draftResult := TrainBeamModel(docs, chars, charToIdx, bos, DraftBeamConfig(), 100, rng, false)
	draftFP := ExtractFloatParams(draftResult.Params)

	prompt := []int{bos, charToIdx['a']}
	r := DecodeSpeculative(prompt, targetFP, draftFP, 12, 4, rng)

	if len(r.Tokens) == 0 {
		t.Error("speculative decoding produced no tokens")
	}
}

func TestDecodeSpeculativeAcceptanceTracking(t *testing.T) {
	targetFP, chars, charToIdx, bos := trainBeamTestModel(t, TargetBeamConfig(), 100)

	docs := []string{"emma", "olivia", "ava", "sophia", "isabella",
		"mia", "charlotte", "amelia", "harper", "evelyn"}
	rng := rand.New(rand.NewPCG(99, 0))
	draftResult := TrainBeamModel(docs, chars, charToIdx, bos, DraftBeamConfig(), 100, rng, false)
	draftFP := ExtractFloatParams(draftResult.Params)

	prompt := []int{bos, charToIdx['a']}
	r := DecodeSpeculative(prompt, targetFP, draftFP, 12, 4, rng)

	// Accepted should not exceed proposed
	if r.Accepted > r.Proposed {
		t.Errorf("accepted (%d) exceeds proposed (%d)", r.Accepted, r.Proposed)
	}
	// Should have generated at least as many tokens as accepted
	if len(r.Tokens) < r.Accepted {
		t.Errorf("fewer tokens (%d) than accepted (%d)", len(r.Tokens), r.Accepted)
	}
}

// === LOG-PROB VALIDITY TESTS ===

func TestAllStrategiesNegativeLogProb(t *testing.T) {
	fp, chars, charToIdx, bos := trainBeamTestModel(t, TargetBeamConfig(), 100)
	prompt := []int{bos, charToIdx['a']}
	rng := rand.New(rand.NewPCG(42, 0))

	strategies := []struct {
		name string
		fn   func() DecodeResult
	}{
		{"Greedy", func() DecodeResult { return DecodeGreedy(prompt, fp, 12) }},
		{"Temperature", func() DecodeResult { return DecodeTemperature(prompt, fp, 12, 0.8, rng) }},
		{"Top-K", func() DecodeResult { return DecodeTopK(prompt, fp, 12, 5, rng) }},
		{"Top-P", func() DecodeResult { return DecodeTopP(prompt, fp, 12, 0.9, rng) }},
		{"Beam", func() DecodeResult { return DecodeBeam(prompt, fp, 12, 3) }},
	}

	for _, s := range strategies {
		r := s.fn()
		if len(r.Tokens) > 0 && r.LogProb >= 0 {
			t.Errorf("%s: log-prob should be negative for non-empty output, got %f", s.name, r.LogProb)
		}
	}

	// Speculative
	docs := []string{"emma", "olivia", "ava", "sophia", "isabella",
		"mia", "charlotte", "amelia", "harper", "evelyn"}
	draftResult := TrainBeamModel(docs, chars, charToIdx, bos, DraftBeamConfig(), 100, rng, false)
	draftFP := ExtractFloatParams(draftResult.Params)
	sr := DecodeSpeculative(prompt, fp, draftFP, 12, 4, rng)
	if len(sr.Tokens) > 0 && sr.LogProb >= 0 {
		t.Errorf("Speculative: log-prob should be negative, got %f", sr.LogProb)
	}
}

// === MAX LENGTH CONSTRAINT TESTS ===

func TestDecodeGreedyMaxLen(t *testing.T) {
	fp, _, charToIdx, bos := trainBeamTestModel(t, TargetBeamConfig(), 100)
	prompt := []int{bos, charToIdx['a']}

	r := DecodeGreedy(prompt, fp, 3)
	if len(r.Tokens) > 3 {
		t.Errorf("greedy exceeded maxLen=3: got %d tokens", len(r.Tokens))
	}
}

func TestDecodeBeamMaxLen(t *testing.T) {
	fp, _, charToIdx, bos := trainBeamTestModel(t, TargetBeamConfig(), 100)
	prompt := []int{bos, charToIdx['a']}

	r := DecodeBeam(prompt, fp, 3, 3)
	if len(r.Tokens) > 3 {
		t.Errorf("beam exceeded maxLen=3: got %d tokens", len(r.Tokens))
	}
}

// === ARGMAX / ARGSORT TESTS ===

func TestArgmax(t *testing.T) {
	tests := []struct {
		input    []float64
		expected int
	}{
		{[]float64{1, 3, 2}, 1},
		{[]float64{5, 1, 2}, 0},
		{[]float64{1, 2, 5}, 2},
		{[]float64{1}, 0},
	}
	for _, tt := range tests {
		got := argmax(tt.input)
		if got != tt.expected {
			t.Errorf("argmax(%v) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestArgsortDesc(t *testing.T) {
	input := []float64{0.1, 0.5, 0.3, 0.8, 0.2}
	sorted := argsortDesc(input)

	// Should be sorted by descending value
	for i := 1; i < len(sorted); i++ {
		if input[sorted[i]] > input[sorted[i-1]] {
			t.Errorf("argsortDesc not sorted at position %d: value %f > %f",
				i, input[sorted[i]], input[sorted[i-1]])
		}
	}
	// First should be index 3 (0.8)
	if sorted[0] != 3 {
		t.Errorf("expected index 3 first, got %d", sorted[0])
	}
}

// === BEAM CONFIG TESTS ===

func TestBeamConfigToGPTConfig(t *testing.T) {
	bc := TargetBeamConfig()
	cfg := bc.ToGPTConfig(27)

	if cfg.NEmbd != beamTargetNEmbd {
		t.Errorf("NEmbd = %d, want %d", cfg.NEmbd, beamTargetNEmbd)
	}
	if cfg.NHead != beamTargetNHead {
		t.Errorf("NHead = %d, want %d", cfg.NHead, beamTargetNHead)
	}
	if cfg.BlockSize != beamBlockSize {
		t.Errorf("BlockSize = %d, want %d", cfg.BlockSize, beamBlockSize)
	}
	if cfg.VocabSize != 27 {
		t.Errorf("VocabSize = %d, want 27", cfg.VocabSize)
	}
}

func TestDraftBeamConfigSmaller(t *testing.T) {
	target := TargetBeamConfig()
	draft := DraftBeamConfig()

	if draft.NEmbd >= target.NEmbd {
		t.Errorf("draft NEmbd (%d) should be smaller than target (%d)", draft.NEmbd, target.NEmbd)
	}
	if draft.NHead >= target.NHead {
		t.Errorf("draft NHead (%d) should be smaller than target (%d)", draft.NHead, target.NHead)
	}
}

// === BENCHMARKS ===

func BenchmarkDecodeGreedy(b *testing.B) {
	docs := []string{"emma", "olivia", "ava", "sophia", "isabella",
		"mia", "charlotte", "amelia", "harper", "evelyn"}
	chars, charToIdx, bos := BuildVocab(docs)
	rng := rand.New(rand.NewPCG(42, 0))
	result := TrainBeamModel(docs, chars, charToIdx, bos, TargetBeamConfig(), 50, rng, false)
	fp := ExtractFloatParams(result.Params)
	prompt := []int{bos, charToIdx['a']}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeGreedy(prompt, fp, 12)
	}
}

func BenchmarkDecodeBeam(b *testing.B) {
	docs := []string{"emma", "olivia", "ava", "sophia", "isabella",
		"mia", "charlotte", "amelia", "harper", "evelyn"}
	chars, charToIdx, bos := BuildVocab(docs)
	rng := rand.New(rand.NewPCG(42, 0))
	result := TrainBeamModel(docs, chars, charToIdx, bos, TargetBeamConfig(), 50, rng, false)
	fp := ExtractFloatParams(result.Params)
	prompt := []int{bos, charToIdx['a']}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeBeam(prompt, fp, 12, 3)
	}
}

func BenchmarkDecodeSpeculative(b *testing.B) {
	docs := []string{"emma", "olivia", "ava", "sophia", "isabella",
		"mia", "charlotte", "amelia", "harper", "evelyn"}
	chars, charToIdx, bos := BuildVocab(docs)
	rng := rand.New(rand.NewPCG(42, 0))
	targetResult := TrainBeamModel(docs, chars, charToIdx, bos, TargetBeamConfig(), 50, rng, false)
	draftResult := TrainBeamModel(docs, chars, charToIdx, bos, DraftBeamConfig(), 50, rng, false)
	targetFP := ExtractFloatParams(targetResult.Params)
	draftFP := ExtractFloatParams(draftResult.Params)
	prompt := []int{bos, charToIdx['a']}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rng2 := rand.New(rand.NewPCG(uint64(i), 0))
		DecodeSpeculative(prompt, targetFP, draftFP, 12, 4, rng2)
	}
}

// === HELPER ===

func tokensToKey(tokens []int) string {
	key := make([]byte, len(tokens))
	for i, t := range tokens {
		key[i] = byte(t)
	}
	return string(key)
}

// Silence unused import
var _ = math.Abs
