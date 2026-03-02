//go:build research

// Beyond greedy: six decoding strategies for language model text generation, from deterministic
// argmax to speculative decoding with a draft-verify two-model pipeline.
//
// Reference: Leviathan et al., "Fast Inference from Transformers via Speculative
// Decoding" (2023). https://arxiv.org/abs/2211.17192
// Also: Holtzman et al., "The Curious Case of Neural Text Degeneration" (2019).
// https://arxiv.org/abs/1904.09751 (nucleus/top-p sampling)
//
// Port of microbeam.py from mathews-tom/no-magic to Go.
// AGPL-3.0 -- see LICENSE in repo root.
package ml

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
)

// === CONSTANTS AND HYPERPARAMETERS ===

// Target model (larger, ~4,200 params) and draft model (smaller, ~1,300 params).
// Both share vocabulary and block_size -- required for speculative decoding since
// the draft model must produce tokens the target model can verify.
const (
	beamTargetNEmbd  = 16
	beamTargetNHead  = 4
	beamTargetNLayer = 1
	beamDraftNEmbd   = 8
	beamDraftNHead   = 2
	beamDraftNLayer  = 1
	beamBlockSize    = 16

	// Training
	beamLR          = 0.01
	beamBeta1       = 0.85
	beamBeta2       = 0.99
	beamEpsAdam     = 1e-8
	beamTargetSteps = 700
	beamDraftSteps  = 500
)

// Signpost: production speculative decoding pairs a 70B target with a 7B draft.
// Our 4,200 / 1,300 param ratio preserves the algorithmic structure. Real speedups
// come from GPU parallelism during the verify pass -- here we measure acceptance
// rate, which is the hardware-independent metric that matters.

// === BEAM CONFIG ===

// BeamConfig holds model dimensions for a target or draft model.
type BeamConfig struct {
	NEmbd   int
	NHead   int
	NLayer  int
	HeadDim int
}

// TargetBeamConfig returns the target (larger) model config.
func TargetBeamConfig() BeamConfig {
	return BeamConfig{
		NEmbd:   beamTargetNEmbd,
		NHead:   beamTargetNHead,
		NLayer:  beamTargetNLayer,
		HeadDim: beamTargetNEmbd / beamTargetNHead,
	}
}

// DraftBeamConfig returns the draft (smaller) model config.
func DraftBeamConfig() BeamConfig {
	return BeamConfig{
		NEmbd:   beamDraftNEmbd,
		NHead:   beamDraftNHead,
		NLayer:  beamDraftNLayer,
		HeadDim: beamDraftNEmbd / beamDraftNHead,
	}
}

// ToGPTConfig converts a BeamConfig to a GPTConfig for a given vocab size.
func (bc BeamConfig) ToGPTConfig(vocabSize int) GPTConfig {
	return GPTConfig{
		NEmbd:     bc.NEmbd,
		NHead:     bc.NHead,
		NLayer:    bc.NLayer,
		BlockSize: beamBlockSize,
		VocabSize: vocabSize,
		InitStd:   gptInitStd,
	}
}

// === TRAINING ===
// Uses the existing autograd engine (value.go) and GPT architecture (microgpt.go).
// The only difference from TrainGPT is configurable model dimensions.

