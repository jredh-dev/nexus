// Package lens provides equivalence functions ("lenses") that determine
// whether two submitted secrets are "the same." Each lens collapses
// distinct byte sequences into a canonical form. If any lens maps a
// new submission to an existing secret's canonical form, the secret
// has been admitted before.
//
// The lenses are the puzzle. Players discover them through experimentation.
package lens

import (
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Lens maps an input string to zero or more canonical forms.
// Returning multiple forms means the input "matches" via multiple paths.
type Lens interface {
	// Name returns a short identifier for this lens (e.g. "casefold").
	Name() string
	// Canonicalize returns canonical forms of the input.
	// An empty slice means this lens doesn't apply.
	Canonicalize(s string) []string
}

// All returns the default set of lenses in evaluation order.
func All() []Lens {
	return []Lens{
		CaseFold{},
		UnicodeCaseFold{},
		Palindrome{},
		HexDecode{},
		Homoglyph{},
	}
}

// CanonicalizeThroughAll runs input through every lens and returns
// all canonical forms keyed by lens name. The "identity" key is
// always present (raw bytes, NFC-normalized).
func CanonicalizeThroughAll(s string, lenses []Lens) map[string][]string {
	out := make(map[string][]string)

	// Identity: NFC-normalize so visually identical unicode compares equal.
	identity := norm.NFC.String(s)
	out["identity"] = []string{identity}

	for _, l := range lenses {
		forms := l.Canonicalize(s)
		if len(forms) > 0 {
			out[l.Name()] = forms
		}
	}
	return out
}

// --- Lens implementations ---

// CaseFold collapses ASCII case: "Hello" → "hello".
type CaseFold struct{}

func (CaseFold) Name() string                   { return "casefold" }
func (CaseFold) Canonicalize(s string) []string { return []string{strings.ToLower(s)} }

// UnicodeCaseFold uses full Unicode case folding (ß → ss, ﬁ → fi).
type UnicodeCaseFold struct{}

func (UnicodeCaseFold) Name() string { return "unicode_casefold" }
func (UnicodeCaseFold) Canonicalize(s string) []string {
	return []string{strings.ToLower(norm.NFC.String(s))}
}

// Palindrome detects palindromic inputs. A palindrome reads the same
// forwards and backwards — submitting one is inherently admitting it twice.
type Palindrome struct{}

func (Palindrome) Name() string { return "palindrome" }
func (Palindrome) Canonicalize(s string) []string {
	lower := strings.ToLower(norm.NFC.String(s))
	var cleaned []rune
	for _, r := range lower {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cleaned = append(cleaned, r)
		}
	}
	if len(cleaned) < 2 {
		return nil
	}
	n := len(cleaned)
	for i := 0; i < n/2; i++ {
		if cleaned[i] != cleaned[n-1-i] {
			return nil
		}
	}
	return []string{"__palindrome__:" + string(cleaned)}
}

// HexDecode treats the input as a hex-encoded string. If it decodes
// to valid UTF-8, the decoded form is a canonical alias.
// "6869" collapses to "hi".
type HexDecode struct{}

func (HexDecode) Name() string { return "hexdecode" }
func (HexDecode) Canonicalize(s string) []string {
	cleaned := strings.TrimSpace(s)
	if len(cleaned) >= 2 && len(cleaned)%2 == 0 {
		if decoded, err := hex.DecodeString(cleaned); err == nil && utf8.Valid(decoded) {
			result := string(decoded)
			if result != s {
				return []string{result}
			}
		}
	}
	return []string{s}
}

// Homoglyph maps common visual lookalikes to their ASCII equivalents.
// Cyrillic "а" (U+0430) → Latin "a" (U+0061), etc.
type Homoglyph struct{}

func (Homoglyph) Name() string { return "homoglyph" }
func (Homoglyph) Canonicalize(s string) []string {
	var b strings.Builder
	for _, r := range s {
		if mapped, ok := homoglyphMap[r]; ok {
			b.WriteRune(mapped)
		} else {
			b.WriteRune(r)
		}
	}
	return []string{b.String()}
}

var homoglyphMap = map[rune]rune{
	// Cyrillic → Latin
	'\u0430': 'a', '\u0435': 'e', '\u043E': 'o', '\u0440': 'p',
	'\u0441': 'c', '\u0443': 'y', '\u0445': 'x',
	'\u0410': 'A', '\u0415': 'E', '\u041E': 'O', '\u0420': 'P',
	'\u0421': 'C', '\u0422': 'T', '\u041D': 'H',
	// Fullwidth → ASCII
	'\uFF41': 'a', '\uFF42': 'b', '\uFF43': 'c',
	// Greek → Latin
	'\u0391': 'A', '\u0392': 'B', '\u0395': 'E', '\u0397': 'H',
	'\u039A': 'K', '\u039C': 'M', '\u039D': 'N', '\u039F': 'O',
	'\u03A1': 'P', '\u03A4': 'T', '\u03A5': 'Y', '\u03A7': 'X',
	'\u03B1': 'a', '\u03BF': 'o',
}
