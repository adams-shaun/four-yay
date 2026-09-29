package rules

// stat:Continuous RemoveType$ (CR 613.1d / CR 205.1b, the "isn't a <type>"
// family): a Continuous static's RemoveType$ value is removed at layer 4
// from every object the static's Affected$ matches, while the static's
// condition holds. The corpus's canonical carriers are the Theros god cycle
// (Purphoros: "As long as your devotion to red is less than five,
// Purphoros isn't a creature.") and Melting ("All lands are no longer
// snow."). Before this read landed, layers.go's type-static emission only
// gated on the AddType family, so every RemoveType$-only static was dead
// and every god stayed a creature at any devotion.
//
// Pinned by the Oracle audit scenario
// testdata/oracle/conditional-static/purphoros-god-of-the-forge.json
// (low-devotion-not-a-creature), retired from oracleKnownDivergent when the
// read landed; the paramcensus rows (Mogis/Purphoros
// param:stat:Continuous.RemoveType) retired with it.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// purphorosShape asserts the audited corpus shape and returns the SVar body.
// The assertions ride on the exact static spelling, so a corpus pin change
// that hollows the pin out fails here rather than passing vacuously.
func purphorosShape(t *testing.T, purphoros *cards.Card) string {
	t.Helper()
	f := purphoros.Faces[0]
	var stat *cards.Static
	for i := range f.Statics {
		s := &f.Statics[i]
		if s.Mode == "Continuous" && s.Params["Affected"] == "Card.Self" && s.Params["RemoveType"] != "" {
			stat = s
		}
	}
	if stat == nil {
		t.Fatalf("corpus Purphoros carries no Continuous Card.Self RemoveType$ static: %+v", f.Statics)
	}
	if got := stat.Params["RemoveType"]; got != "Creature" {
		t.Fatalf("Purphoros static RemoveType = %q, want Creature (params %v)", got, stat.Params)
	}
	if got := stat.Params["CheckSVar"]; got != "X" {
		t.Fatalf("Purphoros static CheckSVar = %q, want X (params %v)", got, stat.Params)
	}
	if got := stat.Params["SVarCompare"]; got != "LT5" {
		t.Fatalf("Purphoros static SVarCompare = %q, want LT5 (params %v)", got, stat.Params)
	}
	body := f.SVars["X"]
	if body != "Count$Devotion.Red" {
		t.Fatalf("corpus Purphoros SVar:X = %q, want Count$Devotion.Red", body)
	}
	return body
}

func TestPurphorosRemoveTypeDevotionGate(t *testing.T) {
	purph := corpusCard(t, "Purphoros, God of the Forge")
	body := purphorosShape(t, purph)

	e := layerEngine(t)
	purphID := onBoardCard(t, e, 0, purph)

	// Precondition: the god is on seat 0's battlefield and its devotion SVar
	// is the object's own.
	if got := e.G.Obj(purphID).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition broken: Purphoros zone = %v, want ZBattlefield", got)
	}
	if got := e.G.Obj(purphID).Face().SVars["X"]; got != body {
		t.Fatalf("precondition broken: battlefield Purphoros SVar:X = %q, want %q", got, body)
	}
	ctx := &effects.Ctx{Controller: 0, Source: purphID}
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 1 {
		t.Fatalf("precondition broken: devotion to red with Purphoros alone = (%d, %v), want (1, true)", n, ok)
	}

	// Low devotion: the LT5 gate holds, so the RemoveType$ Creature grant
	// applies -- Purphoros is an enchantment and NOT a creature. (Without
	// the read this is exactly where the Oracle audit diverged: the printed
	// "Creature" word survived every derivation.)
	ty := e.Derived(purphID).Types
	if hasTypeWord(ty, "Creature") {
		t.Fatalf("devotion-1 Purphoros types include Creature, want none (types %v)", ty)
	}
	if !hasTypeWord(ty, "Enchantment") {
		t.Fatalf("devotion-1 Purphoros lost its printed Enchantment (types %v)", ty)
	}
	if !hasTypeWord(ty, "Legendary") {
		t.Fatalf("devotion-1 Purphoros lost its printed Legendary supertype (types %v)", ty)
	}

	// Precondition for the other side: four one-red-pip creatures bring the
	// devotion to exactly five, so the gate no longer holds and the two
	// boards genuinely differ.
	for i := 0; i < 4; i++ {
		pip := onBoardCard(t, e, 0, card(t, "Name:Red Pip Goblin\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n"))
		if got := e.G.Obj(pip).Zone; got != state.ZBattlefield {
			t.Fatalf("precondition broken: red-pip fixture zone = %v, want ZBattlefield", got)
		}
	}
	n, ok := effects.EvalCountOK(e, ctx, body)
	if !ok || n != 5 {
		t.Fatalf("precondition broken: devotion to red with four pips = (%d, %v), want (5, true)", n, ok)
	}

	// Devotion five: the condition is false, the grant is withheld whole,
	// and Purphoros is a 6/5 creature again.
	ty = e.Derived(purphID).Types
	if !hasTypeWord(ty, "Creature") {
		t.Fatalf("devotion-5 Purphoros types lack Creature (types %v)", ty)
	}
	if got := e.Power(purphID); got != 6 || e.Toughness(purphID) != 5 {
		t.Fatalf("devotion-5 Purphoros P/T = %d/%d, want 6/5", e.Power(purphID), e.Toughness(purphID))
	}
}

// TestMeltingRemovesSnowFromLands pins the second corpus value shape: a
// RemoveType$ static whose Affected$ is a filter over OTHER objects (Melting,
// "All lands are no longer snow.") and whose removed word is a supertype.
func TestMeltingRemovesSnowFromLands(t *testing.T) {
	melting := corpusCard(t, "Melting")
	var stat *cards.Static
	for i := range melting.Faces[0].Statics {
		s := &melting.Faces[0].Statics[i]
		if s.Mode == "Continuous" && s.Params["Affected"] == "Land" && s.Params["RemoveType"] != "" {
			stat = s
		}
	}
	if stat == nil {
		t.Fatalf("corpus Melting carries no Continuous Affected$ Land RemoveType$ static: %+v", melting.Faces[0].Statics)
	}
	if got := stat.Params["RemoveType"]; got != "Snow" {
		t.Fatalf("Melting static RemoveType = %q, want Snow (params %v)", got, stat.Params)
	}

	e := layerEngine(t)
	snowID := onBoardCard(t, e, 0, corpusCard(t, "Mouth of Ronom"))

	// Precondition: the snow land really is Snow before the static is live.
	ty := e.Derived(snowID).Types
	if !hasTypeWord(ty, "Snow") {
		t.Fatalf("precondition broken: Mouth of Ronom types %v, want Snow present", ty)
	}

	meltingID := onBoardCard(t, e, 0, melting)
	if got := e.G.Obj(meltingID).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition broken: Melting zone = %v, want ZBattlefield", got)
	}

	// The land's Snow word is gone; its land type survives.
	ty = e.Derived(snowID).Types
	if hasTypeWord(ty, "Snow") {
		t.Fatalf("Melting in play: land types %v still include Snow", ty)
	}
	if !hasTypeWord(ty, "Land") {
		t.Fatalf("Melting in play: land lost its printed Land type (types %v)", ty)
	}
}
