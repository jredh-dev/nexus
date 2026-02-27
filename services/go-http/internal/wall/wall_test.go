package wall

import (
	"strings"
	"testing"

	"github.com/jredh-dev/nexus/services/go-http/internal/store"
)

func TestWallEmpty(t *testing.T) {
	s := store.New()
	w := New(s)
	defer w.Stop()

	text, pageIdx, totalPages, totalLies := w.Page()
	if text != "" || pageIdx != 0 || totalPages != 0 || totalLies != 0 {
		t.Fatalf("expected empty wall, got text=%q page=%d/%d lies=%d", text, pageIdx, totalPages, totalLies)
	}
}

func TestWallSinglePage(t *testing.T) {
	s := store.New()

	// Submit and betray some secrets
	s.Submit("hello", "alice")
	s.Submit("Hello", "bob") // casefold collision → "hello" becomes lie

	s.Submit("racecar", "charlie") // palindrome self-betrayal

	w := New(s)
	defer w.Stop()

	text, pageIdx, totalPages, totalLies := w.Page()
	if totalLies != 2 {
		t.Fatalf("expected 2 lies, got %d", totalLies)
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

	// Create enough lies to span multiple pages.
	// We need > PageSize lies. Each pair of submits creates 1 lie.
	// Submit unique values, then betray them.
	for i := 0; i < PageSize+500; i++ {
		val := "secret-" + itoa(i)
		s.Submit(val, "seeder")
	}
	// Betray them all — submit again with different case
	for i := 0; i < PageSize+500; i++ {
		val := "Secret-" + itoa(i) // CaseFold collision
		s.Submit(val, "exposer")
	}

	w := New(s)
	defer w.Stop()

	_, _, totalPages, totalLies := w.Page()
	if totalLies != PageSize+500 {
		t.Fatalf("expected %d lies, got %d", PageSize+500, totalLies)
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
	s.Submit("Alpha", "user2") // exposes "alpha"

	s.Submit("kayak", "user3") // palindrome self-betrayal

	w := New(s)
	defer w.Stop()

	text, _, _, _ := w.Page()
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
