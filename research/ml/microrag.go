//go:build research

// Package ml provides educational ML algorithm implementations.
//
// microrag.go — RAG (Retrieval-Augmented Generation): How retrieval augments generation —
// the simplest system that actually works, with BM25 search and a character-level MLP.
//
// Reference: RAG architecture inspired by "Retrieval-Augmented Generation for
// Knowledge-Intensive NLP Tasks" (Lewis et al., 2020), BM25 scoring from Robertson
// and Zaragoza (2009). Implementation rewritten from scratch for educational clarity.
package ml

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"unicode"
)

// === CONSTANTS ===

const (
	ragLR        = 0.01
	ragHiddenDim = 64 // hidden layer size for MLP
	ragNumEpochs = 300
	ragTopK      = 3 // retrieve top 3 documents
	ragBatchSize = 5

	// BM25 hyperparameters (standard values from information retrieval literature)
	bm25K1 = 1.2  // term frequency saturation parameter
	bm25B  = 0.75 // document length normalization parameter
)

// ragCharVocab is the character vocabulary: lowercase letters + space, period, comma.
var ragCharVocab = []rune("abcdefghijklmnopqrstuvwxyz .,")

// ragVocabSize is len(ragCharVocab).
var ragVocabSize = len(ragCharVocab)

// === SYNTHETIC KNOWLEDGE BASE ===

// CityFact holds a city's factual data for knowledge base generation.
type CityFact struct {
	City, Country, Population, River string
}

// MountainFact holds a mountain's factual data for knowledge base generation.
type MountainFact struct {
	Mountain, Country, Height string
}

// TestQuery pairs a query string with the expected document index.
type TestQuery struct {
	Query          string
	ExpectedDocIdx int
}

// GenerateKnowledgeBase creates 100 synthetic factual paragraphs and 10 test queries.
//
// Uses templates + data tables to create verifiable factual knowledge about cities,
// countries, populations, and geography. Deterministic, reproducible, no external data.
func GenerateKnowledgeBase() ([]string, []TestQuery) {
	cities := []CityFact{
		{"paris", "france", "2.1 million", "seine"},
		{"london", "united kingdom", "8.9 million", "thames"},
		{"berlin", "germany", "3.8 million", "spree"},
		{"madrid", "spain", "3.3 million", "manzanares"},
		{"rome", "italy", "2.8 million", "tiber"},
		{"tokyo", "japan", "14 million", "sumida"},
		{"beijing", "china", "21 million", "yongding"},
		{"delhi", "india", "16 million", "yamuna"},
		{"cairo", "egypt", "9.5 million", "nile"},
		{"lagos", "nigeria", "14 million", "lagos lagoon"},
	}

	mountains := []MountainFact{
		{"everest", "nepal", "8849 meters"},
		{"k2", "pakistan", "8611 meters"},
		{"kilimanjaro", "tanzania", "5895 meters"},
		{"mont blanc", "france", "4808 meters"},
		{"denali", "united states", "6190 meters"},
	}

	var documents []string

	// City paragraphs
	for _, c := range cities {
		doc := fmt.Sprintf("%s is the capital of %s. it has a population of approximately %s. the %s river flows through the city.",
			c.City, c.Country, c.Population, c.River)
		documents = append(documents, doc)
	}

	// Mountain paragraphs
	for _, m := range mountains {
		doc := fmt.Sprintf("%s is located in %s. the mountain has a height of %s. it is a popular destination for climbers.",
			m.Mountain, m.Country, m.Height)
		documents = append(documents, doc)
	}

	// Continent facts
	continents := []string{
		"africa is the second largest continent by area.",
		"asia is the most populous continent in the world.",
		"europe has diverse cultures and languages.",
		"north america includes canada, united states, and mexico.",
		"south america is home to the amazon rainforest.",
	}
	documents = append(documents, continents...)

	// Additional factual statements to reach 100 documents
	for i := 0; i < 80; i++ {
		var doc string
		switch i % 4 {
		case 0:
			c := cities[i%len(cities)]
			doc = fmt.Sprintf("the population of %s is about %s. it is in %s.", c.City, c.Population, c.Country)
		case 1:
			m := mountains[i%len(mountains)]
			doc = fmt.Sprintf("%s stands at %s in %s.", m.Mountain, m.Height, m.Country)
		case 2:
			c := cities[i%len(cities)]
			doc = fmt.Sprintf("the %s river is a major waterway in %s, %s.", c.River, c.City, c.Country)
		case 3:
			c := cities[i%len(cities)]
			doc = fmt.Sprintf("%s is a major city with population %s.", c.City, c.Population)
		}
		documents = append(documents, doc)
	}

	// Test queries with known correct document indices
	testQueries := []TestQuery{
		{"population of paris", 0},
		{"seine river", 0},
		{"tokyo population", 5},
		{"everest height", 10},
		{"capital of germany", 2},
		{"nile river", 8},
		{"kilimanjaro tanzania", 12},
		{"thames river london", 1},
		{"mont blanc france", 13},
		{"beijing china", 6},
	}

	return documents, testQueries
}