// TrainBeamModel trains a GPT model with the given config and training parameters.
// Returns the trained params and final loss.
func TrainBeamModel(docs []string, chars []rune, charToIdx map[rune]int, bos int,
	bc BeamConfig, steps int, rng *rand.Rand, verbose bool) *GPTTrainResult {

	vocabSize := len(chars) + 1
	cfg := bc.ToGPTConfig(vocabSize)

	params := InitGPTParams(rng, cfg)
	paramList := params.AllParams()
	if verbose {
		fmt.Printf("Parameters: %d\n", len(paramList))
	}

	adam := NewAdamState(len(paramList))

	shuffled := make([]string, len(docs))
	copy(shuffled, docs)
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	lossHistory := make([]float64, steps)
	var lastLoss float64

	for step := 0; step < steps; step++ {
		doc := shuffled[step%len(shuffled)]
		tokens := Tokenize(doc, charToIdx, bos)

		seqLen := cfg.BlockSize
		if len(tokens)-1 < seqLen {
			seqLen = len(tokens) - 1
		}

		keys := make([][]*[]*Value, cfg.NLayer)
		values := make([][]*[]*Value, cfg.NLayer)

		losses := make([]*Value, seqLen)
		for pos := 0; pos < seqLen; pos++ {
			logits := GPTForward(tokens[pos], pos, &keys, &values, params)
			probs := VSoftmax(logits)
			losses[pos] = probs[tokens[pos+1]].SafeLog().Neg()
		}

		loss := VSum(losses).DivScalar(float64(seqLen))
		loss.Backward()

		lrT := beamLR * (1.0 - float64(step)/float64(steps))
		for i, p := range paramList {
			g := p.Grad
			adam.M[i] = beamBeta1*adam.M[i] + (1-beamBeta1)*g
			adam.V[i] = beamBeta2*adam.V[i] + (1-beamBeta2)*g*g
			mHat := adam.M[i] / (1 - math.Pow(beamBeta1, float64(step+1)))
			vHat := adam.V[i] / (1 - math.Pow(beamBeta2, float64(step+1)))
			p.Data -= lrT * mHat / (math.Sqrt(vHat) + beamEpsAdam)
			p.Grad = 0.0
		}

		lastLoss = loss.Data
		lossHistory[step] = lastLoss

		if verbose && ((step+1)%100 == 0 || step == 0) {
			fmt.Printf("  step %4d/%4d | loss: %.4f\n", step+1, steps, lastLoss)
		}
	}

	if verbose {
		fmt.Printf("  Final loss: %.4f\n\n", lastLoss)
	}

	return &GPTTrainResult{
		Params:      params,
		FinalLoss:   lastLoss,
		LossHistory: lossHistory,
	}
}

// === FLOAT FORWARD PASS HELPERS ===
// After training, weights are extracted as plain floats. All six decoding
// strategies operate here -- no autograd overhead, enabling clean comparison.

// BeamKV is a KV cache for float-based inference: [nLayer] of {k, v} lists.
type BeamKV = [][]float64Slice

// MakeBeamKV creates a fresh empty KV cache for nLayer layers.
func MakeBeamKV(nLayer int) BeamKV {
	return make(BeamKV, nLayer)
}

// CloneBeamKV deep-copies a KV cache so beam branches don't share mutable state.
func CloneBeamKV(cache BeamKV) BeamKV {
	out := make(BeamKV, len(cache))
	for li, layer := range cache {
		out[li] = make([]float64Slice, len(layer))
		for t, vec := range layer {
			cp := make([]float64, len(vec))
			copy(cp, vec)
			out[li][t] = cp
		}
	}
	return out
}

// beamForward runs the float GPT forward pass, reusing GPTForwardFloat from microquant.go.
// Note: GPTForwardFloat uses a KV cache where keys and values are interleaved in a single
// [][]float64Slice per layer. For beam search we need separate key/value caches since
// CloneBeamKV needs to deep-copy them independently for each beam branch.
//
// We use a lightweight wrapper that manages two parallel caches (keys + values) and calls
// the same linear/softmax/rmsnorm primitives.
func beamForward(tokenID, posID int, keys, values *BeamKV, fp *FloatGPTParams) []float64 {
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
				dot := 0.0
				for j := 0; j < headDim; j++ {
					dot += qHead[j] * cachedK[t][hs+j]
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
				x[i] = 0
			}
		}
		x = LinearFloat(x, layer.MLPFC2)
		for i := range x {
			x[i] += xResidual[i]
		}
	}

	return LinearFloat(x, fp.LMHead)
}

// feedPrompt processes prompt tokens through the model, returning (keys, values, lastLogits).
func feedPrompt(prompt []int, fp *FloatGPTParams) (BeamKV, BeamKV, []float64) {
	keys := MakeBeamKV(fp.Config.NLayer)
	values := MakeBeamKV(fp.Config.NLayer)
	var logits []float64
	for i, tok := range prompt {
		logits = beamForward(tok, i, &keys, &values, fp)
	}
	return keys, values, logits
}

// === DECODING STRATEGIES ===
// Each strategy takes a prompt, weights, and config, returns generated tokens
// plus total log-probability. They differ ONLY in token selection.

// DecodeResult holds the output of a decoding strategy.
type DecodeResult struct {
	Tokens   []int
	LogProb  float64
	Proposed int // speculative decoding: total draft tokens proposed
	Accepted int // speculative decoding: total draft tokens accepted
}

