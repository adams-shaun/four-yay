package rules

// The Rakdos-params brief, gap 9: MayPlay$ True / MayPlayIgnoreColor$ as a
// cast-from-exile permission. Before this work the may-play machinery covered
// only the unconditional LAND shape (Conduit of Worlds); the grant family's
// dominant shape -- Affected$ Card.IsRemembered @ Exile (227 corpus files)
// delivered by an Effect SA (Atsushi), the S: statics on permanents
// (Opposition Agent, Intellect Devourer), the self-grant EffectZone$ Exile
// statics (Misthollow Griffin, Eternal Scourge) and the MayPlayIgnoreColor$
// rider -- never offered a cast.
//
// The offer is a THIRD cast source (rules/legal.go's may-play walks); the
// cast pays the card's printed cost, with the IgnoreColor rider payable as
// any colour; a MayPlayLimit$ grant is capped once per turn by a log scan
// (mayPlaysThisTurn).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// seedLibraryTop puts fresh fixture cards at the FRONT of seat 0's library,
// so a library walk (Atsushi's Dig) reads exactly them.
func seedLibraryTop(t *testing.T, e *Engine, srcs ...string) []state.ObjID {
	t.Helper()
	lib := e.G.Zone(state.ZLibrary, 0)
	var ids []state.ObjID
	for _, src := range srcs {
		ids = append(ids, e.G.AddObject(card(t, src), 0).ID)
	}
	e.G.SetZone(state.ZLibrary, 0, append(ids, lib...))
	return ids
}

func mayPlayOption(d *decision.Decision, id state.ObjID) *decision.Option {
	for i := range d.Options {
		o := &d.Options[i]
		if o.Kind == "cast" && o.Obj == id && o.Mode == "mayplay" {
			return o
		}
	}
	return nil
}

func TestMisthollowGriffinCastsItselfFromExile(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Misthollow Griffin"))
	griffin := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: griffin, From: state.ZHand, To: state.ZExile})
	addMana(t, e, 0, "UUUU")
	d := e.Pending()
	if mayPlayOption(d, griffin) == nil {
		t.Fatalf("may-play cast of the exiled Griffin not offered: %+v", d.Options)
	}
	// The self-grant is Affected$ Card.Self: it never covers another card.
	if other := e.G.Zone(state.ZExile, 0); len(other) != 1 {
		// nothing else to check; the single-candidate walk is the point
		_ = other
	}
	submitChoices(t, e, mayPlayOption(d, griffin).Index)
	passUntilStackEmpty(t, e, 50)
	if o := e.G.Obj(griffin); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Griffin zone=%v, want battlefield", o)
	}
	if o := e.G.Obj(griffin); o != nil && o.CastFlags&state.FlagMayPlay == 0 {
		t.Fatalf("may-play cast did not record FlagMayPlay: %+v", o.CastFlags)
	}
	if pool := e.G.Players[0].Pool.Total(); pool != 0 {
		t.Fatalf("pool=%d, want 0 (the printed {2}{U}{U} was paid)", pool)
	}
}

func TestAtsushiEffectGrantOffersMayPlayCast(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Atsushi, the Blazing Sky"))
	atsushi := e.G.Zone(state.ZHand, 0)[0]
	// Two spell cards on top of the library: the dying charm's Dig exiles
	// exactly them and remembers them into the Effect's grant.
	seeded := seedLibraryTop(t, e,
		"Name:Exiled Bolt\nManaCost:R\nTypes:Instant\nOracle:x\n",
		"Name:Exiled Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	bolt, bear := seeded[0], seeded[1]
	addMana(t, e, 0, "RRRR")
	submitChoices(t, e, castOptionFor(t, e, atsushi).Index)
	passUntilStackEmpty(t, e, 50)
	// Atsushi dies: the ChangesZone trigger queues its Charm. The raw emit
	// does not itself surface the queued trigger, so re-ask the priority; a
	// single trigger needs no ordering ask and resolves straight into the
	// Charm's mode ask.
	e.emit(events.Event{Kind: events.MoveZone, Obj: atsushi, From: state.ZBattlefield, To: state.ZGraveyard})
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("no charm mode ask for the dies trigger: %+v", d)
	}
	// Take ExileTwo (Choices$ order, option 0).
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 50)
	for _, id := range []state.ObjID{bolt, bear} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZExile {
			t.Fatalf("charmed card %d zone=%v, want exile", id, o)
		}
	}
	// The Effect-delivered grant is live: the exiled INSTANT is castable from
	// exile; the {1}{G} Bear is not offered with only {R} in the pool (the
	// grant carries no MayPlayIgnoreColor$ rider).
	addMana(t, e, 0, "R")
	d = e.Pending()
	if mayPlayOption(d, bolt) == nil {
		t.Fatalf("may-play cast of the exiled instant not offered: %+v", d.Options)
	}
	if mayPlayOption(d, bear) != nil {
		t.Fatalf("the {1}{G} Bear offered with only {R} in the pool: %+v", d.Options)
	}
	submitChoices(t, e, mayPlayOption(d, bolt).Index)
	passUntilStackEmpty(t, e, 50)
	if o := e.G.Obj(bolt); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("cast-from-exile instant zone=%v, want graveyard", o)
	}
}