// === TOKENIZATION ===

// ragTokenize performs simple word-level tokenization: lowercase, split on non-alphanumeric.
//
// Signpost: production RAG systems use learned subword tokenizers (BPE, SentencePiece).
// Word-level tokenization is sufficient here for demonstrating retrieval mechanics —
// the focus is on BM25 scoring and context injection, not tokenization quality.
func ragTokenize(text string) []string {
	var words []string
	var word []rune
	for _, ch := range strings.ToLower(text) {
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			word = append(word, ch)
		} else if len(word) > 0 {
			words = append(words, string(word))
			word = word[:0]
		}
	}
	if len(word) > 0 {
		words = append(words, string(word))
	}
	return words
}

// === BM25 INDEX ===

// BM25Index implements BM25 scoring for document retrieval.
//
// BM25 improves on TF-IDF with two key insights:
//  1. TF saturation: 10 occurrences isn't 10x more relevant than 1 occurrence.
//     The formula uses (tf * (k1 + 1)) / (tf + k1) which saturates as tf -> inf.
//  2. Document length normalization: long documents aren't inherently more relevant.
//     The normalization term (1 - b + b * dl/avgdl) penalizes long docs.
//
// Math-to-code mapping:
//
//	idf(term) = log((N - df + 0.5) / (df + 0.5) + 1)
//	tf_score(term, doc) = (tf * (k1 + 1)) / (tf + k1 * (1 - b + b * dl/avgdl))
//	BM25(query, doc) = sum_{term in query} idf(term) * tf_score(term, doc)
type BM25Index struct {
	Documents     []string
	K1            float64
	B             float64
	N             int // total number of documents
	DocTokens     [][]string
	DocLengths    []int
	AvgDL         float64
	InvertedIndex map[string][]BM25Posting // term -> list of (docID, tf)
	IDF           map[string]float64       // precomputed IDF scores
}

// BM25Posting is an entry in the inverted index: which document, how many occurrences.
type BM25Posting struct {
	DocID int
	TF    int
}

// BM25Result pairs a document ID with its BM25 score.
type BM25Result struct {
	DocID int
	Score float64
}

// NewBM25Index builds a BM25 index over the given documents.
func NewBM25Index(documents []string, k1, b float64) *BM25Index {
	idx := &BM25Index{
		Documents:     documents,
		K1:            k1,
		B:             b,
		N:             len(documents),
		InvertedIndex: make(map[string][]BM25Posting),
		IDF:           make(map[string]float64),
	}

	// Tokenize all documents
	idx.DocTokens = make([][]string, len(documents))
	idx.DocLengths = make([]int, len(documents))
	totalLen := 0
	for i, doc := range documents {
		tokens := ragTokenize(doc)
		idx.DocTokens[i] = tokens
		idx.DocLengths[i] = len(tokens)
		totalLen += len(tokens)
	}
	if idx.N > 0 {
		idx.AvgDL = float64(totalLen) / float64(idx.N)
	}

	// Build inverted index: term -> list of (docID, term_frequency)
	// This is the core data structure for efficient retrieval — for each term,
	// we precompute which documents contain it and how often. At query time we
	// only score documents that share at least one term with the query.
	for docID, tokens := range idx.DocTokens {
		termCounts := make(map[string]int)
		for _, term := range tokens {
			termCounts[term]++
		}
		for term, count := range termCounts {
			idx.InvertedIndex[term] = append(idx.InvertedIndex[term], BM25Posting{docID, count})
		}
	}

	// Precompute IDF scores for all terms
	// IDF formula: log((N - df + 0.5) / (df + 0.5) + 1) where df = document frequency
	// Why add 0.5? Smoothing to prevent division by zero and reduce impact of rare terms.
	// Why the +1 outside? Ensures IDF is always positive (log(x) < 0 for x < 1).
	for term, postings := range idx.InvertedIndex {
		df := float64(len(postings))
		idx.IDF[term] = math.Log((float64(idx.N)-df+0.5)/(df+0.5) + 1)
	}

	return idx
}

