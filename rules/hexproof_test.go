package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Task kw-hexproof1: K:Hexproof arrived in the derived keyword set but NO
// rule read it, so the CR 702.11 targeting gate was absent — a hexproof
// permanent (Lotus Field, Tectonic Split, Witchstalker) was offered to every
// targeting spell or ability, its opponents' included. The fix wires
// e.hexproofBlocksTarget into the two targeting gates, candidatesFor (the
// offer census every cast/ability/trigger ask routes through) and
// legalTargets (the CR 608.2b resolution recheck), behind the same
// battlefield-zone gate protection, shroud and CantTarget use. Unlike shroud
// (CR 702.14, symmetric), hexproof is ASYMMETRIC (CR 702.11b): its
// controller may still target it, and a quality-bearing K:Hexproof:<Spec>
// form (CR 702.11c) withholds only against a source carrying that quality.

// darkBlastScript is a black instant that targets only creatures — the
// quality test's black source, paired with the existing shockScript (red).
const darkBlastScript = "Name:Dark Blast\nManaCost:B\nTypes:Instant\n" +
	"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 2\nOracle:x\n"

// addHexproofCardToHand puts an arbitrary compiled card into seat p's hand
// eventlessly (the handEngine decks are Mountains; the fixture card exists
// nowhere to find), recording it in the hand zone so the cast walk offers it.
// Mirrors shroud_test.go's addShockToHand for the two-spell quality test.
func addHexproofCardToHand(t *testing.T, e *Engine, p state.PlayerID, src string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, src), p)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, p, append(e.G.Zone(state.ZHand, p), o.ID))
	return o.ID
}

// TestPrintedHexproofWithholdsOpponentButNotController is the asymmetry leaf
// (CR 702.11b): a printed K:Hexproof creature is not a legal target of an
// OPPONENT's spell or ability, so an opposing Shock with no other legal
// target is not even offered (the CR 601.2c cast-withhold arm), while its
// controller's own Shock is offered normally and the hexproof permanent IS a
// legal target — the one way hexproof differs from shroud, whose own-
// controller case is pinned the other way in shroud_test.go.
func TestPrintedHexproofWithholdsOpponentButNotController(t *testing.T) {
	hexed := "Name:Hexed Elf\nManaCost:G\nTypes:Creature Elf\nK:Hexproof\nPT:2/2\nOracle:x\n"
	e := handEngine(t)
	hexID := onBoard(t, e, 1, hexed)
	if !e.hexproofBlocksTarget(hexID, 0, 0) {
		t.Fatalf("printed K:Hexproof did not reach the derived keyword set: %v", e.Keywords(hexID))
	}
	if e.hexproofBlocksTarget(hexID, 1, 0) {
		t.Fatalf("hexproof withheld its own controller's targeting (CR 702.11b asymmetry broken)")
	}

	// The opponent (seat 0): Shock is offered for no legal target — withheld.
	sh0 := addShockToHand(t, e, 0)
	addMana(t, e, 0, "R")
	e.askPriority(0)
	if d := e.Pending(); castOffered(e, sh0) {
		t.Fatalf("opponent's Shock offered although its only creature target has hexproof: %+v", d.Options)
	}

	// The controller (seat 1): the cast is offered and the hexproof creature
	// is a legal target of its own spell.
	sh1 := addShockToHand(t, e, 1)
	addMana(t, e, 1, "R")
	e.askPriority(1)
	if d := e.Pending(); !castOffered(e, sh1) {
		t.Fatalf("controller's own Shock not offered against its own hexproof creature: %+v", d.Options)
	}
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Shock target decision, got %+v", d)
	}
	found := false
	for _, o := range d.Options {
		if o.Obj == hexID {
			found = true
		}
	}
	if !found {
		t.Fatalf("controller's own hexproof creature not offered as its Shock target: %+v", d.Options)
	}
}

