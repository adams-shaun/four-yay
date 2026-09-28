package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The wasCastFrom<Zone> filter predicate family (task wascastfrom): the
// origin-zone cast provenance readable in object filter specs. Sevinne's
// Reclamation is the real corpus carrier the brief names (Desert Bloom, otc):
// its DBCopy sub gates on ConditionDefined$ Self | ConditionPresent$
// Card.wasCastFromGraveyard, so the flashback cast must produce the copy and
// an ordinary hand cast must not.

// sevinneTestEngine deals seat 0 a deck whose first cards are the named
// corpus card followed by Grizzly Bears (Sevinne's return-a-permanent target
// and the copy's re-target candidate), then Mountains. Seat 1 draws
// Mountains. The cards named by hands/gys are moved into place with logged
// MoveZones the replay re-derives.
func sevinneTestEngine(t *testing.T, lead, second string) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	a := searchCorpusCard(t, reg, lead)
	b := searchCorpusCard(t, reg, second)
	mtn := searchCorpusCard(t, reg, "Mountain")
	deck := []*cards.Card{a, b}
	for len(deck) < 40 {
		deck = append(deck, mtn)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mtn
	}
	cfg := seatZeroStart(Config{Seed: 64021, Names: []string{"sevinne", "opp"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	leadID := searchMoveByName(t, e, a.Faces[0].Name, state.ZHand)
	secondID := searchMoveByName(t, e, b.Faces[0].Name, state.ZHand)
	return e, cfg, leadID, secondID
}

// countCopies counts the StackCopy events the log carries for obj.
func countCopies(t *testing.T, e *Engine, obj state.ObjID) int {
	t.Helper()
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.StackCopy && ev.Obj == obj {
			n++
		}
	}
	return n
}

func TestSevinneReclamationFlashbackCastCopies(t *testing.T) {
	t.Parallel()
	e, cfg, sev, bear := sevinneTestEngine(t, "Sevinne's Reclamation", "Grizzly Bears")
	// Precondition: the spell really is in the graveyard with the flashback
	// keyword, and the Bears really are its legal return target (MV 2 <= 3).
	if o := e.G.Obj(sev); o.Zone != state.ZHand {
		t.Fatalf("Sevinne in %s, want hand", o.Zone)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: sev, From: state.ZHand, To: state.ZGraveyard})
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZHand, To: state.ZGraveyard})
	// Flashback {4}{W} costs five mana; fund it before asking for the offer
	// (legalActions prices its options, so an unfunded flashback is withheld).
	addMana(t, e, 0, "WWWWW")
	var fb *decision.Option
	for _, o := range castOptions(t, e) {
		if o.Mode == "flashback" && o.Obj == sev {
			fb = &o
		}
	}
	if fb == nil {
		t.Fatal("flashback not offered from the graveyard")
	}
	for _, o := range castOptions(t, e) {
		if o.Mode == "flashback" && o.Obj == sev {
			fb = &o
		}
	}
	if fb == nil {
		t.Fatal("flashback not offered with the pool funded")
	}
	submitChoices(t, e, fb.Index)
	// The return-target ask: the Bears (the only eligible permanent card in
	// the graveyard).
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the return-target ask, got %+v", d)
	}
	found := false
	for _, o := range d.Options {
		if o.Obj == bear {
			found = true
		}
	}
	if !found {
		t.Fatalf("the graveyard Bears are not offered: %+v", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)
	// The Optional$ True copy clause now poses the may-copy election before the
	// copy is made (task api-copyspellability-optional). Accept it, then drain.
	// A stack copy was never cast (CR 707.10), so the copy's own clause must
	// NOT fire: driveSevinneResolutionToEnd declines any nested copy_optional
	// and returns how many it saw, which must be zero.
	nd := passUntilNonPriority(t, e, 30)
	if nd == nil || nd.Kind != decision.KChoose || nd.ResumeKind != "copy_optional" {
		t.Fatalf("expected Sevinne's Optional$ may-copy election, got %+v", nd)
	}
	submitChoices(t, e, nd.Options[0].Index)
	nestedDeclines := driveSevinneResolutionToEnd(t, e, 40)
	if nestedDeclines != 0 {
		t.Fatalf("the copy posed its own copy clause %d time(s): Card.wasCastFromGraveyard must be false on a copy", nestedDeclines)
	}
	// The spell rest in exile (flashback, CR 702.33b) and the Bears came
	// back to the battlefield.
	if o := e.G.Obj(sev); o.Zone != state.ZExile {
		t.Fatalf("flashbacked Sevinne went to %s, want exile", o.Zone)
	}
	if o := e.G.Obj(bear); o.Zone != state.ZBattlefield {
		t.Fatalf("the returned Bears went to %s, want battlefield", o.Zone)
	}
	if got := countCopies(t, e, sev); got != 1 {
		t.Fatalf("flashback cast produced %d copies, want exactly 1", got)
	}
	replayCheck(t, e, cfg)
}