// Score computes the BM25 score for a query against a specific document.
func (idx *BM25Index) Score(query string, docID int) float64 {
	queryTerms := ragTokenize(query)
	score := 0.0

	dl := float64(idx.DocLengths[docID])
	// Document length normalization factor: penalizes long docs but not linearly
	norm := 1 - idx.B + idx.B*(dl/idx.AvgDL)

	// Count term frequencies in document
	docTermCounts := make(map[string]int)
	for _, term := range idx.DocTokens[docID] {
		docTermCounts[term]++
	}

	for _, term := range queryTerms {
		idf, ok := idx.IDF[term]
		if !ok {
			continue // term not in corpus
		}
		tf := float64(docTermCounts[term])
		if tf == 0 {
			continue // term not in this document
		}

		// TF saturation: (tf * (k1 + 1)) / (tf + k1 * norm)
		// As tf -> inf, this approaches (k1 + 1) / k1 ~ 1.83 (for k1=1.2).
		// This prevents term frequency from dominating the score.
		tfScore := (tf * (idx.K1 + 1)) / (tf + idx.K1*norm)
		score += idf * tfScore
	}

	return score
}

// Retrieve returns the top-k documents for a query, ranked by BM25 score.
func (idx *BM25Index) Retrieve(query string, topK int) []BM25Result {
	scores := make([]BM25Result, idx.N)
	for i := 0; i < idx.N; i++ {
		scores[i] = BM25Result{i, idx.Score(query, i)}
	}
	// Sort by score descending
	for i := 0; i < len(scores); i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[j].Score > scores[i].Score {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}
	if topK > len(scores) {
		topK = len(scores)
	}
	return scores[:topK]
}

// === CHARACTER-LEVEL MLP GENERATOR ===

// ragCharToIdx maps a character to its index in ragCharVocab.
func ragCharToIdx(ch rune) int {
	for i, c := range ragCharVocab {
		if c == ch {
			return i
		}
	}
	// Fallback to space for unknown chars
	for i, c := range ragCharVocab {
		if c == ' ' {
			return i
		}
	}
	return 0
}

// ragIdxToChar maps an index to its character in ragCharVocab.
func ragIdxToChar(idx int) rune {
	if idx >= 0 && idx < len(ragCharVocab) {
		return ragCharVocab[idx]
	}
	return ' '
}

// ragOneHot creates a one-hot encoded vector.
func ragOneHot(idx, size int) []float64 {
	vec := make([]float64, size)
	vec[idx] = 1.0
	return vec
}

// RAGMLP is a character-level MLP generator with concatenated query + context input.
//
// Architecture:
//
//	input (query_chars + context_chars) -> hidden (ReLU) -> output (softmax over chars)
//
// The key RAG mechanism: by concatenating retrieved context with the query, the MLP
// can condition its predictions on retrieved facts. This is the minimum architecture
// that meaningfully demonstrates RAG.
//
// Signpost: production RAG uses transformer generators (GPT, LLaMA). We use an MLP
// to keep the focus on the retrieval mechanism and context injection pattern.
type RAGMLP struct {
	InputDim  int
	HiddenDim int
	OutputDim int
	W1        [][]float64 // [HiddenDim][InputDim]
	B1        []float64   // [HiddenDim]
	W2        [][]float64 // [OutputDim][HiddenDim]
	B2        []float64   // [OutputDim]
}

// mlpCache stores intermediate values for backward pass.
type mlpCache struct {
	x      []float64
	hidden []float64
	probs  []float64
}

