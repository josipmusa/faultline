package rules

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func rule(id string) Rule {
	return Rule{
		ID:      id,
		Name:    id,
		Enabled: true,
		Match:   Match{Host: id + ".example", Header: map[string]string{"X-Test": "1"}},
		Fault:   Fault{Type: "delay", Params: Params{"ms": 100}},
	}
}

func mustAdd(t *testing.T, s *Store, rs ...Rule) {
	t.Helper()
	for _, r := range rs {
		if err := s.Add(r); err != nil {
			t.Fatalf("Add(%q): %v", r.ID, err)
		}
	}
}

func ids(rs []Rule) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.ID
	}
	return out
}

func TestAddAndGet(t *testing.T) {
	s := New()
	mustAdd(t, s, rule("a"))

	got, err := s.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "a" || got.Match.Host != "a.example" || got.Fault.Params["ms"] != 100 {
		t.Errorf("Get returned %+v", got)
	}
}

func TestAddRejectsDuplicateID(t *testing.T) {
	s := New()
	mustAdd(t, s, rule("a"))

	err := s.Add(rule("a"))
	if !errors.Is(err, ErrExists) {
		t.Errorf("Add duplicate = %v, want ErrExists", err)
	}
	if n := len(s.List()); n != 1 {
		t.Errorf("store holds %d rules, want 1", n)
	}
}

func TestAddRejectsEmptyID(t *testing.T) {
	s := New()

	err := s.Add(rule(""))
	if !errors.Is(err, ErrEmptyID) {
		t.Errorf("Add with no id = %v, want ErrEmptyID", err)
	}
}

func TestGetMissing(t *testing.T) {
	s := New()

	if _, err := s.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get missing = %v, want ErrNotFound", err)
	}
}

func TestListPreservesInsertionOrder(t *testing.T) {
	s := New()
	mustAdd(t, s, rule("c"), rule("a"), rule("b"))

	if got := ids(s.List()); fmt.Sprint(got) != "[c a b]" {
		t.Errorf("List order = %v, want [c a b]", got)
	}
}

func TestListOfEmptyStore(t *testing.T) {
	if got := New().List(); len(got) != 0 {
		t.Errorf("List of an empty store = %v, want empty", got)
	}
}

func TestStoreKeepsCopies(t *testing.T) {
	st := New()
	original := rule("a")
	mustAdd(t, st, original)

	// Mutating what the caller still holds must not reach into the store.
	original.Match.Header["X-Test"] = "tampered"

	got, err := st.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Match.Header["X-Test"] != "1" {
		t.Error("Add must store a copy, not the caller's rule")
	}

	// Mutating what Get handed back must not reach into the store either.
	got.Match.Header["X-Test"] = "tampered"
	again, err := st.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if again.Match.Header["X-Test"] != "1" {
		t.Error("Get must return a copy")
	}

	// Nor must mutating what List handed back.
	list := st.List()
	list[0].Match.Header["X-Test"] = "tampered"
	if final, _ := st.Get("a"); final.Match.Header["X-Test"] != "1" {
		t.Error("List must return copies")
	}
}

func TestUpdateReplacesInPlace(t *testing.T) {
	st := New()
	mustAdd(t, st, rule("a"), rule("b"), rule("c"))

	updated := rule("b")
	updated.Name = "renamed"
	updated.Fault = Fault{Type: "status", Params: Params{"code": 503}}
	if err := st.Update(updated); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := st.Get("b")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "renamed" || got.Fault.Type != "status" || got.Fault.Params["code"] != 503 {
		t.Errorf("Update did not apply: %+v", got)
	}
	if order := ids(st.List()); fmt.Sprint(order) != "[a b c]" {
		t.Errorf("Update changed the order to %v, want [a b c]", order)
	}
}

func TestUpdateMissing(t *testing.T) {
	st := New()

	if err := st.Update(rule("nope")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update missing = %v, want ErrNotFound", err)
	}
}

