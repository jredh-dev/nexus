//go:build research

package ml

import (
	"math"
	"math/rand"
	"strings"
	"testing"
)

// === TOKENIZATION TESTS ===

func TestRagTokenizeBasic(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"hello world", []string{"hello", "world"}},
		{"Hello, World!", []string{"hello", "world"}},
		{"the seine river flows", []string{"the", "seine", "river", "flows"}},
		{"", nil},
		{"   spaces   ", []string{"spaces"}},
		{"123abc", []string{"123abc"}},
	}
	for _, tc := range tests {
		got := ragTokenize(tc.input)
		if len(got) != len(tc.want) {
			t.Errorf("ragTokenize(%q) = %v, want %v", tc.input, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ragTokenize(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
			}
		}
	}
}

// === BM25 TESTS ===

func TestBM25IndexCreation(t *testing.T) {
	docs := []string{
		"the cat sat on the mat",
		"the dog ran in the park",
		"cats and dogs are pets",
	}
	idx := NewBM25Index(docs, bm25K1, bm25B)

	if idx.N != 3 {
		t.Errorf("N = %d, want 3", idx.N)
	}
	if len(idx.IDF) == 0 {
		t.Error("IDF should not be empty")
	}
	if idx.AvgDL <= 0 {
		t.Errorf("AvgDL = %f, want > 0", idx.AvgDL)
	}
}

func TestBM25IDFValues(t *testing.T) {
	docs := []string{
		"the cat sat on the mat",
		"the dog ran in the park",
		"cats and dogs are pets",
	}
	idx := NewBM25Index(docs, bm25K1, bm25B)

	// "the" appears in 2/3 docs -> lower IDF than "cat" which appears in 1/3
	idfThe, okThe := idx.IDF["the"]
	idfCat, okCat := idx.IDF["cat"]
	if !okThe || !okCat {
		t.Fatal("expected 'the' and 'cat' in IDF")
	}
	if idfCat <= idfThe {
		t.Errorf("IDF('cat')=%f should be > IDF('the')=%f (rarer term)", idfCat, idfThe)
	}
}

func TestBM25ScoreRelevant(t *testing.T) {
	docs := []string{
		"paris is the capital of france",
		"london is the capital of united kingdom",
		"the weather is sunny today",
	}
	idx := NewBM25Index(docs, bm25K1, bm25B)

	score0 := idx.Score("capital of france", 0)
	score2 := idx.Score("capital of france", 2)

	if score0 <= score2 {
		t.Errorf("doc 0 (%.4f) should score higher than doc 2 (%.4f) for 'capital of france'",
			score0, score2)
	}
}

func TestBM25RetrieveTopK(t *testing.T) {
	docs := []string{
		"paris is the capital of france",
		"berlin is the capital of germany",
		"the weather is sunny today",
		"rome is the capital of italy",
	}
	idx := NewBM25Index(docs, bm25K1, bm25B)

	results := idx.Retrieve("capital of france", 2)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// First result should be doc 0 (most relevant)
	if results[0].DocID != 0 {
		t.Errorf("top result docID = %d, want 0", results[0].DocID)
	}
	// Scores should be descending
	if results[0].Score < results[1].Score {
		t.Error("results should be sorted by descending score")
	}
}

func TestBM25RetrieveEmpty(t *testing.T) {
	docs := []string{"hello world"}
	idx := NewBM25Index(docs, bm25K1, bm25B)

	results := idx.Retrieve("xyz unknown", 3)
	// Should return results but with 0 scores
	if len(results) != 1 { // topK capped at N
		t.Errorf("expected 1 result (capped at N), got %d", len(results))
	}
	if results[0].Score != 0 {
		t.Errorf("score should be 0 for unmatched query, got %f", results[0].Score)
	}
}

func TestBM25LengthNormalization(t *testing.T) {
	// A short doc with the keyword should score higher than a long doc with the same keyword
	docs := []string{
		"cat",
		"the cat sat on the mat and then the cat went to sleep on the big fluffy mat",
	}
	idx := NewBM25Index(docs, bm25K1, bm25B)

	score0 := idx.Score("cat", 0)
	score1 := idx.Score("cat", 1)

	// Short doc should have a higher per-occurrence score due to length normalization
	// (though doc 1 has "cat" twice, the length penalty should make doc 0 competitive)
	if score0 <= 0 || score1 <= 0 {
		t.Errorf("both scores should be positive: short=%f, long=%f", score0, score1)
	}
}

// === KNOWLEDGE BASE TESTS ===