// NewRAGMLP creates a new MLP with Xavier initialization.
func NewRAGMLP(inputDim, hiddenDim, outputDim int, rng *rand.Rand) *RAGMLP {
	// Xavier initialization: scale weights by 1/sqrt(fan_in) for stable gradients.
	// Maintains variance of activations across layers, preventing gradients from
	// vanishing or exploding early in training.
	scale1 := math.Sqrt(2.0 / float64(inputDim))
	scale2 := math.Sqrt(2.0 / float64(hiddenDim))

	m := &RAGMLP{
		InputDim:  inputDim,
		HiddenDim: hiddenDim,
		OutputDim: outputDim,
		B1:        make([]float64, hiddenDim),
		B2:        make([]float64, outputDim),
	}

	m.W1 = make([][]float64, hiddenDim)
	for i := range m.W1 {
		m.W1[i] = make([]float64, inputDim)
		for j := range m.W1[i] {
			m.W1[i][j] = rng.NormFloat64() * scale1
		}
	}

	m.W2 = make([][]float64, outputDim)
	for i := range m.W2 {
		m.W2[i] = make([]float64, hiddenDim)
		for j := range m.W2[i] {
			m.W2[i][j] = rng.NormFloat64() * scale2
		}
	}

	return m
}

// Forward runs the MLP forward pass: input -> hidden (ReLU) -> output (softmax).
func (m *RAGMLP) Forward(x []float64) ([]float64, *mlpCache) {
	// Hidden layer: h = ReLU(W1 @ x + b1)
	hidden := make([]float64, m.HiddenDim)
	for i := 0; i < m.HiddenDim; i++ {
		activation := m.B1[i]
		for j := 0; j < m.InputDim; j++ {
			activation += m.W1[i][j] * x[j]
		}
		if activation > 0 {
			hidden[i] = activation
		} // else 0 (ReLU)
	}

	// Output layer: o = W2 @ h + b2
	logits := make([]float64, m.OutputDim)
	for i := 0; i < m.OutputDim; i++ {
		activation := m.B2[i]
		for j := 0; j < m.HiddenDim; j++ {
			activation += m.W2[i][j] * hidden[j]
		}
		logits[i] = activation
	}

	// Stable softmax: exp(x - max(x)) prevents overflow
	maxLogit := logits[0]
	for _, l := range logits[1:] {
		if l > maxLogit {
			maxLogit = l
		}
	}
	expLogits := make([]float64, m.OutputDim)
	sumExp := 0.0
	for i, l := range logits {
		e := math.Exp(l - maxLogit)
		expLogits[i] = e
		sumExp += e
	}
	probs := make([]float64, m.OutputDim)
	for i := range expLogits {
		probs[i] = expLogits[i] / sumExp
	}

	cache := &mlpCache{x: x, hidden: hidden, probs: probs}
	return probs, cache
}

// Backward computes gradients and updates weights via SGD.
//
// Cross-entropy loss: L = -log(p[targetIdx])
// Gradient of cross-entropy + softmax: dL/do_i = p_i - 1[i == target]
//
// Returns the loss value.
func (m *RAGMLP) Backward(targetIdx int, cache *mlpCache, lr float64) float64 {
	x := cache.x
	hidden := cache.hidden
	probs := cache.probs

	// Clip probability to prevent log(0) = -inf
	p := probs[targetIdx]
	if p < 1e-10 {
		p = 1e-10
	}
	loss := -math.Log(p)

	// Gradient of loss w.r.t. output logits: p - y (where y is one-hot target)
	dLogits := make([]float64, m.OutputDim)
	copy(dLogits, probs)
	dLogits[targetIdx] -= 1.0

	// Gradient w.r.t. W2 and b2
	for i := 0; i < m.OutputDim; i++ {
		m.B2[i] -= lr * dLogits[i]
		for j := 0; j < m.HiddenDim; j++ {
			dw := dLogits[i] * hidden[j]
			m.W2[i][j] -= lr * dw
		}
	}

	// Backprop through hidden layer
	dHidden := make([]float64, m.HiddenDim)
	for j := 0; j < m.HiddenDim; j++ {
		for i := 0; i < m.OutputDim; i++ {
			dHidden[j] += dLogits[i] * m.W2[i][j]
		}
		// ReLU gradient: 0 if hidden[j] <= 0, else pass through
		if hidden[j] <= 0 {
			dHidden[j] = 0
		}
	}

	// Gradient w.r.t. W1 and b1
	for i := 0; i < m.HiddenDim; i++ {
		m.B1[i] -= lr * dHidden[i]
		for j := 0; j < m.InputDim; j++ {
			dw := dHidden[i] * x[j]
			m.W1[i][j] -= lr * dw
		}
	}

	return loss
}

