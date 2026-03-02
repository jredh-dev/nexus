//go:build research

// How text becomes numbers -- the compression algorithm hiding inside every LLM.
// Byte-Pair Encoding learns a vocabulary by iteratively merging the most frequent
// adjacent token pairs, then encodes new text by replaying those merges in priority order.
//
// Reference: Philip Gage, "A New Algorithm for Data Compression" (1994).
// GPT-2's byte-level BPE variant (Radford et al., 2019) starts from raw bytes
// rather than characters -- that's the version implemented here.
//
// Port of mathews-tom/no-magic microtokenizer.py to Go.
// AGPL-3.0 -- see LICENSE in repo root.
package ml

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// === CONSTANTS ===

const (
	// Final vocab = 256 byte tokens + 256 merges = 512 tokens.
	// Signpost: production tokenizers (GPT-2, GPT-4) use 50K+ merges trained on
	// hundreds of gigabytes. 256 merges on 18KB is a toy, but the algorithm is identical.
	DefaultNumMerges = 256

	tokenizerDataURL  = "https://raw.githubusercontent.com/karpathy/makemore/master/names.txt"
	tokenizerDataFile = "names.txt"
)

// BPEPair represents an adjacent pair of token IDs. Used as map key for counting.
// Go's [2]int is hashable and value-typed -- no need for Python's tuple hashing overhead.
type BPEPair [2]int

// BPEMerge records one BPE merge rule: replace (A, B) with NewID.
type BPEMerge struct {
	Pair  BPEPair
	NewID int
}

// === DATA LOADING ===

// loadTokenizerData downloads the dataset if not cached locally and returns the raw bytes.
func loadTokenizerData(url, filename string) ([]byte, error) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		fmt.Printf("Downloading %s...\n", filename)
		resp, err := http.Get(url)
		if err != nil {
			return nil, fmt.Errorf("download failed: %w", err)
		}
		defer resp.Body.Close()

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}

		if err := os.WriteFile(filename, data, 0644); err != nil {
			return nil, fmt.Errorf("write file: %w", err)
		}

		return data, nil
	}

	return os.ReadFile(filename)
}

// === BPE TRAINING ===

// BPEPairCounts counts the frequency of every adjacent token pair.
//
// For sequence s = [s_0, s_1, ..., s_n], we count all (s_i, s_{i+1}) pairs.
// Example: [a, b, c, b, c] -> {(a,b): 1, (b,c): 2, (c,b): 1}.
// This is the core statistic BPE uses to decide what to merge next.
func BPEPairCounts(ids []int) map[BPEPair]int {
	counts := make(map[BPEPair]int, len(ids)/2) // pre-size: at most n-1 unique pairs
	for i := 0; i < len(ids)-1; i++ {
		p := BPEPair{ids[i], ids[i+1]}
		counts[p]++
	}
	return counts
}

// BPEApplyMerge replaces every occurrence of pair with newID in a single left-to-right pass.
//
// Overlapping pairs resolve left-to-right: in [a, a, a] merging (a,a) produces
// [new, a], not [a, new]. This matches the standard BPE convention and ensures
// the merge operation is deterministic regardless of pair overlap patterns.
//
// Signpost: this O(n) scan runs once per merge, giving O(n * M) total training
// cost for M merges. Production implementations (SentencePiece, tiktoken) use
// priority queues for O(n log n) total, but the output is identical.
func BPEApplyMerge(ids []int, pair BPEPair, newID int) []int {
	// Pre-allocate: merged can be at most len(ids) (no merges) and at least
	// len(ids)/2 + 1 (every pair merged). Use len(ids) to avoid reallocation.
	merged := make([]int, 0, len(ids))
	i := 0
	for i < len(ids) {
		if i < len(ids)-1 && ids[i] == pair[0] && ids[i+1] == pair[1] {
			merged = append(merged, newID)
			i += 2 // consumed both tokens in the pair
		} else {
			merged = append(merged, ids[i])
			i++
		}
	}
	return merged
}

