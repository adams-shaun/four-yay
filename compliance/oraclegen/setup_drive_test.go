package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// setupModeDecision is Zuko, Conflicted's turn-1 first-main-phase charm: a
// modal pick the runner's setup drive answered with its fallback before step
// zero. XMage poses the same chooseMode dialog during its setup drive, so the
// answer must be scripted on the pre-build setup queue.
func setupModeDecision() rules.OracleDecision {
	return rules.OracleDecision{Step: -1, Seat: 0, Kind: "mode", GorgeKind: "modes",
		Options: 4, Min: 1, Max: 1, Picks: []string{"Draw a card."}, PickIdx: []int{0},
		PickRefs: []string{"p0:Zuko, Conflicted"}, PickKinds: []string{"mode"}, Resume: "modes"}
}

// TestSetupDriveModeScriptsNumericMode pins the modal-pick class: a Step<0
// charm mode the runner answered with its fallback scripts the same numeric
// mode the step-time transcriber emits, on the pre-build setup queue
// (setup_mode), so XMage's setup drive does not answer the dialog itself.
func TestSetupDriveModeScriptsNumericMode(t *testing.T) {
	d := setupModeDecision()
	modes := map[string]int{"Draw a card.": 1}
	// Precondition: the decision really is a setup-drive ask this predicate
	// owns, and the mode map really resolves the pick (else the answer would
	// be the fallback position, not the measured mode 1).
	if d.Step >= 0 || d.GorgeKind != "modes" {
		t.Fatalf("precondition: not a setup-drive mode decision: %+v", d)
	}
	if n, ok := modeNumberFor(d, 0, modes); !ok || n != 1 {
		t.Fatalf("precondition: mode map must resolve %q to 1, got %d ok=%v", d.Picks[0], n, ok)
	}
	got := xanswersSetup([]rules.OracleDecision{d}, 1, modes, nil, nil)
	want := [][]XAnswer{{{0, "setup_mode", "1"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswersSetup = %#v, want %#v (setup mode scripts the numeric mode)", got, want)
	}
}

// setupRevealPair is Gathering Stone's ETB + upkeep peek pair: two optional
// booleans (mill "Yes", then reveal "Yes") the setup drive answered with its
// fallback.
func setupRevealPair() []rules.OracleDecision {
	mill := rules.OracleDecision{Step: -1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Yes"}, PickIdx: []int{0},
		PickRefs: []string{"Yes"}, PickKinds: []string{"yes"}, Resume: "defined_library_optional"}
	reveal := rules.OracleDecision{Step: -1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Yes — reveal Grizzly Bears"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Grizzly Bears"}, ObjectPicks: []string{"p0:Grizzly Bears"},
		PickKinds: []string{"yes"}, Resume: "reveal_optional"}
	return []rules.OracleDecision{mill, reveal}
}

// TestSetupDriveOptionalBooleansScriptYes pins the optional-boolean class: the
// mill and reveal asks Gathering Stone's triggers pose during the setup drive
// script the boolean XMage asks (chooseUse), in decision order, so the reveal
// is not silently declined by XMage's AI.
func TestSetupDriveOptionalBooleansScriptYes(t *testing.T) {
	ds := setupRevealPair()
	// Precondition: both decisions are the bare two-way boolean yesNo
	// recognises (a non-yes/no pick would fall to the labelled path instead),
	// so the scripted value is the boolean, not the label.
	for i, d := range ds {
		if v, ok := yesNo(d); !ok || v != "yes" {
			t.Fatalf("precondition: decision %d must be a yes boolean, got %q ok=%v", i, v, ok)
		}
	}
	got := xanswersSetup(ds, 2, nil, nil, nil)
	want := [][]XAnswer{{{0, "setup_choice", "yes"}, {0, "setup_choice", "yes"}}, nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswersSetup = %#v, want %#v (setup booleans script yes in order)", got, want)
	}
}

// TestSetupDriveLeavesAsEntersAndOrderPaths pins the boundary: the new
// default arm must not swallow the as-enters colour/type choice or the
// setup-placed trigger order, and the same-source order stays unqueued.
func TestSetupDriveLeavesAsEntersAndOrderPaths(t *testing.T) {
	color := rules.OracleDecision{Step: -1, Seat: 0, Kind: "choose_n", Resume: "etb",
		Options: 1, Min: 1, Max: 1, Picks: []string{"White"}, PickIdx: []int{0}, PickKinds: []string{"color"}}
	order := triggerOrderDecision(-1, "p0:Adrenaline Jockey", "p0:Rangers' Refueler")
	setup := setupWith("p0", "Adrenaline Jockey", "Rangers' Refueler")
	// Preconditions: the colour choice and the order are the two classes the
	// default arm must leave to their own predicates.
	if !IsSetupChoice(color) {
		t.Fatalf("precondition: %+v must be an as-enters setup choice", color)
	}
	if !triggerOrderSetupPosed(order, setup) {
		t.Fatalf("precondition: %+v must be setup-posed", order.PickRefs)
	}
	if as := setupDriveAnswers(color, nil); len(as) != 0 {
		t.Fatalf("setupDriveAnswers swallowed an as-enters choice: %v", as)
	}
	if as := setupDriveAnswers(order, nil); len(as) != 0 {
		t.Fatalf("setupDriveAnswers swallowed a trigger order: %v", as)
	}
	got := xanswersSetup([]rules.OracleDecision{order, color}, 1, nil, nil, setup)
	want := [][]XAnswer{{{0, "setup_choice", "White"}, {0, "setup_choice", "Adrenaline Jockey"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswersSetup = %#v, want %#v (as-enters leads, order follows)", got, want)
	}
	// A same-source order stays unqueued (the exclusion triggerOrderSetupPosed
	// keeps), and the new default arm must not queue it either.
	same := triggerOrderDecision(-1, "p0:Gathering Stone", "p0:Gathering Stone")
	if as := setupDriveAnswers(same, nil); len(as) != 0 {
		t.Fatalf("setupDriveAnswers queued a same-source order: %v", as)
	}
}

// TestSetupDriveLeavesOutOfScopeKinds pins the deliberate boundary: a
// Leyline's pregame opening ask and an as-enters "choose a number" are not
// scripted, because XMage answers them without the setup drive's help (and a
// wrong scripted answer would be a loud leftover, not a silent desync).
func TestSetupDriveLeavesOutOfScopeKinds(t *testing.T) {
	opening := rules.OracleDecision{Step: -1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"No"}, PickIdx: []int{1},
		PickRefs: []string{"p0:Leyline of the Void"}, PickKinds: []string{"opening_no"}, Resume: "opening"}
	number := rules.OracleDecision{Step: -1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 13, Min: 1, Max: 1, Picks: []string{"3"}, PickIdx: []int{2},
		PickRefs: []string{"3"}, PickKinds: []string{"number"}, Resume: "etb"}
	// Preconditions: these are exactly the shapes the scope boundary excludes
	// (a non-yes/no pick), so an empty result is the boundary holding, not a
	// vacuous input.
	if _, ok := yesNo(opening); ok {
		t.Fatalf("precondition: opening ask must not be a yesNo: %+v", opening)
	}
	if _, ok := yesNo(number); ok {
		t.Fatalf("precondition: number ask must not be a yesNo: %+v", number)
	}
	if as := setupDriveAnswers(opening, nil); len(as) != 0 {
		t.Fatalf("setupDriveAnswers scripted an opening ask: %v", as)
	}
	if as := setupDriveAnswers(number, nil); len(as) != 0 {
		t.Fatalf("setupDriveAnswers scripted a number ask: %v", as)
	}
}

// TestSetupDriveUnmappedModeIsLeftToXMage pins the mode boundary: a plain
// GenericChoice's label (Ghostly Dancers' "return an enchantment card ... or
// unlock a locked door") is not in the charm map, and XMage poses it on its
// CHOICE dialog, not chooseMode. The step-time arm's positional fallback
// would answer the wrong queue, so the setup arm must script nothing -- as it
// must for an unless-pay "mode" decision XMage does not pose at all
// (Devouring Sugarmaw).
func TestSetupDriveUnmappedModeIsLeftToXMage(t *testing.T) {
	generic := rules.OracleDecision{Step: -1, Seat: 0, Kind: "mode", GorgeKind: "modes",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Return an enchantment card from your graveyard to your hand"},
		PickIdx: []int{0}, PickRefs: []string{"p0:Ghostly Dancers"}, PickKinds: []string{"mode"}, Resume: "modes"}
	unless := rules.OracleDecision{Step: -1, Seat: 0, Kind: "mode", GorgeKind: "modes",
		Options: 1, Min: 1, Max: 1, Picks: []string{"Don't pay"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Devouring Sugarmaw"}, PickKinds: []string{"mode"}, Resume: "unless_pay"}
	// Precondition: neither label resolves in an empty charm map, so the only
	// thing keeping them unscripted is the boundary (an empty map, not a map
	// that happens to omit these).
	if _, ok := modeNumberFor(generic, 0, map[string]int{}); ok {
		t.Fatalf("precondition: %q must not resolve as a charm mode", generic.Picks[0])
	}
	if _, ok := modeNumberFor(unless, 0, map[string]int{}); ok {
		t.Fatalf("precondition: %q must not resolve as a charm mode", unless.Picks[0])
	}
	if as := setupDriveAnswers(generic, map[string]int{}); len(as) != 0 {
		t.Fatalf("setupDriveAnswers scripted an unmapped GenericChoice mode: %v", as)
	}
	if as := setupDriveAnswers(unless, map[string]int{}); len(as) != 0 {
		t.Fatalf("setupDriveAnswers scripted an unless-pay mode: %v", as)
	}
}