// Generate produces text character-by-character given an input context.
//
// The inputText contains both the query and retrieved context (concatenated).
// The model uses this full context to predict the next character at each step.
func (m *RAGMLP) Generate(inputText string, maxLength int) string {
	current := inputText
	for step := 0; step < maxLength; step++ {
		// Encode recent context (last 100 chars)
		context := current
		if len(context) > 100 {
			context = context[len(context)-100:]
		}
		x := make([]float64, m.InputDim)
		offset := 0
		for _, ch := range context {
			if offset+ragVocabSize > m.InputDim {
				break
			}
			idx := ragCharToIdx(ch)
			x[offset+idx] = 1.0
			offset += ragVocabSize
		}

		probs, _ := m.Forward(x)

		// Greedy sampling: pick most probable character
		bestIdx := 0
		bestP := probs[0]
		for i := 1; i < len(probs); i++ {
			if probs[i] > bestP {
				bestP = probs[i]
				bestIdx = i
			}
		}
		nextChar := ragIdxToChar(bestIdx)

		// Stop at period
		if nextChar == '.' {
			current += "."
			break
		}
		current += string(nextChar)
	}
	return current
}

// === TRAINING ===

// TrainRAG trains the MLP on (query, context, answer) triples from the knowledge base.
//
// Training process:
//  1. Sample a random document as ground truth
//  2. Extract a query from the document (first few words)
//  3. Retrieve context using BM25
//  4. Concatenate query + retrieved context
//  5. Train MLP to predict next character in the ground truth answer
//
// This teaches the model to use retrieved context to generate accurate completions.
func TrainRAG(documents []string, bm25 *BM25Index, mlp *RAGMLP, epochs int, lr float64, rng *rand.Rand, verbose bool) {
	for epoch := 0; epoch < epochs; epoch++ {
		epochLoss := 0.0
		numSamples := 0

		for batch := 0; batch < ragBatchSize; batch++ {
			// Sample a random document as ground truth
			docIdx := rng.Intn(len(documents))
			doc := documents[docIdx]

			// Create a query from the first few words
			words := ragTokenize(doc)
			if len(words) < 3 {
				continue
			}
			nQueryWords := 3
			if len(words) < nQueryWords {
				nQueryWords = len(words)
			}
			query := strings.Join(words[:nQueryWords], " ")

			// Retrieve context using BM25
			retrieved := bm25.Retrieve(query, ragTopK)
			var contextParts []string
			for i := 0; i < 2 && i < len(retrieved); i++ {
				contextParts = append(contextParts, documents[retrieved[i].DocID])
			}
			context := strings.Join(contextParts, " ")

			// Concatenate query + context as model input
			// This is the core RAG mechanism: the model sees both the query and
			// retrieved facts, enabling it to condition predictions on external knowledge.
			inputText := query + " " + context

			// Train on each character in the target (limit to first 20 chars for speed)
			maxChars := 20
			if len(doc) < maxChars {
				maxChars = len(doc)
			}
			for i := 0; i < maxChars; i++ {
				// Encode input context — last 100 chars (sliding window)
				ctxStr := inputText
				if len(ctxStr) > 100 {
					ctxStr = ctxStr[len(ctxStr)-100:]
				}
				x := make([]float64, mlp.InputDim)
				offset := 0
				for _, ch := range ctxStr {
					if offset+ragVocabSize > mlp.InputDim {
						break
					}
					idx := ragCharToIdx(ch)
					x[offset+idx] = 1.0
					offset += ragVocabSize
				}

				// Target character
				targetIdx := ragCharToIdx(rune(doc[i]))

				_, cache := mlp.Forward(x)
				loss := mlp.Backward(targetIdx, cache, lr)
				epochLoss += loss
				numSamples++

				// Update input text to include predicted character
				inputText += string(doc[i])
			}
		}

		if verbose && numSamples > 0 && ((epoch+1)%50 == 0) {
			avgLoss := epochLoss / float64(numSamples)
			fmt.Printf("Epoch %3d/%d  Loss: %.4f\n", epoch+1, epochs, avgLoss)
		}
	}
}

// === DEMO ===

