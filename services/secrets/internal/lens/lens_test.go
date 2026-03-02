package lens

import (
	"testing"
)

func TestCaseFold(t *testing.T) {
	l := CaseFold{}
	tests := []struct {
		input string
		want  []string
	}{
		{"Hello", []string{"hello"}},
		{"hello", []string{"hello"}}, // always returns canonical
		{"WORLD", []string{"world"}},
		{"123", []string{"123"}}, // passthrough
	}
	for _, tt := range tests {
		got := l.Canonicalize(tt.input)
		if !sliceEq(got, tt.want) {
			t.Errorf("CaseFold(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestPalindrome(t *testing.T) {
	l := Palindrome{}
	tests := []struct {
		input   string
		wantNil bool
	}{
		{"racecar", false},  // palindrome
		{"Race Car", false}, // palindrome (case/space insensitive)
		{"hello", true},     // not a palindrome
		{"a", true},         // too short
		{"aa", false},       // minimal palindrome
		{"12321", false},    // numeric palindrome
	}
	for _, tt := range tests {
		got := l.Canonicalize(tt.input)
		if tt.wantNil && got != nil {
			t.Errorf("Palindrome(%q) = %v, want nil", tt.input, got)
		}
		if !tt.wantNil && got == nil {
			t.Errorf("Palindrome(%q) = nil, want non-nil", tt.input)
		}
	}
}

func TestHexDecode(t *testing.T) {
	l := HexDecode{}
	tests := []struct {
		input string
		want  []string
	}{
		{"6869", []string{"hi"}},          // hex for "hi"
		{"48656c6c6f", []string{"Hello"}}, // hex for "Hello"
		{"nothex", []string{"nothex"}},    // not hex, passthrough
		{"zz", []string{"zz"}},            // invalid hex, passthrough
		{"1", []string{"1"}},              // odd length, passthrough
	}
	for _, tt := range tests {
		got := l.Canonicalize(tt.input)
		if !sliceEq(got, tt.want) {
			t.Errorf("HexDecode(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestHomoglyph(t *testing.T) {
	l := Homoglyph{}
	tests := []struct {
		input string
		want  []string
	}{
		{"\u0430", []string{"a"}},        // Cyrillic а → Latin a
		{"hello", []string{"hello"}},     // all ASCII, passthrough
		{"\u0430\u0435", []string{"ae"}}, // Cyrillic аe → Latin ae
	}
	for _, tt := range tests {
		got := l.Canonicalize(tt.input)
		if !sliceEq(got, tt.want) {
			t.Errorf("Homoglyph(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestCanonicalizeThroughAll(t *testing.T) {
	lenses := All()

	// "racecar" should trigger palindrome
	result := CanonicalizeThroughAll("racecar", lenses)
	if _, ok := result["palindrome"]; !ok {
		t.Error("racecar should have palindrome canonical form")
	}

	// "6869" should have hexdecode
	result = CanonicalizeThroughAll("6869", lenses)
	if forms, ok := result["hexdecode"]; !ok {
		t.Error("6869 should have hexdecode canonical form")
	} else if len(forms) == 0 || forms[0] != "hi" {
		t.Errorf("6869 hexdecode should be 'hi', got %v", forms)
	}
}

func sliceEq(a, b []string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
