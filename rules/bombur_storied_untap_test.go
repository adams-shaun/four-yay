package rules

// Storied-untap regression coverage for Bombur, Gentle Dreamer and the
// EnduringStory$ parameter's census.
//
// The set-audit test TestSetAudit_hob_Bombur_DoesNotUntapWithoutEnduringStory
// (rules/setaudit_hob_test.go) pins the NEGATIVE direction: without an
// enduring story the CR 702.175/614.1a untap replacement applies and Bombur
// stays tapped. These two tests pin the rest: the POSITIVE direction (with
// the latch held, Bombur untaps -- so a hardcoded CantHappen cannot pass),
// and the corpus census that names every other EnduringStory carrier.

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestBomburUntapsWithAnEnduringStory is the positive half of the Storied
// untap replacement. The negative half proves the replacement applies with no
// enduring story; this proves the EnduringStory$ False gate is a real
// comparison against the seat latch rather than an unconditional CantHappen:
// with seat 0's latch set, the same untap step must untap Bombur.
func TestBomburUntapsWithAnEnduringStory(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, hobCard(t, reg, "Bombur, Gentle Dreamer"))
	bombur := hobPut(t, e, 0, hobCard(t, reg, "Bombur, Gentle Dreamer"))
	if o := e.G.Obj(bombur); o == nil || !o.Face().HasKeyword("Storied") {
		t.Fatalf("precondition: Bombur lacks Storied: %+v", e.G.Obj(bombur))
	}
	// Precondition: the latch starts clear, so the gate under test has two
	// distinguishable states.
	if e.playerHasEnduringStory(0) {
		t.Fatal("precondition: seat 0 already has an enduring story")
	}
	e.emit(events.Event{Kind: events.EnduringStoryChange, Player: 0, Text: "enduring story"})
	if !e.playerHasEnduringStory(0) {
		t.Fatal("precondition: the EnduringStoryChange event did not set seat 0's latch")
	}
	e.emit(events.Event{Kind: events.Tap, Obj: bombur})
	if !e.G.Obj(bombur).Tapped {
		t.Fatal("precondition: Bombur did not tap")
	}
	e.G.Step, e.G.Active = state.StepUntap, 0
	e.finishUntapStep(0)
	if e.G.Obj(bombur).Tapped {
		t.Fatal("Bombur stayed tapped WITH an enduring story (CR 702.175: the replacement's EnduringStory$ False gate must not apply)")
	}
}

// TestEnduringStoryCensusNamesEveryCarrier pins the corpus set the
// EnduringStory$ parameter and the Condition$ EnduringStory gate touch, so a
// new pin cannot silently add a tenth carrier to a family the engine reads at
// only two sites (rules/replacement.go's replacementConditionHolds and
// rules/layers.go's continuousConditionHolds). Bombur is the SOLE card using
// the parameter form; the other eight gate a static or an ability line with
// Condition$ EnduringStory.
func TestEnduringStoryCensusNamesEveryCarrier(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	wantParamOnly := map[string]bool{"Bombur, Gentle Dreamer": true}
	want := map[string]bool{
		"Bombur, Gentle Dreamer":       true, // R: Untap ... EnduringStory$ False
		"Balin, Loremaster":            true, // SVar:...DB$ DealDamage ... Condition$ EnduringStory
		"Bifur, Melodic Rider":         true, // S:Mode$ Panharmonicon ... Condition$ EnduringStory
		"Dáin, Lord of the Iron Hills": true, // S:Mode$ CantAttackUnless ... Condition$ EnduringStory
		"Fíli the Pathfinder":          true, // S:Mode$ Continuous ... Condition$ EnduringStory
		"Kíli the Resourceful":         true, // S:Mode$ AlternativeCost ... Condition$ EnduringStory
		"Óin the Brave":                true, // S:Mode$ Continuous ... Condition$ EnduringStory
		"Ori, Keeper of Songs":         true, // S:Mode$ Continuous ... Condition$ EnduringStory
		"Thorin Oakenshield":           true, // S:Mode$ Continuous ... Condition$ EnduringStory
	}
	gotParam := map[string]bool{}
	gotAll := map[string]bool{}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			if faceCarriesEnduringStoryParam(f) {
				gotParam[f.Name] = true
				gotAll[f.Name] = true
			}
			if faceCarriesEnduringStoryCondition(f) {
				gotAll[f.Name] = true
			}
		}
	}
	if len(gotAll) == 0 {
		t.Fatal("census precondition: no EnduringStory carrier found -- the corpus registry is empty or the scan reads nothing")
	}
	if diff := nameSetDiff(gotParam, wantParamOnly); diff != "" {
		t.Errorf("EnduringStory$ parameter census moved (the parameter form is read only by replacementConditionHolds): %s", diff)
	}
	if diff := nameSetDiff(gotAll, want); diff != "" {
		t.Errorf("EnduringStory carrier census moved: %s", diff)
	}
}

// faceCarriesEnduringStoryParam reports whether f has an R: line carrying the
// EnduringStory$ parameter form.
func faceCarriesEnduringStoryParam(f *cards.Face) bool {
	for _, r := range f.Repls {
		if v, ok := r.Params["EnduringStory"]; ok && strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

// faceCarriesEnduringStoryCondition reports whether f gates any static, ability
// or SVar body on Condition$ EnduringStory.
func faceCarriesEnduringStoryCondition(f *cards.Face) bool {
	has := func(m map[string]string) bool {
		return strings.TrimSpace(m["Condition"]) == "EnduringStory"
	}
	for _, st := range f.Statics {
		if has(st.Params) {
			return true
		}
	}
	for _, a := range f.Abilities {
		if a != nil && has(a.Params) {
			return true
		}
	}
	for _, v := range f.SVars {
		if strings.Contains(v, "Condition$ EnduringStory") {
			return true
		}
	}
	return false
}

// nameSetDiff returns a stable description of the symmetric difference between
// got and want.
func nameSetDiff(got, want map[string]bool) string {
	var missing, extra []string
	for n := range want {
		if !got[n] {
			missing = append(missing, n)
		}
	}
	for n := range got {
		if !want[n] {
			extra = append(extra, n)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return ""
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return "missing=[" + strings.Join(missing, ", ") + "] extra=[" + strings.Join(extra, ", ") + "]"
}
