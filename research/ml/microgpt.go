//go:build research

// The autoregressive language model from first principles: GPT learns to predict the next
// character in a sequence using nothing but matrix multiplication, attention, and gradient descent.
//
// Reference: This implementation follows the GPT-2 architecture (Radford et al., 2019)
// with pedagogical simplifications: RMSNorm instead of LayerNorm, ReLU instead of GELU,
// no bias terms. Algorithmic flow inspired by Karpathy's microgpt.py but rewritten from
// scratch with comprehensive commenting for educational clarity.
//
// Port of microgpt.py from mathews-tom/no-magic to Go.
// AGPL-3.0 -- see LICENSE in repo root.
package ml

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
)

// === CONSTANTS AND HYPERPARAMETERS ===

const (
	// Model architecture
	gptNEmbd   = 16                  // embedding dimension (d_model in Transformer papers)
	gptNHead   = 4                   // number of attention heads
	gptNLayer  = 1                   // number of transformer blocks
	gptBlock   = 16                  // context window size (maximum sequence length)
	gptHeadDim = gptNEmbd / gptNHead // dimension per attention head (16/4 = 4)

	// Training parameters
	gptLR      = 0.01 // Adam base learning rate
	gptBeta1   = 0.85 // Adam first moment decay
	gptBeta2   = 0.99 // Adam second moment decay
	gptEpsAdam = 1e-8 // Adam epsilon (prevents division by zero)
	gptSteps   = 1000 // total training steps

	// Init
	gptInitStd = 0.08 // weight initialization standard deviation
)

// Signpost: ~4,200 parameters total. Production GPTs have billions. The architecture
// is identical (attention is attention), but this toy scale lets us train on CPU in
// minutes rather than weeks on GPU clusters.

// === GPT CONFIG (exported, for tests) ===

// GPTConfig holds the model architecture hyperparameters.
type GPTConfig struct {
	NEmbd     int     // embedding dimension
	NHead     int     // number of attention heads
	NLayer    int     // number of transformer blocks
	BlockSize int     // context window length
	VocabSize int     // number of tokens
	InitStd   float64 // weight init standard deviation
}

// DefaultGPTConfig returns the default tiny GPT config matching the Python original.
func DefaultGPTConfig(vocabSize int) GPTConfig {
	return GPTConfig{
		NEmbd:     gptNEmbd,
		NHead:     gptNHead,
		NLayer:    gptNLayer,
		BlockSize: gptBlock,
		VocabSize: vocabSize,
		InitStd:   gptInitStd,
	}
}

// HeadDim returns the dimension per attention head.
func (c GPTConfig) HeadDim() int {
	return c.NEmbd / c.NHead
}

// === GPT PARAMETERS ===

// GPTParams holds all model parameters: embeddings, attention, MLP, and LM head.
// This is the "state_dict" -- the complete specification of the trained model.
type GPTParams struct {
	Config GPTConfig

	// Token and position embeddings
	// Wte: [vocabSize][nEmbd] -- maps token IDs to vectors
	// Wpe: [blockSize][nEmbd] -- maps positions to vectors
	Wte [][]*Value
	Wpe [][]*Value

	// Per-layer transformer weights
	Layers []GPTLayerParams

	// Language model head: [vocabSize][nEmbd]
	LMHead [][]*Value
}

// GPTLayerParams holds the weights for a single transformer block.
type GPTLayerParams struct {
	// Attention weights (Q, K, V projections and output projection)
	// All are [nEmbd][nEmbd]
	AttnWQ [][]*Value
	AttnWK [][]*Value
	AttnWV [][]*Value
	AttnWO [][]*Value

	// MLP weights (2-layer feedforward with 4x expansion)
	// FC1: [4*nEmbd][nEmbd] -- expand, FC2: [nEmbd][4*nEmbd] -- contract
	// The 4x expansion is a GPT convention -- gives the MLP more capacity to
	// process the attention output without increasing the residual stream width.
	MLPFC1 [][]*Value
	MLPFC2 [][]*Value
}

