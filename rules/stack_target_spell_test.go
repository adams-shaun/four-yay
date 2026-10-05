package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const manaLeakSpellSrc = "Name:Mana Leak\nManaCost:U\nTypes:Instant\n" +
	"A:SP$ Counter | TargetType$ Spell | TgtPrompt$ Select target spell | ValidTgts$ Card | SpellDescription$ Counter target spell.\nOracle:x\n"

// targetSpellFixture builds the scenario the brief names: a real-corpus-shaped
// counterspell (TargetType$ Spell + ValidTgts$ Card, no TgtZone$) in seat 0's
// hand, a creature spell in seat 0's hand, and an opponent-controlled
// battlefield creature -- the "Gurmag Angler #304" a broken askTarget used to
// offer for the prompt "Select target spell". It returns the engine plus the
// Mana Leak, Bear-spell and battlefield-creature object IDs.
func targetSpellFixture(t *testing.T) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	bearSrc := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	gurmagSrc := "Name:Gurmag\nManaCost:4 B\nTypes:Creature Zombie\nPT:5/5\nOracle:x\n"
	e := handEngine(t, card(t, manaLeakSpellSrc), card(t, bearSrc))
	g := e.G.AddObject(card(t, gurmagSrc), 1)
	g.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{g.ID})

	var leakID, bearID state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		switch e.G.Obj(id).Face().Name {
		case "Mana Leak":
			leakID = id
		case "Bear":
			bearID = id
		}
	}
	if leakID == 0 || bearID == 0 {
		t.Fatalf("fixture hand missing cards: leak=%d bear=%d", leakID, bearID)
	}
	return e, leakID, bearID, g.ID
}

// passToCast submits "pass" on each priority decision until the pending seat
// can take the given cast option, returning that option's index.
func passToCast(t *testing.T, e *Engine, castObj state.ObjID) int {
	t.Helper()
	for i := 0; i < 8; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("expected a priority decision while seeking cast of %d, got %+v", castObj, d)
		}
		for _, o := range d.Options {
			if o.Kind == "cast" && o.Obj == castObj {
				return o.Index
			}
		}
		pass := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pass = o.Index
			}
		}
		if pass < 0 {
			t.Fatalf("priority decision with no pass and no cast of %d: %+v", castObj, d.Options)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	t.Fatalf("never got to cast of %d", castObj)
	return -1
}

// TestTargetTypeSpellOffersOnlyStackObjectsAndCounters is the test the suite
// was blind to: it goes THROUGH cast/askTarget with a real-corpus-shaped
// counterspell (TargetType$ Spell + ValidTgts$ Card, no TgtZone$) and asserts
// that (1) a battlefield permanent is NOT offered for the "Select target
// spell" prompt, (2) a spell on the stack IS offered, and (3) the
// counterspell actually counters it end to end -- the targeted spell reaches
// its owner's graveyard and its creature never arrives on the battlefield.
func TestTargetTypeSpellOffersOnlyStackObjectsAndCounters(t *testing.T) {
	t.Parallel()
	e, leakID, bearID, gurmagID := targetSpellFixture(t)
	e.G.Players[0].Pool[state.MU] = 5
	e.G.Players[0].Pool[state.MG] = 5
	e.askPriority(0)

	// Put a creature spell on the stack by casting Bear (it has no targets).
	submitChoices(t, e, passToCast(t, e, bearID))
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the Bear spell", e.G.Stack)
	}
	targetSpell := e.G.Stack[0]

	// Cast Mana Leak in response; its askTarget now fires a KTarget decision.
	submitChoices(t, e, passToCast(t, e, leakID))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}

	// 1. The opponent's battlefield creature is NOT offered for "Select
	// target spell".
	for _, o := range d.Options {
		if o.Obj == gurmagID {
			t.Fatalf("battlefield permanent %d offered for 'Select target spell': %+v", gurmagID, d.Options)
		}
	}
	// 2. The spell on the stack IS offered, and the counterspell itself is
	// NOT: CR 115.5 makes a spell on the stack an illegal target for itself,
	// and askTarget runs after PutOnStack, so the counterspell's own id is
	// sitting in the stack zone next to the Bear it should be able to hit.
	spellIdx := -1
	for _, o := range d.Options {
		if o.Obj == targetSpell {
			spellIdx = o.Index
		}
		if o.Obj == leakID {
			t.Fatalf("counterspell %d offered itself as a target: %+v", leakID, d.Options)
		}
	}
	if spellIdx < 0 {
		t.Fatalf("spell on the stack not offered among %+v", d.Options)
	}

	// 3. Choose the stack spell and let everything resolve: the Bear is
	// countered, reaches its owner's graveyard, and never becomes a permanent,
	// while the battlefield creature is left alone.
	submitChoices(t, e, spellIdx)
	for i := 0; i < 8 && len(e.G.Stack) > 0; i++ {
		castFirst(t, e, "pass")
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack did not empty: %v", e.G.Stack)
	}
	if got := e.G.Obj(targetSpell).Zone; got != state.ZGraveyard {
		t.Fatalf("countered spell went to %s, want graveyard", got)
	}
	if got := len(e.G.Zone(state.ZBattlefield, 0)); got != 0 {
		t.Fatalf("countered Bear spell resolved onto the battlefield: %v", e.G.Zone(state.ZBattlefield, 0))
	}
	if got := e.G.Obj(gurmagID).Zone; got != state.ZBattlefield {
		t.Fatalf("battlefield creature moved to %s", got)
	}
}

