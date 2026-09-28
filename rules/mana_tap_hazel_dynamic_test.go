package rules

// Hazel of the Rootbloom's dynamic mana cost:
//
//	A:AB$ Mana | Cost$ T PayLife<2> tapXType<X/Card.token/token> \
//	  | Produced$ Combo Any | Amount$ X
//	SVar:X:Count$xPaid
//
// The prerequisite (mana-tapxtype-cost) made the LITERAL tapXType<N/Spec>
// mana cost offered and paid, but refused every Dyn part in manaTapsPayable,
// so Hazel was never offered. These tests pin the dynamic election: 0..N
// untapped tokens are tapped, the elected count IS the activation's X, and
// Produced$ Combo Any allocates exactly X units. X=0 is legal (it pays the
// {T} and 2 life and adds nothing) and must pose no empty decision.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// battlefieldTokens returns the ids of the token permanents currently on
// seat p's battlefield, in the game's stable object order.
func battlefieldTokens(e *Engine, p state.PlayerID) []state.ObjID {
	var ids []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.IsToken {
			ids = append(ids, id)
		}
	}
	return ids
}

// mintToken emits a real TokenCreate and returns the new token's id. The
// registry must already be installed at e.G.Tokens.
func mintToken(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	before := len(battlefieldTokens(e, p))
	e.emit(events.Event{Kind: events.TokenCreate, Player: p, Text: name})
	after := battlefieldTokens(e, p)
	if len(after) != before+1 {
		t.Fatalf("fixture: TokenCreate %q minted %d tokens, want 1", name, len(after)-before)
	}
	return after[len(after)-1]
}

// hazelDynamicBoard puts Hazel and one nontoken creature on seat 0 and
// installs the token registry. The Priority decision is re-derived after the
// raw setup moves (the caller may mint tokens afterward and re-derive again).
func hazelDynamicBoard(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	e, cfg, ids := manaTapBoard(t, seed, "Hazel of the Rootbloom", "Llanowar Elves")
	cfg.Tokens = testutil.CorpusRegistry(t).Tokens
	e.G.Tokens = cfg.Tokens
	hazel, nontoken := ids["Hazel of the Rootbloom"], ids["Llanowar Elves"]
	if e.G.Obj(hazel).Zone != state.ZBattlefield {
		t.Fatal("fixture: Hazel is not on the battlefield")
	}
	if e.G.Obj(hazel).IsToken {
		t.Fatal("fixture: Hazel must be a nontoken permanent, not a tap candidate")
	}
	if e.G.Obj(nontoken).IsToken {
		t.Fatal("fixture: the nontoken control must not be a tap candidate")
	}
	return e, cfg, hazel, nontoken
}

// reprioritize re-derives the priority options after raw setup moves that did
// not go through the decision loop.
func reprioritize(t *testing.T, e *Engine) {
	t.Helper()
	e.pending = nil
	e.priorityRound()
	edrSeatZeroPriority(t, e)
}