// InitGPTParams initializes all model parameters with Gaussian noise.
//
// Standard deviation of 0.08 is chosen empirically for this tiny model --
// larger models typically use std = 1/sqrt(d_in) (Xavier/Glorot initialization)
// to keep activations from exploding or vanishing through deep layers. With
// only 1 layer, the initialization is less critical.
func InitGPTParams(rng *rand.Rand, cfg GPTConfig) *GPTParams {
	p := &GPTParams{Config: cfg}

	p.Wte = MakeVMatrix(rng, cfg.VocabSize, cfg.NEmbd, cfg.InitStd)
	p.Wpe = MakeVMatrix(rng, cfg.BlockSize, cfg.NEmbd, cfg.InitStd)

	p.Layers = make([]GPTLayerParams, cfg.NLayer)
	for i := range p.Layers {
		p.Layers[i] = GPTLayerParams{
			AttnWQ: MakeVMatrix(rng, cfg.NEmbd, cfg.NEmbd, cfg.InitStd),
			AttnWK: MakeVMatrix(rng, cfg.NEmbd, cfg.NEmbd, cfg.InitStd),
			AttnWV: MakeVMatrix(rng, cfg.NEmbd, cfg.NEmbd, cfg.InitStd),
			AttnWO: MakeVMatrix(rng, cfg.NEmbd, cfg.NEmbd, cfg.InitStd),
			MLPFC1: MakeVMatrix(rng, 4*cfg.NEmbd, cfg.NEmbd, cfg.InitStd),
			MLPFC2: MakeVMatrix(rng, cfg.NEmbd, 4*cfg.NEmbd, cfg.InitStd),
		}
	}

	p.LMHead = MakeVMatrix(rng, cfg.VocabSize, cfg.NEmbd, cfg.InitStd)

	return p
}

// AllParams returns a flat slice of all trainable parameters for optimizer bookkeeping.
func (p *GPTParams) AllParams() []*Value {
	var all []*Value
	appendMatrix := func(m [][]*Value) {
		for _, row := range m {
			all = append(all, row...)
		}
	}
	appendMatrix(p.Wte)
	appendMatrix(p.Wpe)
	for i := range p.Layers {
		appendMatrix(p.Layers[i].AttnWQ)
		appendMatrix(p.Layers[i].AttnWK)
		appendMatrix(p.Layers[i].AttnWV)
		appendMatrix(p.Layers[i].AttnWO)
		appendMatrix(p.Layers[i].MLPFC1)
		appendMatrix(p.Layers[i].MLPFC2)
	}
	appendMatrix(p.LMHead)
	return all
}

// === GPT FORWARD PASS ===