// TestCounterspellWithOnlyItselfOnStackFizzles keeps the historical oracle
// name while reaching CR 608.2b legally: Mana Leak targets a spell, then that
// spell leaves the stack in response. Leak must fizzle without a Resolve event.
func TestCounterspellWithOnlyItselfOnStackFizzles(t *testing.T) {
	t.Parallel()
	e, leakID, bearID, _ := targetSpellFixture(t)
	e.G.Players[0].Pool[state.MU] = 5
	e.G.Players[0].Pool[state.MG] = 5
	e.askPriority(0)

	submitChoices(t, e, passToCast(t, e, bearID))
	targetSpell := e.G.Stack[0]
	submitChoices(t, e, passToCast(t, e, leakID))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected target decision, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == targetSpell {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("spell on stack not offered as target: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	// A response removes the chosen spell before Mana Leak resolves, leaving
	// Leak itself as the only stack object and all of its targets illegal.
	e.emit(events.Event{Kind: events.MoveZone, Obj: targetSpell, From: state.ZStack, To: state.ZGraveyard})
	passUntilStackEmpty(t, e, 8)

	if z := e.G.Obj(leakID).Zone; z != state.ZGraveyard {
		t.Fatalf("fizzled counterspell ended in %s, want graveyard", z)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Resolve && ev.Obj == leakID {
			t.Fatalf("counterspell resolved instead of fizzling: %+v", ev)
		}
	}
}

const graveRaisingSrc = "Name:Grave Raising\nManaCost:1 B\nTypes:Sorcery\n" +
	"A:SP$ ChangeZone | TargetType$ Card | TgtZone$ Graveyard | ValidTgts$ Card | " +
	"Origin$ Graveyard | Destination$ Hand | SpellDescription$ Return target card from graveyard to hand.\nOracle:x\n"

// TestTgtZoneGraveyardTargetOfferedAndResolves is the 122-card-shape test the
// legalTargets zone-widening exposed: a TgtZone$ Graveyard spell is offered a
// graveyard card at cast time, that target stays LEGAL at resolution (the old
// hard `o.Zone == ZBattlefield` check rejected it, fizzling every such spell),
// and the effect really happens -- the card returns to its owner's hand. It
// fails on the tree before the legalTargets zoneIn fix.
func TestTgtZoneGraveyardTargetOfferedAndResolves(t *testing.T) {
	t.Parallel()
	raiseSrc := graveRaisingSrc
	wastedSrc := "Name:Wasted\nManaCost:1\nTypes:Creature\nPT:1/1\nOracle:x\n"
	e := handEngine(t, card(t, raiseSrc))

	// A card sitting in seat 0's graveyard, the only legal target.
	w := e.G.AddObject(card(t, wastedSrc), 0)
	w.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{w.ID})

	var raiseID state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(id).Face().Name == "Grave Raising" {
			raiseID = id
		}
	}
	if raiseID == 0 {
		t.Fatalf("grave-reclaim spell not in hand")
	}

	e.G.Players[0].Pool[state.MB] = 2
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, raiseID))

	// 1. Offered at cast time: the graveyard card is a candidate.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	gIdx := -1
	for _, o := range d.Options {
		if o.Obj == w.ID {
			gIdx = o.Index
		}
	}
	if gIdx < 0 {
		t.Fatalf("graveyard card %d not offered for the TgtZone$ Graveyard spell: %+v", w.ID, d.Options)
	}

	// 2. Choose it and let everything resolve.
	submitChoices(t, e, gIdx)
	for i := 0; i < 8 && len(e.G.Stack) > 0; i++ {
		castFirst(t, e, "pass")
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack did not empty: %v", e.G.Stack)
	}
	// 3. The effect happened: the graveyard target reached the hand, not
	// fizzled back where it was.
	if z := e.G.Obj(w.ID).Zone; z != state.ZHand {
		t.Fatalf("graveyard target ended in %s, want hand (spell fizzled at resolution?)", z)
	}
}

