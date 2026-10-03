// WithCountersType$/WithCountersAmount$ on api:Token (Printlifter Ooze:
// "create a 0/0 green Ooze creature token with trample. The token enters
// with X +1/+1 counters on it, where X is the number of other creatures you
// control") and api:CopyPermanent (littjara_mirrorlake's "a token that's a
// copy of target creature you control, except it enters with an additional
// +1/+1 counter on it"): the token-creation entry counters. Before the fix
// both primitives ignored the parameters -- a Printlifter Ooze token entered
// as a 0/0 with no counters and died immediately to the CR 704.5f
// zero-toughness state-based action.
//
// The tests drive real compiled corpus cards (never committed Forge script
// text). No carrier of either param family is in any repo deck, so the
// golden heads and the acceptance ratchet are untouched by construction.
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// tokenWithCountersEngine deals seatZeroStart two-seat games where seat 0's
// hand holds one copy of each named fixture and the rest of both decks are
// Forests.
func tokenWithCountersEngine(t *testing.T, reg *cards.Registry, hand ...string) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	deck := make([]*cards.Card, 0, 40)
	for _, name := range hand {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for len(deck) < 40 {
		deck = append(deck, forest)
	}
	opp := make([]*cards.Card, 0, 40)
	for len(opp) < 40 {
		opp = append(opp, forest)
	}
	cfg := seatZeroStart(Config{Seed: 7713, Names: []string{"tokcounters", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// newestTokenOnBattlefield finds the object the test's mint created: the
// highest-ID token on the battlefield (every mint in these tests is the last
// object the game created). A nil return is the caller's failing precondition.
func newestTokenOnBattlefield(t *testing.T, e *Engine) *state.Object {
	t.Helper()
	for i := len(e.G.Objs) - 1; i >= 0; i-- {
		o := &e.G.Objs[i]
		if o.IsToken && o.Zone == state.ZBattlefield {
			return o
		}
	}
	t.Fatalf("no token on the battlefield (objs %d)", len(e.G.Objs))
	return nil
}

// TestIncubobTokenEntersWithACounter is the literal-amount carrier end to
// end: Incubob is an ordinary castable sorcery whose
// `A:SP$ Token | WithCountersType$ P1P1 | WithCountersAmount$ 1` must make
// the minted Incubator token enter WITH its +1/+1 counter -- a 1/1, not a
// 0/0 that the zero-toughness SBA sweeps before the test can even look.
func TestIncubobTokenEntersWithACounter(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := tokenWithCountersEngine(t, reg, "Incubob")
	incubob := searchMoveByName(t, e, "Incubob", state.ZHand)
	addMana(t, e, 0, "BB")
	submitChoices(t, e, castOptionFor(t, e, incubob).Index)
	passUntilStackEmpty(t, e, 30)

	tok := newestTokenOnBattlefield(t, e)
	if got := tok.Counter("P1P1"); got != 1 {
		t.Fatalf("Incubator token P1P1 counters = %d, want 1", got)
	}
	der := e.Derived(tok.ID)
	if der.Power != 1 || der.Toughness != 1 {
		t.Fatalf("Incubator token derived P/T = %d/%d, want 1/1 (0/0 script + one counter)", der.Power, der.Toughness)
	}
	// CR 704.5f precondition proved the other way: the counter is what keeps
	// the token alive through a state-based-action check.
	e.checkStateBased()
	if z := e.G.Obj(tok.ID).Zone; z != state.ZBattlefield {
		t.Fatalf("Incubator token zone after the SBA sweep = %s, want battlefield", z)
	}
	replayCheck(t, e, cfg)
}

// TestPrintlifterOozeTokenEntersWithComputedCounters is the dynamic-X carrier
// (the brief's named card): the token enters with X +1/+1 counters, X =
// Count$Valid Creature.YouCtrl on the real compiled SVar:X. Printlifter Ooze
// and one other creature are on the battlefield, so the token enters with 2
// counters and survives the zero-toughness SBA.
//
// The trigger that would fire this body is T:Mode$ TurnFaceUp -- a trigger
// mode and the turn-face-up mechanic it rides (CR 708.6 for
// disguise/morph/manifest) do not exist in this build yet, so no engine path
// can fire it; the pin resolves the REAL compiled TrigToken body through the
// ordinary effects resolution instead (the Kari Zev AtEOT pin's shape), with
// the face's own SVar table bound exactly as the engine binds it for a
// resolving trigger body.
func TestPrintlifterOozeTokenEntersWithComputedCounters(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := tokenWithCountersEngine(t, reg, "Printlifter Ooze", "Llanowar Elves")
	ooze := searchMoveByName(t, e, "Printlifter Ooze", state.ZBattlefield)
	elves := searchMoveByName(t, e, "Llanowar Elves", state.ZBattlefield)
	// Preconditions: both creatures on the battlefield under seat 0, so
	// Count$Valid Creature.YouCtrl reads 2 -- a nonzero X, which is the whole
	// point of the pin.
	for _, id := range []state.ObjID{ooze, elves} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
			t.Fatalf("precondition: fixture %d not on seat 0's battlefield (%+v)", id, o)
		}
	}

	card := searchCorpusCard(t, reg, "Printlifter Ooze")
	sa := cards.ResolveSVar(card.Faces[0].SVars, "TrigToken")
	if sa == nil {
		t.Fatal("Printlifter Ooze TrigToken SVar unresolved")
	}
	if sa.Params["WithCountersType"] != "P1P1" || sa.Params["WithCountersAmount"] != "X" {
		t.Fatalf("compiled TrigToken params drifted: %+v", sa.Params)
	}
	effects.Resolve(e, &effects.Ctx{Source: ooze, Controller: 0,
		SVars: card.Faces[0].SVars}, sa)

	tok := newestTokenOnBattlefield(t, e)
	if got := tok.Counter("P1P1"); got != 2 {
		t.Fatalf("Ooze token P1P1 counters = %d, want 2 (Printlifter + Llanowar)", got)
	}
	der := e.Derived(tok.ID)
	if der.Power != 2 || der.Toughness != 2 {
		t.Fatalf("Ooze token derived P/T = %d/%d, want 2/2", der.Power, der.Toughness)
	}
	// The 0-toughness SBA the old behaviour died to: the token survives it.
	e.checkStateBased()
	if z := e.G.Obj(tok.ID).Zone; z != state.ZBattlefield {
		t.Fatalf("Ooze token zone after the SBA sweep = %s, want battlefield", z)
	}
	replayCheck(t, e, cfg)
}

// TestPrintlifterOozeUnresolvableAmountIsLoudAndSkipped pins the degrade: an
// amount the Num grammar cannot resolve (here SVar:X unbound in the
// resolution's table) is ONE loud Note naming the value and the token enters
// WITHOUT counters -- never a silent wrong count, never a silent default.
func TestPrintlifterOozeUnresolvableAmountIsLoudAndSkipped(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := tokenWithCountersEngine(t, reg, "Printlifter Ooze")
	ooze := searchMoveByName(t, e, "Printlifter Ooze", state.ZBattlefield)

	// A hand-built fixture (inline-authored, never committed script text)
	// whose amount is genuinely unresolvable: bare X would resolve to Ctx.X
	// (the engine-wide bare-X convention), so the degrade needs a value the
	// Num grammar rejects outright.
	sa := &cards.SA{Kind: "DB", API: "Token", Params: map[string]string{
		"TokenScript":        "g_0_0_ooze_trample",
		"WithCountersType":   "P1P1",
		"WithCountersAmount": "not-a-number",
	}}
	effects.Resolve(e, &effects.Ctx{Source: ooze, Controller: 0, SVars: map[string]string{}}, sa)

	notes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "WithCountersAmount$ not-a-number is not implemented") {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("unresolvable WithCountersAmount$ X produced %d notes, want exactly 1", notes)
	}
	tok := newestTokenOnBattlefield(t, e)
	if got := tok.Counter("P1P1"); got != 0 {
		t.Fatalf("Ooze token P1P1 counters = %d, want 0 (the skipped set)", got)
	}
}
