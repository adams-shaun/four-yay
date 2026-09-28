// embalm_test.go pins kw:Embalm (CR 702.128) and kw:Eternalize (CR 702.129)
// on the real corpus cards the ticket names: the keyword expands (in
// cards/keywords.go) into ONE graveyard-zone activated ability whose cost
// exiles the card itself and whose DB$ CopyPermanent effect mints a token
// copy carrying the keyword's modified characteristics -- white Zombie for
// Embalm, 4/4 black Zombie for Eternalize, on top of the copied card's own
// types. The engine reads the expansion even from the shared, stale
// ir.gob.gz cache: LoadRegistry re-links decoded faces, so a Go-side
// keyword expansion is present without a recompile (cards/registry.go), and
// these tests therefore use the ordinary CorpusRegistry.
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// embalmAbilityOption finds the pending priority decision's ability option
// for id whose label names the given keyword, failing the test when absent.
func embalmAbilityOption(t *testing.T, e *Engine, id state.ObjID, keyword string) int {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == id && strings.Contains(o.Label, keyword) {
			return o.Index
		}
	}
	t.Fatalf("no %s ability option for %d in %+v", keyword, id, d.Options)
	return -1
}

// TestAngelOfSanctionsEmbalmExilesAndMintsAWhiteZombieCopy drives the real
// Angel of Sanctions script: move it to the graveyard, pay Embalm {5}{W},
// activate the ability, and assert the card exiles itself as the cost and a
// token copy enters as a white Zombie Angel with the copied 3/4 body.
func TestAngelOfSanctionsEmbalmExilesAndMintsAWhiteZombieCopy(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 1101, []string{"Angel of Sanctions"}, nil, nil)
	id := findCardObj(t, e, 0, "Angel of Sanctions", state.ZGraveyard)
	addMana(t, e, 0, "CCCCCW") // Embalm {5}{W}
	submitChoices(t, e, embalmAbilityOption(t, e, id, "Embalm"))
	passUntilStackEmpty(t, e, 40)

	if o := e.G.Obj(id); o.Zone != state.ZExile {
		t.Fatalf("Angel of Sanctions zone %s after Embalm, want exile", o.Zone)
	}
	tokID := embalmToken(t, e, "Angel of Sanctions")
	if got := e.Colors(tokID); got != "W" {
		t.Fatalf("embalmed Angel colours %q, want W", got)
	}
	d := e.Derived(tokID)
	if !hasWord(d.Types, "Zombie") || !hasWord(d.Types, "Angel") || !hasWord(d.Types, "Creature") {
		t.Fatalf("embalmed Angel types %v, want Creature Angel Zombie", d.Types)
	}
	if d.Power != 3 || d.Toughness != 4 {
		t.Fatalf("embalmed Angel P/T %d/%d, want 3/4", d.Power, d.Toughness)
	}
	if !e.HasKeyword(tokID, "Flying") {
		t.Fatal("embalmed Angel lost the copied Flying")
	}
	replayCheck(t, e, cfg)
}

// TestTimelessDragonEternalizeExilesAndMintsAFourFourBlackZombieCopy drives
// the real Timeless Dragon script: Eternalize {2}{W}{W} changes the copied
// 5/5 white Dragon into a 4/4 black Zombie Dragon.
func TestTimelessDragonEternalizeExilesAndMintsAFourFourBlackZombieCopy(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 1102, []string{"Timeless Dragon"}, nil, nil)
	id := findCardObj(t, e, 0, "Timeless Dragon", state.ZGraveyard)
	addMana(t, e, 0, "CCWW") // Eternalize {2}{W}{W}
	submitChoices(t, e, embalmAbilityOption(t, e, id, "Eternalize"))
	passUntilStackEmpty(t, e, 40)

	if o := e.G.Obj(id); o.Zone != state.ZExile {
		t.Fatalf("Timeless Dragon zone %s after Eternalize, want exile", o.Zone)
	}
	tokID := embalmToken(t, e, "Timeless Dragon")
	if got := e.Colors(tokID); got != "B" {
		t.Fatalf("eternalized Dragon colours %q, want B", got)
	}
	d := e.Derived(tokID)
	if !hasWord(d.Types, "Zombie") || !hasWord(d.Types, "Dragon") {
		t.Fatalf("eternalized Dragon types %v, want Creature Dragon Zombie", d.Types)
	}
	if d.Power != 4 || d.Toughness != 4 {
		t.Fatalf("eternalized Dragon P/T %d/%d, want 4/4", d.Power, d.Toughness)
	}
	replayCheck(t, e, cfg)
}