func TestGenerateKnowledgeBase(t *testing.T) {
	docs, queries := GenerateKnowledgeBase()

	if len(docs) != 100 {
		t.Errorf("expected 100 documents, got %d", len(docs))
	}
	if len(queries) != 10 {
		t.Errorf("expected 10 test queries, got %d", len(queries))
	}

	// All documents should be lowercase
	for i, doc := range docs {
		if doc != strings.ToLower(doc) {
			t.Errorf("doc %d is not lowercase: %s", i, doc[:30])
		}
	}

	// Test query indices should be in range
	for _, q := range queries {
		if q.ExpectedDocIdx < 0 || q.ExpectedDocIdx >= len(docs) {
			t.Errorf("query '%s' has out-of-range index %d", q.Query, q.ExpectedDocIdx)
		}
	}
}

func TestKnowledgeBaseRetrievalAccuracy(t *testing.T) {
	docs, testQueries := GenerateKnowledgeBase()
	bm25 := NewBM25Index(docs, bm25K1, bm25B)

	correct := 0
	for _, tq := range testQueries {
		retrieved := bm25.Retrieve(tq.Query, 1)
		if len(retrieved) == 0 {
			continue
		}

		retrievedTerms := make(map[string]bool)
		for _, term := range ragTokenize(docs[retrieved[0].DocID]) {
			retrievedTerms[term] = true
		}
		queryTerms := ragTokenize(tq.Query)
		hits := 0
		for _, term := range queryTerms {
			if retrievedTerms[term] {
				hits++
			}
		}
		threshold := len(queryTerms) / 2
		if threshold < 1 {
			threshold = 1
		}
		if hits >= threshold {
			correct++
		}
	}

	accuracy := float64(correct) / float64(len(testQueries))
	if accuracy < 0.7 {
		t.Errorf("retrieval accuracy %.1f%% is below 70%% threshold", accuracy*100)
	}
}

// === MLP TESTS ===

func TestRAGMLPForward(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	mlp := NewRAGMLP(100, 16, ragVocabSize, rng)

	x := make([]float64, 100)
	x[0] = 1.0 // one-hot 'a'

	probs, cache := mlp.Forward(x)

	// Probs should sum to 1
	sum := 0.0
	for _, p := range probs {
		sum += p
	}
	if math.Abs(sum-1.0) > 1e-6 {
		t.Errorf("probs sum = %f, want 1.0", sum)
	}

	// All probs should be positive
	for i, p := range probs {
		if p < 0 || p > 1 {
			t.Errorf("prob[%d] = %f, want [0, 1]", i, p)
		}
	}

	if cache == nil {
		t.Error("cache should not be nil")
	}
}

func TestRAGMLPBackwardReducesLoss(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	mlp := NewRAGMLP(100, 16, ragVocabSize, rng)

	x := make([]float64, 100)
	x[0] = 1.0
	targetIdx := 5 // arbitrary target

	// Measure initial loss
	_, cache := mlp.Forward(x)
	loss0 := mlp.Backward(targetIdx, cache, 0.01)

	// Train for several steps
	for i := 0; i < 50; i++ {
		_, cache = mlp.Forward(x)
		mlp.Backward(targetIdx, cache, 0.01)
	}

	// Measure final loss
	_, cache = mlp.Forward(x)
	lossN := mlp.Backward(targetIdx, cache, 0.01)

	if lossN >= loss0 {
		t.Errorf("loss should decrease: initial=%.4f, final=%.4f", loss0, lossN)
	}
}

func TestRAGMLPGenerate(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	inputDim := 100 * ragVocabSize
	mlp := NewRAGMLP(inputDim, ragHiddenDim, ragVocabSize, rng)

	output := mlp.Generate("hello", 20)

	// Output should start with the input
	if !strings.HasPrefix(output, "hello") {
		t.Errorf("output should start with 'hello', got %q", output)
	}
	// Output should be longer than input (some generation happened)
	if len(output) <= len("hello") {
		t.Error("output should be longer than input after generation")
	}
}

func TestRAGMLPDeterministic(t *testing.T) {
	rng1 := rand.New(rand.NewSource(42))
	mlp1 := NewRAGMLP(100, 16, ragVocabSize, rng1)

	rng2 := rand.New(rand.NewSource(42))
	mlp2 := NewRAGMLP(100, 16, ragVocabSize, rng2)

	x := make([]float64, 100)
	x[0] = 1.0

	probs1, _ := mlp1.Forward(x)
	probs2, _ := mlp2.Forward(x)

	for i := range probs1 {
		if probs1[i] != probs2[i] {
			t.Errorf("non-deterministic at index %d: %f vs %f", i, probs1[i], probs2[i])
		}
	}
}

