package rules

// combatres-cantattack-present: the IsPresent$/IsPresent2$/PresentCompare$/
// PresentZone$/ClassBand$ family on a face CantAttack static. effects'
// CantAttackParamsReadableForRules previously rejected a static carrying those
// keys, so rules/layers.go's attackBlocked skipped it whole and the whole
// "can't attack unless you control an artifact / ..." card shape did nothing
// on the attack half. The whitelist now admits the family (continuousGateHolds
// already evaluated it for every other static family), and
// rules/statics.go's countStaticPresent now scans Exile and Hand for the
// PresentZone$ half.
//
// Every leaf below drives a REAL corpus carrier through e.attackBlocked (the
// pure per-(attacker, defender) read the offer filter and the validator share),
// in BOTH directions, with its precondition asserted (the carrier is on the
// battlefield, the counted objects really are in the zone the rule reads, the
// compared value really straddles the threshold). The block half of one
// CantAttack,CantBlock carrier (Ketramose) is asserted through blockRestricted,
// the block-legality read; before countStaticPresent learned Exile, that half
// was ALWAYS-blocking because the count was stuck at 0.

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// inZoneCard places a synthetic card in a named zone for seat p and stales the
// derived memos exactly as the eventless placements elsewhere do (see onBoard
// in layers_test.go). The object's Controller and Owner are the seat, so both
// .YouCtrl and .YouOwn specs read it.
func inZoneCard(t *testing.T, e *Engine, p state.PlayerID, zone state.Zone, src string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, src), p)
	o.Zone = zone
	e.G.SetZone(zone, p, append(e.G.Zone(zone, p), o.ID))
	e.staticEpoch = -1
	e.activeEpoch = -1
	return o.ID
}

// TestCantAttackPresentArtifactEQ0 drives Desperate Castaways' real corpus
// static
//
//	S:Mode$ CantAttack | ValidCard$ Card.Self | IsPresent$ Artifact.YouCtrl |
//	PresentCompare$ EQ0
//
// ("can't attack unless you control an artifact"). With no artifact the gate
// holds and it is blocked; one artifact of its controller releases it.
func TestCantAttackPresentArtifactEQ0(t *testing.T) {
	t.Parallel()
	e := attackBlockedRegressionEngine(t)
	castaways := onBoardCard(t, e, 0, corpusCard(t, "Desperate Castaways"))
	if o := e.G.Obj(castaways); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Desperate Castaways is not on the battlefield")
	}
	if n := e.countPresent("Artifact.YouCtrl", castaways, 0); n != 0 {
		t.Fatalf("precondition: seat 0 controls %d artifacts, want 0", n)
	}
	if !e.attackBlocked(castaways, 1, 0) {
		t.Fatal("Desperate Castaways attacked although its controller controlled no artifact (IsPresent$ Artifact.YouCtrl EQ0 unread)")
	}
	onBoard(t, e, 0, "Name:Bauble\nTypes:Artifact\nOracle:x\n")
	if n := e.countPresent("Artifact.YouCtrl", castaways, 0); n != 1 {
		t.Fatalf("precondition: seat 0 controls %d artifacts, want 1", n)
	}
	if e.attackBlocked(castaways, 1, 0) {
		t.Fatal("Desperate Castaways stayed blocked although its controller controlled an artifact")
	}
}