// TestEmbalmOnlyOfferedFromTheGraveyard: the ability is a graveyard
// activation, so the same card in hand must not offer it (CR 702.128a).
func TestEmbalmOnlyOfferedFromTheGraveyard(t *testing.T) {
	t.Parallel()
	e, _, _ := altCostEngine(t, 1103, []string{"Angel of Sanctions"}, nil, nil)
	id := findCardObj(t, e, 0, "Angel of Sanctions", state.ZHand)
	addMana(t, e, 0, "CCCCCW")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == id && strings.Contains(o.Label, "Embalm") {
			t.Fatalf("Embalm offered from hand: %+v", o)
		}
	}
}

// embalmToken returns the single token on seat 0's battlefield whose copied
// card name matches, failing when the count is not exactly one.
func embalmToken(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	var found []state.ObjID
	for _, tid := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(tid)
		if o.IsToken && o.Face() != nil && o.Face().Name == name {
			found = append(found, tid)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s token copies %d, want 1", name, len(found))
	}
	return found[0]
}

// hasWord reports whether words contains want (a small local helper so the
// type assertions read plainly).
func hasWord(words []string, want string) bool {
	for _, w := range words {
		if w == want {
			return true
		}
	}
	return false
}

// TestAppliedGeometryAddTypesMultiTypeToken pins the multi-type AddTypes$
// reading in the shared CopyPermanent modification reader, via the real
// corpus card: Forge's multi-type separator inside the comma-list is " & "
// (the same grammar rules/layers.go's statList reads for the same parameter
// on S: statics), so Applied Geometry's "Creature & Fractal" is TWO types,
// and the minted copy of the Bear is a Fractal Creature (plus its copied
// Creature Bear types), NOT a single garbage "Creature & Fractal" word.
func TestAppliedGeometryAddTypesMultiTypeToken(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 1103, []string{"Applied Geometry"}, []string{altBearSrc}, nil)
	bear := findCardObj(t, e, 0, "Bear", state.ZBattlefield)
	ag := findCardObj(t, e, 0, "Applied Geometry", state.ZHand)
	addMana(t, e, 0, "CCGU") // Applied Geometry {2}{G}{U}
	submitChoices(t, e, castModeOption(t, e, ag, ""))
	// The cast's one target ask: the Bear (ValidTgts$ Permanent.nonAura+YouCtrl).
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Applied Geometry target ask: %+v", d)
	}
	bearIdx := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			bearIdx = o.Index
		}
	}
	if bearIdx < 0 {
		t.Fatalf("Bear not offered as a CopyPermanent target: %+v", d.Options)
	}
	submitChoices(t, e, bearIdx)
	passUntilStackEmpty(t, e, 40)

	tokID := embalmToken(t, e, "Bear")
	dv := e.Derived(tokID)
	if !hasWord(dv.Types, "Fractal") || !hasWord(dv.Types, "Bear") || !hasWord(dv.Types, "Creature") {
		t.Fatalf("copied Bear token types %v, want Creature Bear Fractal", dv.Types)
	}
	// The same resolution also carries SetPower$ 0 / SetToughness$ 0 and the
	// chained DBPutCounter's six +1/+1 counters (RememberTokens$ True puts
	// the mint into Remembered): 0/0 + six counters = 6/6.
	if dv.Power != 6 || dv.Toughness != 6 {
		t.Fatalf("copied Bear token P/T %d/%d, want 6/6 (0/0 plus six +1/+1)", dv.Power, dv.Toughness)
	}
	replayCheck(t, e, cfg)
}
