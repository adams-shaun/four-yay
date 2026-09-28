package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestDaybreakCoronetCastsOntoAuraBearingCreature is the gameplay-visible
// leaf for the two-token space grammar "EnchantedBy <Type>.<qual>" (the
// twin of TestAttachedToPredicateUnlocksCorpusTargeting's AttachedTo work).
//
// Daybreak Coronet reads:
//
//	K:Enchant:Creature.EnchantedBy Aura.Other:creature with another Aura attached to it
//
// With the whole "EnchantedBy Aura.Other" token failing closed as one
// unknown predicate (a space is not a spec delimiter, so the token survives
// intact), MatchesSpecFrom matched NOTHING: castTargetsAvailable saw zero
// legal targets and withheld the cast (CR 601.2c) on every battlefield
// state -- the Coronet could never be cast. With the two-token grammar in
// place the cast is offered exactly when a creature bears an Aura, the
// Aura-bearing creature is the only legal target, the Coronet resolves onto
// it, and its static (+3/+3, first strike, vigilance, lifelink -- the bare
// EnchantedBy Affected$ line) applies. The post-attach SBA re-check
// (rules/attach.go auraStillMatchesEnchant) routes through the same spec and
// passes too, so the Coronet is not swept (the attached Coronet is the
// source, so its own attachment does not satisfy Other -- the Unholy
// Strength below does).
func TestDaybreakCoronetCastsOntoAuraBearingCreature(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	coronet := mustCorpusCard(t, reg, "Daybreak Coronet") // K:Enchant:Creature.EnchantedBy Aura.Other
	e := handEngine(t, coronet)
	coronetID := e.G.Zone(state.ZHand, 0)[0]
	bear := onBoard(t, e, 0, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	bare := onBoard(t, e, 0, "Name:Elk\nManaCost:1 G\nTypes:Creature Elk\nPT:2/2\nOracle:x\n")
	strength := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Unholy Strength"))
	e.emit(events.Event{Kind: events.Attach, Obj: strength, IDs: []state.ObjID{bear}})
	addMana(t, e, 0, "WW")

	var castOpt decision.Option
	found := false
	for _, o := range castOptions(t, e) {
		if o.Obj == coronetID {
			castOpt, found = o, true
			break
		}
	}
	if !found {
		t.Fatalf("Daybreak Coronet's cast must be offered now that Creature.EnchantedBy Aura.Other has a legal target")
	}
	submitChoices(t, e, castOpt.Index)

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a KTarget decision after announcing the cast, got %+v", d)
	}
	bearIdx, offeredBare, offeredSelf, offeredStrength := -1, false, false, false
	for _, o := range d.Options {
		switch o.Obj {
		case bear:
			bearIdx = o.Index
		case bare:
			offeredBare = true
		case coronetID /* self */ :
			offeredSelf = true
		case strength:
			offeredStrength = true
		}
	}
	if bearIdx < 0 {
		t.Fatalf("the Aura-bearing creature must be offered as the target: %+v", d.Options)
	}
	if offeredBare {
		t.Fatalf("the bare creature must not be offered (no Aura attached): %+v", d.Options)
	}
	if offeredSelf {
		t.Fatalf("Daybreak Coronet itself must not be offered (its ValidTgts$ names Creature): %+v", d.Options)
	}
	if offeredStrength {
		t.Fatalf("the attached Aura must not be offered: %+v", d.Options)
	}
	submitChoices(t, e, bearIdx)
	passUntilStackEmpty(t, e, 20)

	if a := e.G.Obj(coronetID); a == nil || a.AttachedTo != bear || a.Zone != state.ZBattlefield {
		t.Fatalf("the Coronet must be attached to the Aura-bearing creature on the battlefield, got %+v", a)
	}
	// The static applies to the bearer: +3/+3 over Unholy Strength's +2/+1
	// (2/2 -> 7/6) and the three keywords from the Coronet's own Affected$
	// Creature.EnchantedBy line.
	p, tp, _ := e.Characteristics(bear)
	if p != 7 || tp != 6 {
		t.Fatalf("bearer P/T = %d/%d, want 7/6 (+3/+3 Coronet over +2/+1 Unholy Strength)", p, tp)
	}
	for _, kw := range []string{"First Strike", "Vigilance", "Lifelink"} {
		if !e.HasKeyword(bear, kw) {
			t.Fatalf("bearer must have %s from the Coronet's static", kw)
		}
	}
}

