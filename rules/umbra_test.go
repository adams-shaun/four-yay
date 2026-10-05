package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Umbra armor (CR 702.90), pinned end to end on real corpus cards: a bearer
// wearing an umbra Aura survives its destruction (all damage removed, the
// Aura destroyed instead, bearer neither tapped nor removed from combat), at
// every destruction choke point (Destroy, DestroyAll -- including with
// NoRegen$ True -- and the lethal-damage SBA), the save is one-shot, and
// Umbra Mystic's dotted `Affected$ Aura.AttachedTo Permanent.YouCtrl` grant
// lands and is honoured. The fixture style is rules/bestow_test.go's (the
// real-corpus-card + replayCheck shape).

const umbraBearerSrc = "Name:Umbra Bearer\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
const umbraOppBearerSrc = "Name:Umbra Opp Bearer\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// attachCorpusAura places the corpus Aura card on seat p's battlefield
// attached to bearer: a staged entry that names its bearer, so the engine
// attaches it with a real logged events.Attach as it enters (never a bare
// field write, so the layer statics see it and a replay reproduces it).
func attachCorpusAura(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card, bearer state.ObjID) state.ObjID {
	t.Helper()
	return stageAuraCard(t, e, p, c, bearer)
}

// castDestroyAt finds spellID's cast option in the pending priority, casts
// it, answers a target ask with target if one appears (a Destroy; a
// targetless DestroyAll goes straight through), and drains the stack.
func castDestroyAt(t *testing.T, e *Engine, spellID, target state.ObjID) {
	t.Helper()
	idx := -1
	for _, o := range castOptions(t, e) {
		if o.Obj == spellID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for spell %d in %+v", spellID, castOptions(t, e))
	}
	submitChoices(t, e, idx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		oid := indexOfObjOption(d, target)
		if oid < 0 {
			t.Fatalf("target ask does not offer %d: %+v", target, d.Options)
		}
		submitChoices(t, e, oid)
	}
	passUntilStackEmpty(t, e, 20)
}

// TestBearUmbraSavesBearerFromDestroySpell is the filing card: the bearer
// wearing Bear Umbra survives a real Murder (the effDestroy choke point),
// undamaged and untapped, while the Aura goes to its owner's graveyard.
func TestBearUmbraSavesBearerFromDestroySpell(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	umbra := mustCorpusCard(t, reg, "Bear Umbra")
	murder := mustCorpusCard(t, reg, "Murder")
	e, cfg := tokenReplGame(t, 7, umbra, murder)
	murderID := moveSeededCard(t, e, 0, murder, state.ZHand)
	bearer := putToken(t, e, 0, umbraBearerSrc, state.ZBattlefield)
	umbraID := attachCorpusAura(t, e, 0, umbra, bearer)
	addMana(t, e, 0, "BBB") // Murder is {1}{B}{B}

	castDestroyAt(t, e, murderID, bearer)

	b := e.G.Obj(bearer)
	if b.Zone != state.ZBattlefield || b.Damage != 0 || b.Tapped {
		t.Fatalf("bearer zone %s damage %d tapped %v, want battlefield/0/false", b.Zone, b.Damage, b.Tapped)
	}
	if z := e.G.Obj(umbraID).Zone; z != state.ZGraveyard {
		t.Fatalf("Bear Umbra zone %s, want graveyard (destroyed instead)", z)
	}
	if e.HasKeyword(bearer, "Umbra armor") {
		t.Fatal("bearer must not carry umbra armor itself — the keyword lives on the Aura")
	}
	replayCheck(t, e, cfg)
}

// TestUmbraArmorSaveIsOneShot: after the save the Aura is gone, so the NEXT
// destruction is ordinary. This also pins the lethal-damage SBA choke point
// (rules/sba.go's destroyLethalDamage) end to end.
func TestUmbraArmorSaveIsOneShot(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	umbra := mustCorpusCard(t, reg, "Snake Umbra") // {1}{G}, +1/+1 — bearer becomes 3/3
	murder := mustCorpusCard(t, reg, "Murder")
	e, _ := tokenReplGame(t, 11, umbra, murder)
	murderID := moveSeededCard(t, e, 0, murder, state.ZHand)
	bearer := putToken(t, e, 0, umbraBearerSrc, state.ZBattlefield)
	attachCorpusAura(t, e, 0, umbra, bearer)
	addMana(t, e, 0, "BBB") // Murder is {1}{B}{B}

	castDestroyAt(t, e, murderID, bearer)
	if e.G.Obj(bearer).Zone != state.ZBattlefield {
		t.Fatal("first destruction must be replaced (bearer survives)")
	}

	// Second destruction: lethal damage through the SBA sweep. No umbra
	// Aura remains, no shield — the bearer dies ordinarily.
	e.emit(events.Event{Kind: events.Damage, Obj: bearer, Amount: 2})
	e.checkStateBased()
	if z := e.G.Obj(bearer).Zone; z == state.ZBattlefield {
		t.Fatalf("bearer zone %s after second destruction, want gone (the save must be one-shot)", z)
	}
}

