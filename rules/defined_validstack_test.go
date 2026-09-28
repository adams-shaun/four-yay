package rules

// Defined$ ValidStack (CR 701.5a): the non-targeted "counter all ..." shape
// -- Glen Elendra's Answer, Swift Silence, Reverse the Polarity's counter
// mode, Summary Dismissal, Kadena's Silencer -- resolves to every stack
// object matching its comma-separated spec, evaluated at resolution time
// with the SAME stack-kind matcher target legality's TargetType$ census uses
// (state.StackKindOf / state.StackKindTokenOf / state.StackKindAdmits; the
// grammar moved to state because effects cannot import rules). A countered
// spell goes to its owner's graveyard; a countered ability takes the CR
// 608.2m exile parking; RememberCountered$ / RememberCounteredSA$ hand the
// count to the SubAbility$ ("Draw a card for each spell countered this
// way", "Create a Faerie for each spell and ability countered").
//
// These leaves pass on this build, so per the CR-lane rule (a passing leaf's
// guard comes OFF and the ordinary suite defends it) they run in the
// ordinary lane with no GORGE_CR_CONFORMANCE guard.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// answerSrc is Glen Elendra's Answer's shape: counter every opponent spell
// and ability, remember each, and create one token per countered object.
const answerSrc = "Name:Answer\nManaCost:2 U U\nTypes:Instant\n" +
	"A:SP$ Counter | Defined$ ValidStack Spell.OppCtrl,Activated.OppCtrl,Triggered.OppCtrl | " +
	"RememberCounteredSA$ True | SubAbility$ DBToken\n" +
	"SVar:DBToken:DB$ Token | TokenAmount$ X | TokenScript$ ub_1_1_faerie_flying | SubAbility$ DBCleanup\n" +
	"SVar:X:Count$RememberedSize\n" +
	"SVar:DBCleanup:DB$ Cleanup | ClearRemembered$ True\n" +
	"Oracle:x\n"

// silenceSrc is Swift Silence's shape: counter all OTHER spells (any
// controller -- the spec carries no controller qualifier), remember each,
// and draw one card per countered spell.
const silenceSrc = "Name:Silence\nManaCost:2 W U U\nTypes:Instant\n" +
	"A:SP$ Counter | Defined$ ValidStack Spell.Other | RememberCountered$ True | SubAbility$ DBDraw\n" +
	"SVar:DBDraw:DB$ Draw | NumCards$ X | SubAbility$ DBCleanup\n" +
	"SVar:X:Remembered$Amount\n" +
	"SVar:DBCleanup:DB$ Cleanup | ClearRemembered$ True\n" +
	"Oracle:x\n"

// polaritySrc is Reverse the Polarity's shape: a Charm whose counter mode is
// `Defined$ ValidStack Spell.Other`. The mode is announced at cast time
// (CR 601.2b) and resolution executes it without asking again.
const polaritySrc = "Name:Polarity\nManaCost:1 U U\nTypes:Instant\n" +
	"A:SP$ Charm | Choices$ DBCounter,DBPump\n" +
	"SVar:DBCounter:DB$ Counter | Defined$ ValidStack Spell.Other | SpellDescription$ Counter all other spells.\n" +
	"SVar:DBPump:DB$ PumpAll | ValidCards$ Creature | KW$ HIDDEN CARDNAME's power and toughness are switched\n" +
	"Oracle:x\n"

// dismissalSrc is Summary Dismissal's counter half: abilities only, no
// controller qualifier -- both seats' abilities are countered.
const dismissalSrc = "Name:Dismissal\nManaCost:2 U U\nTypes:Instant\n" +
	"A:SP$ Counter | Defined$ ValidStack Activated,Triggered\n" +
	"Oracle:x\n"

// faerieTokenSrc backs the TokenScript$ stem answerSrc names. Inline: a test
// fixture must never depend on the GPL corpus.
const faerieTokenSrc = "Name:Faerie Token\nManaCost:no cost\nColors:blue,black\n" +
	"Types:Creature Faerie\nPT:1/1\nK:Flying\nOracle:x\n"