func TestSevinneReclamationHandCastDoesNotCopy(t *testing.T) {
	t.Parallel()
	e, cfg, sev, bear := sevinneTestEngine(t, "Sevinne's Reclamation", "Grizzly Bears")
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZHand, To: state.ZGraveyard})
	addMana(t, e, 0, "WWW")
	castObj(t, e, sev)
	if o := e.G.Obj(bear); o.Zone != state.ZBattlefield {
		t.Fatalf("the returned Bears went to %s, want battlefield", o.Zone)
	}
	if got := countCopies(t, e, sev); got != 0 {
		t.Fatalf("ordinary hand cast produced %d copies, want 0", got)
	}
	replayCheck(t, e, cfg)
}

// The origin-zone family beyond the graveyard spelling: wasCastFromExile,
// wasCastFromYourGraveyard, wasCastFromTheirHand and
// wasCastFromYourGraveyardByYou (task wascastfrom). Each is pinned end to
// end on a real corpus carrier; the hand-engine fixtures never commit Forge
// script text (the flashback spell is written inline).

// flashFixture is a one-mana instant whose flashback costs {R} — a cheap
// non-hand-origin cast every origin test can use.
const flashFixture = "Name:Flash\nManaCost:R\nTypes:Instant\nK:Flashback:R\n" +
	"A:SP$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"

func TestBurningVengeanceYourGraveyardTriggerFiresOnFlashback(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Burning Vengeance"), card(t, flashFixture))
	bv := e.G.Obj(e.G.Zone(state.ZHand, 0)[0])
	bv.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{bv.ID})
	flash := e.G.Obj(e.G.Zone(state.ZHand, 0)[1])
	flash.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{flash.ID})
	e.G.SetZone(state.ZHand, 0, nil)
	e.G.Players[0].Pool[state.MR] = 1
	castMode(t, e, flash.ID, "flashback")
	finishCast(t, e, flash.ID)
	// Drain the trigger: it resolves through a real target ask (DealDamage
	// ValidTgts$ Any), answered with the first option.
	d := passUntilAsk(t, e)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the damage target ask, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	dmg := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Amount == 2 {
			dmg = true
		}
	}
	if !dmg {
		t.Fatal("Burning Vengeance dealt no 2 damage on the flashback cast")
	}
}

func TestBurningVengeanceHandCastFiresNothing(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Burning Vengeance"), card(t, flashFixture))
	bv := e.G.Obj(e.G.Zone(state.ZHand, 0)[0])
	bv.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{bv.ID})
	flash := e.G.Obj(e.G.Zone(state.ZHand, 0)[1])
	e.G.Players[0].Pool[state.MR] = 1
	e.G.SetZone(state.ZHand, 0, []state.ObjID{flash.ID})
	castMode(t, e, flash.ID, "")
	finishCast(t, e, flash.ID)
	passUntilStackEmpty(t, e, 20)
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage {
			t.Fatalf("hand cast dealt damage %+v; the YourGraveyard trigger must be silent", ev)
		}
	}
}

