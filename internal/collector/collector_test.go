// SPDX-License-Identifier: AGPL-3.0-or-later

package collector_test

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/jredh-dev/nexus/internal/collector"
)

// ---- helpers ---------------------------------------------------------------

func floatField(v float64) float64 { return v }

// ---- Collector tests -------------------------------------------------------

func TestAddSnapshot_Basic(t *testing.T) {
	t.Parallel()
	c := collector.New[int](time.Minute)
	c.Add(1)
	c.Add(2)
	c.Add(3)

	got := c.Snapshot()
	if len(got) != 3 {
		t.Fatalf("want 3 items, got %d", len(got))
	}
	for i, want := range []int{1, 2, 3} {
		if got[i] != want {
			t.Errorf("item[%d]: want %d, got %d", i, want, got[i])
		}
	}
}

func TestSnapshot_ReturnsACopy(t *testing.T) {
	t.Parallel()
	c := collector.New[int](time.Minute)
	c.Add(42)
	s := c.Snapshot()
	s[0] = 99 // mutate copy
	s2 := c.Snapshot()
	if s2[0] != 42 {
		t.Errorf("snapshot mutation leaked back into collector: got %d, want 42", s2[0])
	}
}

func TestTTL_StaleItemsExpired(t *testing.T) {
	t.Parallel()
	c := collector.New[string](50 * time.Millisecond)
	c.Add("old")
	time.Sleep(60 * time.Millisecond)
	c.Add("new")

	got := c.Snapshot()
	if len(got) != 1 {
		t.Fatalf("want 1 item after TTL expiry, got %d: %v", len(got), got)
	}
	if got[0] != "new" {
		t.Errorf("want \"new\", got %q", got[0])
	}
}

func TestTTL_AllExpired(t *testing.T) {
	t.Parallel()
	c := collector.New[int](20 * time.Millisecond)
	c.Add(1)
	c.Add(2)
	time.Sleep(30 * time.Millisecond)

	got := c.Snapshot()
	if len(got) != 0 {
		t.Errorf("want empty snapshot after all items expire, got %v", got)
	}
}

func TestTTL_ExpiryOnAdd(t *testing.T) {
	t.Parallel()
	c := collector.New[int](30 * time.Millisecond)
	c.Add(10)
	time.Sleep(40 * time.Millisecond)
	c.Add(20) // this Add should expire 10

	if n := c.Len(); n != 1 {
		t.Errorf("want Len=1 after stale item expires on Add, got %d", n)
	}
}

func TestLen(t *testing.T) {
	t.Parallel()
	c := collector.New[int](time.Minute)
	if c.Len() != 0 {
		t.Errorf("new collector: want Len=0, got %d", c.Len())
	}
	c.Add(1)
	c.Add(2)
	if c.Len() != 2 {
		t.Errorf("after 2 adds: want Len=2, got %d", c.Len())
	}
}

func TestClear(t *testing.T) {
	t.Parallel()
	c := collector.New[int](time.Minute)
	c.Add(1)
	c.Add(2)
	c.Clear()
	if c.Len() != 0 {
		t.Errorf("after Clear: want Len=0, got %d", c.Len())
	}
	if got := c.Snapshot(); len(got) != 0 {
		t.Errorf("after Clear: want empty snapshot, got %v", got)
	}
}

func TestConcurrent_AddSnapshot(t *testing.T) {
	t.Parallel()
	c := collector.New[int](time.Minute)

	const goroutines = 20
	const perGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				c.Add(id*perGoroutine + i)
				_ = c.Snapshot()
			}
		}(g)
	}
	wg.Wait()

	n := c.Len()
	if n < 0 || n > goroutines*perGoroutine {
		t.Errorf("unexpected Len after concurrent adds: %d", n)
	}
}

func TestConcurrent_AddLenClear(t *testing.T) {
	t.Parallel()
	c := collector.New[int](time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); c.Add(1) }()
		go func() { defer wg.Done(); _ = c.Len() }()
		go func() { defer wg.Done(); c.Clear() }()
	}
	wg.Wait()
}

// ---- Empty / single-item edge cases ----------------------------------------