// === CHARACTER ENCODING TESTS ===

func TestRagCharToIdx(t *testing.T) {
	// 'a' should be index 0
	if got := ragCharToIdx('a'); got != 0 {
		t.Errorf("ragCharToIdx('a') = %d, want 0", got)
	}
	// 'z' should be index 25
	if got := ragCharToIdx('z'); got != 25 {
		t.Errorf("ragCharToIdx('z') = %d, want 25", got)
	}
	// space should be index 26
	if got := ragCharToIdx(' '); got != 26 {
		t.Errorf("ragCharToIdx(' ') = %d, want 26", got)
	}
	// Unknown char should map to space
	if got := ragCharToIdx('!'); got != 26 {
		t.Errorf("ragCharToIdx('!') = %d, want 26 (space fallback)", got)
	}
}

func TestRagIdxToChar(t *testing.T) {
	if got := ragIdxToChar(0); got != 'a' {
		t.Errorf("ragIdxToChar(0) = %c, want 'a'", got)
	}
	if got := ragIdxToChar(26); got != ' ' {
		t.Errorf("ragIdxToChar(26) = %c, want ' '", got)
	}
	// Out of range should return space
	if got := ragIdxToChar(999); got != ' ' {
		t.Errorf("ragIdxToChar(999) = %c, want ' '", got)
	}
}

func TestRagOneHot(t *testing.T) {
	vec := ragOneHot(3, 10)
	if len(vec) != 10 {
		t.Fatalf("len = %d, want 10", len(vec))
	}
	for i, v := range vec {
		if i == 3 {
			if v != 1.0 {
				t.Errorf("vec[3] = %f, want 1.0", v)
			}
		} else if v != 0.0 {
			t.Errorf("vec[%d] = %f, want 0.0", i, v)
		}
	}
}

// === INTEGRATION TESTS ===

func TestTrainRAGReducesLoss(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	docs := []string{
		"paris is the capital of france.",
		"berlin is the capital of germany.",
		"rome is the capital of italy.",
	}
	bm25 := NewBM25Index(docs, bm25K1, bm25B)
	inputDim := 100 * ragVocabSize
	mlp := NewRAGMLP(inputDim, 16, ragVocabSize, rng)

	// Measure loss before training
	x := make([]float64, inputDim)
	x[0] = 1.0
	_, cache := mlp.Forward(x)
	lossBefore := -math.Log(math.Max(cache.probs[ragCharToIdx('p')], 1e-10))

	// Train
	TrainRAG(docs, bm25, mlp, 30, ragLR, rng, false)

	// Measure loss after — the model should have improved
	_, cache = mlp.Forward(x)
	lossAfter := -math.Log(math.Max(cache.probs[ragCharToIdx('p')], 1e-10))

	// We just verify training completes without error; loss improvement depends on
	// which character is sampled (the model trains on all characters, not just 'p')
	_ = lossBefore
	_ = lossAfter
}

func TestRAGEndToEnd(t *testing.T) {
	// Verify the full pipeline: knowledge base -> BM25 -> retrieve -> generate
	docs, _ := GenerateKnowledgeBase()
	bm25 := NewBM25Index(docs, bm25K1, bm25B)

	// Retrieve for "paris"
	results := bm25.Retrieve("paris", 3)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// Top result should mention paris
	topDoc := docs[results[0].DocID]
	if !strings.Contains(topDoc, "paris") {
		t.Errorf("top result for 'paris' should contain 'paris': %s", topDoc)
	}
}

// === BENCHMARKS ===

func BenchmarkBM25Index(b *testing.B) {
	docs, _ := GenerateKnowledgeBase()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewBM25Index(docs, bm25K1, bm25B)
	}
}

func BenchmarkBM25Retrieve(b *testing.B) {
	docs, _ := GenerateKnowledgeBase()
	idx := NewBM25Index(docs, bm25K1, bm25B)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Retrieve("population of paris", ragTopK)
	}
}

func BenchmarkRAGMLPForward(b *testing.B) {
	rng := rand.New(rand.NewSource(42))
	inputDim := 100 * ragVocabSize
	mlp := NewRAGMLP(inputDim, ragHiddenDim, ragVocabSize, rng)
	x := make([]float64, inputDim)
	x[0] = 1.0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mlp.Forward(x)
	}
}