// DecodeGreedy always picks the highest-probability token. Deterministic.
//
// Simple but suboptimal: commits to the locally best choice at each step,
// which can miss globally better sequences. Greedy decoding is optimal only
// when the model is perfectly calibrated (it never is).
func DecodeGreedy(prompt []int, fp *FloatGPTParams, maxLen int) DecodeResult {
	keys, values, logits := feedPrompt(prompt, fp)
	var gen []int
	lp := 0.0
	for i := 0; i < maxLen; i++ {
		pos := len(prompt) + len(gen)
		if pos >= fp.Config.BlockSize {
			break
		}
		probs := SoftmaxFloat(logits)
		tok := argmax(probs)
		if tok == fp.Config.VocabSize-1 { // BOS = vocabSize-1
			break
		}
		lp += math.Log(math.Max(probs[tok], 1e-10))
		gen = append(gen, tok)
		logits = beamForward(tok, pos, &keys, &values, fp)
	}
	return DecodeResult{Tokens: gen, LogProb: lp}
}

// DecodeTemperature scales logits by temperature before sampling.
//
// Temperature reshapes the probability distribution without changing its
// ranking. T < 1 sharpens (more deterministic), T > 1 flattens (more random).
// The math: softmax(logits/T) concentrates mass on the mode as T -> 0
// and approaches uniform as T -> inf.
func DecodeTemperature(prompt []int, fp *FloatGPTParams, maxLen int,
	temperature float64, rng *rand.Rand) DecodeResult {

	keys, values, logits := feedPrompt(prompt, fp)
	var gen []int
	lp := 0.0
	for i := 0; i < maxLen; i++ {
		pos := len(prompt) + len(gen)
		if pos >= fp.Config.BlockSize {
			break
		}
		scaled := make([]float64, len(logits))
		for j, l := range logits {
			scaled[j] = l / temperature
		}
		probs := SoftmaxFloat(scaled)
		tok := weightedSampleFloat(probs, rng)
		if tok == fp.Config.VocabSize-1 {
			break
		}
		lp += math.Log(math.Max(probs[tok], 1e-10))
		gen = append(gen, tok)
		logits = beamForward(tok, pos, &keys, &values, fp)
	}
	return DecodeResult{Tokens: gen, LogProb: lp}
}

// DecodeTopK only considers the k most likely tokens, zeroes out the rest.
//
// Prevents sampling from the long tail of unlikely tokens. The cutoff is
// fixed regardless of the model's confidence -- this rigidity is top-k's
// weakness compared to top-p.
func DecodeTopK(prompt []int, fp *FloatGPTParams, maxLen, k int,
	rng *rand.Rand) DecodeResult {

	keys, values, logits := feedPrompt(prompt, fp)
	bos := fp.Config.VocabSize - 1
	var gen []int
	lp := 0.0
	for i := 0; i < maxLen; i++ {
		pos := len(prompt) + len(gen)
		if pos >= fp.Config.BlockSize {
			break
		}
		probs := SoftmaxFloat(logits)
		ranked := argsortDesc(probs)
		topSet := make(map[int]bool, k)
		for j := 0; j < k && j < len(ranked); j++ {
			topSet[ranked[j]] = true
		}
		filt := make([]float64, len(probs))
		total := 0.0
		for j := range probs {
			if topSet[j] {
				filt[j] = probs[j]
				total += probs[j]
			}
		}
		for j := range filt {
			filt[j] /= total
		}
		tok := weightedSampleFloat(filt, rng)
		if tok == bos {
			break
		}
		// Log-prob from the ORIGINAL distribution -- measures model confidence
		lp += math.Log(math.Max(probs[tok], 1e-10))
		gen = append(gen, tok)
		logits = beamForward(tok, pos, &keys, &values, fp)
	}
	return DecodeResult{Tokens: gen, LogProb: lp}
}

