// Package lens provides equivalence functions ("lenses") that determine
// whether two submitted secrets are "the same." Each lens collapses
// distinct byte sequences into a canonical form. If any lens maps a
// new submission to an existing secret's canonical form, the secret
// is exposed as a lie.
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

	// Identity: NFC-normalize so visually identical unicode compares equal
	// at the base level.
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

func (CaseFold) Name() string { return "casefold" }
func (CaseFold) Canonicalize(s string) []string {
	lower := strings.ToLower(s)
	if lower == s {
		return nil // already lowercase, no additional collapse
	}
	return []string{lower}
}

// UnicodeCaseFold uses full Unicode case folding.
// This catches things like ß → ss, ﬁ → fi.
type UnicodeCaseFold struct{}

func (UnicodeCaseFold) Name() string { return "unicode_casefold" }
func (UnicodeCaseFold) Canonicalize(s string) []string {
	folded := strings.ToLower(norm.NFC.String(s))
	if folded == norm.NFC.String(s) {
		return nil
	}
	return []string{folded}
}

// Palindrome detects palindromic inputs. A palindrome is "the same
// forwards and backwards" — submitting it is inherently submitting
// it twice. The word betrays itself.
type Palindrome struct{}

func (Palindrome) Name() string { return "palindrome" }
func (Palindrome) Canonicalize(s string) []string {
	lower := strings.ToLower(norm.NFC.String(s))
	// Strip non-letter/digit for palindrome check (so "race car" matches)
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
	isPalin := true
	for i := 0; i < n/2; i++ {
		if cleaned[i] != cleaned[n-1-i] {
			isPalin = false
			break
		}
	}
	if !isPalin {
		return nil
	}
	// A palindrome self-collapses: its canonical form is itself reversed,
	// which equals itself. Mark it with a special tag so the store knows
	// this is a self-betrayal, not a collision with another secret.
	return []string{"__palindrome__:" + string(cleaned)}
}

// HexDecode treats the input as a hex-encoded string. If it decodes
// to valid UTF-8, the decoded form is a canonical alias.
// "6869" collapses to "hi".
type HexDecode struct{}

func (HexDecode) Name() string { return "hexdecode" }
func (HexDecode) Canonicalize(s string) []string {
	// Only try if it looks like hex (even length, all hex chars)
	cleaned := strings.TrimSpace(s)
	if len(cleaned) < 2 || len(cleaned)%2 != 0 {
		return nil
	}
	decoded, err := hex.DecodeString(cleaned)
	if err != nil {
		return nil
	}
	if !utf8.Valid(decoded) {
		return nil
	}
	result := string(decoded)
	// Don't collapse if decoded == input (already plaintext hex chars)
	if result == s {
		return nil
	}
	return []string{result}
}

// Homoglyph maps common visual lookalikes to their ASCII equivalents.
// Cyrillic "а" (U+0430) → Latin "a" (U+0061), etc.
type Homoglyph struct{}

func (Homoglyph) Name() string { return "homoglyph" }
func (Homoglyph) Canonicalize(s string) []string {
	var b strings.Builder
	changed := false
	for _, r := range s {
		if mapped, ok := homoglyphMap[r]; ok {
			b.WriteRune(mapped)
			changed = true
		} else {
			b.WriteRune(r)
		}
	}
	if !changed {
		return nil
	}
	return []string{b.String()}
}

// homoglyphMap maps visually similar Unicode characters to ASCII equivalents.
// This is intentionally incomplete — discovering the gaps is part of the game.
var homoglyphMap = map[rune]rune{
	// Cyrillic → Latin
	'\u0430': 'a', // а
	'\u0435': 'e', // е
	'\u043E': 'o', // о
	'\u0440': 'p', // р
	'\u0441': 'c', // с
	'\u0443': 'y', // у
	'\u0445': 'x', // х
	'\u0410': 'A', // А
	'\u0415': 'E', // Е
	'\u041E': 'O', // О
	'\u0420': 'P', // Р
	'\u0421': 'C', // С
	'\u0422': 'T', // Т
	'\u041D': 'H', // Н

	// Fullwidth → ASCII
	'\uFF41': 'a', // ａ
	'\uFF42': 'b', // ｂ
	'\uFF43': 'c', // ｃ

	// Greek → Latin
	'\u0391': 'A', // Α (Alpha)
	'\u0392': 'B', // Β (Beta)
	'\u0395': 'E', // Ε (Epsilon)
	'\u0397': 'H', // Η (Eta)
	'\u039A': 'K', // Κ (Kappa)
	'\u039C': 'M', // Μ (Mu)
	'\u039D': 'N', // Ν (Nu)
	'\u039F': 'O', // Ο (Omicron)
	'\u03A1': 'P', // Ρ (Rho)
	'\u03A4': 'T', // Τ (Tau)
	'\u03A5': 'Y', // Υ (Upsilon)
	'\u03A7': 'X', // Χ (Chi)
	'\u03B1': 'a', // α (alpha)
	'\u03BF': 'o', // ο (omicron)
}