func TestSnapshot_Empty(t *testing.T) {
	t.Parallel()
	c := collector.New[int](time.Minute)
	got := c.Snapshot()
	if got == nil {
		t.Error("Snapshot on empty collector should return non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("want empty, got %v", got)
	}
}

func TestSnapshot_SingleItem(t *testing.T) {
	t.Parallel()
	c := collector.New[string](time.Minute)
	c.Add("only")
	got := c.Snapshot()
	if len(got) != 1 || got[0] != "only" {
		t.Errorf("want [\"only\"], got %v", got)
	}
}

// ---- Reducer tests ---------------------------------------------------------

func TestAvg_Basic(t *testing.T) {
	t.Parallel()
	items := []float64{1, 2, 3, 4, 5}
	avg := collector.Avg(floatField)(items)
	if avg != 3.0 {
		t.Errorf("want 3.0, got %f", avg)
	}
}

func TestAvg_Empty(t *testing.T) {
	t.Parallel()
	avg := collector.Avg(floatField)(nil)
	if avg != 0 {
		t.Errorf("want 0 for empty slice, got %f", avg)
	}
}

func TestAvg_Single(t *testing.T) {
	t.Parallel()
	avg := collector.Avg(floatField)([]float64{7})
	if avg != 7 {
		t.Errorf("want 7, got %f", avg)
	}
}

func TestMedian_Odd(t *testing.T) {
	t.Parallel()
	items := []float64{5, 1, 3} // unsorted on purpose
	med := collector.Median(floatField)(items)
	if med != 3 {
		t.Errorf("want median=3, got %f", med)
	}
}

func TestMedian_Even(t *testing.T) {
	t.Parallel()
	// Even: lower middle index (len/2 = 2 → index 2 in sorted [1,2,3,4])
	items := []float64{4, 1, 3, 2}
	med := collector.Median(floatField)(items)
	if med != 3 {
		t.Errorf("want median=3 (lower-middle), got %f", med)
	}
}

func TestMedian_Empty(t *testing.T) {
	t.Parallel()
	med := collector.Median(floatField)(nil)
	if med != 0 {
		t.Errorf("want 0 for empty, got %f", med)
	}
}

func TestMedian_DoesNotMutateInput(t *testing.T) {
	t.Parallel()
	items := []float64{3, 1, 2}
	_ = collector.Median(floatField)(items)
	if items[0] != 3 || items[1] != 1 || items[2] != 2 {
		t.Error("Median mutated the input slice")
	}
}

func TestCount_Basic(t *testing.T) {
	t.Parallel()
	items := []int{10, 20, 30}
	n := collector.Count[int]()(items)
	if n != 3 {
		t.Errorf("want 3, got %d", n)
	}
}

func TestCount_Empty(t *testing.T) {
	t.Parallel()
	n := collector.Count[int]()(nil)
	if n != 0 {
		t.Errorf("want 0, got %d", n)
	}
}

func TestRate_Basic(t *testing.T) {
	t.Parallel()
	// 60 items over 15 minutes → 4 items/min
	items := make([]int, 60)
	rate := collector.Rate[int](15 * time.Minute)(items)
	const want = 4.0
	if math.Abs(rate-want) > 1e-9 {
		t.Errorf("want rate=%.4f, got %.4f", want, rate)
	}
}

func TestRate_Empty(t *testing.T) {
	t.Parallel()
	rate := collector.Rate[int](15 * time.Minute)(nil)
	if rate != 0 {
		t.Errorf("want 0 for empty slice, got %f", rate)
	}
}

func TestRate_ZeroWindow(t *testing.T) {
	t.Parallel()
	items := []int{1, 2, 3}
	rate := collector.Rate[int](0)(items)
	if rate != 0 {
		t.Errorf("want 0 for zero window, got %f", rate)
	}
}

func TestLast_Basic(t *testing.T) {
	t.Parallel()
	items := []int{1, 2, 3}
	p := collector.Last[int]()(items)
	if p == nil {
		t.Fatal("want non-nil pointer, got nil")
	}
	if *p != 3 {
		t.Errorf("want 3, got %d", *p)
	}
}

func TestLast_Empty(t *testing.T) {
	t.Parallel()
	p := collector.Last[int]()(nil)
	if p != nil {
		t.Errorf("want nil for empty slice, got %v", p)
	}
}

func TestLast_Single(t *testing.T) {
	t.Parallel()
	items := []string{"only"}
	p := collector.Last[string]()(items)
	if p == nil || *p != "only" {
		t.Errorf("want \"only\", got %v", p)
	}
}

func TestLast_ReturnsCopy(t *testing.T) {
	t.Parallel()
	items := []int{10, 20}
	p := collector.Last[int]()(items)
	*p = 99
	if items[1] != 20 {
		t.Error("Last returned a pointer into the original slice")
	}
}

func TestFilter_Basic(t *testing.T) {
	t.Parallel()
	items := []int{1, 2, 3, 4, 5}
	evens := collector.Filter(func(i int) bool { return i%2 == 0 })(items)
	if len(evens) != 2 || evens[0] != 2 || evens[1] != 4 {
		t.Errorf("want [2 4], got %v", evens)
	}
}

func TestFilter_Empty(t *testing.T) {
	t.Parallel()
	result := collector.Filter(func(i int) bool { return true })(nil)
	if result == nil {
		t.Error("Filter on nil should return non-nil slice")
	}
	if len(result) != 0 {
		t.Errorf("want empty, got %v", result)
	}
}

func TestFilter_NoneMatch(t *testing.T) {
	t.Parallel()
	items := []int{1, 3, 5}
	result := collector.Filter(func(i int) bool { return i%2 == 0 })(items)
	if len(result) != 0 {
		t.Errorf("want empty, got %v", result)
	}
}

func TestFilter_AllMatch(t *testing.T) {
	t.Parallel()
	items := []int{2, 4, 6}
	result := collector.Filter(func(i int) bool { return i%2 == 0 })(items)
	if len(result) != 3 {
		t.Errorf("want 3, got %d", len(result))
	}
}

// ---- Integration: Collector + Reducers ------------------------------------

func TestCollectorWithReducers(t *testing.T) {
	t.Parallel()
	type event struct{ v float64 }
	c := collector.New[event](time.Minute)
	for _, v := range []float64{10, 20, 30, 40, 50} {
		c.Add(event{v})
	}
	snap := c.Snapshot()

	field := func(e event) float64 { return e.v }

	if avg := collector.Avg(field)(snap); avg != 30 {
		t.Errorf("Avg: want 30, got %f", avg)
	}
	if med := collector.Median(field)(snap); med != 30 {
		t.Errorf("Median: want 30, got %f", med)
	}
	if n := collector.Count[event]()(snap); n != 5 {
		t.Errorf("Count: want 5, got %d", n)
	}
	rate := collector.Rate[event](time.Minute)(snap)
	if math.Abs(rate-5.0/1.0) > 1e-9 {
		t.Errorf("Rate: want 5.0, got %f", rate)
	}
	if p := collector.Last[event]()(snap); p == nil || p.v != 50 {
		t.Errorf("Last: want 50, got %v", p)
	}
	hi := collector.Filter(func(e event) bool { return e.v > 25 })(snap)
	if len(hi) != 3 {
		t.Errorf("Filter>25: want 3, got %d", len(hi))
	}
}