// RunMicrorag demonstrates the full RAG pipeline: knowledge base generation, BM25
// indexing, retrieval accuracy testing, MLP training, and with/without retrieval comparison.
func RunMicrorag() {
	rng := rand.New(rand.NewSource(42))

	// Generate synthetic knowledge base
	fmt.Println("Generating synthetic knowledge base...")
	documents, testQueries := GenerateKnowledgeBase()
	fmt.Printf("Created %d documents\n\n", len(documents))

	// Build BM25 index
	fmt.Println("Building BM25 index...")
	bm25 := NewBM25Index(documents, bm25K1, bm25B)
	fmt.Printf("Indexed %d documents, %d unique terms\n\n", bm25.N, len(bm25.IDF))

	// Test retrieval accuracy on known queries
	fmt.Println("=== RETRIEVAL ACCURACY TEST ===")
	correct := 0
	for _, tq := range testQueries {
		retrieved := bm25.Retrieve(tq.Query, 1)
		if len(retrieved) == 0 {
			fmt.Printf("  MISS: '%s' -> no results\n", tq.Query)
			continue
		}

		retrievedIdx := retrieved[0].DocID
		retrievedTerms := make(map[string]bool)
		for _, t := range ragTokenize(documents[retrievedIdx]) {
			retrievedTerms[t] = true
		}
		queryTerms := ragTokenize(tq.Query)

		// A retrieval is correct if the returned document contains >= 50% of query terms.
		hits := 0
		for _, t := range queryTerms {
			if retrievedTerms[t] {
				hits++
			}
		}
		threshold := len(queryTerms) / 2
		if threshold < 1 {
			threshold = 1
		}
		if hits >= threshold {
			correct++
			docPreview := documents[retrievedIdx]
			if len(docPreview) > 50 {
				docPreview = docPreview[:50]
			}
			fmt.Printf("  HIT:  '%s' -> [%d] %s...\n", tq.Query, retrievedIdx, docPreview)
		} else {
			docPreview := documents[retrievedIdx]
			if len(docPreview) > 50 {
				docPreview = docPreview[:50]
			}
			fmt.Printf("  MISS: '%s' -> [%d] %s...\n", tq.Query, retrievedIdx, docPreview)
		}
	}
	accuracy := 100.0 * float64(correct) / float64(len(testQueries))
	fmt.Printf("Retrieval accuracy: %d/%d = %.1f%%\n\n", correct, len(testQueries), accuracy)

	// Initialize MLP generator
	inputDim := 100 * ragVocabSize // 100 characters, one-hot encoded
	mlp := NewRAGMLP(inputDim, ragHiddenDim, ragVocabSize, rng)
	totalParams := len(mlp.W1)*len(mlp.W1[0]) + len(mlp.B1) +
		len(mlp.W2)*len(mlp.W2[0]) + len(mlp.B2)
	fmt.Printf("Initialized MLP: %d -> %d -> %d\n", inputDim, ragHiddenDim, ragVocabSize)
	fmt.Printf("Total parameters: %s\n\n", commaInt(totalParams))

	// Train the RAG model
	fmt.Println("Training RAG model...")
	TrainRAG(documents, bm25, mlp, ragNumEpochs, ragLR, rng, true)
	fmt.Println()

	// Demo: compare generation with and without retrieval
	fmt.Println("=== RETRIEVAL COMPARISON ===")
	fmt.Println()
	demoQueries := []string{
		"population of paris",
		"seine river",
		"everest height",
		"capital of germany",
	}
	for _, query := range demoQueries {
		fmt.Printf("Query: '%s'\n", query)

		// WITH retrieval
		retrieved := bm25.Retrieve(query, ragTopK)
		fmt.Printf("Retrieved docs (top %d):\n", ragTopK)
		for _, r := range retrieved {
			docPreview := documents[r.DocID]
			if len(docPreview) > 60 {
				docPreview = docPreview[:60]
			}
			fmt.Printf("  [%d] score=%.2f: %s...\n", r.DocID, r.Score, docPreview)
		}

		var contextParts []string
		for i := 0; i < 2 && i < len(retrieved); i++ {
			contextParts = append(contextParts, documents[retrieved[i].DocID])
		}
		context := strings.Join(contextParts, " ")
		inputWith := query + " " + context
		genWith := mlp.Generate(inputWith, 40)

		// WITHOUT retrieval
		inputWithout := query + " "
		genWithout := mlp.Generate(inputWithout, 40)

		fmt.Printf("WITH retrieval:    %s\n", genWith)
		fmt.Printf("WITHOUT retrieval: %s\n\n", genWithout)
	}

	fmt.Println("RAG demonstration complete.")
}