func TestAerialExtortionistDrawsOnANonHandCast(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, flashFixture))
	ext := e.G.AddObject(corpusAlternativeCard(t, "Aerial Extortionist"), 1)
	ext.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{ext.ID})
	flash := e.G.Obj(e.G.Zone(state.ZHand, 0)[0])
	flash.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{flash.ID})
	e.G.SetZone(state.ZHand, 0, nil)
	e.G.Players[0].Pool[state.MR] = 1
	castMode(t, e, flash.ID, "flashback")
	finishCast(t, e, flash.ID)
	e.Advance()
	passUntilStackEmpty(t, e, 20)
	if got := len(e.G.Zone(state.ZHand, 1)); got != 1 {
		t.Fatalf("seat 1 holds %d cards after the opponent's non-hand cast, want the trigger's 1 draw", got)
	}
}

func TestAerialExtortionistHandCastDrawsNothing(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, flashFixture))
	ext := e.G.AddObject(corpusAlternativeCard(t, "Aerial Extortionist"), 1)
	ext.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{ext.ID})
	flash := e.G.Obj(e.G.Zone(state.ZHand, 0)[0])
	e.G.Players[0].Pool[state.MR] = 1
	castMode(t, e, flash.ID, "")
	finishCast(t, e, flash.ID)
	e.Advance()
	passUntilStackEmpty(t, e, 20)
	if got := len(e.G.Zone(state.ZHand, 1)); got != 0 {
		t.Fatalf("seat 1 holds %d cards after a hand cast, want 0 draws", got)
	}
}

func TestDelayedBlastFireballForetellCastDealsFive(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Delayed Blast Fireball"))
	id := e.G.Obj(e.G.Zone(state.ZHand, 0)[0]).ID
	e.G.Players[0].Pool[state.MC] = 2
	foretellIt(t, e, id)
	if e.WasCastFromExile(id) {
		t.Fatal("a foretold card that was never cast reads WasCastFromExile true")
	}
	driveToTurn3Main(t, e)
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MR] = 4, 2
	if e.WasCastFromExile(id) {
		t.Fatal("the still-uncast card reads WasCastFromExile true after the exile move")
	}
	submitOption(t, e, "foretell_cast", "Cast Delayed Blast Fireball (foretold)")
	finishCast(t, e, id)
	if !e.WasCastFromExile(id) {
		t.Fatal("the foretell cast's provenance is not exile")
	}
	// X=5: each opponent takes 5 (the {2} hand cast would deal 2).
	if got := e.G.Players[1].Life; got != 15 {
		t.Fatalf("opponent at %d life after the foretell cast, want 20-5=15", got)
	}
}

func TestDelayedBlastFireballHandCastDealsTwo(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Delayed Blast Fireball"))
	id := e.G.Obj(e.G.Zone(state.ZHand, 0)[0]).ID
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MR] = 1, 2
	castMode(t, e, id, "")
	finishCast(t, e, id)
	if e.WasCastFromExile(id) {
		t.Fatal("a hand cast reads WasCastFromExile true")
	}
	if got := e.G.Players[1].Life; got != 18 {
		t.Fatalf("opponent at %d life after the hand cast, want 20-2=18", got)
	}
}

func TestArchfiendsVesselCastFromGraveyardByYouExilesAndTokens(t *testing.T) {
	t.Parallel()
	e := handEngineTokens(t, corpusAlternativeCard(t, "Archfiend's Vessel"))
	vessel := e.G.Obj(e.G.Zone(state.ZHand, 0)[0])
	// A graveyard-origin cast by seat 0, then the battlefield entry: the
	// ByYou trigger (Card.Self+wasCastFromYourGraveyardByYou, no Origin$ of
	// its own) is the one that fires — the sibling Origin$ Graveyard trigger
	// stays silent because the entry's move is stack→battlefield.
	e.emit(events.Event{Kind: events.PutOnStack, Obj: vessel.ID, Player: 0,
		From: state.ZGraveyard, To: state.ZStack, Text: "Archfiend's Vessel"})
	e.emit(events.Event{Kind: events.MoveZone, Obj: vessel.ID,
		From: state.ZStack, To: state.ZBattlefield})
	e.Advance()
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(vessel.ID); o.Zone != state.ZExile {
		t.Fatalf("the cast-from-graveyard entry stayed in %s, want exiled by the trigger", o.Zone)
	}
	demons := 0
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone == state.ZBattlefield && o.Face() != nil && o.Face().Name == "Demon Token" {
			demons++
		}
	}
	if demons != 1 {
		t.Fatalf("the trigger minted %d Demon tokens, want 1", demons)
	}
}