// TestManaTapPermanentCostHazelDynamicX is the dynamic tapXType<X/token>
// election's own acceptance: offer, election bounds, exact tapped set,
// life payment, X-bound Combo Any allocation and replay identity.
func TestManaTapPermanentCostHazelDynamicX(t *testing.T) {
	t.Parallel()
	t.Run("X=2 taps both tokens and allocates two distinct colours", func(t *testing.T) {
		e, cfg, hazel, nontoken := hazelDynamicBoard(t, 2026092701)
		tokA := mintToken(t, e, 0, "g_1_1_squirrel")
		tokB := mintToken(t, e, 0, "c_a_treasure_sac")
		reprioritize(t, e)

		// Precondition: the two tokens are untapped and the nontoken control
		// is untapped too, so "only tokens were tapped" is a real comparison.
		for _, id := range []state.ObjID{tokA, tokB, nontoken} {
			if e.G.Obj(id).Tapped {
				t.Fatalf("fixture: %d must start untapped", id)
			}
		}
		if e.G.Players[0].Life <= 2 {
			t.Fatalf("fixture: life %d cannot pay the 2-life cost", e.G.Players[0].Life)
		}
		if !hasActivateOption(e, hazel) {
			t.Fatalf("Hazel not offered with two untapped tokens: %+v", e.Pending().Options)
		}
		submitChoices(t, e, activateOption(t, e, hazel))

		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Min != 0 || d.Max != 2 {
			t.Fatalf("dynamic tap election = %+v, want KChoose Min=0 Max=2", d)
		}
		// Exactly the two tokens, each once, and nothing else: no nontoken
		// and no duplicate option for one token.
		seen := map[state.ObjID]int{}
		for _, o := range d.Options {
			if o.Kind != "tapcost" {
				t.Fatalf("tap option kind = %q, want tapcost: %+v", o.Kind, o)
			}
			if o.Obj != tokA && o.Obj != tokB {
				t.Fatalf("ineligible tap option %+v (nontoken=%d)", o, nontoken)
			}
			seen[o.Obj]++
		}
		if len(seen) != 2 || seen[tokA] != 1 || seen[tokB] != 1 {
			t.Fatalf("tap options = %v, want each token exactly once", seen)
		}

		var picks []int
		for _, o := range d.Options {
			picks = append(picks, o.Index)
		}
		submitChoices(t, e, picks...)

		// The elected count (2) must drive the Combo Any allocation: exactly
		// two units, and we choose two DISTINCT colours so a duplicate or a
		// wrong count is visible in the pool.
		d = e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Min != 2 || d.Max != 2 {
			t.Fatalf("Combo Any allocation ask = %+v, want KChoose Min=Max=2", d)
		}
		findPick := func(sym string) int {
			for _, o := range d.Options {
				if o.Kind == "mana" && o.ManaSymbol == sym {
					return o.Index
				}
			}
			t.Fatalf("no Add %s option in %+v", sym, d.Options)
			return -1
		}
		submitChoices(t, e, findPick("U"), findPick("R"))
		if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
			t.Fatalf("a colour/election decision remains after the allocation: %+v", d)
		}

		if !e.G.Obj(hazel).Tapped {
			t.Error("Hazel's own {T} was not paid")
		}
		if !e.G.Obj(tokA).Tapped || !e.G.Obj(tokB).Tapped {
			t.Errorf("elected tokens not tapped: A=%v B=%v", e.G.Obj(tokA).Tapped, e.G.Obj(tokB).Tapped)
		}
		if e.G.Obj(nontoken).Tapped {
			t.Error("the nontoken permanent was tapped though it was never offered")
		}
		if got := e.G.Players[0].Life; got != 18 {
			t.Errorf("life = %d, want 18 (2 life paid once)", got)
		}
		pool := e.G.Players[0].Pool
		if pool.Total() != 2 || pool[state.MU] != 1 || pool[state.MR] != 1 {
			t.Errorf("pool = %v, want exactly one U and one R", pool)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("X=0 with no eligible token asks nothing and pays the cost", func(t *testing.T) {
		e, cfg, hazel, _ := hazelDynamicBoard(t, 2026092702)
		// Precondition: there is genuinely no token on the battlefield.
		if toks := battlefieldTokens(e, 0); len(toks) != 0 {
			t.Fatalf("fixture: want no tokens, found %v", toks)
		}
		if !hasActivateOption(e, hazel) {
			t.Fatalf("Hazel not offered at X=0 with no tokens: %+v", e.Pending().Options)
		}
		submitChoices(t, e, activateOption(t, e, hazel))
		if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
			t.Fatalf("X=0 posed a decision %+v, want none", d)
		}
		if !e.G.Obj(hazel).Tapped {
			t.Error("Hazel's {T} cost was not paid at X=0")
		}
		if got := e.G.Players[0].Life; got != 18 {
			t.Errorf("life = %d, want 18", got)
		}
		if got := e.G.Players[0].Pool.Total(); got != 0 {
			t.Errorf("pool total = %d, want 0 at X=0", got)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("electing zero among available tokens adds nothing and taps none", func(t *testing.T) {
		e, cfg, hazel, _ := hazelDynamicBoard(t, 2026092703)
		tok := mintToken(t, e, 0, "g_1_1_squirrel")
		reprioritize(t, e)
		// Precondition: there IS a real candidate to decline, so the empty
		// answer below is a choice, not a board that offered nothing.
		if e.G.Obj(tok).Tapped {
			t.Fatal("fixture: token must start untapped")
		}
		if !hasActivateOption(e, hazel) {
			t.Fatalf("Hazel not offered: %+v", e.Pending().Options)
		}
		submitChoices(t, e, activateOption(t, e, hazel))
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Min != 0 || d.Max != 1 {
			t.Fatalf("tap election = %+v, want KChoose Min=0 Max=1", d)
		}
		// Submit the legal empty answer (Min 0): the count is X=0.
		submitChoices(t, e)
		if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
			t.Fatalf("X=0 among candidates posed a follow-up decision: %+v", d)
		}
		if e.G.Obj(tok).Tapped {
			t.Error("token was tapped although the election returned zero")
		}
		if !e.G.Obj(hazel).Tapped {
			t.Error("Hazel's {T} cost was not paid")
		}
		if got := e.G.Players[0].Life; got != 18 {
			t.Errorf("life = %d, want 18", got)
		}
		if got := e.G.Players[0].Pool.Total(); got != 0 {
			t.Errorf("pool total = %d, want 0 for X=0", got)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("tapped token and untapped nontoken are ineligible", func(t *testing.T) {
		e, cfg, hazel, nontoken := hazelDynamicBoard(t, 2026092704)
		tok := mintToken(t, e, 0, "g_1_1_squirrel")
		e.emit(events.Event{Kind: events.Tap, Obj: tok})
		reprioritize(t, e)
		// Precondition: exactly one candidate is ruled out by being tapped and
		// the other by being a nontoken, so both eligibility clauses bind.
		if !e.G.Obj(tok).Tapped {
			t.Fatal("fixture: token must be tapped")
		}
		if e.G.Obj(nontoken).Tapped {
			t.Fatal("fixture: nontoken must be untapped")
		}
		if !hasActivateOption(e, hazel) {
			t.Fatalf("Hazel should still allow X=0: %+v", e.Pending().Options)
		}
		submitChoices(t, e, activateOption(t, e, hazel))
		if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
			t.Fatalf("ineligible candidates produced a bogus ask: %+v", d)
		}
		if e.G.Obj(tok).Tapped != true || e.G.Obj(nontoken).Tapped {
			t.Fatal("ineligible permanent state changed")
		}
		if got := e.G.Players[0].Life; got != 18 {
			t.Errorf("life = %d, want 18", got)
		}
		replayCheck(t, e, cfg)
	})
}