func TestDelete(t *testing.T) {
	st := New()
	mustAdd(t, st, rule("a"), rule("b"), rule("c"))

	if err := st.Delete("b"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if order := ids(st.List()); fmt.Sprint(order) != "[a c]" {
		t.Errorf("after Delete List = %v, want [a c]", order)
	}
	if _, err := st.Get("b"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
}

func TestDeleteMissing(t *testing.T) {
	st := New()

	if err := st.Delete("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete missing = %v, want ErrNotFound", err)
	}
}

func TestEnableAndDisable(t *testing.T) {
	st := New()
	mustAdd(t, st, rule("a"))

	if err := st.Disable("a"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if got, _ := st.Get("a"); got.Enabled {
		t.Error("Disable did not clear the enabled flag")
	}

	if err := st.Enable("a"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if got, _ := st.Get("a"); !got.Enabled {
		t.Error("Enable did not set the enabled flag")
	}
}

func TestEnableDisableMissing(t *testing.T) {
	st := New()

	if err := st.Enable("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Enable missing = %v, want ErrNotFound", err)
	}
	if err := st.Disable("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Disable missing = %v, want ErrNotFound", err)
	}
}

func TestChangesNotifiesOnEveryMutation(t *testing.T) {
	st := New()
	changes := st.Changes()

	mutations := []struct {
		name string
		do   func() error
	}{
		{"add", func() error { return st.Add(rule("a")) }},
		{"update", func() error { return st.Update(rule("a")) }},
		{"disable", func() error { return st.Disable("a") }},
		{"enable", func() error { return st.Enable("a") }},
		{"delete", func() error { return st.Delete("a") }},
	}
	for _, m := range mutations {
		drain(changes)
		if err := m.do(); err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		select {
		case <-changes:
		default:
			t.Errorf("%s did not notify", m.name)
		}
	}
}

func TestChangesSilentOnFailedMutation(t *testing.T) {
	st := New()
	changes := st.Changes()
	mustAdd(t, st, rule("a"))
	drain(changes)

	failures := []struct {
		name string
		do   func() error
	}{
		{"duplicate add", func() error { return st.Add(rule("a")) }},
		{"empty id add", func() error { return st.Add(rule("")) }},
		{"update missing", func() error { return st.Update(rule("nope")) }},
		{"delete missing", func() error { return st.Delete("nope") }},
		{"enable missing", func() error { return st.Enable("nope") }},
	}
	for _, f := range failures {
		if err := f.do(); err == nil {
			t.Fatalf("%s: want an error", f.name)
		}
		select {
		case <-changes:
			t.Errorf("%s notified but changed nothing", f.name)
		default:
		}
	}
}

func TestChangesCoalescesAndNeverBlocks(t *testing.T) {
	st := New()
	changes := st.Changes()

	// Nobody is reading, so these must neither block nor pile up.
	for i := range 100 {
		mustAdd(t, st, rule(fmt.Sprint(i)))
	}

	select {
	case <-changes:
	default:
		t.Fatal("want one pending notification")
	}
	select {
	case <-changes:
		t.Error("notifications should coalesce into one pending signal")
	default:
	}
}

func TestConcurrentAddAndRead(t *testing.T) {
	st := New()
	const writers, readers, perWriter = 8, 8, 50

	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perWriter {
				if err := st.Add(rule(fmt.Sprintf("w%d-r%d", w, i))); err != nil {
					t.Errorf("Add: %v", err)
					return
				}
			}
		}()
	}
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perWriter {
				for _, r := range st.List() {
					_ = r.Matches(r.Match.Host, "GET", "/", nil)
				}
				_, _ = st.Get("w0-r0")
			}
		}()
	}
	wg.Wait()

	if n := len(st.List()); n != writers*perWriter {
		t.Errorf("store holds %d rules, want %d", n, writers*perWriter)
	}
}

func drain(ch <-chan struct{}) {
	select {
	case <-ch:
	default:
	}
}
