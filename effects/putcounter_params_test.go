package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestPutCounterKnownKeysSorted: the unread lookup binary-searches the table.
func TestPutCounterKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(putCounterKnownKeys[:]) || len(slices.Compact(slices.Clone(putCounterKnownKeys[:]))) != len(putCounterKnownKeys) {
		t.Fatal("putCounterKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompilePutCounter pins the compiled shapes the entry-counter fold, the
// offer gates and the resolution share: the count fallbacks, the canonical
// kind and its comma split, the shape selectors, the entry-fold gate and the
// unread report.
func TestCompilePutCounter(t *testing.T) {
	sa := &cards.SA{API: "PutCounter", Params: map[string]string{
		"CounterType": "Stun", "Adapt": "3", "ETB": "True", "Placer": " Controller ",
		"Optional": "True", "RememberPut": "True", "ChoiceTitle": "Pick", "Hidden": "True",
	}}
	p := PutCounterOf(sa)
	if p.CounterNumSet || p.CounterNum.Present || !p.AdaptSet || p.Adapt.Text != "3" || p.MonstrositySet || p.RenownSet {
		t.Fatalf("count = %+v", p)
	}
	if p.CounterType != "Stun" || p.Kind != "STUN" || !slices.Equal(p.Kinds, []string{"STUN"}) {
		t.Fatalf("kind = %q %q %v", p.CounterType, p.Kind, p.Kinds)
	}
	if !p.ETB || p.Placer != "Controller" || !p.OptionalTrue || !p.RememberPut || p.RememberCards || p.ChoiceTitle != "Pick" {
		t.Fatalf("flags = %+v", p)
	}
	if !p.EntryFoldBlocked {
		t.Fatal("Optional$/Adapt$ must block the entry fold")
	}
	if !slices.Equal(p.Unread, []string{"Hidden"}) {
		t.Fatalf("unread = %v, want [Hidden]", p.Unread)
	}
	if PutCounterOf(sa) != p {
		t.Fatal("front cache missed the same Params map")
	}
	q := PutCounterOf(&cards.SA{API: "PutCounter", Params: map[string]string{
		"CounterNum": "2", "CounterType": "P1P1,LOYALTY", "Defined": "Self", "ETB": "True",
	}})
	if !q.CounterNumSet || q.Kind != "P1P1,LOYALTY" || !slices.Equal(q.Kinds, []string{"P1P1", "LOYALTY"}) || q.EntryFoldBlocked || q.Unread != nil {
		t.Fatalf("plain entry body = %+v", q)
	}
	r := PutCounterOf(&cards.SA{API: "PutCounter", Params: map[string]string{
		"Bolster": "2", "Divided": "x", "Choices": " Creature.YouCtrl ", "MinChoiceAmount": "0", "ChoiceAmount": "3",
	}})
	if r.Kind != "P1P1" || !r.Bolster.Present || r.Support.Present || r.Choices != "Creature.YouCtrl" ||
		r.MinChoiceAmount != (ParamText{Text: "0", Present: true}) || r.ChoiceAmount.Text != "3" || !r.EntryFoldBlocked {
		t.Fatalf("pick shape = %+v", r)
	}
}

// TestPutCounterOfIsAllocationFree: a configured record or a front-cache hit
// allocates nothing (the entry-counter fold reads it on every entry).
func TestPutCounterOfIsAllocationFree(t *testing.T) {
	bound := &cards.SA{API: "PutCounter", Params: map[string]string{"CounterNum": "1", "ETB": "True"}}
	f := NewSAFacts(bound)
	cached := &cards.SA{API: "PutCounter", Params: map[string]string{"CounterNum": "2"}}
	PutCounterOf(cached)
	if n := allocsPerRun(100, func() {
		_ = PutCounterOf(bound)
		_ = PutCounterOf(cached)
	}); n != 0 {
		t.Fatalf("PutCounterOf allocated %v objects per run; want 0", n)
	}
	if f.PutCounter == nil || !f.PutCounter.boundTo(bound.Params) {
		t.Fatal("NewSAFacts did not compile the PutCounter half")
	}
}