// wrennShapeSrc is the brief's inline minimal fixture, shaped like Wrenn and
// Six's [+1] targeting parameters (exact Origin$ Graveyard, Destination$
// Hand, TargetMin$ 0, TargetMax$ 1, ValidTgts$ Land.YouOwn, NO TgtZone$) on
// an ordinary activated ability -- no planeswalker/loyalty machinery needed
// to exercise the shared target census. Nothing is copied from any Forge
// script file.
const wrennShapeSrc = "Name:Wrenn Shape\nManaCost:1 G\nTypes:Creature\nPT:1/1\n" +
	"A:AB$ ChangeZone | Cost$ G | Origin$ Graveyard | Destination$ Hand | TargetMin$ 0 | TargetMax$ 1 | " +
	"ValidTgts$ Land.YouOwn | TgtPrompt$ Select target land card in your graveyard | " +
	"SpellDescription$ Return up to one target land card from your graveyard to your hand.\nOracle:x\n"

// hillSrc is the land both the graveyard and the battlefield sides of the
// fixture use; both are seat 0's, so both satisfy Land.YouOwn and only the
// zone census can tell them apart.
const hillSrc = "Name:Hill\nTypes:Land\nOracle:x\n"

// TestOriginGraveyardAbilityTargetsGraveyardLand is the Wrenn and Six [+1]
// regression: an object-targeted ChangeZone with an exact Origin$ Graveyard
// and no TgtZone$ must offer the eligible graveyard land -- NOT the matching
// owned land already on the battlefield (which effChangeZone's own Origin$
// guard would refuse at resolution anyway) -- and choosing the graveyard
// option must resolve the ability and put that land in hand. It fails on the
// tree before the origin-derived targetZones fallback (the census searched
// the battlefield) and fails again if the fallback is removed.
func TestOriginGraveyardAbilityTargetsGraveyardLand(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, wrennShapeSrc))
	grave := e.G.AddObject(card(t, hillSrc), 0)
	grave.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{grave.ID})
	play := e.G.AddObject(card(t, hillSrc), 0)
	play.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{play.ID})

	var src state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(id).Face().Name == "Wrenn Shape" {
			src = id
		}
	}
	if src == 0 {
		t.Fatalf("fixture ability source not in hand")
	}
	// The source must be a permanent for its activated ability to be
	// offered (same direct SetZone fixture move targetSpellFixture uses).
	var rest []state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if id != src {
			rest = append(rest, id)
		}
	}
	e.G.SetZone(state.ZHand, 0, rest)
	e.G.Obj(src).Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append([]state.ObjID{src}, play.ID))

	e.G.Players[0].Pool[state.MG] = 2
	e.askPriority(0)
	opt, ok := findAbilityOption(e, src, 0)
	if !ok {
		t.Fatalf("ability option not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)

	// The real target-offer path: the ability's KTarget decision.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	graveIdx, playIdx := -1, -1
	for _, o := range d.Options {
		switch o.Obj {
		case grave.ID:
			graveIdx = o.Index
		case play.ID:
			playIdx = o.Index
		}
	}
	if playIdx >= 0 {
		t.Fatalf("battlefield land %d offered for the Origin$ Graveyard ability: %+v", play.ID, d.Options)
	}
	if graveIdx < 0 {
		t.Fatalf("graveyard land %d not offered among %+v", grave.ID, d.Options)
	}
	submitChoices(t, e, graveIdx)

	passUntilStackEmpty(t, e, 8)
	if z := e.G.Obj(grave.ID).Zone; z != state.ZHand {
		t.Fatalf("chosen graveyard land ended in %s, want hand", z)
	}
	if z := e.G.Obj(play.ID).Zone; z != state.ZBattlefield {
		t.Fatalf("battlefield land moved to %s", z)
	}
}

// volcanicVisionShapeSrc mirrors Volcanic Vision's target shape: an
// object-targeted ChangeZone with an exact Origin$ Graveyard, no TgtZone$,
// and a ValidTgts$ whose card-type bases (Instant/Sorcery) are also stack
// object kinds. Inferring the stack from that base bypasses the origin-implied
// graveyard route and makes the fetch inert. Nothing is copied from the
// corpus file.
const volcanicVisionShapeSrc = "Name:Vision Shape\nManaCost:1 B\nTypes:Sorcery\n" +
	"A:SP$ ChangeZone | Origin$ Graveyard | Destination$ Hand | " +
	"ValidTgts$ Instant.YouCtrl,Sorcery.YouCtrl | " +
	"TgtPrompt$ Select target instant or sorcery card in your graveyard | " +
	"SpellDescription$ Return target instant or sorcery card from your graveyard to your hand.\nOracle:x\n"

// TestOriginGraveyardInstantSorceryTargetsGraveyard is the Volcanic Vision
// regression the prior ValidTgts$ inference broke: with Origin$ Graveyard and
// no TgtZone$, an `Instant.YouCtrl,Sorcery.YouCtrl` target must be offered the
// graveyard instant card and must NOT be offered a spell sitting on the stack
// (whose base token also names a stack kind). It fails on a tree that lets the
// ValidTgts$ inference outrank the origin-implied zone, and fails again if the
// origin route is removed entirely.
func TestOriginGraveyardInstantSorceryTargetsGraveyard(t *testing.T) {
	t.Parallel()
	visionSrc := volcanicVisionShapeSrc
	instSrc := "Name:Shock\nManaCost:R\nTypes:Instant\nOracle:x\n"
	bearSrc := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e := handEngine(t, card(t, visionSrc), card(t, bearSrc))

	// The eligible instant card sits in seat 0's graveyard; a creature spell
	// is put on the stack so a stack-searching implementation has a live
	// candidate to (wrongly) offer.
	grave := e.G.AddObject(card(t, instSrc), 0)
	grave.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{grave.ID})

	var visionID, bearID state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		switch e.G.Obj(id).Face().Name {
		case "Vision Shape":
			visionID = id
		case "Bear":
			bearID = id
		}
	}
	if visionID == 0 || bearID == 0 {
		t.Fatalf("fixture hand missing cards: vision=%d bear=%d", visionID, bearID)
	}
	sa := e.G.Obj(visionID).Face().Abilities[0]
	if sa.Params["TgtZone"] != "" || sa.Params["TargetType"] != "" {
		t.Fatalf("fixture no longer has the unqualified target shape: params=%v", sa.Params)
	}
	// Precondition the assertion depends on: the graveyard and stack routes
	// are actually distinct for this spec, and the graveyard card is where
	// the Origin$ names.
	if grave.Zone != state.ZGraveyard {
		t.Fatalf("graveyard precondition lost: card is in %s", grave.Zone)
	}

	e.G.Players[0].Pool[state.MB] = 5
	e.G.Players[0].Pool[state.MG] = 5
	e.askPriority(0)

	// Put the creature spell on the stack first, then cast Vision Shape.
	submitChoices(t, e, passToCast(t, e, bearID))
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Zone != state.ZStack {
		t.Fatalf("stack-spell precondition failed: stack=%v", e.G.Stack)
	}
	bearOnStack := e.G.Stack[0]

	submitChoices(t, e, passToCast(t, e, visionID))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	graveIdx := -1
	for _, o := range d.Options {
		if o.Obj == bearOnStack {
			t.Fatalf("stack spell %d offered for an Origin$ Graveyard fetch: %+v", bearOnStack, d.Options)
		}
		if o.Obj == grave.ID {
			graveIdx = o.Index
		}
	}
	if graveIdx < 0 {
		t.Fatalf("graveyard instant %d not offered among %+v", grave.ID, d.Options)
	}

	submitChoices(t, e, graveIdx)
	passUntilStackEmpty(t, e, 8)
	if z := e.G.Obj(grave.ID).Zone; z != state.ZHand {
		t.Fatalf("chosen graveyard instant ended in %s, want hand (fetch was inert)", z)
	}
}

