package rules

// Mana-ability cost parts (fuzz-strive bug 2). A mana ability activates off
// the stack (CR 605.3a) through resolveManaAbilityRefOriginal and the
// manaDiscardActivation continuation, which used to settle only
// mana/life/{T}/Mill/SubCounter/Sac/Discard/Exile (and, since the Grinning
// Ignus fix, the self-Return). Every other part was silently FREE (Aether
// Hub / Servant of the Conduit's PayEnergy<1>, Cryptex's CollectEvidence) or
// priced as one phantom generic mana because ParseCost did not model it
// (Wall of Roots' AddCounter<1/M0M1>, Oasis Ritualist's Exert<1/CARDNAME>,
// Pili-Pala's {Q}). The path now pays PayEnergy/AddCounter/Exert through
// events and refuses (fails closed on) every part it cannot settle.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// manaCostBoard seeds seat 0 with the named corpus card on the battlefield
// and `energy` energy counters, at seat 0's main-phase priority with the
// offer re-derived after the energy arrived.
func manaCostBoard(t *testing.T, seed uint64, name string, energy int32) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := edrBoard(t, reg, seed, map[string]state.Zone{name: state.ZBattlefield})
	if energy > 0 {
		e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "ENERGY", Amount: energy})
		e.priorityRound()
		edrSeatZeroPriority(t, e)
	}
	return e, cfg, ids[name]
}

// answerManaChoose answers the pending KChoose (an ability pick or a colour
// pick) with the option whose label is want. A single-colour want also
// matches a prefixed colour option ("Pay 1 life: Add B"), so a paid
// activation's colour ask is selectable by its production alone.
func answerManaChoose(t *testing.T, e *Engine, want string) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected a mana KChoose for %q, got %+v", want, d)
	}
	for _, o := range d.Options {
		if o.Label == want {
			submitChoices(t, e, o.Index)
			return
		}
	}
	if len(want) == len("Add ")+1 && strings.HasPrefix(want, "Add ") {
		colour := want[len("Add "):]
		for _, o := range d.Options {
			if o.Kind == "mana" && o.ManaSymbol == colour {
				submitChoices(t, e, o.Index)
				return
			}
		}
	}
	t.Fatalf("no %q option in %q: %+v", want, d.Prompt, d.Options)
}