// BPETrain learns BPE merge rules by greedily merging the most frequent adjacent pair.
//
// Each merge absorbs the single most redundant pair in the corpus -- a greedy
// compression step that naturally discovers morphological units ("an" + "a",
// "el" + "la") without any linguistic rules. The merge table is ordered by
// priority: merge 0 was most frequent in the original corpus, merge 1 most
// frequent after merge 0, and so on. This ordering is critical for encoding.
//
// Returns: ordered slice of BPEMerge where NewID = 256 + merge_index.
func BPETrain(ids []int, nMerges int) []BPEMerge {
	// Work on a copy so we don't mutate the caller's slice.
	working := make([]int, len(ids))
	copy(working, ids)

	merges := make([]BPEMerge, 0, nMerges)

	for i := 0; i < nMerges; i++ {
		counts := BPEPairCounts(working)
		if len(counts) == 0 {
			// Entire corpus collapsed to a single token (or is empty). Rare in
			// practice, but correct to handle: no more pairs means no more merges.
			break
		}

		// Find the pair with the highest count.
		// Tie-break by smallest pair (lower first element, then lower second)
		// so output is deterministic despite Go map iteration order.
		var bestPair BPEPair
		bestCount := 0
		for p, c := range counts {
			if c > bestCount || (c == bestCount && (p[0] < bestPair[0] || (p[0] == bestPair[0] && p[1] < bestPair[1]))) {
				bestCount = c
				bestPair = p
			}
		}

		newID := 256 + i // byte IDs 0-255 reserved; merges start at 256
		working = BPEApplyMerge(working, bestPair, newID)
		merges = append(merges, BPEMerge{Pair: bestPair, NewID: newID})

		if (i+1)%32 == 0 || i == 0 {
			fmt.Printf("  merge %3d/%d: (%3d, %3d) -> %3d  freq=%5d  corpus_len=%d\n",
				i+1, nMerges, bestPair[0], bestPair[1], newID, bestCount, len(working))
		}
	}

	return merges
}

// === ENCODING & DECODING ===

// BPEBuildVocab builds a token ID -> byte string lookup table.
//
// Base vocabulary: 256 entries mapping each byte value to its single-byte slice.
// Each merge extends the table: vocab[newID] = vocab[a] + vocab[b].
// This recursive expansion means decoding is just a table lookup -- no merge
// replay needed, and round-trip correctness is guaranteed by construction.
func BPEBuildVocab(merges []BPEMerge) map[int][]byte {
	vocab := make(map[int][]byte, 256+len(merges))
	for i := 0; i < 256; i++ {
		vocab[i] = []byte{byte(i)}
	}
	for _, m := range merges {
		// Concatenate the byte representations of the two merged tokens.
		a := vocab[m.Pair[0]]
		b := vocab[m.Pair[1]]
		combined := make([]byte, len(a)+len(b))
		copy(combined, a)
		copy(combined[len(a):], b)
		vocab[m.NewID] = combined
	}
	return vocab
}

// BPEEncode converts a string to BPE token IDs by replaying merges in priority order.
//
// Critical: merges are applied in the order they were learned (priority order),
// NOT by re-counting frequencies on the new text. Priority order ensures
// deterministic tokenization -- the same string always produces the same token
// sequence, regardless of what other text the tokenizer was trained on.
// Re-counting frequencies would make the output dependent on the input batch,
// breaking the contract that tokenization is a pure function of the input string.
//
// Signpost: this O(n * M) naive encoding checks every merge against the full
// sequence. Production tokenizers (tiktoken, HuggingFace) use trie structures
// for O(n) encoding, but produce identical output.
func BPEEncode(text string, merges []BPEMerge) []int {
	// Start with raw UTF-8 bytes as token IDs.
	raw := []byte(text)
	ids := make([]int, len(raw))
	for i, b := range raw {
		ids[i] = int(b)
	}

	// Replay merges in priority order.
	for _, m := range merges {
		ids = BPEApplyMerge(ids, m.Pair, m.NewID)
	}
	return ids
}