// GPTForward processes a single token at position posID and returns logits over the
// vocabulary. The keys and values slices accumulate the KV cache -- a running history
// of all past tokens' key/value projections, which lets us implement causal attention
// without an explicit mask matrix.
//
// Args:
//   - tokenID: integer in [0, vocabSize) identifying the input token
//   - posID: integer in [0, blockSize) indicating position in sequence
//   - keys: KV cache for keys, [nLayer][seqLen][nEmbd]
//   - values: KV cache for values, [nLayer][seqLen][nEmbd]
//   - params: model weight matrices
//
// Returns logits (unnormalized log-probabilities) over vocabulary, length vocabSize.
func GPTForward(tokenID, posID int, keys, values *[][]*[]*Value, params *GPTParams) []*Value {
	cfg := params.Config
	headDim := cfg.HeadDim()

	// -- Embedding layer --
	// Look up learned vectors for this token and position, then add them.
	// tok_emb encodes "what" (the token), pos_emb encodes "where" (position in sequence).
	tokEmb := params.Wte[tokenID]
	posEmb := params.Wpe[posID]
	x := make([]*Value, cfg.NEmbd)
	for i := range x {
		x[i] = tokEmb[i].Add(posEmb[i])
	}

	// Normalize embeddings before feeding to transformer blocks
	x = VRMSNorm(x)

	// -- Transformer layers --
	for li := range params.Layers {
		layer := &params.Layers[li]

		// Residual connection pattern: x_new = x + f(x)
		// This "highway" lets gradients flow directly backward through the model
		// without passing through attention or MLP, preventing vanishing gradients.
		xResidual := x

		// Pre-norm: normalize before attention (modern architectures do this rather
		// than post-norm because it stabilizes training in deep models)
		x = VRMSNorm(x)

		// -- Multi-head self-attention --
		// Project input to queries, keys, values
		q := VLinear(x, layer.AttnWQ)
		k := VLinear(x, layer.AttnWK)
		v := VLinear(x, layer.AttnWV)

		// Append k, v to cache for this layer. This builds the KV cache incrementally:
		// at position t, (*keys)[li] contains [k_0, k_1, ..., k_t].
		kRef := &(*keys)[li]
		vRef := &(*values)[li]
		*kRef = append(*kRef, &k)
		*vRef = append(*vRef, &v)

		// Process each attention head independently, then concatenate outputs
		xAttn := make([]*Value, 0, cfg.NEmbd)
		for head := 0; head < cfg.NHead; head++ {
			headStart := head * headDim

			// Slice out this head's portion of the q vector
			qHead := q[headStart : headStart+headDim]

			// Build per-head key/value slices from cache
			cachedK := *kRef
			cachedV := *vRef
			seqLen := len(cachedK)

			// Compute attention scores: how much should we attend to each past token?
			// score(q, k_t) = (q . k_t) / sqrt(d_head)
			// The sqrt(d_head) scaling prevents scores from growing too large as
			// dimensionality increases (which would make softmax saturate).
			scale := 1.0 / math.Sqrt(float64(headDim))
			attnLogits := make([]*Value, seqLen)
			for t := 0; t < seqLen; t++ {
				kT := (*cachedK[t])[headStart : headStart+headDim]
				dot := V(0)
				for j := 0; j < headDim; j++ {
					dot = dot.Add(qHead[j].Mul(kT[j]))
				}
				attnLogits[t] = dot.MulScalar(scale)
			}

			// Convert scores to probabilities via softmax
			attnWeights := VSoftmax(attnLogits)

			// Weighted sum of values: output[j] = sum_t attn_weights[t] * v[t][j]
			// This is the "attention" mechanism: we look at all past tokens (via their
			// value vectors) and weight each by its relevance (attention weight).
			headOutput := make([]*Value, headDim)
			for j := 0; j < headDim; j++ {
				sum := V(0)
				for t := 0; t < seqLen; t++ {
					vT := (*cachedV[t])[headStart : headStart+headDim]
					sum = sum.Add(attnWeights[t].Mul(vT[j]))
				}
				headOutput[j] = sum
			}

			xAttn = append(xAttn, headOutput...)
		}

		// Signpost: Why KV caching provides causal masking without an explicit mask --
		// At position t, (*keys)[li] only contains keys for positions 0..t, so the
		// attention scores loop naturally excludes future tokens. This incremental
		// construction is equivalent to applying a lower-triangular mask in a batch
		// setting, but more efficient for autoregressive generation.

		// Project concatenated head outputs back to residual dimension
		x = VLinear(xAttn, layer.AttnWO)

		// First residual connection (around attention)
		for i := range x {
			x[i] = x[i].Add(xResidual[i])
		}
		xResidual = x

		// -- MLP (feedforward network) --
		x = VRMSNorm(x)
		x = VLinear(x, layer.MLPFC1) // expand
		for i := range x {
			x[i] = x[i].ReLU() // nonlinearity
		}
		x = VLinear(x, layer.MLPFC2) // contract

		// Second residual connection (around MLP)
		for i := range x {
			x[i] = x[i].Add(xResidual[i])
		}
	}

	// -- Output layer --
	// Project final hidden state to vocabulary logits
	return VLinear(x, params.LMHead)
}

// === ADAM OPTIMIZER ===

// AdamState holds per-parameter first and second moment estimates.
type AdamState struct {
	M []float64 // first moment (momentum)
	V []float64 // second moment (variance)
}

// NewAdamState initializes optimizer state for n parameters.
func NewAdamState(n int) *AdamState {
	return &AdamState{
		M: make([]float64, n),
		V: make([]float64, n),
	}
}

// AdamStep performs one Adam optimizer step with linear learning rate decay.
//
// Adam update rule:
//
//	m_t = beta1*m_{t-1} + (1-beta1)*g_t         (momentum)
//	v_t = beta2*v_{t-1} + (1-beta2)*g_t^2       (variance)
//	theta_t = theta_{t-1} - lr * m_hat / (sqrt(v_hat) + eps)
//
// Bias correction divides by (1-beta^t) to correct for zero-initialization of m, v.
// Linear LR decay: lr_t = lr_0 * (1 - step/totalSteps).
func AdamStep(params []*Value, state *AdamState, step, totalSteps int, lr, beta1, beta2, eps float64) {
	// Linear learning rate decay: prevents overshooting as the loss landscape
	// sharpens near the optimum.
	lrT := lr * (1.0 - float64(step)/float64(totalSteps))

	for i, p := range params {
		g := p.Grad
		state.M[i] = beta1*state.M[i] + (1-beta1)*g
		state.V[i] = beta2*state.V[i] + (1-beta2)*g*g

		// Bias correction: m and v are biased toward zero in early steps because
		// they're initialized to 0. Dividing by (1 - beta^t) corrects for this.
		mHat := state.M[i] / (1 - math.Pow(beta1, float64(step+1)))
		vHat := state.V[i] / (1 - math.Pow(beta2, float64(step+1)))

		// Parameter update. Epsilon prevents division by zero when vHat is tiny.
		p.Data -= lrT * mHat / (math.Sqrt(vHat) + eps)

		// Zero gradient for next iteration
		p.Grad = 0.0
	}
}