// validStackFixture mints one object of each relevant stack kind directly
// (the same licence handEngine takes): two card-object spells controlled by
// seat 1, an activated and a triggered ability object controlled by seat 1
// (sourced from the tapper on seat 1's battlefield), and a card-object spell
// controlled by seat 0 -- all beneath whatever the test then casts. Stack
// order is the arena order given to SetZone.
type validStackIds struct {
	oppSpell1, oppSpell2, activated, triggered, ownSpell state.ObjID
}

func validStackFixture(t *testing.T) (*Engine, validStackIds) {
	t.Helper()
	e := counterHands(t, nil, nil, nil, []*cards.Card{card(t, tapperSrc)})
	perm := e.G.Zone(state.ZBattlefield, 1)[0]
	face := e.G.Obj(perm).Face()

	oppSpell1 := e.G.AddObject(card(t, grizzlySrc), 1)
	oppSpell1.Zone = state.ZStack
	oppSpell2 := e.G.AddObject(card(t, grizzlySrc), 1)
	oppSpell2.Zone = state.ZStack
	activated := e.G.AddObject(nil, 1)
	activated.Ability = face.Abilities[0]
	activated.Source = perm
	activated.Zone = state.ZStack
	triggered := e.G.AddObject(nil, 1)
	triggered.Ability = face.Triggers[0].Effect
	triggered.Source = perm
	triggered.Zone = state.ZStack
	ownSpell := e.G.AddObject(card(t, grizzlySrc), 0)
	ownSpell.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{oppSpell1.ID, oppSpell2.ID, activated.ID, triggered.ID, ownSpell.ID})
	return e, validStackIds{oppSpell1.ID, oppSpell2.ID, activated.ID, triggered.ID, ownSpell.ID}
}

// castUntargetedFromHand casts the hand card named name from seat 0 -- the
// caller must have charged its pool -- and passes both seats' priority so the
// spell resolves with the fixture objects still beneath it on the stack.
func castUntargetedFromHand(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	id := handObj(t, e, 0, name)
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, id))
	passOnce(t, e) // caster receives priority back (CR 117.3c) and passes
	passOnce(t, e) // seat 1 passes; the spell resolves
	return id
}

// counteredEvents collects every MoveZone Text "countered" for id.
func counteredEvents(e *Engine, id state.ObjID) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.Text == "countered" {
			out = append(out, ev)
		}
	}
	return out
}

// tokenCreates counts TokenCreate events carrying the token script stem.
func tokenCreates(e *Engine, stem string) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TokenCreate && ev.Text == stem {
			n++
		}
	}
	return n
}

// draws counts Draw events in the log.
func draws(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Draw {
			n++
		}
	}
	return n
}

// TestAnswerCountersOpponentSpellsAndAbilitiesNotItsOwn (CR 701.5a, 608.2m):
// Glen Elendra's Answer's `Defined$ ValidStack Spell.OppCtrl,Activated.
// OppCtrl,Triggered.OppCtrl` counters every opponent spell (to the graveyard)
// and ability (to the exile parking), never its controller's own spell, and
// creates one Faerie token per countered object (SVar:X:Count$RememberedSize
// after RememberCounteredSA$ True).
func TestAnswerCountersOpponentSpellsAndAbilitiesNotItsOwn(t *testing.T) {
	t.Parallel()
	e, ids := validStackFixture(t)
	e.G.SetZone(state.ZHand, 0, []state.ObjID{})
	e.G.SetZone(state.ZHand, 1, []state.ObjID{})
	ans := e.G.AddObject(card(t, answerSrc), 0)
	ans.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{ans.ID})
	e.G.Players[0].Pool[state.MU] = 2
	e.G.Players[0].Pool[state.MG] = 2
	e.G.Tokens = map[string]*cards.Card{"ub_1_1_faerie_flying": card(t, faerieTokenSrc)}

	castUntargetedFromHand(t, e, "Answer")

	if z := e.G.Obj(ids.oppSpell1).Zone; z != state.ZGraveyard {
		t.Fatalf("opponent spell went to %s, want graveyard", z)
	}
	if z := e.G.Obj(ids.oppSpell2).Zone; z != state.ZGraveyard {
		t.Fatalf("second opponent spell went to %s, want graveyard", z)
	}
	if z := e.G.Obj(ids.activated).Zone; z != state.ZExile {
		t.Fatalf("countered activated ability went to %s, want the CR 608.2m exile parking", z)
	}
	if z := e.G.Obj(ids.triggered).Zone; z != state.ZExile {
		t.Fatalf("countered triggered ability went to %s, want the CR 608.2m exile parking", z)
	}
	if z := e.G.Obj(ids.ownSpell).Zone; z != state.ZStack {
		t.Fatalf("controller's own spell was countered (now in %s), want it still on the stack", z)
	}
	for _, id := range []state.ObjID{ids.ownSpell, ans.ID} {
		if n := len(counteredEvents(e, id)); n != 0 {
			t.Fatalf("object %d was countered %d time(s); only OppCtrl objects may be", id, n)
		}
	}
	if n := tokenCreates(e, "ub_1_1_faerie_flying"); n != 4 {
		t.Fatalf("created %d Faerie tokens, want one per countered object (2 spells + 2 abilities = 4)", n)
	}
}