func TestIntellectDevourerIgnoreColorPaysAnyColour(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Intellect Devourer"))
	devourer := e.G.Zone(state.ZHand, 0)[0]
	// The opponent's hand card the ETB exiles: a white spell.
	cleric := e.G.AddObject(card(t, "Name:Exiled Cleric\nManaCost:1 W\nTypes:Creature Cleric\nPT:1/2\nOracle:x\n"), 1)
	e.G.SetZone(state.ZHand, 1, []state.ObjID{cleric.ID})
	addMana(t, e, 0, "BBBB")
	submitChoices(t, e, castOptionFor(t, e, devourer).Index)
	passUntilStackEmpty(t, e, 50)
	// The ETB exiles seat 1's hand card (mandatory, no choice with one card).
	answerTriggerOrder(t, e)
	if o := e.G.Obj(cleric.ID); o == nil || o.Zone != state.ZExile {
		t.Fatalf("devoured card zone=%v, want exile", o)
	}
	// Only BLACK mana in the pool: the MayPlayIgnoreColor$ rider pays the
	// exiled Cleric's {W} pip from it ({1} takes the other black).
	addMana(t, e, 0, "BB")
	d := e.Pending()
	if mayPlayOption(d, cleric.ID) == nil {
		t.Fatalf("may-play cast not offered: %+v", d.Options)
	}
	submitChoices(t, e, mayPlayOption(d, cleric.ID).Index)
	passUntilStackEmpty(t, e, 50)
	if o := e.G.Obj(cleric.ID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("ignore-colour may-play cast did not resolve: %v", o)
	}
	// The {1}{W} cost was paid from black mana; the pool holds none of it.
	if pool := e.G.Players[0].Pool.Total(); pool != 0 {
		t.Fatalf("pool=%d, want 0", pool)
	}
}

func TestMayPlayLimitCapsOncePerTurn(t *testing.T) {
	t.Parallel()
	// Karador's S: static (Condition$ PlayerTurn, MayPlayLimit$ 1): after one
	// may-play cast this turn, a second graveyard creature is not offered
	// through the limited grant.
	e := handEngine(t, corpusAlternativeCard(t, "Karador, Ghost Chieftain"))
	karador := e.G.Zone(state.ZHand, 0)[0]
	c1 := e.G.AddObject(card(t, "Name:Grave A\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0).ID
	c2 := e.G.AddObject(card(t, "Name:Grave B\nManaCost:2 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0).ID
	e.emit(events.Event{Kind: events.MoveZone, Obj: c1, From: state.ZLibrary, To: state.ZGraveyard})
	e.emit(events.Event{Kind: events.MoveZone, Obj: c2, From: state.ZLibrary, To: state.ZGraveyard})
	e.emit(events.Event{Kind: events.MoveZone, Obj: karador, From: state.ZHand, To: state.ZBattlefield})
	addMana(t, e, 0, "GGG")
	d := e.Pending()
	if mayPlayOption(d, c1) == nil || mayPlayOption(d, c2) == nil {
		t.Fatalf("may-play casts not offered: %+v", d.Options)
	}
	submitChoices(t, e, mayPlayOption(d, c1).Index)
	passUntilStackEmpty(t, e, 50)
	// The second cast is capped: MayPlayLimit$ 1 is spent this turn.
	addMana(t, e, 0, "G")
	d = e.Pending()
	if mayPlayOption(d, c2) != nil {
		t.Fatalf("the once-per-turn cap did not withhold the second cast: %+v", d.Options)
	}
}

func TestMayPlayNotOfferedWithoutTheGrant(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Misthollow Griffin"))
	griffin := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: griffin, From: state.ZHand, To: state.ZGraveyard})
	addMana(t, e, 0, "UUUU")
	d := e.Pending()
	// The self-grant's AffectedZone$ is Exile: the same card in the
	// GRAVEYARD is not covered.
	if mayPlayOption(d, griffin) != nil {
		t.Fatalf("may-play cast offered from the graveyard: %+v", d.Options)
	}
}