// === TRAINING ===

// GPTTrainResult holds the outcome of a training run.
type GPTTrainResult struct {
	Params      *GPTParams
	FinalLoss   float64
	LossHistory []float64 // loss at each step
}

// TrainGPT trains a character-level GPT on the given documents.
//
// Each document is a string. A vocabulary is built from unique characters plus
// a BOS (beginning-of-sequence) token. Training uses the Adam optimizer with
// linear learning rate decay.
func TrainGPT(docs []string, steps int, rng *rand.Rand, verbose bool) *GPTTrainResult {
	// Build vocabulary from unique characters in the corpus
	charSet := map[rune]bool{}
	for _, doc := range docs {
		for _, ch := range doc {
			charSet[ch] = true
		}
	}
	chars := make([]rune, 0, len(charSet))
	for ch := range charSet {
		chars = append(chars, ch)
	}
	sort.Slice(chars, func(i, j int) bool { return chars[i] < chars[j] })

	bos := len(chars) // BOS token ID (appended to char set)
	vocabSize := len(chars) + 1

	// Char -> index mapping
	charToIdx := make(map[rune]int, len(chars))
	for i, ch := range chars {
		charToIdx[ch] = i
	}

	if verbose {
		fmt.Printf("Vocabulary size: %d (characters + BOS token)\n", vocabSize)
	}

	// Initialize parameters
	cfg := DefaultGPTConfig(vocabSize)
	params := InitGPTParams(rng, cfg)

	// Flatten all parameters for optimizer bookkeeping
	paramList := params.AllParams()
	if verbose {
		fmt.Printf("Parameters: %d\n\n", len(paramList))
	}

	// Initialize Adam optimizer state
	adam := NewAdamState(len(paramList))

	// Shuffle docs
	shuffled := make([]string, len(docs))
	copy(shuffled, docs)
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	// Training loop
	if verbose {
		fmt.Println("Training...")
	}
	lossHistory := make([]float64, steps)
	var lastLoss float64

	for step := 0; step < steps; step++ {
		// Cycle through the dataset
		doc := shuffled[step%len(shuffled)]

		// Tokenize: [BOS, char_0, char_1, ..., char_n, BOS]
		tokens := make([]int, 0, len(doc)+2)
		tokens = append(tokens, bos)
		for _, ch := range doc {
			tokens = append(tokens, charToIdx[ch])
		}
		tokens = append(tokens, bos)

		// Truncate to block_size (context window limit)
		seqLen := cfg.BlockSize
		if len(tokens)-1 < seqLen {
			seqLen = len(tokens) - 1
		}

		// Initialize KV cache for this sequence (fresh for each document)
		keys := make([][]*[]*Value, cfg.NLayer)
		values := make([][]*[]*Value, cfg.NLayer)

		// Compute loss across the sequence (cross-entropy at each position)
		losses := make([]*Value, seqLen)
		for pos := 0; pos < seqLen; pos++ {
			inputToken := tokens[pos]
			targetToken := tokens[pos+1]

			// Forward pass
			logits := GPTForward(inputToken, pos, &keys, &values, params)

			// Convert logits to probabilities
			probs := VSoftmax(logits)

			// Negative log-likelihood loss: -log(p(target))
			// Cross-entropy for classification: we want the model to assign
			// high probability to the actual next token.
			losses[pos] = probs[targetToken].SafeLog().Neg()
		}

		// Average loss over the sequence (scale-invariant to doc length)
		loss := VSum(losses).DivScalar(float64(seqLen))

		// Backward pass
		loss.Backward()

		// Adam optimizer step
		AdamStep(paramList, adam, step, steps, gptLR, gptBeta1, gptBeta2, gptEpsAdam)

		lastLoss = loss.Data
		lossHistory[step] = lastLoss

		if verbose && ((step+1)%100 == 0 || step == 0) {
			fmt.Printf("  step %4d/%4d | loss: %.4f\n", step+1, steps, lastLoss)
		}
	}

	if verbose {
		fmt.Printf("\nTraining complete. Final loss: %.4f\n\n", lastLoss)
	}

	return &GPTTrainResult{
		Params:      params,
		FinalLoss:   lastLoss,
		LossHistory: lossHistory,
	}
}