// answerRitualistAbility answers Oasis Ritualist's ability pick with its
// exert ability (exert true) or its plain {T} ability. Both produce "Add any
// color" (the exert one behind its "Exert it: " cost prefix), so the option
// is matched by the ability's own cost.
func answerRitualistAbility(t *testing.T, e *Engine, rit state.ObjID, exert bool) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the ability pick, got %+v", d)
	}
	abs := e.availableManaAbilities(0, rit)
	for _, o := range d.Options {
		if o.Ability < 0 || o.Ability >= len(abs) {
			continue
		}
		if (len(e.parseCost(abs[o.Ability].Params["Cost"]).Exert) > 0) == exert {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no ability option with exert=%v: %+v", exert, d.Options)
}

func TestManaCostPartsParsePrecisely(t *testing.T) {
	wall := ParseCost("AddCounter<1/M0M1>")
	if len(wall.AddCounter) != 1 || wall.AddCounter[0].N != 1 || wall.AddCounter[0].Spec != "M0M1" ||
		wall.Generic != 0 || len(wall.Unknown) != 0 {
		t.Fatalf("AddCounter<1/M0M1> = %+v, want one M0M1 part, no phantom generic", wall)
	}
	exert := ParseCost("R T Exert<1/CARDNAME>")
	if len(exert.Exert) != 1 || exert.Generic != 0 || len(exert.Unknown) != 0 || !exert.Tap {
		t.Fatalf("R T Exert<1/CARDNAME> = %+v, want an Exert part, no phantom generic", exert)
	}
	// A chooser-anchored AddCounter is a different payment and stays reported.
	other := ParseCost("B AddCounter<1/M1M1/Creature.YouCtrl/a creature you control>")
	if len(other.AddCounter) != 0 || len(other.Unknown) == 0 {
		t.Fatalf("chooser-anchored AddCounter = %+v, want the reported fallback", other)
	}
}

// TestAetherHubManaAbilityPaysEnergy: "{T}, Pay {E}: Add one mana of any
// color" spends the energy; with none, the energy ability is not an option.
func TestAetherHubManaAbilityPaysEnergy(t *testing.T) {
	e, cfg, hub := manaCostBoard(t, 81, "Aether Hub", 1)
	submitChoices(t, e, activateOption(t, e, hub))
	answerManaChoose(t, e, "Pay 1 energy: Add any color")
	answerManaChoose(t, e, "Add G")
	if got := e.G.Players[0].Counter("ENERGY"); got != 0 {
		t.Fatalf("energy after Aether Hub's PayEnergy<1> ability = %d, want 0", got)
	}
	if got := e.G.Players[0].Pool[state.MG]; got != 1 {
		t.Fatalf("pool G = %d, want 1", got)
	}
	replayCheck(t, e, cfg)
}

func TestAetherHubWithoutEnergyOffersOnlyColourless(t *testing.T) {
	e, _, hub := manaCostBoard(t, 82, "Aether Hub", 0)
	abs := e.availableManaAbilities(0, hub)
	if len(abs) != 1 || abs[0].Params["Produced"] != "C" {
		t.Fatalf("Aether Hub with 0 energy offers %d mana abilities, want only the {C} one", len(abs))
	}
}

// TestServantOfTheConduitNeedsEnergy: the Servant's only mana ability costs
// {E}; with no energy it is not offered at all, with one it spends it.
func TestServantOfTheConduitNeedsEnergy(t *testing.T) {
	e, _, servant := manaCostBoard(t, 83, "Servant of the Conduit", 0)
	if hasActivateOption(e, servant) {
		t.Fatal("Servant of the Conduit offered for mana with no energy to pay {E}")
	}
	e, cfg, servant := manaCostBoard(t, 84, "Servant of the Conduit", 2)
	submitChoices(t, e, activateOption(t, e, servant))
	answerManaChoose(t, e, "Add U")
	if got := e.G.Players[0].Counter("ENERGY"); got != 1 {
		t.Fatalf("energy after one activation = %d, want 1", got)
	}
	if got := e.G.Players[0].Pool[state.MU]; got != 1 {
		t.Fatalf("pool U = %d, want 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestWallOfRootsManaAbilityPutsItsCounter: "Put a -0/-1 counter on this
// creature: Add {G}" costs the counter, not a phantom {1}: with an empty
// pool it activates, the Wall carries one M0M1 counter and the pool holds
// exactly {G}.
func TestWallOfRootsManaAbilityPutsItsCounter(t *testing.T) {
	e, cfg, wall := manaCostBoard(t, 85, "Wall of Roots", 0)
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("fixture pool not empty: %v", e.G.Players[0].Pool)
	}
	submitChoices(t, e, activateOption(t, e, wall))
	if got := e.G.Obj(wall).Counter("M0M1"); got != 1 {
		t.Fatalf("Wall of Roots M0M1 counters = %d, want 1", got)
	}
	if pool := e.G.Players[0].Pool; pool[state.MG] != 1 || pool.Total() != 1 {
		t.Fatalf("pool %v, want exactly {G}", pool)
	}
	replayCheck(t, e, cfg)
}

// TestOasisRitualistExertManaAbilityExerts: the "{T}, Exert: Add two mana of
// any one color" ability exerts the Ritualist (CR 701.39: it won't untap
// during its controller's next untap step) instead of charging a phantom
// {1} -- with an empty pool it activates, and the plain {T} ability does not
// exert.
func TestOasisRitualistExertManaAbilityExerts(t *testing.T) {
	e, cfg, rit := manaCostBoard(t, 86, "Oasis Ritualist", 0)
	submitChoices(t, e, activateOption(t, e, rit))
	answerRitualistAbility(t, e, rit, true)
	answerManaChoose(t, e, "Add W")
	o := e.G.Obj(rit)
	if !o.ExertSkipUntap || !o.ExertedThisTurn || !o.Tapped {
		t.Fatalf("Oasis Ritualist after its exert mana ability: tapped=%v exerted=%v skipUntap=%v, want all true",
			o.Tapped, o.ExertedThisTurn, o.ExertSkipUntap)
	}
	if pool := e.G.Players[0].Pool; pool[state.MW] != 2 || pool.Total() != 2 {
		t.Fatalf("pool %v, want exactly {W}{W}", pool)
	}
	replayCheck(t, e, cfg)

	e2, cfg2, rit2 := manaCostBoard(t, 87, "Oasis Ritualist", 0)
	submitChoices(t, e2, activateOption(t, e2, rit2))
	answerRitualistAbility(t, e2, rit2, false)
	answerManaChoose(t, e2, "Add W")
	if o := e2.G.Obj(rit2); o.ExertSkipUntap || o.ExertedThisTurn {
		t.Fatal("the plain {T} mana ability exerted Oasis Ritualist")
	}
	replayCheck(t, e2, cfg2)
}

// TestManaAbilityUnsettleablePartsFailClosed: a mana ability whose cost
// carries a part the off-stack path cannot settle is refused, never
// activated with that part free -- Pili-Pala's {Q} (an unmodelled token
// that used to be one phantom generic) and Cryptex's CollectEvidence<3>
// (a modelled part the mana path has no settle for).
func TestManaAbilityUnsettleablePartsFailClosed(t *testing.T) {
	for _, name := range []string{"Pili-Pala", "Cryptex"} {
		e, _, id := manaCostBoard(t, 88, name, 0)
		// Give the seat plenty of mana and graveyard fodder so only the
		// unsettleable part can be the reason for refusal.
		for _, r := range "WUBRGCCCCC" {
			e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(r), Amount: 1})
		}
		for _, ma := range e.G.Obj(id).Face().ManaAbilities() {
			if e.manaAbilityPayable(0, id, ma) {
				t.Fatalf("%s: mana ability with cost %q is payable, want refused", name, ma.Params["Cost"])
			}
		}
	}
}

// TestHopeTenderExertAbilityExertsInsteadOfAPhantomMana is the stack-ability
// half of the Exert<1/CARDNAME> parse: "{1}, {T}, Exert this creature: Untap
// two target lands" charges exactly {1} (the token used to add a phantom
// generic, so it cost {2}) and exerts Hope Tender through the activation
// settle's events.Exert.
func TestHopeTenderExertAbilityExertsInsteadOfAPhantomMana(t *testing.T) {
	e, cfg, tender, mtns := activationLimitBoard(t, "Hope Tender", 2)
	submitChoices(t, e, activateOption(t, e, mtns[0]))
	submitChoices(t, e, activateOption(t, e, mtns[1]))
	if got := e.G.Players[0].Pool.Total(); got != 2 {
		t.Fatalf("pool after tapping two Mountains = %d, want 2", got)
	}
	submitChoices(t, e, abilityOption(t, e, tender, 1).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the two-land target ask, got %+v", d)
	}
	var picks []int
	for _, o := range d.Options {
		if o.Obj == mtns[0] || o.Obj == mtns[1] {
			picks = append(picks, o.Index)
		}
	}
	submitChoices(t, e, picks...)
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("pool after paying {1} = %d, want 1 (no phantom generic for Exert)", got)
	}
	if o := e.G.Obj(tender); !o.ExertSkipUntap || !o.ExertedThisTurn {
		t.Fatalf("Hope Tender not exerted by its Exert cost: exerted=%v skipUntap=%v", o.ExertedThisTurn, o.ExertSkipUntap)
	}
	passUntilStackEmpty(t, e, 10)
	for _, m := range mtns {
		if e.G.Obj(m).Tapped {
			t.Fatalf("Mountain %d still tapped after Hope Tender resolved", m)
		}
	}
	replayCheck(t, e, cfg)
}
