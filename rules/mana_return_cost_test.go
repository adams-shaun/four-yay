package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestGrinningIgnusManaAbilityReturnsItself: Grinning Ignus's mana ability
// `A:AB$ Mana | Cost$ R Return<1/CARDNAME> | Produced$ C C R` is an
// off-stack mana activation (CR 605.3a), and its Return<1/CARDNAME> cost
// part must be PAID: the Ignus goes to its owner's hand as the {C}{C}{R}
// arrives. The mana path used to pay only the mana/tap/sacrifice/discard/
// exile/mill/counter parts and silently skip the return, so the Ignus stayed
// on the battlefield and a bot re-activated it forever (cardfuzz batch1
// line 10: +2 colourless per cycle, never a state the loop could leave).
func TestGrinningIgnusManaAbilityReturnsItself(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := edrBoard(t, reg, 71, map[string]state.Zone{
		"Grinning Ignus": state.ZBattlefield,
		"Mountain":       state.ZBattlefield,
	})
	ignus, mtn := ids["Grinning Ignus"], ids["Mountain"]
	submitChoices(t, e, activateOption(t, e, mtn))
	if got := e.G.Players[0].Pool[state.MR]; got != 1 {
		t.Fatalf("Mountain tap: pool R = %d, want 1", got)
	}
	submitChoices(t, e, activateOption(t, e, ignus))
	if z := e.G.Obj(ignus).Zone; z != state.ZHand {
		t.Fatalf("paying Return<1/CARDNAME> did not return Grinning Ignus to hand: zone %v", z)
	}
	pool := e.G.Players[0].Pool
	if pool[state.MR] != 1 || pool[state.MC] != 2 {
		t.Fatalf("Grinning Ignus produced pool %v, want {C}{C}{R} after paying {R}", pool)
	}
	if hasActivateOption(e, ignus) {
		t.Fatal("Grinning Ignus is still offered for mana from its owner's hand")
	}
	replayCheck(t, e, cfg)
}

// activationLimitBoard seeds seat 0 with the named corpus card and `lands`
// Mountains on the battlefield, at seat 0's main-phase priority.
func activationLimitBoard(t *testing.T, name string, lands int) (*Engine, Config, state.ObjID, []state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c := mustCorpusCard(t, reg, name)
	cfg := Config{Seed: 73, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append([]*cards.Card{c}, mountainDeck(t, 39)...), mountainDeck(t, 40)}}
	e := New(cfg)
	src := moveByName(t, e, 0, name, state.ZBattlefield)
	var mtns []state.ObjID
	for i := 0; i < lands; i++ {
		mtns = append(mtns, moveByName(t, e, 0, "Mountain", state.ZBattlefield))
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.Advance()
	edrSeatZeroPriority(t, e)
	return e, cfg, src, mtns
}

func delayedRegistersFor(e *Engine, id state.ObjID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.DelayedRegister && ev.Obj == id {
			n++
		}
	}
	return n
}

// TestFarrelitePriestSacrificeOnlyFromFourthActivation: Farrelite Priest's
// "{1}: Add {W}. If this ability has been activated four or more times this
// turn, sacrifice it at the beginning of the next end step" gates its
// DB$ Pump | AtEOT$ Sacrifice sub on ConditionActivationLimit$ GE4. The
// gate used to be an unknown Condition key, so the sub ran on EVERY
// activation: the first {1} already scheduled the sacrifice (cardfuzz
// batch1 line 3's delayed_register on each cycle). The first three
// activations must register nothing; the fourth registers the sacrifice.
func TestFarrelitePriestSacrificeOnlyFromFourthActivation(t *testing.T) {
	t.Parallel()
	e, cfg, priest, mtns := activationLimitBoard(t, "Farrelite Priest", 4)
	for i, m := range mtns {
		submitChoices(t, e, activateOption(t, e, m))
		submitChoices(t, e, activateOption(t, e, priest))
		want := 0
		if i == 3 {
			want = 1
		}
		if got := delayedRegistersFor(e, priest); got != want {
			t.Fatalf("after activation %d: %d delayed sacrifice registrations, want %d", i+1, got, want)
		}
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 4 || pool[state.MW] < 1 {
		t.Fatalf("four Mountain+Priest cycles left pool %v, want 4 mana including {W}", pool)
	}
	replayCheck(t, e, cfg)
}

// TestDragonWhelpSacrificeOnlyFromFourthActivation is the stack-ability half
// of the same gate: Dragon Whelp's "{R}: +1/+0 ... four or more times"
// resolves through resolveTop, whose Ctx binds the AbilityPush census.
func TestDragonWhelpSacrificeOnlyFromFourthActivation(t *testing.T) {
	t.Parallel()
	e, cfg, whelp, mtns := activationLimitBoard(t, "Dragon Whelp", 4)
	for i, m := range mtns {
		submitChoices(t, e, activateOption(t, e, m))
		submitChoices(t, e, abilityOption(t, e, whelp, 0).Index)
		passUntilStackEmpty(t, e, 10)
		edrSeatZeroPriority(t, e)
		want := 0
		if i == 3 {
			want = 1
		}
		if got := delayedRegistersFor(e, whelp); got != want {
			t.Fatalf("after activation %d: %d delayed sacrifice registrations, want %d", i+1, got, want)
		}
	}
	replayCheck(t, e, cfg)
}