// BPEDecode converts token IDs back to a string via byte lookup and UTF-8 decoding.
//
// Every token maps to a definite byte sequence through the vocab table, so
// BPEDecode(BPEEncode(text)) == text is guaranteed for any valid UTF-8 input.
// Decoding is trivially simple by design -- all the complexity lives in encoding.
func BPEDecode(ids []int, vocab map[int][]byte) string {
	var buf []byte
	for _, id := range ids {
		buf = append(buf, vocab[id]...)
	}
	return string(buf)
}

// === DEMO ===

// RunMicrotokenizer trains a BPE tokenizer on the names corpus and demonstrates
// encoding, decoding, compression ratio, and round-trip correctness.
func RunMicrotokenizer() {
	raw, err := loadTokenizerData(tokenizerDataURL, tokenizerDataFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading data: %v\n", err)
		os.Exit(1)
	}

	// Starting from raw bytes means every possible input is representable --
	// there are no "unknown token" problems. This is the key insight of byte-level
	// BPE: the base vocabulary covers all of Unicode (via UTF-8 byte sequences)
	// without needing a character-level vocabulary for every writing system.
	corpusIDs := make([]int, len(raw))
	for i, b := range raw {
		corpusIDs[i] = int(b)
	}

	fmt.Printf("Corpus: %d bytes, base vocab: 256 byte tokens\n", len(raw))
	fmt.Printf("Training %d merges (final vocab: %d tokens)\n\n", DefaultNumMerges, 256+DefaultNumMerges)

	// -- Train --
	fmt.Println("Training BPE...")
	merges := BPETrain(corpusIDs, DefaultNumMerges)
	vocab := BPEBuildVocab(merges)
	fmt.Printf("\nTraining complete: %d merges learned\n\n", len(merges))

	// -- Round-trip tests --
	testStrings := []string{"Emma", "Xiomara", "Mary-Jane", "O'Brien", "", "Z"}
	fmt.Println("Round-trip tests:")
	for _, s := range testStrings {
		encoded := BPEEncode(s, merges)
		decoded := BPEDecode(encoded, vocab)

		status := "PASS"
		if decoded != s {
			status = "FAIL"
		}

		display := fmt.Sprintf("%q", s)
		if s == "" {
			display = `""`
		}
		fmt.Printf("  [%s] %-14s -> %2d tokens -> %q\n", status, display, len(encoded), decoded)
	}
	fmt.Println()

	// -- Compression ratio --
	corpusEncoded := BPEEncode(string(raw), merges)
	ratio := float64(len(raw)) / float64(len(corpusEncoded))
	fmt.Printf("Compression: %d bytes -> %d tokens (ratio: %.2fx)\n\n",
		len(raw), len(corpusEncoded), ratio)

	// -- Top 20 merges --
	fmt.Println("Top 20 merges (earliest = highest priority):")
	limit := 20
	if len(merges) < limit {
		limit = len(merges)
	}
	for i := 0; i < limit; i++ {
		m := merges[i]
		aStr := string(vocab[m.Pair[0]])
		bStr := string(vocab[m.Pair[1]])
		mergedStr := string(vocab[m.NewID])
		fmt.Printf("  %2d. %6q + %-6q -> %q\n", i+1, aStr, bStr, mergedStr)
	}
	fmt.Println()

	// -- Tokenization example --
	example := "Elizabeth"
	exampleTokens := BPEEncode(example, merges)
	pieces := make([]string, len(exampleTokens))
	for i, tid := range exampleTokens {
		pieces[i] = string(vocab[tid])
	}
	fmt.Printf("Tokenization example: %q\n", example)
	fmt.Printf("  Bytes:  %v\n", []byte(example))
	fmt.Printf("  Tokens: %v\n", exampleTokens)
	fmt.Printf("  Pieces: [%s]\n", strings.Join(pieces, " "))
}
