package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

func routed(t *testing.T, ds []rules.OracleDecision, steps int) []XAnswer {
	t.Helper()
	out := xanswers(ds, steps, nil, nil)
	if out == nil {
		t.Fatal("no answers scripted")
	}
	var all []XAnswer
	for _, s := range out {
		all = append(all, s...)
	}
	return all
}

// A cost ask with one payable option is not posed by XMage's OrCost; the cost
// pick that follows is its own selection ask, answered by name.
func TestSoleAltCostRoutesCostPickByName(t *testing.T) {
	ask := rules.OracleDecision{Kind: "choose_n", Options: 2, Picks: []string{"Discard 1 card"}, PickIdx: []int{0},
		PickKinds: []string{"altaddcost"}, Min: 1, Max: 1, AltPayable: 1}
	pick := rules.OracleDecision{Kind: "choose_n", Options: 1, Picks: []string{"Forest"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Forest"}, ObjectPicks: []string{"p0:Forest"}, PickKinds: []string{"discard"}, Min: 1, Max: 1}
	got := routed(t, []rules.OracleDecision{ask, pick}, 1)
	want := []XAnswer{{0, "choice", "Forest"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discard cost: got %v want %v", got, want)
	}
	// Both options payable: XMage asks the either-or first, so the routing
	// must not own it.
	ask.AltPayable = 2
	if _, owned := newAnswerRouting([]rules.OracleDecision{ask, pick}).route(0); owned {
		t.Fatal("two payable costs: either-or ask must stay with the generic path")
	}
	sac := pick
	sac.Picks, sac.PickRefs, sac.ObjectPicks, sac.PickKinds = []string{"Llanowar Elves"}, []string{"p0:Llanowar Elves"}, []string{"p0:Llanowar Elves"}, []string{"sacrifice"}
	ask.AltPayable = 1
	got = routed(t, []rules.OracleDecision{ask, sac}, 1)
	if want := []XAnswer{{0, "choice", "Llanowar Elves"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sacrifice cost: got %v want %v", got, want)
	}
}

// Optional$ confirmation before an "up to one" graveyard pick: XMage poses
// only the pick.
func TestUpToOneGraveyardPickDropsConfirmation(t *testing.T) {
	confirm := rules.OracleDecision{Step: 1, Kind: "choose_n", Options: 2, Picks: []string{"Yes"}, PickIdx: []int{0},
		PickKinds: []string{"yes"}, Resume: "hidden_pick_confirm", Min: 1, Max: 1}
	pick := rules.OracleDecision{Step: 1, Kind: "choose_n", Options: 3, Picks: []string{"Wastes"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Wastes#27"}, ObjectPicks: []string{"p0:Wastes#27"}, PickKinds: []string{"hidden_pick"},
		Resume: "hidden_pick", Min: 0, Max: 1}
	got := routed(t, []rules.OracleDecision{confirm, pick}, 2)
	for _, a := range got {
		if a.Value == "yes" {
			t.Fatalf("confirmation scripted: %v", got)
		}
	}
	if len(got) != 1 || got[0].Value != "Wastes" {
		t.Fatalf("got %v, want the pick alone", got)
	}
}

// A declined "up to" pick posed as a card selection (Destined Confrontation)
// gets the choice skip, not a boolean; a look-at-the-top pick keeps the
// measured pair.
func TestDeclinedUpToPickIsAChoiceSkip(t *testing.T) {
	declined := rules.OracleDecision{Step: 1, Seat: 1, Kind: "choose_n", Options: 1, Resume: "choice", Min: 0, Max: 1}
	got := routed(t, []rules.OracleDecision{declined}, 2)
	if want := []XAnswer{{1, "choice", "[choice_skip]"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	look := rules.OracleDecision{Step: 1, Seat: 1, Kind: "choose_n", Options: 1, Picks: []string{"Continue"}, PickIdx: []int{0},
		PickKinds: []string{"yes"}, Resume: "look_ack", Min: 1, Max: 1}
	got = routed(t, []rules.OracleDecision{look, declined}, 2)
	for _, a := range got {
		if a.Value == "[choice_skip]" {
			t.Fatalf("look-at-top pick rerouted: %v", got)
		}
	}
}

// Single-opponent implicit choice: XMage's TargetOpponent ask comes first on
// the choice queue, by the opponent's name.
func TestImplicitOpponentChoiceIsScripted(t *testing.T) {
	opp := rules.OracleDecision{Step: 1, Kind: "choose_n", Options: 1, Picks: []string{""}, PickIdx: []int{0}, PickRefs: []string{"p1"},
		PickKinds: []string{"player"}, Resume: "choice", Min: 1, Max: 1}
	typ := rules.OracleDecision{Step: 1, Kind: "choose_n", Options: 8, Picks: []string{"Artifact"}, PickIdx: []int{0},
		PickRefs: []string{"Artifact"}, PickKinds: []string{"type"}, Resume: "choosetype", Min: 1, Max: 1}
	got := routed(t, []rules.OracleDecision{opp, typ}, 2)
	want := []XAnswer{{0, "choice", "p1"}, {0, "choice", "Artifact"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

// Grub's Command's clash: the controller picks the opponent, then each seat
// answers the put-back boolean (bottom = no).
func TestClashPicksOpponentThenBooleans(t *testing.T) {
	a := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", Options: 2, Picks: []string{"Put it on the bottom"}, PickKinds: []string{"bottom"}, Resume: "clash_placement", Min: 1, Max: 1}
	b := a
	b.Seat = 1
	got := routed(t, []rules.OracleDecision{a, b}, 2)
	want := []XAnswer{{0, "choice", "p1"}, {0, "choice", "no"}, {1, "choice", "no"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
