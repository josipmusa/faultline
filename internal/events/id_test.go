package events

import (
	"strconv"
	"sync"
	"testing"
)

func TestNextIDIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id := NextID()
		if id == "" {
			t.Fatal("NextID returned an empty id")
		}
		if seen[id] {
			t.Fatalf("NextID returned %q twice", id)
		}
		seen[id] = true
	}
}

func TestNextIDIncreases(t *testing.T) {
	first, err := strconv.ParseUint(NextID(), 10, 64)
	if err != nil {
		t.Fatalf("parsing id: %v", err)
	}
	second, err := strconv.ParseUint(NextID(), 10, 64)
	if err != nil {
		t.Fatalf("parsing id: %v", err)
	}
	if second <= first {
		t.Errorf("ids %d then %d, want increasing", first, second)
	}
}

func TestNextIDIsSafeForConcurrentUse(t *testing.T) {
	const workers, each = 8, 200

	var mu sync.Mutex
	seen := map[string]bool{}

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range each {
				id := NextID()
				mu.Lock()
				if seen[id] {
					t.Errorf("duplicate id %q", id)
				}
				seen[id] = true
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	if len(seen) != workers*each {
		t.Errorf("got %d distinct ids, want %d", len(seen), workers*each)
	}
}