func TestArchfiendsVesselHandOriginEntryStaysPut(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Archfiend's Vessel"))
	vessel := e.G.Obj(e.G.Zone(state.ZHand, 0)[0])
	// A hand-origin "cast" (provenance hand) followed by the entry: the
	// ByYou token must NOT hold — this is the spelling-distinctive negative
	// the shared graveyard read would fail (wasCastFromGraveyard would not
	// hold either, so the isolate is the token, not the read).
	e.emit(events.Event{Kind: events.PutOnStack, Obj: vessel.ID, Player: 0,
		From: state.ZHand, To: state.ZStack, Text: "Archfiend's Vessel"})
	e.emit(events.Event{Kind: events.MoveZone, Obj: vessel.ID,
		From: state.ZStack, To: state.ZBattlefield})
	e.Advance()
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(vessel.ID); o.Zone != state.ZBattlefield {
		t.Fatalf("a hand-origin entry was exiled to %s; the ByYou token must not hold", o.Zone)
	}
}

// TestRoryWilliamsWasCastFromExilePredicate pins the real Rory Williams
// carrier's negative exile-origin predicate and its census recognition.
func TestRoryWilliamsWasCastFromExilePredicate(t *testing.T) {
	t.Parallel()
	if got := effects.UnknownPredicates("Card.Self+!wasCastFromExile"); len(got) != 0 {
		t.Fatalf("Rory's real trigger predicate is still unknown: %v", got)
	}
	reg := searchTestRegistry(t)
	rory := searchCorpusCard(t, reg, "Rory Williams")
	e := handEngine(t, rory)
	id := e.G.Zone(state.ZHand, 0)[0]
	// Rory's real trigger carries Card.Self+!wasCastFromExile. Verify the
	// provenance half on that real carrier's object after an actual exile-origin
	// PutOnStack record; this is the state the SpellCast matcher consumes.
	e.emit(events.Event{Kind: events.PutOnStack, Obj: id, From: state.ZExile, To: state.ZStack,
		Player: 0, Text: "Rory Williams"})
	if e.G.Obj(id).Zone != state.ZStack {
		t.Fatalf("Rory test object is in %s, want stack", e.G.Obj(id).Zone)
	}
	if _, ok := e.castProvenanceAdmits("Card.wasCastFromExile", id, 0); !ok {
		t.Fatal("Rory's exile-origin cast was not recognised")
	}
	if _, ok := e.castProvenanceAdmits("Card.!wasCastFromExile", id, 0); ok {
		t.Fatal("Rory's negated exile-origin predicate incorrectly held")
	}
}

// TestRorySpellCastTriggerReadsBothOrigins drives Rory's REAL compiled trigger
// (ValidCard$ Card.Self+!wasCastFromExile) through the SpellCast matcher for
// both origins. The hand-origin cast must MATCH (the negated predicate holds)
// and the exile-origin cast must NOT (the predicate strips the only
// alternative). This is the matcher-level half the end-to-end test cannot
// isolate: with the origin read inert (holds always false) BOTH casts match
// and a non-exile trigger wrongly fires on an exile cast, so the assertion
// fails.
func TestRorySpellCastTriggerReadsBothOrigins(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	eval := func(from state.Zone) (bool, *Engine, state.ObjID) {
		rory := searchCorpusCard(t, reg, "Rory Williams")
		e := handEngine(t, rory)
		id := e.G.Zone(state.ZHand, 0)[0]
		face := e.G.Obj(id).Face()
		if face == nil || len(face.Triggers) == 0 || face.Triggers[0].Mode != "SpellCast" {
			t.Fatalf("Rory precondition failed: trigger is %+v, want a SpellCast", face)
		}
		if !strings.Contains(face.Triggers[0].Params["ValidCard"], "!wasCastFromExile") {
			t.Fatalf("Rory's real trigger ValidCard is %q, want it to carry !wasCastFromExile",
				face.Triggers[0].Params["ValidCard"])
		}
		ev := events.Event{Kind: events.PutOnStack, Obj: id, Player: 0, From: from, To: state.ZStack}
		e.emit(ev)
		return e.spellCastEval(face.Triggers[0], id, ev), e, id
	}
	hand, _, _ := eval(state.ZHand)
	exile, _, _ := eval(state.ZExile)
	if !hand {
		t.Fatal("Rory's trigger did not match an ordinary hand cast")
	}
	if exile {
		t.Fatal("Rory's trigger wrongly matched an exile-origin cast; the !wasCastFromExile gate is inert")
	}
}