// DecodeTopP includes tokens until cumulative probability exceeds p (nucleus sampling).
//
// Adaptive: for confident predictions (one token at 95%), only that token
// is considered. For uncertain predictions, many tokens enter the nucleus.
// This adaptivity is why top-p often outperforms fixed top-k -- the model's
// own confidence determines the effective vocabulary size at each step.
func DecodeTopP(prompt []int, fp *FloatGPTParams, maxLen int,
	p float64, rng *rand.Rand) DecodeResult {

	keys, values, logits := feedPrompt(prompt, fp)
	bos := fp.Config.VocabSize - 1
	var gen []int
	lp := 0.0
	for i := 0; i < maxLen; i++ {
		pos := len(prompt) + len(gen)
		if pos >= fp.Config.BlockSize {
			break
		}
		probs := SoftmaxFloat(logits)
		ranked := argsortDesc(probs)
		cumsum := 0.0
		nucleus := make(map[int]bool)
		for _, idx := range ranked {
			nucleus[idx] = true
			cumsum += probs[idx]
			if cumsum >= p {
				break
			}
		}
		filt := make([]float64, len(probs))
		total := 0.0
		for j := range probs {
			if nucleus[j] {
				filt[j] = probs[j]
				total += probs[j]
			}
		}
		for j := range filt {
			filt[j] /= total
		}
		tok := weightedSampleFloat(filt, rng)
		if tok == bos {
			break
		}
		lp += math.Log(math.Max(probs[tok], 1e-10))
		gen = append(gen, tok)
		logits = beamForward(tok, pos, &keys, &values, fp)
	}
	return DecodeResult{Tokens: gen, LogProb: lp}
}

// DecodeBeam maintains top-B candidate sequences, expanding and pruning at each step.
//
// Finds higher log-probability sequences than greedy by exploring multiple
// paths simultaneously. Beam search is NOT sampling -- it is a deterministic
// search algorithm. Two runs with the same input produce identical output.
// The key tradeoff: beam_width * cost_per_step compute for potentially much
// better global solutions. Used heavily in machine translation.
func DecodeBeam(prompt []int, fp *FloatGPTParams, maxLen, beamWidth int) DecodeResult {
	bos := fp.Config.VocabSize - 1

	// Each beam: (cumLogProb, tokens, keys, values, pendingLogits)
	type beam struct {
		logProb float64
		tokens  []int
		keys    BeamKV
		values  BeamKV
		logits  []float64
	}

	initKeys, initValues, initLogits := feedPrompt(prompt, fp)
	beams := []beam{{
		logProb: 0.0,
		tokens:  nil,
		keys:    CloneBeamKV(initKeys),
		values:  CloneBeamKV(initValues),
		logits:  initLogits,
	}}

	type completed struct {
		logProb float64
		tokens  []int
	}
	var done []completed

	for step := 0; step < maxLen; step++ {
		var candidates []beam
		for _, b := range beams {
			pos := len(prompt) + len(b.tokens)
			if pos >= fp.Config.BlockSize {
				done = append(done, completed{b.logProb, b.tokens})
				continue
			}
			probs := SoftmaxFloat(b.logits)
			ranked := argsortDesc(probs)
			for _, idx := range ranked[:min(beamWidth, len(ranked))] {
				tokenLP := math.Log(math.Max(probs[idx], 1e-10))
				if idx == bos {
					done = append(done, completed{b.logProb + tokenLP, b.tokens})
					continue
				}
				// Each expansion gets its own KV cache copy (beams diverge)
				newKeys := CloneBeamKV(b.keys)
				newValues := CloneBeamKV(b.values)
				newLogits := beamForward(idx, pos, &newKeys, &newValues, fp)
				newToks := make([]int, len(b.tokens)+1)
				copy(newToks, b.tokens)
				newToks[len(b.tokens)] = idx
				candidates = append(candidates, beam{
					logProb: b.logProb + tokenLP,
					tokens:  newToks,
					keys:    newKeys,
					values:  newValues,
					logits:  newLogits,
				})
			}
		}
		if len(candidates) == 0 {
			break
		}
		// Prune: keep only top beamWidth by cumulative log-prob
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].logProb > candidates[j].logProb
		})
		if len(candidates) > beamWidth {
			candidates = candidates[:beamWidth]
		}
		beams = candidates
	}

	// Collect all results (completed + remaining beams)
	for _, b := range beams {
		done = append(done, completed{b.logProb, b.tokens})
	}
	if len(done) == 0 {
		return DecodeResult{}
	}
	best := done[0]
	for _, c := range done[1:] {
		if c.logProb > best.logProb {
			best = c
		}
	}
	return DecodeResult{Tokens: best.tokens, LogProb: best.logProb}
}