// TestUmbraArmorSavesBearerFromLethalDamage pins the rules/sba.go choke
// point on its own, without any Destroy spell: the first lethal hit is
// replaced (damage removed, Aura destroyed), a second one kills.
func TestUmbraArmorSavesBearerFromLethalDamage(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	umbra := mustCorpusCard(t, reg, "Bear Umbra")
	e, _ := tokenReplGame(t, 13, umbra)
	bearer := putToken(t, e, 0, umbraBearerSrc, state.ZBattlefield)
	umbraID := attachCorpusAura(t, e, 0, umbra, bearer)

	// The bearer is a 4/4 under Bear Umbra's +2/+2 static; four damage is
	// exactly lethal.
	e.emit(events.Event{Kind: events.Damage, Obj: bearer, Amount: 4})
	e.checkStateBased()
	b := e.G.Obj(bearer)
	if b.Zone != state.ZBattlefield || b.Damage != 0 {
		t.Fatalf("bearer zone %s damage %d, want battlefield/0 after the umbra save", b.Zone, b.Damage)
	}
	if z := e.G.Obj(umbraID).Zone; z != state.ZGraveyard {
		t.Fatalf("Bear Umbra zone %s, want graveyard", z)
	}

	e.emit(events.Event{Kind: events.Damage, Obj: bearer, Amount: 2})
	e.checkStateBased()
	if z := e.G.Obj(bearer).Zone; z == state.ZBattlefield {
		t.Fatalf("bearer zone %s after the second lethal hit, want gone (the Aura was already spent)", z)
	}
}

// TestUmbraArmorSavesBearerFromDestroyAll pins the effDestroyAll choke
// point, through the real Wrath of God — whose NoRegen$ True must NOT
// suppress umbra armor (it is not regeneration).
func TestUmbraArmorSavesBearerFromDestroyAll(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	umbra := mustCorpusCard(t, reg, "Bear Umbra")
	wrath := mustCorpusCard(t, reg, "Wrath of God")
	e, cfg := tokenReplGame(t, 17, umbra, wrath)
	wrathID := moveSeededCard(t, e, 0, wrath, state.ZHand)
	bearer := putToken(t, e, 0, umbraBearerSrc, state.ZBattlefield)
	umbraID := attachCorpusAura(t, e, 0, umbra, bearer)
	// A second creature with no umbra Aura dies normally.
	plain := putToken(t, e, 0, umbraBearerSrc, state.ZBattlefield)
	addMana(t, e, 0, "WWWW")

	castDestroyAt(t, e, wrathID, bearer)

	if z := e.G.Obj(bearer).Zone; z != state.ZBattlefield {
		t.Fatalf("umbra bearer zone %s, want battlefield (NoRegen$ True must not block umbra armor)", z)
	}
	if b := e.G.Obj(bearer); b.Damage != 0 {
		t.Fatalf("saved bearer damage %d, want 0", b.Damage)
	}
	if z := e.G.Obj(umbraID).Zone; z != state.ZGraveyard {
		t.Fatalf("Bear Umbra zone %s, want graveyard", z)
	}
	if z := e.G.Obj(plain).Zone; z == state.ZBattlefield {
		t.Fatalf("non-umbra creature zone %s, want gone (destroyed; a token ceases in the graveyard, CR 111.7)", z)
	}
	replayCheck(t, e, cfg)
}

// TestUmbraArmorDoesNotApplyToPlainAuraBearer is the control: a bearer
// wearing an unrelated Aura (Unholy Strength) is destroyed normally.
func TestUmbraArmorDoesNotApplyToPlainAuraBearer(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	strength := mustCorpusCard(t, reg, "Unholy Strength")
	murder := mustCorpusCard(t, reg, "Murder")
	e, cfg := tokenReplGame(t, 19, strength, murder)
	murderID := moveSeededCard(t, e, 0, murder, state.ZHand)
	bearer := putToken(t, e, 0, umbraBearerSrc, state.ZBattlefield)
	strengthID := attachCorpusAura(t, e, 0, strength, bearer)
	addMana(t, e, 0, "BBB") // Murder is {1}{B}{B}

	castDestroyAt(t, e, murderID, bearer)

	if z := e.G.Obj(bearer).Zone; z == state.ZBattlefield {
		t.Fatalf("bearer zone %s, want gone (no umbra armor to save it; the token ceases in the graveyard, CR 111.7)", z)
	}
	if z := e.G.Obj(strengthID).Zone; z != state.ZGraveyard {
		t.Fatalf("Unholy Strength zone %s, want graveyard", z)
	}
	replayCheck(t, e, cfg)
}