// TestDaybreakCoronetNeverOfferedWithoutAura pins the negative half of the
// CR 601.2c gate the grammar unlocks: with no Aura-bearing creature on the
// battlefield the cast is still withheld -- recognising the predicate must
// not have widened the filter to "any creature".
func TestDaybreakCoronetNeverOfferedWithoutAura(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	coronet := mustCorpusCard(t, reg, "Daybreak Coronet")
	e := handEngine(t, coronet)
	coronetID := e.G.Zone(state.ZHand, 0)[0]
	onBoard(t, e, 0, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	addMana(t, e, 0, "WW")
	for _, o := range castOptions(t, e) {
		if o.Obj == coronetID {
			t.Fatalf("a bare battlefield must not offer the Coronet cast (no creature bears an Aura)")
		}
	}
}

// TestEnchantedByAuraYouCtrlKillianTriggerDraws pins the YouCtrl half of the
// two-token grammar on its corpus carrier:
//
//	Killian, Decisive Mentor: T:Mode$ AttackersDeclared |
//	  ValidAttackers$ Creature.EnchantedBy Aura.YouCtrl | Execute$ TrigDraw
//
// Attacking with a creature bearing an Aura YOU control queues the trigger
// and draws a card; an unenchanted attacker and an attacker bearing an
// OPPONENT's Aura never queue it (the qualifier is evaluated against the
// attached object, not the candidate).
func TestEnchantedByAuraYouCtrlKillianTriggerDraws(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	killian := mustCorpusCard(t, reg, "Killian, Decisive Mentor")
	bearSrc := "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

	for _, tc := range []struct {
		name      string
		auraSeat  state.PlayerID
		attach    bool
		wantQueue bool
	}{
		{"your aura on the attacker", 0, true, true},
		{"no aura on the attacker", 0, false, false},
		{"opponent's aura on the attacker", 1, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := handEngine(t)
			onBoardCard(t, e, 0, killian)
			bear := onBoard(t, e, 0, bearSrc)
			if tc.attach {
				strength := onBoardCard(t, e, tc.auraSeat, mustCorpusCard(t, reg, "Unholy Strength"))
				e.emit(events.Event{Kind: events.Attach, Obj: strength, IDs: []state.ObjID{bear}})
			}
			handBefore := len(e.G.Zone(state.ZHand, 0))
			e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{bear}})
			if (len(e.pendingTriggers) > 0) != tc.wantQueue {
				t.Fatalf("pendingTriggers = %d, want queue = %v", len(e.pendingTriggers), tc.wantQueue)
			}
			if !tc.wantQueue {
				return
			}
			e.putTriggersOnStack()
			e.resolveTop()
			if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
				t.Fatalf("hand = %d, want %d (the trigger's draw resolved)", got, handBefore+1)
			}
		})
	}
}

// TestFaceOfDivinityAnotherAuraStatic pins the second Aura.Other carrier's
// static:
//
//	Face of Divinity: Affected$ Creature.EnchantedBy+EnchantedBy Aura.Other
//
// The creature Face itself enchants gains first strike and lifelink only
// while ANOTHER Aura is attached -- Face is the resolving source, so its own
// attachment must not satisfy Other (this is the half of the Other semantics
// a Coronet cast cannot exercise: during the cast the source is not yet
// attached).
func TestFaceOfDivinityAnotherAuraStatic(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	face := mustCorpusCard(t, reg, "Face of Divinity")
	e := handEngine(t)
	bear := onBoard(t, e, 0, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	faceID := onBoardCard(t, e, 0, face)
	e.emit(events.Event{Kind: events.Attach, Obj: faceID, IDs: []state.ObjID{bear}})
	if e.HasKeyword(bear, "First Strike") || e.HasKeyword(bear, "Lifelink") {
		t.Fatalf("with only Face itself attached, the another-Aura static must not grant first strike/lifelink")
	}
	strength := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Unholy Strength"))
	e.emit(events.Event{Kind: events.Attach, Obj: strength, IDs: []state.ObjID{bear}})
	if !e.HasKeyword(bear, "First Strike") || !e.HasKeyword(bear, "Lifelink") {
		t.Fatalf("with another Aura attached, Face's static must grant first strike and lifelink")
	}
	if p, tp, _ := e.Characteristics(bear); p != 6 || tp != 5 {
		t.Fatalf("bearer P/T = %d/%d, want 6/5 (Face's +2/+2 over Unholy Strength's +2/+1)", p, tp)
	}
}
