package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Task put-optional: api:PutCounter.Optional$ True was unread -- "you may put
// a counter" ALWAYS put, with no election recorded anywhere. effPutCounter
// now poses a real yes/no KChoose (the attach_optional precedent, the shared
// Ask boundary), the answer is applied in place, and a decline places nothing while the chained SubAbility$
// still runs (the chain is owned by Resolve, never skipped by a decline).
//
// These are the effects-package unit pins, against the REAL compiled corpus
// SAs (the same discipline corpusCounterSA records -- a synthetic
// map[string]string fixture has shipped bugs here before): the ask's shape
// and the two answered re-entries, plus the no-host deterministic decline
// stand-in (R-9). The engine-side end-to-end pins on the trigger-shaped
// carriers live in rules/putcounter_optional_test.go.

// corpusPutCounterSA returns the REAL compiled PutCounter sub-ability of a
// named corpus card, searching Abilities, every Trigger.Effect, every
// Repl.With and each of their Sub chains, and asserting it really carries
// Optional$ True -- a caller can never be handed an SA that only looks like
// the shape under test. It also returns the owning face's SVar table, the
// same Ctx.SVars resolveTop's branches seed, so a chained sub (Black Widow's
// DBEffect) can resolve its StaticAbilities$ SVar bodies.
func corpusPutCounterSA(t *testing.T, name string) (*cards.SA, map[string]string) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus has no %q", name)
	}
	var found *cards.SA
	var svars map[string]string
	walk := func(sa *cards.SA) {
		for ; sa != nil && found == nil; sa = sa.Sub {
			if sa.API == "PutCounter" && strings.EqualFold(strings.TrimSpace(sa.Params["Optional"]), "True") {
				found = sa
				return
			}
		}
	}
	for _, f := range c.Faces {
		for _, a := range f.Abilities {
			walk(a)
		}
		for _, tr := range f.Triggers {
			walk(tr.Effect)
		}
		for _, r := range f.Repls {
			walk(r.With)
		}
		if found != nil {
			svars = f.SVars
			break
		}
	}
	if found == nil {
		t.Fatalf("corpus card %q has no compiled Optional$ PutCounter SA", name)
	}
	return found, svars
}

// putCounterObject adds one Creature to seat 0's battlefield on h and
// returns its id.
func putCounterObject(t *testing.T, h *fakeHost) state.ObjID {
	t.Helper()
	card := mkCard(t, "Name:PutTee\nTypes:Creature\nPT:1/1\nOracle:x\n")
	o := h.g.AddObject(card, 0)
	o.Zone = state.ZBattlefield
	return o.ID
}

// counterChangeCount counts the CounterChange events the host recorded for
// id (0 when id==0: the any-object count, for the player-recipient shapes).
func counterChangeCount(h *fakeHost, id state.ObjID) int {
	n := 0
	for _, ev := range h.log {
		if ev.Kind == events.CounterChange && (id == 0 || ev.Obj == id) {
			n++
		}
	}
	return n
}

// TestPutCounterOptionalDeadRecipientNeverAsks pins the no-ask gate on a
// recipient that is genuinely NOT a live object: Defined$ Self names an
// unallocated id, so g.Obj returns nil and putCounterWouldPlace is false --
// decline and accept are the same, so no decision is posed and the
// resolution completes silently. (CR 122.1 task note: this premise used to
// be merely "off the battlefield", which the same task made a real
// recipient; a dead id is what "nothing to place on" now means.)
func TestPutCounterOptionalDeadRecipientNeverAsks(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(2))
	dead := state.ObjID(999999) // never allocated in a fresh 2-seat game
	if h.g.Obj(dead) != nil {
		t.Fatalf("precondition: id %d is live, want an unallocated recipient", dead)
	}
	sa, _ := corpusPutCounterSA(t, "Talus Paladin")

	Resolve(h, &Ctx{Source: dead, Controller: 0}, sa)
	if h.asked != nil {
		t.Fatalf("election posed with no live recipient: %+v", h.asked)
	}
	if got := counterChangeCount(&h.fakeHost, dead); got != 0 {
		t.Fatalf("placed %d counter(s) on an unallocated object", got)
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API") {
			t.Fatalf("PutCounter did not run (unregistered?): %q", ev.Text)
		}
	}
}

// TestPutCounterOptionalNoAskHostDeclinesAndRunsTheChain pins the R-9
// stand-in AND the decline-runs-the-chain semantics on Black Widow's real
// DBPutCounter (Defined$ Self, RememberPut$ True, Optional$ True,
// SubAbility$ DBEffect): a host that cannot ask takes the deterministic
// DECLINE (no counter), and the chained DBEffect still runs -- on the
// decline path its Remembered-empty ConditionCompare$ EQ0 gate holds, so the
// may-play Effect registers exactly as the oracle's "If you don't, ..." says.
// On the unmodified tree there was no election at all (the put always
// happened), so both the decline-count and the suspension-free completion are
// discriminative.
func TestPutCounterOptionalNoAskHostDeclinesAndRunsTheChain(t *testing.T) {
	h := newHost(t, 2)
	src := putCounterObject(t, h)
	sa, sv := corpusPutCounterSA(t, "Black Widow, Super Spy")

	Resolve(h, &Ctx{Source: src, Controller: 0, SVars: sv}, sa)
	if got := counterChangeCount(h, src); got != 0 {
		t.Fatalf("no-ask host placed %d counter(s), want the deterministic decline (0)", got)
	}
	if len(h.continuous) != 1 {
		t.Fatalf("continuous effects registered = %d, want 1 (the chained DBEffect still ran on the decline)", len(h.continuous))
	}
}