// TestSilenceDrawsPerCounteredSpell (CR 608.2g via Remembered$Amount): Swift
// Silence's `Defined$ ValidStack Spell.Other` counters every OTHER spell --
// both controllers' -- and draws exactly one card per countered spell. The
// spec carries no controller qualifier, so the caster's own other spells are
// countered too; only the resolving source itself is excluded by Other.
func TestSilenceDrawsPerCounteredSpell(t *testing.T) {
	t.Parallel()
	e, ids := validStackFixture(t)
	e.G.SetZone(state.ZHand, 0, []state.ObjID{})
	e.G.SetZone(state.ZHand, 1, []state.ObjID{})
	sil := e.G.AddObject(card(t, silenceSrc), 0)
	sil.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{sil.ID})
	e.G.Players[0].Pool[state.MW] = 1
	e.G.Players[0].Pool[state.MU] = 2
	e.G.Players[0].Pool[state.MG] = 2

	before := draws(e)
	castUntargetedFromHand(t, e, "Silence")

	for _, id := range []state.ObjID{ids.oppSpell1, ids.oppSpell2, ids.ownSpell} {
		if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
			t.Fatalf("spell %d went to %s, want graveyard (Spell.Other has no controller qualifier)", id, z)
		}
	}
	for _, id := range []state.ObjID{ids.activated, ids.triggered, sil.ID} {
		if n := len(counteredEvents(e, id)); n != 0 {
			t.Fatalf("object %d was countered; the Spell.Other spec names spells only, not the source or abilities", id)
		}
	}
	if got := draws(e) - before; got != 3 {
		t.Fatalf("drew %d cards, want one per countered spell (3)", got)
	}
}

// TestPolarityCounterModeCountersAllOtherSpells: Reverse the Polarity's
// Charm counter mode, announced at cast time (CR 601.2b) and executed at
// resolution through effCharm's pre-seeded Modes, counters every other spell
// on the stack -- opponent's and the caster's own -- but never the resolving
// Charm itself.
func TestPolarityCounterModeCountersAllOtherSpells(t *testing.T) {
	t.Parallel()
	e, ids := validStackFixture(t)
	e.G.SetZone(state.ZHand, 0, []state.ObjID{})
	e.G.SetZone(state.ZHand, 1, []state.ObjID{})
	pol := e.G.AddObject(card(t, polaritySrc), 0)
	pol.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{pol.ID})
	e.G.Players[0].Pool[state.MU] = 2
	e.G.Players[0].Pool[state.MG] = 1

	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, pol.ID))
	// CR 601.2b: the modal spell announces its mode right after reaching the
	// stack. DBCounter is Choices$' first mode.
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.Player != 0 {
		t.Fatalf("expected seat 0's cast-time mode announcement, got %+v", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("expected both Charm modes offered, got %+v", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)
	passOnce(t, e) // caster receives priority back and passes
	passOnce(t, e) // seat 1 passes; Polarity resolves its counter mode

	for _, id := range []state.ObjID{ids.oppSpell1, ids.oppSpell2, ids.ownSpell} {
		if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
			t.Fatalf("spell %d went to %s, want graveyard", id, z)
		}
	}
	if n := len(counteredEvents(e, pol.ID)); n != 0 {
		t.Fatalf("Polarity countered itself %d time(s); Other excludes the resolving source", n)
	}
	for _, id := range []state.ObjID{ids.activated, ids.triggered} {
		if n := len(counteredEvents(e, id)); n != 0 {
			t.Fatalf("ability object %d was countered; the mode names spells only", id)
		}
	}
}

