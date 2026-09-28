package rules

// Transforming Flourish is the absorbed clause of this ticket's consolidated
// acceptance (agent-20260919T194848Z-a2b4e083): its single-target `SP$ Destroy`
// carries `RememberDestroyed$ True`, and the chained DBDig (a DB$ DigUntil) is
// gated on `ConditionDefined$ Remembered | ConditionPresent$ Card`. Before
// effDestroy read RememberDestroyed$, the destroyed permanent was never added
// to the ability's remembered set, so the gate was never satisfied and the
// whole dig leg silently skipped.
//
// Measured shape (GPL corpus, never copied into a tracked fixture):
//
//	A:SP$ Destroy | ValidTgts$ Artifact.YouDontCtrl,Creature.YouDontCtrl |
//	    SubAbility$ DBDig | RememberDestroyed$ True
//	SVar:DBDig:DB$ DigUntil | ... | ConditionDefined$ Remembered |
//	    ConditionPresent$ Card | ConditionCompare$ GE1 | Valid$ Card.nonLand
//	SVar:DBPlay:DB$ Play | ... | Defined$ Remembered | Optional$ True
//
// The victim's controller exiles from the top of their library until they
// exile a nonland card, then may cast it. The assertion is end-to-end: the
// victim reaches the graveyard, the source remembers it, and the gated dig
// actually ran (the seeded nonland lands in exile).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestTransformingFlourishDestroyRemembersVictimAndRunsGatedDig(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	flourish := mustCorpusCard(t, reg, "Transforming Flourish")
	victimCard := card(t, "Name:Test Beast\nManaCost:2\nTypes:Creature\nPT:3/3\nOracle:x\n")
	nonland := card(t, "Name:Test Relic\nManaCost:1\nTypes:Artifact\nOracle:x\n")

	build := func(s uint64) Config {
		return Config{Seed: s, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{
				append([]*cards.Card{flourish}, mountainDeck(t, 39)...),
				append([]*cards.Card{victimCard, nonland}, mountainDeck(t, 38)...),
			},
			Tokens: reg.Tokens,
		}
	}
	cfg := seatZeroStart(build(73))
	e := New(cfg)
	e.Advance()

	// The victim is a real battlefield permanent under seat 1; the nonland is
	// a real card in seat 1's library (the dig's scan source).
	victim := moveSeededCard(t, e, 1, victimCard, state.ZBattlefield)
	nonlandID := moveSeededCard(t, e, 1, nonland, state.ZLibrary)
	if o := e.G.Obj(victim); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: victim not a seat-1 battlefield permanent: %+v", e.G.Obj(victim))
	}
	if o := e.G.Obj(nonlandID); o == nil || o.Zone != state.ZLibrary {
		t.Fatalf("precondition: nonland not in seat-1 library: %+v", e.G.Obj(nonlandID))
	}
	flourishID := moveSeededCard(t, e, 0, flourish, state.ZHand)
	if o := e.G.Obj(flourishID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Transforming Flourish not in seat 0's hand: %+v", e.G.Obj(flourishID))
	}
	addMana(t, e, 0, "RRR")

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision to cast into: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == flourishID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Transforming Flourish: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	// Answer the target ask and any optional bids along the chain (the
	// DBPlay "you may cast it" ask is answered NO so the chain completes with
	// the exiled nonland in place).
	for i := 0; i < 40 && len(e.G.Stack) > 0; i++ {
		pd := e.Pending()
		if pd == nil {
			break
		}
		switch pd.Kind {
		case decision.KPriority:
			submitChoicePass(t, e)
		case decision.KTarget:
			ti := indexOfObjOption(pd, victim)
			if ti < 0 {
				t.Fatalf("Flourish target ask does not offer the victim %d: %+v", victim, pd.Options)
			}
			submitChoices(t, e, ti)
		case decision.KTriggerOptional:
			submitChoices(t, e, optionIndexOfKind(t, pd, "no"))
		case decision.KModes:
			// DBPlay's "you may cast it without paying its mana cost"
			// ask (Min 0): decline by submitting no mode.
			submitChoices(t, e)
		case decision.KChoose:
			// Any optional "may cast without paying" bid: decline.
			noIdx := -1
			for _, o := range pd.Options {
				if o.Kind == "no" {
					noIdx = o.Index
				}
			}
			if noIdx < 0 {
				t.Fatalf("unexpected KChoose in the Flourish chain: %+v", pd.Options)
			}
			submitChoices(t, e, noIdx)
		default:
			t.Fatalf("unexpected decision in the Flourish chain: %+v", pd)
		}
	}

	// The destroy happened...
	if o := e.G.Obj(victim); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("victim after resolution: %+v, want it destroyed into the graveyard", e.G.Obj(victim))
	}
	// ...and the source recorded it (RememberDestroyed$ True), the association
	// the chained DBDig's gate reads. The association rides the Choose
	// "remembered" event; assert it in the log because the spell's source
	// object leaves the battlefield/stack once the resolution completes.
	sawRemember := false
	for _, ev := range e.L.Events {
		if ev.Kind != events.Choose || ev.Counter != "remembered" {
			continue
		}
		for _, id := range ev.IDs {
			if id == victim {
				sawRemember = true
			}
		}
	}
	if !sawRemember {
		t.Fatalf("no remembered association for victim %d -- RememberDestroyed$ was not read", victim)
	}
	// The gated dig ran: the seeded nonland was exiled from the top of seat
	// 1's library (the chain's FoundDestination$/RevealedDestination$ Exile).
	if o := e.G.Obj(nonlandID); o == nil || o.Zone != state.ZExile {
		t.Fatalf("nonland after resolution: %+v, want it exiled by the gated DBDig chain", e.G.Obj(nonlandID))
	}
	replayCheck(t, e, cfg)
}