// TestRoryExilesItselfWithTimeCountersOnCast pins the player-visible carrier:
// a real hand cast fires Rory's SpellCast trigger, moves it to exile, and puts
// the three TIME counters on the exiled card.
func TestRoryExilesItselfWithTimeCountersOnCast(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	rory := searchCorpusCard(t, reg, "Rory Williams")
	e := handEngine(t, rory)
	id := e.G.Zone(state.ZHand, 0)[0]
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("Rory precondition failed: object is %+v, want hand", o)
	}
	addMana(t, e, 0, "WU")
	castObj(t, e, id)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZExile {
		t.Fatalf("Rory hand cast ended in %v, want exile", o.Zone)
	}
	if got := o.Counter("TIME"); got != 3 {
		t.Fatalf("Rory exile trigger put %d TIME counters, want 3", got)
	}
	if hasNote(e, "unimplemented API ChangeZone") {
		t.Fatal("Rory's real exile trigger fell through the ChangeZone handler")
	}
}

// TestCastOriginAdmitsChainUnit pins the chain's exact-token strip and the
// ByYou scoping at the rules level: the log shapes each spelling reads, and
// the copy guard.
func TestCastOriginAdmitsChainUnit(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, flashFixture))
	id := e.G.Obj(e.G.Zone(state.ZHand, 0)[0]).ID
	// A card with no cast at all: a positive-only origin spec matches
	// nothing (every alternative carries the unmet token and drops), while
	// its negation survives.
	if _, ok := e.castProvenanceAdmits("Card.wasCastFromExile", id, 0); ok {
		t.Fatal("an uncast card's positive-only origin spec must match nothing")
	}
	if _, ok := e.castProvenanceAdmits("!wasCastFromExile", id, 0); !ok {
		t.Fatal("an uncast card's negated origin spec must survive")
	}
	e.emit(events.Event{Kind: events.PutOnStack, Obj: id, Player: 0,
		From: state.ZExile, To: state.ZStack, Text: "Flash"})
	if s, ok := e.castProvenanceAdmits("Card.wasCastFromExile+!token", id, 0); !ok || s != "Card.!token" {
		t.Fatalf("exile-origin spec did not strip to %q (got %q, ok %v)", "Card.!token", s, ok)
	}
	if _, ok := e.castProvenanceAdmits("Card.wasCastFromTheirHand", id, 0); ok {
		t.Fatal("wasCastFromTheirHand must not hold on an exile-origin cast")
	}
	if _, ok := e.castProvenanceAdmits("!wasCastFromTheirHand", id, 0); !ok {
		t.Fatal("the negated TheirHand spelling must survive on an exile-origin cast")
	}
	// The ByYou scoping: seat 1 evaluating "your graveyard" against seat 0's
	// graveyard cast reads false.
	e2 := handEngine(t, card(t, flashFixture))
	id2 := e2.G.Zone(state.ZHand, 0)[0]
	e2.emit(events.Event{Kind: events.PutOnStack, Obj: id2, Player: 0,
		From: state.ZGraveyard, To: state.ZStack, Text: "Flash"})
	if s, ok := e2.castProvenanceAdmits("Card.wasCastFromYourGraveyardByYou", id2, 0); !ok || s != "Card" {
		t.Fatalf("ByYou spec did not strip for its own caster (got %q, ok %v)", s, ok)
	}
	if _, ok := e2.castProvenanceAdmits("Card.wasCastFromYourGraveyardByYou", id2, 1); ok {
		t.Fatal("seat 1's ByYou read must not hold on seat 0's graveyard cast")
	}
	if _, ok := e2.castProvenanceAdmits("Card.wasCastFromYourGraveyard", id2, 0); !ok {
		t.Fatal("the plain YourGraveyard spelling must strip for its own caster")
	}
}