// TestCantAttackBarePresentEnchantment drives Wirecat's real corpus static
//
//	S:Mode$ CantAttack,CantBlock | ValidCard$ Card.Self | IsPresent$ Enchantment
//
// ("can't attack or block if an enchantment is on the battlefield"). A BARE
// IsPresent$ with no PresentCompare$ defaults to GE1, so the restriction bites
// only while an enchantment is present -- the opposite direction from the EQ0
// shape above, and the reason the family must be read, not skipped.
func TestCantAttackBarePresentEnchantment(t *testing.T) {
	t.Parallel()
	e := attackBlockedRegressionEngine(t)
	wirecat := onBoardCard(t, e, 0, corpusCard(t, "Wirecat"))
	if o := e.G.Obj(wirecat); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Wirecat is not on the battlefield")
	}
	// Wirecat itself is an Artifact Creature, never an Enchantment, so the
	// count really starts at zero.
	if n := e.countPresent("Enchantment", wirecat, 0); n != 0 {
		t.Fatalf("precondition: %d enchantments on the battlefield, want 0", n)
	}
	if e.attackBlocked(wirecat, 1, 0) {
		t.Fatal("Wirecat was blocked with no enchantment on the battlefield (bare IsPresent$ must not read as always-enforced)")
	}
	onBoard(t, e, 0, "Name:Aura-ish\nTypes:Enchantment\nOracle:x\n")
	if n := e.countPresent("Enchantment", wirecat, 0); n != 1 {
		t.Fatalf("precondition: %d enchantments on the battlefield, want 1", n)
	}
	if !e.attackBlocked(wirecat, 1, 0) {
		t.Fatal("Wirecat attacked although an enchantment was on the battlefield (bare IsPresent$ Enchantment unread)")
	}
}

// TestCantAttackPresentCreatureGT1 drives Shauku, Endbringer's real corpus
// static
//
//	S:Mode$ CantAttack | ValidCard$ Card.Self | IsPresent$ Creature |
//	PresentCompare$ GT1
//
// ("can't attack if another creature is on the battlefield"). Shauku itself is
// a creature, so the count starts at 1 (not > 1) and it may attack; a second
// creature tips it to 2 and blocks it. The precondition asserts both counts.
func TestCantAttackPresentCreatureGT1(t *testing.T) {
	t.Parallel()
	e := attackBlockedRegressionEngine(t)
	shauku := onBoardCard(t, e, 0, corpusCard(t, "Shauku, Endbringer"))
	if o := e.G.Obj(shauku); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Shauku, Endbringer is not on the battlefield")
	}
	if n := e.countPresent("Creature", shauku, 0); n != 1 {
		t.Fatalf("precondition: %d creatures on the battlefield, want 1 (Shauku alone)", n)
	}
	if e.attackBlocked(shauku, 1, 0) {
		t.Fatal("Shauku was blocked while it was the only creature (PresentCompare$ GT1 must need a SECOND creature)")
	}
	onBoard(t, e, 1, "Name:Other\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if n := e.countPresent("Creature", shauku, 0); n != 2 {
		t.Fatalf("precondition: %d creatures on the battlefield, want 2", n)
	}
	if !e.attackBlocked(shauku, 1, 0) {
		t.Fatal("Shauku attacked although another creature was on the battlefield (IsPresent$ Creature GT1 unread)")
	}
}

// TestCantAttackPresentHandLE6 drives Kefnet the Mindful's real corpus static
//
//	S:Mode$ CantAttack,CantBlock | ValidCard$ Card.Self |
//	IsPresent$ Card.YouOwn | PresentZone$ Hand | PresentCompare$ LE6
//
// ("can't attack or block unless you have seven or more cards in hand"). This
// is the PresentZone$ Hand half countStaticPresent previously returned 0 for:
// before the fix the count was stuck at 0, LE6 always held, and Kefnet could
// never attack. Six cards block it; the seventh releases it.
func TestCantAttackPresentHandLE6(t *testing.T) {
	t.Parallel()
	e := attackBlockedRegressionEngine(t)
	kefnet := onBoardCard(t, e, 0, corpusCard(t, "Kefnet the Mindful"))
	if o := e.G.Obj(kefnet); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Kefnet the Mindful is not on the battlefield")
	}
	// The dealt opening hand is not the fixture under test: empty it (and
	// stale the derived memos, since this placement is eventless).
	e.G.SetZone(state.ZHand, 0, nil)
	e.staticEpoch = -1
	e.activeEpoch = -1
	for i := 0; i < 6; i++ {
		inZoneCard(t, e, 0, state.ZHand, "Name:Filler\nTypes:Sorcery\nOracle:x\n")
	}
	if n := len(e.G.Zone(state.ZHand, 0)); n != 6 {
		t.Fatalf("precondition: seat 0's hand holds %d cards, want 6", n)
	}
	if !e.attackBlocked(kefnet, 1, 0) {
		t.Fatal("Kefnet attacked with six cards in hand (PresentZone$ Hand LE6 unread -- the count read 0)")
	}
	inZoneCard(t, e, 0, state.ZHand, "Name:Filler\nTypes:Sorcery\nOracle:x\n")
	if n := len(e.G.Zone(state.ZHand, 0)); n != 7 {
		t.Fatalf("precondition: seat 0's hand holds %d cards, want 7", n)
	}
	if e.attackBlocked(kefnet, 1, 0) {
		t.Fatal("Kefnet stayed blocked with seven cards in hand, where its LE6 gate fails")
	}
}