// TestDismissalCountersAbilitiesOfBothSeats (CR 701.5a): Summary Dismissal's
// `Defined$ ValidStack Activated,Triggered` carries no controller qualifier,
// so both seats' ability objects are countered, and the card-object spells
// beneath them are not.
func TestDismissalCountersAbilitiesOfBothSeats(t *testing.T) {
	t.Parallel()
	e, ids := validStackFixture(t)
	e.G.SetZone(state.ZHand, 0, []state.ObjID{})
	e.G.SetZone(state.ZHand, 1, []state.ObjID{})
	dis := e.G.AddObject(card(t, dismissalSrc), 0)
	dis.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{dis.ID})
	e.G.Players[0].Pool[state.MU] = 2
	e.G.Players[0].Pool[state.MG] = 2

	castUntargetedFromHand(t, e, "Dismissal")

	if z := e.G.Obj(ids.activated).Zone; z != state.ZExile {
		t.Fatalf("opponent's activated ability went to %s, want exile", z)
	}
	if z := e.G.Obj(ids.triggered).Zone; z != state.ZExile {
		t.Fatalf("opponent's triggered ability went to %s, want exile", z)
	}
	if z := e.G.Obj(ids.oppSpell1).Zone; z != state.ZStack {
		t.Fatalf("spell object %d moved to %s; the Activated,Triggered spec names no spell kind", ids.oppSpell1, z)
	}
	if n := len(counteredEvents(e, dis.ID)); n != 0 {
		t.Fatalf("Dismissal was countered %d time(s); a non-Spell token never admits the source spell", n)
	}
}

// TestStackKindTokenOfAbilityBase pins the state-side grammar addition
// (task abcopy3): `Ability` parses to the Activated+Triggered kind set --
// the mirror of SpellAbility minus Spell -- and admits an ability wrapper
// of either kind while never admitting a spell. Near-miss bases stay
// unknown (ok=false), which is what keeps copy.go's known-token guard
// fail-closed for a genuinely unparsed token.
func TestStackKindTokenOfAbilityBase(t *testing.T) {
	t.Parallel()
	tok, ok := state.StackKindTokenOf("Ability.YouCtrl")
	if !ok {
		t.Fatal("Ability.YouCtrl did not parse as a stack-kind token")
	}
	if !tok.Kinds[state.StackKindActivated] || !tok.Kinds[state.StackKindTriggered] {
		t.Fatalf("Ability kinds = %+v, want Activated+Triggered", tok.Kinds)
	}
	if tok.Kinds[state.StackKindSpell] {
		t.Fatalf("Ability must never admit the Spell kind: %+v", tok.Kinds)
	}
	if !tok.YouCtrl {
		t.Fatalf("YouCtrl qualifier lost: %+v", tok)
	}
	if _, ok := state.StackKindTokenOf("Ability2"); ok {
		t.Fatal("Ability2 parsed as a stack-kind token; near-miss bases must stay unknown")
	}
	// Admission, both kinds: an ability wrapper (Face nil) of each kind is
	// admitted; a card object (a spell) is not.
	tok, _ = state.StackKindTokenOf("Ability")
	ab := &state.Object{ID: 7, Controller: 0}
	if !state.StackKindAdmits([]state.StackKindToken{tok}, state.StackKindActivated, ab, 0, 0) {
		t.Fatal("Ability does not admit an activated wrapper")
	}
	if !state.StackKindAdmits([]state.StackKindToken{tok}, state.StackKindTriggered, ab, 0, 0) {
		t.Fatal("Ability does not admit a triggered wrapper")
	}
	if state.StackKindAdmits([]state.StackKindToken{tok}, state.StackKindSpell, ab, 0, 0) {
		t.Fatal("Ability admits a spell object")
	}
}