// DecodeSpeculative uses a draft model to generate k tokens, then the target model verifies.
//
// The key insight: verifying k tokens with the target model costs roughly
// the same as generating 1 token (on a GPU, k forward passes batch into one).
// If the draft tokens match the target's distribution, we get ~k tokens per
// target verification -- a significant speedup.
//
// Acceptance (Leviathan et al.): accept each draft token with probability
// min(1, p_target/p_draft). On rejection, resample from max(0, p_target -
// p_draft) and discard subsequent drafts. This is lossless: the output
// distribution exactly matches the target model.
//
// Returns DecodeResult with Proposed and Accepted counts.
func DecodeSpeculative(prompt []int, targetFP, draftFP *FloatGPTParams,
	maxLen, draftK int, rng *rand.Rand) DecodeResult {

	bos := targetFP.Config.VocabSize - 1

	tKeys, tValues, tLogits := feedPrompt(prompt, targetFP)
	dKeys, dValues, dLogits := feedPrompt(prompt, draftFP)

	var gen []int
	lp := 0.0
	totalProposed := 0
	totalAccepted := 0

	for len(gen) < maxLen {
		cur := len(prompt) + len(gen)
		remaining := draftK
		if maxLen-len(gen) < remaining {
			remaining = maxLen - len(gen)
		}
		if cur >= targetFP.Config.BlockSize || remaining <= 0 {
			break
		}

		// Phase 1: Draft model proposes k tokens greedily (fast, small model)
		var draftToks []int
		var draftProbs [][]float64
		tmpDKeys := CloneBeamKV(dKeys)
		tmpDValues := CloneBeamKV(dValues)
		tmpDLogits := make([]float64, len(dLogits))
		copy(tmpDLogits, dLogits)

		for di := 0; di < remaining; di++ {
			pos := cur + di
			if pos >= draftFP.Config.BlockSize {
				break
			}
			dp := SoftmaxFloat(tmpDLogits)
			draftProbs = append(draftProbs, dp)
			dtok := argmax(dp)
			if dtok == bos {
				break
			}
			draftToks = append(draftToks, dtok)
			tmpDLogits = beamForward(dtok, pos, &tmpDKeys, &tmpDValues, draftFP)
		}

		if len(draftToks) == 0 {
			// Draft produced BOS -- fall back to one target greedy step
			tp := SoftmaxFloat(tLogits)
			ttok := argmax(tp)
			if ttok == bos {
				break
			}
			lp += math.Log(math.Max(tp[ttok], 1e-10))
			gen = append(gen, ttok)
			tLogits = beamForward(ttok, cur, &tKeys, &tValues, targetFP)
			dLogits = beamForward(ttok, cur, &dKeys, &dValues, draftFP)
			continue
		}

		totalProposed += len(draftToks)

		// Phase 2: Target model verifies each draft token
		// On GPU this would be one batched forward pass. The acceptance logic is
		// identical to the parallel version regardless of serial/parallel execution.
		var accepted []int
		tmpTKeys := CloneBeamKV(tKeys)
		tmpTValues := CloneBeamKV(tValues)
		tmpTLogits := make([]float64, len(tLogits))
		copy(tmpTLogits, tLogits)

		for vi := 0; vi < len(draftToks); vi++ {
			tp := SoftmaxFloat(tmpTLogits)
			dp := draftProbs[vi]
			dtok := draftToks[vi]
			// Rejection sampling: accept with p = min(1, p_target/p_draft)
			ratio := math.Min(1.0, tp[dtok]/math.Max(dp[dtok], 1e-10))
			if rng.Float64() < ratio {
				accepted = append(accepted, dtok)
				lp += math.Log(math.Max(tp[dtok], 1e-10))
				tmpTLogits = beamForward(dtok, cur+vi, &tmpTKeys, &tmpTValues, targetFP)
			} else {
				// Reject: resample from max(0, p_target - p_draft)
				adj := make([]float64, len(tp))
				adjSum := 0.0
				for j := range tp {
					adj[j] = math.Max(0.0, tp[j]-dp[j])
					adjSum += adj[j]
				}
				var rtok int
				if adjSum > 0 {
					for j := range adj {
						adj[j] /= adjSum
					}
					rtok = weightedSampleFloat(adj, rng)
				} else {
					rtok = weightedSampleFloat(tp, rng)
				}
				if rtok != bos {
					accepted = append(accepted, rtok)
					lp += math.Log(math.Max(tp[rtok], 1e-10))
					beamForward(rtok, cur+vi, &tmpTKeys, &tmpTValues, targetFP)
				}
				break // Discard all remaining draft tokens after rejection
			}
		}

		totalAccepted += len(accepted)

		// Commit accepted tokens to both real KV caches
		for ai, atok := range accepted {
			tLogits = beamForward(atok, cur+ai, &tKeys, &tValues, targetFP)
			dLogits = beamForward(atok, cur+ai, &dKeys, &dValues, draftFP)
			gen = append(gen, atok)
		}

		if len(accepted) == 0 {
			tp := SoftmaxFloat(tLogits)
			ttok := argmax(tp)
			if ttok == bos {
				break
			}
			lp += math.Log(math.Max(tp[ttok], 1e-10))
			gen = append(gen, ttok)
			tLogits = beamForward(ttok, cur, &tKeys, &tValues, targetFP)
			dLogits = beamForward(ttok, cur, &dKeys, &dValues, draftFP)
		}
	}

	return DecodeResult{
		Tokens:   gen,
		LogProb:  lp,
		Proposed: totalProposed,
		Accepted: totalAccepted,
	}
}