// TestCantAttackPresentExileLT7 drives Ketramose, the New Dawn's real corpus
// static
//
//	S:Mode$ CantAttack,CantBlock | ValidCard$ Card.Self |
//	IsPresent$ Card | PresentZone$ Exile | PresentCompare$ LT7
//
// ("can't attack or block unless there are seven or more cards in exile").
// This exercises BOTH the attack half (PresentZone$ Exile in countStaticPresent
// previously read 0, so before the fix Ketramose could always attack) and the
// block half (blockRestricted's CantBlock loop has no whitelist, so the same
// stuck-at-0 count already made Ketramose ALWAYS unable to block -- a live
// over-restriction this fix repairs). Six exiled cards block both halves;
// the seventh releases both.
func TestCantAttackPresentExileLT7(t *testing.T) {
	t.Parallel()
	e := attackBlockedRegressionEngine(t)
	ketramose := onBoardCard(t, e, 0, corpusCard(t, "Ketramose, the New Dawn"))
	attacker := onBoard(t, e, 1, "Name:Raider\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n")
	if o := e.G.Obj(ketramose); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Ketramose is not on the battlefield")
	}
	// Ketramose itself is on the battlefield, so the exile scan must read
	// zero here (the battlefield is not exile).
	if n := len(e.G.Zone(state.ZExile, 0)) + len(e.G.Zone(state.ZExile, 1)); n != 0 {
		t.Fatalf("precondition: %d cards in exile, want 0", n)
	}
	for i := 0; i < 6; i++ {
		inZoneCard(t, e, 0, state.ZExile, "Name:Exiled\nTypes:Sorcery\nOracle:x\n")
	}
	if n := len(e.G.Zone(state.ZExile, 0)); n != 6 {
		t.Fatalf("precondition: seat 0's exile holds %d cards, want 6", n)
	}
	if !e.attackBlocked(ketramose, 1, 0) {
		t.Fatal("Ketramose attacked with six cards in exile (PresentZone$ Exile LT7 unread -- the count read 0)")
	}
	if !e.blockRestricted(ketramose, attacker) {
		t.Fatal("Ketramose blocked with six cards in exile (the CantBlock half's Exile count read 0)")
	}
	inZoneCard(t, e, 0, state.ZExile, "Name:Exiled\nTypes:Sorcery\nOracle:x\n")
	if n := len(e.G.Zone(state.ZExile, 0)); n != 7 {
		t.Fatalf("precondition: seat 0's exile holds %d cards, want 7", n)
	}
	if e.attackBlocked(ketramose, 1, 0) {
		t.Fatal("Ketramose stayed blocked at seven cards in exile, where its LT7 gate fails")
	}
	if e.blockRestricted(ketramose, attacker) {
		t.Fatal("Ketramose stayed unable to block at seven cards in exile, where its LT7 gate fails")
	}
}