// TestPrintedHexproofExcludedFromTargetOptions pins the offer-level bite with
// a second, non-hexproof creature on the board: the opponent's Shock is
// offered (a legal target exists) but the target decision offers the plain
// Elf and never the hexproof one — the same offer/recheck agreement shroud's
// TestPrintedShroudExcludedFromTargetOptions pins, on the asymmetric keyword.
func TestPrintedHexproofExcludedFromTargetOptions(t *testing.T) {
	hexed := "Name:Hexed Elf\nManaCost:G\nTypes:Creature Elf\nK:Hexproof\nPT:2/2\nOracle:x\n"
	e := handEngine(t)
	hexID := onBoard(t, e, 1, hexed)
	plainID := onBoard(t, e, 1, "Name:Elf\nManaCost:G\nTypes:Creature Elf\nPT:2/4\nOracle:x\n")

	sh0 := addShockToHand(t, e, 0)
	addMana(t, e, 0, "R")
	e.askPriority(0)
	if d := e.Pending(); !castOffered(e, sh0) {
		t.Fatalf("Shock not offered with a legal (non-hexproof) target present: %+v", d.Options)
	}
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Shock target decision, got %+v", d)
	}
	foundPlain := false
	for _, o := range d.Options {
		if o.Obj == hexID {
			t.Errorf("hexproof creature offered as Shock target to the opponent")
		}
		if o.Obj == plainID {
			foundPlain = true
		}
	}
	if !foundPlain {
		t.Fatalf("plain creature missing from the target options: %+v", d.Options)
	}
}

// TestHexproofFromBlackIsQualityAware pins CR 702.11c: a K:Hexproof:Black
// permanent (Knight of Grace) withholds only against a BLACK source. The
// opponent's black Dark Blast is withheld (no legal target), while the same
// opponent's red Shock is offered and offers the Knight as a target — the
// quality is matched against the TARGETING SOURCE, not the hexproof card.
// Two independent engines keep the black and red pools from sharing state.
func TestHexproofFromBlackIsQualityAware(t *testing.T) {
	black := handEngine(t)
	grace := corpusCard(t, "Knight of Grace")
	onBoardCard(t, black, 1, grace)
	blast := addHexproofCardToHand(t, black, 0, darkBlastScript)
	addMana(t, black, 0, "B")
	black.askPriority(0)
	if d := black.Pending(); castOffered(black, blast) {
		t.Fatalf("black Dark Blast offered against hexproof-from-black: %+v", d.Options)
	}

	red := handEngine(t)
	graceID := onBoardCard(t, red, 1, corpusCard(t, "Knight of Grace"))
	sh := addShockToHand(t, red, 0)
	addMana(t, red, 0, "R")
	red.askPriority(0)
	if d := red.Pending(); !castOffered(red, sh) {
		t.Fatalf("red Shock not offered against hexproof-from-black: %+v", d.Options)
	}
	castFirst(t, red, "cast")
	d := red.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Shock target decision, got %+v", d)
	}
	found := false
	for _, o := range d.Options {
		if o.Obj == graceID {
			found = true
		}
	}
	if !found {
		t.Fatalf("hexproof-from-black creature not offered to a red source: %+v", d.Options)
	}
}

// TestCorpusLotusFieldHexproofWithholdsOpponent pins the real deck card: the
// corpus-compiled Lotus Field (a plain K:Hexproof land in the pro-shaper
// deck) is not a legal target of an opponent's Shock, while its controller
// may target it. This is the card-level ratchet proof the coverage table
// requires before kw:Hexproof may count as supported.
func TestCorpusLotusFieldHexproofWithholdsOpponent(t *testing.T) {
	e := handEngine(t)
	lotus := corpusCard(t, "Lotus Field")
	lotusID := onBoardCard(t, e, 1, lotus)
	if !e.hexproofBlocksTarget(lotusID, 0, 0) {
		t.Fatalf("corpus Lotus Field's K:Hexproof did not block an opponent: %v", e.Keywords(lotusID))
	}
	if e.hexproofBlocksTarget(lotusID, 1, 0) {
		t.Fatalf("corpus Lotus Field blocked its own controller")
	}
}