// === DEMO ===

// RunMicrobeam trains target and draft models, then compares all 6 decoding strategies.
func RunMicrobeam() {
	fmt.Println("=== MicroBeam: Decoding Strategies Comparison ===")
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

	chars, charToIdx, bos := BuildVocab(docs)
	vocabSize := len(chars) + 1
	_ = vocabSize

	rng := rand.New(rand.NewPCG(42, 0))

	// Train target model (larger)
	fmt.Printf("=== Training Target Model (n_embd=%d, n_layer=%d) ===\n",
		beamTargetNEmbd, beamTargetNLayer)
	targetResult := TrainBeamModel(docs, chars, charToIdx, bos,
		TargetBeamConfig(), beamTargetSteps, rng, true)

	// Train draft model (smaller)
	fmt.Printf("=== Training Draft Model (n_embd=%d, n_layer=%d) ===\n",
		beamDraftNEmbd, beamDraftNLayer)
	draftResult := TrainBeamModel(docs, chars, charToIdx, bos,
		DraftBeamConfig(), beamDraftSteps, rng, true)

	// Extract float weights for inference
	targetFP := ExtractFloatParams(targetResult.Params)
	draftFP := ExtractFloatParams(draftResult.Params)

	// Token-to-string helper
	tok2str := func(toks []int) string {
		var out []rune
		for _, t := range toks {
			if t >= 0 && t < len(chars) {
				out = append(out, chars[t])
			}
		}
		return string(out)
	}

	// === DECODING STRATEGIES COMPARISON ===
	fmt.Println("=== Decoding Strategies Comparison ===")

	type promptEntry struct {
		label string
		toks  []int
	}
	prompts := []promptEntry{
		{"a", []int{bos, charToIdx['a']}},
		{"m", []int{bos, charToIdx['m']}},
	}

	for _, pe := range prompts {
		fmt.Printf("\nPrompt: \"%s\" (BOS + '%s')\n\n", pe.label, pe.label)
		fmt.Printf("%-22s %-16s %10s %12s\n", "Strategy", "Output", "Log-Prob", "Tokens/Step")
		fmt.Println("--------------------------------------------------------------")

		g := DecodeGreedy(pe.toks, targetFP, 12)
		fmt.Printf("%-22s %-16s %10.2f %12s\n", "Greedy", tok2str(g.Tokens), g.LogProb, "1.0")

		t := DecodeTemperature(pe.toks, targetFP, 12, 0.8, rng)
		fmt.Printf("%-22s %-16s %10.2f %12s\n", "Temperature (0.8)", tok2str(t.Tokens), t.LogProb, "1.0")

		k := DecodeTopK(pe.toks, targetFP, 12, 5, rng)
		fmt.Printf("%-22s %-16s %10.2f %12s\n", "Top-k (k=5)", tok2str(k.Tokens), k.LogProb, "1.0")

		p := DecodeTopP(pe.toks, targetFP, 12, 0.9, rng)
		fmt.Printf("%-22s %-16s %10.2f %12s\n", "Top-p (p=0.9)", tok2str(p.Tokens), p.LogProb, "1.0")

		b := DecodeBeam(pe.toks, targetFP, 12, 3)
		fmt.Printf("%-22s %-16s %10.2f %12s\n", "Beam (width=3)", tok2str(b.Tokens), b.LogProb, "1.0")

		s := DecodeSpeculative(pe.toks, targetFP, draftFP, 12, 4, rng)
		tps := 1.0
		if s.Proposed > 0 {
			nRounds := float64(s.Proposed) / 4.0
			tps = float64(s.Accepted) / math.Max(nRounds, 1.0)
		}
		fmt.Printf("%-22s %-16s %10.2f %12.1f\n", "Speculative (k=4)", tok2str(s.Tokens), s.LogProb, tps)
	}

	// === DIVERSITY ANALYSIS ===
	fmt.Println("\n=== Diversity Analysis ===")
	fmt.Println("Generated 20 names with each strategy:")
	fmt.Println()
	nSamp := 20
	seeds := []rune("abcdefghijklmnopqrst")

	type stratEntry struct {
		name string
		fn   func([]int) DecodeResult
	}
	strats := []stratEntry{
		{"Greedy", func(pt []int) DecodeResult { return DecodeGreedy(pt, targetFP, 12) }},
		{"Temperature (0.8)", func(pt []int) DecodeResult { return DecodeTemperature(pt, targetFP, 12, 0.8, rng) }},
		{"Top-k (k=5)", func(pt []int) DecodeResult { return DecodeTopK(pt, targetFP, 12, 5, rng) }},
		{"Top-p (p=0.9)", func(pt []int) DecodeResult { return DecodeTopP(pt, targetFP, 12, 0.9, rng) }},
		{"Beam (width=3)", func(pt []int) DecodeResult { return DecodeBeam(pt, targetFP, 12, 3) }},
	}

	fmt.Printf("%-22s %13s %11s %13s\n", "Strategy", "Unique Names", "Avg Length", "Avg Log-Prob")
	fmt.Println("--------------------------------------------------------------")
	for _, se := range strats {
		names := make([]string, nSamp)
		lps := make([]float64, nSamp)
		for i := 0; i < nSamp; i++ {
			pt := []int{bos, charToIdx[seeds[i]]}
			r := se.fn(pt)
			names[i] = tok2str(r.Tokens)
			lps[i] = r.LogProb
		}
		unique := map[string]bool{}
		totalLen := 0
		for _, n := range names {
			unique[n] = true
			totalLen += len(n)
		}
		avgLP := 0.0
		for _, l := range lps {
			avgLP += l
		}
		avgLP /= float64(nSamp)
		fmt.Printf("%-22s %13d %11.1f %13.2f\n",
			se.name, len(unique), float64(totalLen)/float64(nSamp), avgLP)
	}

	// === SPECULATIVE DECODING STATS ===
	fmt.Println("\n=== Speculative Decoding Stats ===")
	totProp := 0
	totAcc := 0
	for i := 0; i < nSamp; i++ {
		pt := []int{bos, charToIdx[seeds[i]]}
		r := DecodeSpeculative(pt, targetFP, draftFP, 12, 4, rng)
		totProp += r.Proposed
		totAcc += r.Accepted
	}
	accRate := 100.0 * float64(totAcc) / math.Max(float64(totProp), 1.0)
	nRounds := float64(totProp) / 4.0
	toksPerRound := float64(totAcc) / math.Max(nRounds, 1.0)
	fmt.Printf("Draft tokens proposed per step: 4\n")
	fmt.Printf("Total proposed: %d | Total accepted: %d\n", totProp, totAcc)
	fmt.Printf("Average acceptance rate: %.1f%%\n", accRate)
	fmt.Printf("Average tokens accepted per target verify pass: %.1f\n", toksPerRound)
	// Signpost: in production with a well-matched draft model, acceptance rates of
	// 70-90% are common. The real GPU speedup comes from batching the k verification
	// forward passes into a single kernel launch -- our scalar Go cannot show that
	// parallelism, but the acceptance rate is the hardware-independent metric.
}

// === INTERNAL HELPERS ===

// argmax returns the index of the maximum value.
func argmax(v []float64) int {
	best := 0
	for i := 1; i < len(v); i++ {
		if v[i] > v[best] {
			best = i
		}
	}
	return best
}

// argsortDesc returns indices sorted by descending value.
func argsortDesc(v []float64) []int {
	indices := make([]int, len(v))
	for i := range indices {
		indices[i] = i
	}
	sort.Slice(indices, func(a, b int) bool {
		return v[indices[a]] > v[indices[b]]
	})
	return indices
}
