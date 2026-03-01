package wall

import (
	"strings"
	"testing"

	"github.com/jredh-dev/nexus/services/secrets/internal/store"
)

func TestWallEmpty(t *testing.T) {
	s := store.New()
	w := New(s)
	defer w.Stop()

	text, pageIdx, totalPages, totalExposed := w.Page()
	if text != "" || pageIdx != 0 || totalPages != 0 || totalExposed != 0 {
		t.Fatalf("expected empty wall, got text=%q page=%d/%d exposed=%d", text, pageIdx, totalPages, totalExposed)
	}
}

func TestWallSinglePage(t *testing.T) {
	s := store.New()

	// Submit and expose some secrets via count > 1
	s.Submit("hello", "alice")
	s.Submit("Hello", "bob") // casefold collision → count=2, no longer secret

	s.Submit("racecar", "charlie") // palindrome — still count=1, is a secret

	w := New(s)
	defer w.Stop()

	text, pageIdx, totalPages, totalExposed := w.Page()
	if totalExposed != 1 {
		t.Fatalf("expected 1 exposed, got %d", totalExposed)
	}
	if totalPages != 1 {
		t.Fatalf("expected 1 page, got %d", totalPages)
	}
	if pageIdx != 0 {
		t.Fatalf("expected page 0, got %d", pageIdx)
	}
	if text == "" {
		t.Fatal("expected non-empty text")
	}
}

func TestWallRoundRobin(t *testing.T) {
	s := store.New()

	// Create enough exposed entries to span multiple pages.
	// Each pair of submits (same value via casefold) creates 1 exposed entry.
	for i := 0; i < PageSize+500; i++ {
		val := "secret-" + itoa(i)
		s.Submit(val, "seeder")
	}
	// Expose them all — submit again with different case
	for i := 0; i < PageSize+500; i++ {
		val := "Secret-" + itoa(i) // CaseFold collision
		s.Submit(val, "exposer")
	}

	w := New(s)
	defer w.Stop()

	_, _, totalPages, totalExposed := w.Page()
	if totalExposed != PageSize+500 {
		t.Fatalf("expected %d exposed, got %d", PageSize+500, totalExposed)
	}
	if totalPages != 2 {
		t.Fatalf("expected 2 pages, got %d", totalPages)
	}

	// Round-robin: consecutive calls should alternate pages
	_, p1, _, _ := w.Page()
	_, p2, _, _ := w.Page()
	if p1 == p2 {
		t.Fatal("expected different pages on consecutive requests")
	}
}

func TestWallTextContent(t *testing.T) {
	s := store.New()

	s.Submit("alpha", "user1")
	s.Submit("Alpha", "user2") // exposes "alpha" (count=2)

	// "kayak" is a palindrome, but in the new model palindromes don't
	// self-betray — they're just count=1 secrets. To get a second exposed
	// entry, we need another collision.
	s.Submit("beta", "user3")
	s.Submit("Beta", "user4") // exposes "beta" (count=2)

	w := New(s)
	defer w.Stop()

	text, _, _, totalExposed := w.Page()
	if totalExposed != 2 {
		t.Fatalf("expected 2 exposed, got %d", totalExposed)
	}
	lines := strings.Split(text, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