// TestGrantedHexproofBlocksOpponentNotController pins the GRANTED form end to
// end on a real corpus static: Shalai, Voice of Plenty's
// "Affected$ You,Planeswalker.YouCtrl,Creature.YouCtrl+Other | AddKeyword$
// Hexproof" reaches a second creature through the layer-6 AddKeywords path
// exactly as Lightning Greaves' granted Shroud does (shroud_test.go), so the
// opponent's Shock is offered (Shalai itself is a legal target) but never
// offers the granted-hexproof bear, while the controller's own Shock does —
// the CR 702.11b asymmetry applied to the granted form. Before this test only
// the printed and quality forms were pinned; the report's "the granted form
// needs the keyword to exist before AddKeyword$ can convey it" is covered
// here.
func TestGrantedHexproofBlocksOpponentNotController(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Shalai, Voice of Plenty", "Grizzly Bears"}, []string{"Hill Giant"})
	bear := findOnBoard(t, e, 0, "Grizzly Bears")
	shalai := findOnBoard(t, e, 0, "Shalai, Voice of Plenty")

	if !e.HasKeyword(bear, "Hexproof") {
		t.Fatalf("Shalai's AddKeyword$ Hexproof did not reach the second creature (derived %v)", e.Keywords(bear))
	}
	if !e.hexproofBlocksTarget(bear, 1, 0) {
		t.Fatalf("granted Hexproof not visible to the targeting gate helper (derived %v)", e.Keywords(bear))
	}
	if e.hexproofBlocksTarget(bear, 0, 0) {
		t.Fatalf("granted Hexproof withheld its own controller's targeting (CR 702.11b asymmetry broken)")
	}

	// The opponent (seat 1): Shock is offered (Shalai is a legal target) but
	// the granted-hexproof bear is never among the options.
	sh1 := addShockToHand(t, e, 1)
	addMana(t, e, 1, "R")
	e.askPriority(1)
	if d := e.Pending(); !castOffered(e, sh1) {
		t.Fatalf("opponent's Shock not offered although Shalai is a legal target: %+v", d.Options)
	}
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Shock target decision, got %+v", d)
	}
	foundShalai := false
	for _, o := range d.Options {
		if o.Obj == bear {
			t.Errorf("granted-hexproof creature offered as the opponent's Shock target")
		}
		if o.Obj == shalai {
			foundShalai = true
		}
	}
	if !foundShalai {
		t.Fatalf("Shalai missing from the opponent's target options: %+v", d.Options)
	}
	// The opponent's Shock was only a probe of its target options: withdraw
	// the proposal (the CR 733.1 reversal returns it to hand) before seat 0
	// receives priority, rather than leaving a cast in flight under it.
	e.abortCast(e.cast, "test: probe cast withdrawn", false)
	e.pending = nil

	// The controller (seat 0): its own Shock is offered and the
	// granted-hexproof bear IS a legal target — the asymmetry.
	sh0 := addShockToHand(t, e, 0)
	addMana(t, e, 0, "R")
	e.askPriority(0)
	if d := e.Pending(); !castOffered(e, sh0) {
		t.Fatalf("controller's own Shock not offered: %+v", d.Options)
	}
	castFirst(t, e, "cast")
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected controller's Shock target decision, got %+v", d)
	}
	found := false
	for _, o := range d.Options {
		if o.Obj == bear {
			found = true
		}
	}
	if !found {
		t.Fatalf("controller's own Shock does not offer its granted-hexproof creature: %+v", d.Options)
	}
}

// TestHexproofReparseFromCorpusScript keeps the corpus-shape coupling honest:
// the two deck cards must still carry a K:Hexproof line. A future corpus pin
// that dropped it would silently make the tests above vacuous.
func TestHexproofReparseFromCorpusScript(t *testing.T) {
	for _, name := range []string{"Lotus Field", "Tectonic Split"} {
		c := corpusCard(t, name)
		found := false
		for _, kv := range c.Faces[0].Keywords {
			if cards.KeywordHead(kv) == "Hexproof" {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s no longer carries a K:Hexproof keyword: %v", name, c.Faces[0].Keywords)
		}
	}
}