// TestUmbraMysticGrantsUmbraArmorToYourAuras pins Part 2: the Mystic's
// dotted `Affected$ Aura.AttachedTo Permanent.YouCtrl` static grants umbra
// armor to the Auras on its controller's permanents (and the grant is
// honoured by the replacement), never to an Aura on an opponent's permanent.
func TestUmbraMysticGrantsUmbraArmorToYourAuras(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	mystic := mustCorpusCard(t, reg, "Umbra Mystic")
	strength := mustCorpusCard(t, reg, "Unholy Strength")
	murder := mustCorpusCard(t, reg, "Murder")
	e, cfg := tokenReplGameSeats(t, 23, []*cards.Card{mystic, strength, murder}, []*cards.Card{strength})
	mysticID := moveSeededCard(t, e, 0, mystic, state.ZBattlefield)
	_ = mysticID
	murderID := moveSeededCard(t, e, 0, murder, state.ZHand)
	bearer := putToken(t, e, 0, umbraBearerSrc, state.ZBattlefield)
	yoursID := attachCorpusAura(t, e, 0, strength, bearer)
	// The opponent's creature with its own Aura: the grant must NOT reach it.
	opp := putToken(t, e, 1, umbraOppBearerSrc, state.ZBattlefield)
	oppStrengthID := attachCorpusAura(t, e, 1, strength, opp)

	if !e.HasKeyword(yoursID, "Umbra armor") {
		t.Fatalf("Umbra Mystic's grant did not land on your attached Aura (derived: %v)", e.Derived(yoursID).Keywords)
	}
	if e.HasKeyword(oppStrengthID, "Umbra armor") {
		t.Fatal("the grant must not reach an Aura attached to an opponent's permanent")
	}
	addMana(t, e, 0, "BBB") // Murder is {1}{B}{B}
	castDestroyAt(t, e, murderID, bearer)

	if z := e.G.Obj(bearer).Zone; z != state.ZBattlefield {
		t.Fatalf("bearer zone %s, want battlefield (the granted umbra armor saved it)", z)
	}
	if z := e.G.Obj(yoursID).Zone; z != state.ZGraveyard {
		t.Fatalf("granted Aura zone %s, want graveyard (destroyed instead)", z)
	}
	replayCheck(t, e, cfg)
}

// TestAttachedToDottedYouCtrlGrammar pins the Part 2 filter grammar
// directly: the dotted two-token "AttachedTo <class>.YouCtrl" matches
// exactly when the attached object is controlled by the spec's you. A
// dotted qualifier that is itself a recognised predicate is now consumed by
// the grammar (the With Great Power ticket widened the old YouCtrl-only
// allowlist to every qualifier recognisedPredicate knows), so this test also
// pins that widening and the shapes that still fail closed: a nested dotted
// qualifier and a player-side class.
func TestAttachedToDottedYouCtrlGrammar(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	strength := mustCorpusCard(t, reg, "Unholy Strength")
	e, _ := tokenReplGame(t, 29, strength)
	bearer := putToken(t, e, 0, umbraBearerSrc, state.ZBattlefield)
	strengthID := attachCorpusAura(t, e, 0, strength, bearer)
	if e.G.Obj(strengthID).AttachedTo != bearer {
		t.Fatal("attach did not land")
	}

	if !effects.MatchesSpecFrom(e.G, "Aura.AttachedTo Permanent.YouCtrl", strengthID, 0, 0) {
		t.Fatal("dotted AttachedTo Permanent.YouCtrl must match an Aura on your permanent")
	}
	if effects.MatchesSpecFrom(e.G, "Aura.AttachedTo Permanent.YouCtrl", strengthID, 1, 0) {
		t.Fatal("dotted AttachedTo Permanent.YouCtrl must not match when the attached permanent is the opponent's")
	}
	// The qualifier set is now every recognised predicate, so the
	// attached-bearer qualifiers that the With Great Power / Baki's Curse
	// filters use are recognised rather than failing closed.
	if got := effects.UnknownPredicates("Aura.AttachedTo Permanent.EnchantedBy"); len(got) != 0 {
		t.Fatalf("dotted AttachedTo Permanent.EnchantedBy must be recognised after the bearer-qualifier widening: %v", got)
	}
	// A nested dotted qualifier (a second '.') still fails closed: the
	// qualifier must be a SINGLE predicate token.
	if got := effects.UnknownPredicates("Aura.AttachedTo Permanent.YouCtrl.Foo"); len(got) == 0 {
		t.Fatalf("a nested dotted qualifier must stay unknown: %v", got)
	}
	if got := effects.UnknownPredicates("Aura.AttachedTo Player.YouCtrl"); len(got) == 0 {
		t.Fatalf("AttachedTo Player.* (a curse's player attachment) must stay unknown: %v", got)
	}
}