// exact boundaries at the targetZones unit level: the Wrenn shape derives
// the graveyard, an explicit TgtZone$ stays authoritative over Origin$, and
// every shape the fix deliberately does NOT establish (Any/All, unknown or
// multi-zone origins, a player-targeted spec, a non-ChangeZone API) keeps
// the existing battlefield default -- including the player-targeted
// ChangeZone route this task must not turn into an object-target decision.
func TestTargetZonesChangeZoneOriginTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		sa   string
		want []state.Zone
	}{
		{"wrenn-shaped origin-only graveyard", "A:AB$ ChangeZone | Origin$ Graveyard | Destination$ Hand | TargetMin$ 0 | TargetMax$ 1 | ValidTgts$ Land.YouOwn", []state.Zone{state.ZGraveyard}},
		{"origin graveyard outranks a card-type ValidTgts$ base", "A:SP$ ChangeZone | Origin$ Graveyard | Destination$ Hand | ValidTgts$ Instant.YouCtrl,Sorcery.YouCtrl", []state.Zone{state.ZGraveyard}},
		{"explicit TgtZone$ Graveyard stays authoritative", "A:AB$ ChangeZone | TgtZone$ Graveyard | Origin$ Graveyard | Destination$ Hand | ValidTgts$ Card", []state.Zone{state.ZGraveyard}},
		{"explicit TgtZone$ Battlefield wins over Origin$ Graveyard", "A:AB$ ChangeZone | TgtZone$ Battlefield | Origin$ Graveyard | Destination$ Hand | ValidTgts$ Card", []state.Zone{state.ZBattlefield}},
		{"player-targeted keeps the existing player-target route", "A:AB$ ChangeZone | Origin$ Graveyard | Destination$ Hand | ValidTgts$ Player", []state.Zone{state.ZBattlefield}},
		{"You-targeted likewise", "A:AB$ ChangeZone | Origin$ Graveyard | Destination$ Hand | ValidTgts$ You", []state.Zone{state.ZBattlefield}},
		{"Any-targeted likewise", "A:AB$ ChangeZone | Origin$ Graveyard | Destination$ Hand | ValidTgts$ Any", []state.Zone{state.ZBattlefield}},
		{"origin Any", "A:AB$ ChangeZone | Origin$ Any | Destination$ Hand | ValidTgts$ Card", []state.Zone{state.ZBattlefield}},
		{"origin All", "A:AB$ ChangeZone | Origin$ All | Destination$ Hand | ValidTgts$ Card", []state.Zone{state.ZBattlefield}},
		{"multi-zone origin without inZone alternatives", "A:AB$ ChangeZone | Origin$ Graveyard,Hand | Destination$ Hand | ValidTgts$ Card", []state.Zone{state.ZBattlefield}},
		{"multi-zone alternative lacks zone", "A:SP$ ChangeZone | Origin$ Graveyard,Exile | ValidTgts$ Instant.inZoneGraveyard,Card", []state.Zone{state.ZStack}},
		{"multi-zone filter outside origin", "A:SP$ ChangeZone | Origin$ Graveyard,Exile | ValidTgts$ Instant.inZoneBattlefield", []state.Zone{state.ZStack}},
		{"multi-zone unknown filter zone", "A:SP$ ChangeZone | Origin$ Graveyard,Exile | ValidTgts$ Instant.inZoneUnknown", []state.Zone{state.ZStack}},
		{"unknown explicit TgtZone blocks inference", "A:SP$ ChangeZone | TgtZone$ Unknown | Origin$ Graveyard,Exile | ValidTgts$ Instant.inZoneGraveyard,Card.withFlashback+inZoneExile", []state.Zone{state.ZBattlefield}},
		{"unknown origin token", "A:AB$ ChangeZone | Origin$ Somewhere | Destination$ Hand | ValidTgts$ Card", []state.Zone{state.ZBattlefield}},
		{"non-ChangeZone API", "A:AB$ Destroy | Origin$ Graveyard | ValidTgts$ Card", []state.Zone{state.ZBattlefield}},
	}
	for _, tc := range cases {
		c := card(t, "Name:T\nTypes:Creature\n"+tc.sa+"\nOracle:x\n")
		got := targetZones(c.Faces[0].Abilities[0])
		if len(got) != len(tc.want) || got[0] != tc.want[0] {
			t.Errorf("%s: targetZones = %v, want %v", tc.name, got, tc.want)
		}
	}
}