// === INFERENCE ===

// GPTSample generates text from a trained GPT model using temperature-scaled sampling.
//
// Temperature controls randomness: lower = more deterministic, higher = more random.
// Returns a slice of generated strings.
func GPTSample(params *GPTParams, chars []rune, bos int, nSamples int, temperature float64, rng *rand.Rand) []string {
	cfg := params.Config
	results := make([]string, nSamples)

	for s := 0; s < nSamples; s++ {
		// Fresh KV cache for each sample
		keys := make([][]*[]*Value, cfg.NLayer)
		values := make([][]*[]*Value, cfg.NLayer)

		tokenID := bos
		var generated []rune

		for pos := 0; pos < cfg.BlockSize; pos++ {
			// Forward pass
			logits := GPTForward(tokenID, pos, &keys, &values, params)

			// Temperature scaling: divide logits by temperature before softmax.
			// This sharpens (T < 1) or flattens (T > 1) the probability distribution.
			scaled := make([]*Value, len(logits))
			for i, l := range logits {
				scaled[i] = l.DivScalar(temperature)
			}
			probs := VSoftmax(scaled)

			// Sample next token from the probability distribution
			weights := make([]float64, len(probs))
			for i, p := range probs {
				weights[i] = p.Data
			}
			tokenID = weightedSample(weights, rng)

			// Stop if we hit BOS (end-of-sequence marker)
			if tokenID == bos {
				break
			}

			generated = append(generated, chars[tokenID])
		}

		results[s] = string(generated)
	}

	return results
}

// weightedSample draws a single index from a categorical distribution.
func weightedSample(weights []float64, rng *rand.Rand) int {
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

// === DEMO ===

// RunMicrogpt trains a tiny GPT on a small corpus and generates samples.
// This is the Go equivalent of running microgpt.py directly.
func RunMicrogpt() {
	fmt.Println("=== MicroGPT: Character-level Language Model ===")
	fmt.Println()

	// Small built-in corpus (subset of names, since we can't download in stdlib-only)
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

	fmt.Printf("Loading data... %d documents\n", len(docs))

	result := TrainGPT(docs, gptSteps, rng, true)

	// Build vocabulary for sampling (reconstruct from docs)
	charSet := map[rune]bool{}
	for _, doc := range docs {
		for _, ch := range doc {
			charSet[ch] = true
		}
	}
	chars := make([]rune, 0, len(charSet))
	for ch := range charSet {
		chars = append(chars, ch)
	}
	sort.Slice(chars, func(i, j int) bool { return chars[i] < chars[j] })
	bos := len(chars)

	// Generate samples
	temperature := 0.5
	nSamples := 20
	fmt.Printf("Generating %d samples (temperature=%.1f):\n\n", nSamples, temperature)

	samples := GPTSample(result.Params, chars, bos, nSamples, temperature, rng)
	for i, s := range samples {
		fmt.Printf("  %2d. %s\n", i+1, s)
	}
}

// === HELPERS FOR TESTS ===

// BuildVocab builds a sorted character vocabulary and BOS token from documents.
// Returns (chars, charToIdx, bos).
func BuildVocab(docs []string) ([]rune, map[rune]int, int) {
	charSet := map[rune]bool{}
	for _, doc := range docs {
		for _, ch := range doc {
			charSet[ch] = true
		}
	}
	chars := make([]rune, 0, len(charSet))
	for ch := range charSet {
		chars = append(chars, ch)
	}
	sort.Slice(chars, func(i, j int) bool { return chars[i] < chars[j] })

	charToIdx := make(map[rune]int, len(chars))
	for i, ch := range chars {
		charToIdx[ch] = i
	}

	bos := len(chars)
	return chars, charToIdx, bos
}

// Tokenize converts a document to a token sequence with BOS markers: [BOS, char_0, ..., char_n, BOS].
func Tokenize(doc string, charToIdx map[rune]int, bos int) []int {
	tokens := make([]int, 0, len(doc)+2)
	tokens = append(tokens, bos)
	for _, ch := range doc {
		tokens = append(tokens, charToIdx[ch])
	}
	tokens = append(tokens, bos)
	return tokens
}

// JoinSamples returns all non-empty generated strings joined with newlines.
func JoinSamples(samples []string) string {
	var nonEmpty []string
	for _, s := range samples {
		if s != "" {
			nonEmpty = append(nonEmpty, s)
		}
	}
	return strings.Join(nonEmpty, "\n")
}
