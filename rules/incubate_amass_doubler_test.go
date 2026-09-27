package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// A CreateToken replacement (Doubling Season, Anointed Procession) rewrites
// one would-be token into a pair of mints. api:Incubate's counters and
// api:Amass's counters/type grant/RememberAmass$ are per-token riders and
// belong to EVERY mint the plan produced, not just the first: pre-fix both
// primitives emitted the TokenCreate through the plain Emit and applied
// their riders to the single pre-predicted id g.NextID, so the second mint
// entered bare -- and an Amass Army, a 0/0, was then swept by state-based
// actions. These four tests pin the per-mint contract through the real cast
// path with the real corpus carriers and doublers. No doubler, no doubled
// counters: Anointed Procession doubles the tokens only; Doubling Season
// doubles each mint's CounterChange independently (CR 614.5/616.1e).

// doublerIncubatorTokens returns seat p's battlefield Incubator tokens, and
// asserts the doubler precondition the caller relies on: the named enchantment
// is on seat p's battlefield.
func doublerIncubatorTokens(t *testing.T, e *Engine, p state.PlayerID, doubler string) []*state.Object {
	t.Helper()
	if findByName(e, doubler, p) == 0 {
		t.Fatalf("precondition: %s is not on seat %d's battlefield", doubler, p)
	}
	var out []*state.Object
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.Face() != nil && o.Face().Name == "Incubator Token" {
			out = append(out, o)
		}
	}
	return out
}

// runIncubateUnder casts Eyes of Gitaxias (SP$ Incubate | Amount$ 3) with the
// named doubler already on seat 0's battlefield.
func runIncubateUnder(t *testing.T, reg *cards.Registry, doubler string) (*Engine, Config, []*state.Object) {
	t.Helper()
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, doubler), lookup(t, reg, "Eyes of Gitaxias")},
		[]*cards.Card{})
	moveByName(t, e, 0, doubler, state.ZBattlefield)
	moveByName(t, e, 0, "Eyes of Gitaxias", state.ZHand)
	e.priorityRound()
	addMana(t, e, 0, "CCU")
	castNamed(t, e, "Eyes of Gitaxias")
	passUntilStackEmpty(t, e, 60)
	return e, cfg, doublerIncubatorTokens(t, e, 0, doubler)
}

func TestIncubateAnointedProcessionCountersEveryMint(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg, toks := runIncubateUnder(t, reg, "Anointed Procession")
	// Pre-fix: 2 tokens, counters on the first only (3/0). Procession
	// doubles tokens, not counters.
	if len(toks) != 2 {
		t.Fatalf("Incubate under Anointed Procession minted %d Incubator tokens, want 2", len(toks))
	}
	for i, tok := range toks {
		if tok.Zone != state.ZBattlefield {
			t.Fatalf("precondition: token %d zone = %s, want battlefield", i, tok.Zone)
		}
		if got := tok.Counter("P1P1"); got != 3 {
			t.Fatalf("token %d carries %d +1/+1 counters, want 3 (Incubate 3, undoubled)", i, got)
		}
	}
	replayCheck(t, e, cfg)
}

func TestIncubateDoublingSeasonCountersEveryMint(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg, toks := runIncubateUnder(t, reg, "Doubling Season")
	// Pre-fix: 2 tokens, counters on the first only (6/0). Doubling Season
	// doubles the tokens AND each mint's counters independently.
	if len(toks) != 2 {
		t.Fatalf("Incubate under Doubling Season minted %d Incubator tokens, want 2", len(toks))
	}
	for i, tok := range toks {
		if tok.Zone != state.ZBattlefield {
			t.Fatalf("precondition: token %d zone = %s, want battlefield", i, tok.Zone)
		}
		if got := tok.Counter("P1P1"); got != 6 {
			t.Fatalf("token %d carries %d +1/+1 counters, want 6 (3 doubled per mint)", i, got)
		}
	}
	replayCheck(t, e, cfg)
}

// doublerArmyTokens returns seat p's battlefield Army tokens, asserting the
// doubler precondition.
func doublerArmyTokens(t *testing.T, e *Engine, p state.PlayerID, doubler string) []*state.Object {
	t.Helper()
	if findByName(e, doubler, p) == 0 {
		t.Fatalf("precondition: %s is not on seat %d's battlefield", doubler, p)
	}
	var out []*state.Object
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o == nil || !o.IsToken || o.Face() == nil {
			continue
		}
		for _, ty := range o.Face().Types {
			if ty == "Army" {
				out = append(out, o)
				break
			}
		}
	}
	return out
}

// runAmassUnder casts Orcish Bowmasters (its ETB chains DB$ Amass | Type$ Orc
// | Num$ 1) with the named doubler already on seat 0's battlefield.
func runAmassUnder(t *testing.T, reg *cards.Registry, doubler string) (*Engine, Config, []*state.Object) {
	t.Helper()
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, doubler), lookup(t, reg, "Orcish Bowmasters")},
		[]*cards.Card{})
	moveByName(t, e, 0, doubler, state.ZBattlefield)
	moveByName(t, e, 0, "Orcish Bowmasters", state.ZHand)
	e.priorityRound()
	addMana(t, e, 0, "BC")
	castNamed(t, e, "Orcish Bowmasters")
	passToKind(t, e, decision.KTarget)
	submitChoices(t, e, 1) // the ETB's 1 damage to seat 1
	passUntilStackEmpty(t, e, 80)
	return e, cfg, doublerArmyTokens(t, e, 0, doubler)
}

func TestAmassAnointedProcessionCountersEveryMint(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg, armies := runAmassUnder(t, reg, "Anointed Procession")
	// Pre-fix: 1 surviving army, P1P1=1 (the second 0/0 mint was swept by
	// state-based actions before the counters landed). Procession doubles
	// tokens, not counters.
	if len(armies) != 2 {
		t.Fatalf("Amass under Anointed Procession left %d Orc Army tokens, want 2", len(armies))
	}
	for i, o := range armies {
		if got := o.Counter("P1P1"); got != 1 {
			t.Fatalf("army %d carries %d +1/+1 counters, want 1", i, got)
		}
		has := false
		for _, ty := range e.Derived(o.ID).Types {
			if ty == "Orc" {
				has = true
			}
		}
		if !has {
			t.Fatalf("army %d is not also an Orc: %v", i, e.Derived(o.ID).Types)
		}
	}
	replayCheck(t, e, cfg)
}

func TestAmassDoublingSeasonCountersEveryMint(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg, armies := runAmassUnder(t, reg, "Doubling Season")
	// Pre-fix: 1 surviving army, P1P1=2 (the second 0/0 mint swept).
	// Doubling Season doubles the tokens AND each mint's counters.
	if len(armies) != 2 {
		t.Fatalf("Amass under Doubling Season left %d Orc Army tokens, want 2", len(armies))
	}
	for i, o := range armies {
		if got := o.Counter("P1P1"); got != 2 {
			t.Fatalf("army %d carries %d +1/+1 counters, want 2 (1 doubled per mint)", i, got)
		}
		has := false
		for _, ty := range e.Derived(o.ID).Types {
			if ty == "Orc" {
				has = true
			}
		}
		if !has {
			t.Fatalf("army %d is not also an Orc: %v", i, e.Derived(o.ID).Types)
		}
	}
	replayCheck(t, e, cfg)
}