// TestLordOfTheForsakenRestrictedManaPaysAFlashbackCast pins the
// RestrictValid$ read at the PAYMENT (the origin-zone family's one
// cost-path carrier): the demon's {C} mana is restricted to
// Spell.wasCastFromYourGraveyard casts, and the payment evaluates the
// restriction AFTER the push, where the log carries the cast from the
// graveyard — so the restricted mana pays a flashback {2} and nothing else
// is in the pool to pay it with.
func TestLordOfTheForsakenRestrictedManaPaysAFlashbackCast(t *testing.T) {
	t.Parallel()
	flash2 := "Name:Flash2\nManaCost:2 U\nTypes:Instant\nK:Flashback:2\n" +
		"A:SP$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
	e := handEngine(t, corpusAlternativeCard(t, "Lord of the Forsaken"), card(t, flash2))
	lord := e.G.Obj(e.G.Zone(state.ZHand, 0)[0])
	lord.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{lord.ID})
	flash := e.G.Obj(e.G.Zone(state.ZHand, 0)[1])
	flash.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{flash.ID})
	e.G.SetZone(state.ZHand, 0, nil)
	for i := 0; i < 2; i++ {
		e.pending = nil
		e.priorityRound()
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("no priority decision for the activation: %+v", d)
		}
		act := -1
		for _, o := range d.Options {
			if o.Kind == "activate" && o.Obj == lord.ID {
				act = o.Index
			}
		}
		if act < 0 {
			t.Fatalf("the restricted {C} mana ability not offered: %+v", d.Options)
		}
		submitChoices(t, e, act)
		// The {C} producer has no colour ask ({C} is one colourless unit), so
		// the activation settles without a wheel; a wheel, if one ever
		// appears, is answered with the restricted ability's own option.
		if wheel := e.Pending(); wheel != nil && wheel.Kind == decision.KChoose {
			idx := -1
			for i, ab := range lord.Face().ManaAbilities() {
				if ab.Params["RestrictValid"] != "" {
					idx = i
				}
			}
			if idx < 0 {
				t.Fatal("Lord of the Forsaken has no restricted mana ability")
			}
			pick := -1
			for _, o := range wheel.Options {
				if o.Ability == idx {
					pick = o.Index
				}
			}
			if pick < 0 {
				t.Fatalf("no wheel option for the restricted ability: %+v", wheel.Options)
			}
			submitChoices(t, e, pick)
		}
	}
	if got := e.G.Players[0].Pool.Total(); got != 2 {
		t.Fatalf("pool holds %d mana after two activations, want 2", got)
	}
	// The OFFER: the affordability walk (castable → costPayable) prices the
	// restriction pre-push through castProvenanceAdmitsPending's at-zone
	// read — the origin a flashback from here will carry IS the graveyard —
	// so the restricted-only pool offers the cast.
	e.pending = nil
	e.priorityRound()
	off := e.Pending()
	offered := false
	if off != nil {
		for _, o := range off.Options {
			if o.Kind == "cast" && o.Obj == flash.ID && o.Mode == "flashback" {
				offered = true
			}
		}
	}
	if !offered {
		t.Fatalf("the flashback cast is not offered on the restricted-only pool: %+v", off)
	}
	castMode(t, e, flash.ID, "flashback")
	finishCast(t, e, flash.ID)
	if o := e.G.Obj(flash.ID); o.Zone != state.ZExile {
		t.Fatalf("the flashback cast did not resolve off restricted {C}: spell in %s", o.Zone)
	}
}
