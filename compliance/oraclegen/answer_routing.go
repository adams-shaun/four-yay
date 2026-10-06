package oraclegen

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/rules"
)

// answerRouting owns the decisions whose XMage ask is not the one their
// engine kind suggests. Each is a gorge-side step XMage either never poses
// (a chooseUse OrCost with one payable cost, an "up to one" graveyard pick's
// confirmation) or poses as a different dialog (a clash's put-back chooseUse,
// the choice-queue "choose an opponent" ask), and the opponent selection
// gorge made implicitly with one opponent. It is computed once per decision
// list so a decision's role is derived from its neighbours, not a card name.
type answerRouting struct {
	ds       []rules.OracleDecision
	soleCost map[int]bool // the cost pick that follows a one-payable OrCost ask
	opponent map[int]int  // decision index -> opponent seat the controller selects first
	// splitTarget maps a "damage_split" decision index to the divided-target
	// decision it divides; targetSplit is the inverse. The two are paired by
	// recipient set, never by step and seat alone.
	splitTarget map[int]int
	targetSplit map[int]int
}

func newAnswerRouting(ds []rules.OracleDecision) *answerRouting {
	r := &answerRouting{ds: ds, soleCost: map[int]bool{}, opponent: map[int]int{},
		splitTarget: map[int]int{}, targetSplit: map[int]int{}}
	for i := range ds {
		if soleAltCost(ds[i]) && i+1 < len(ds) && sameAsker(ds[i], ds[i+1]) && len(ds[i+1].ObjectPicks) == 1 {
			r.soleCost[i+1] = true
		}
	}
	// A clash is the controller's decision then its opponent's; XMage's
	// ClashEffect asks the controller for the opponent (TargetOpponent) first.
	for i := 0; i+1 < len(ds); i++ {
		a, b := ds[i], ds[i+1]
		if a.Resume == "clash_placement" && b.Resume == "clash_placement" && a.Step == b.Step && a.Seat != b.Seat {
			r.opponent[i] = b.Seat
			i++
		}
	}
	// Memories Returning style: the controller picks from revealed cards, then
	// "chooses an opponent" who puts one on the bottom. Gorge picks the lone
	// opponent without a decision; the first dig pick made by another seat
	// marks where XMage's TargetOpponent ask sits.
	first := map[int]int{}
	for i, d := range ds {
		if d.Resume != "dig" {
			continue
		}
		s, ok := first[d.Step]
		if !ok {
			first[d.Step] = d.Seat
		} else if s >= 0 && d.Seat != s && len(d.Picks) == 1 && digBottomName(d.Picks[0]) != "" {
			r.opponent[i] = d.Seat
			first[d.Step] = -1
		}
	}
	// A divided-damage target ask announces its recipients; the shares arrive
	// later as the engine's own "damage_split" KChoose, whose options are
	// exactly those recipients (effects/damage_deal.go builds them from the
	// chosen Defined$ list). Pair the two by that recipient set so the split
	// answers land at the target ask's own queue position -- searching by step
	// and seat alone lets an unrelated same-step split (a second trigger
	// resolving in the same step) steal the answers, and a split's allocation
	// must precede the chain skips the target ask carries. Each split is
	// claimed at most once, and the nearest preceding unclaimed target wins.
	claimed := map[int]bool{}
	for i := range ds {
		d := ds[i]
		if d.Kind != "target" || d.Divided <= 0 || len(d.PickRefs) == 0 {
			continue
		}
		for j := i + 1; j < len(ds); j++ {
			n := ds[j]
			if n.Resume != "damage_split" || claimed[j] || !sameAsker(d, n) {
				continue
			}
			if !sameRecipients(d.PickRefs, n.PickRefs) {
				continue
			}
			r.splitTarget[j] = i
			r.targetSplit[i] = j
			claimed[j] = true
			break
		}
	}
	return r
}

// sameRecipients reports whether two pick-ref lists name the same set of
// targets. A damage_split repeats one ref per point of damage assigned, so
// duplicates are expected and only the distinct refs matter.
func sameRecipients(a, b []string) bool {
	distinct := func(refs []string) map[string]bool {
		m := map[string]bool{}
		for _, r := range refs {
			m[r] = true
		}
		return m
	}
	x, y := distinct(a), distinct(b)
	if len(x) != len(y) {
		return false
	}
	for k := range x {
		if !y[k] {
			return false
		}
	}
	return true
}

// ownsSplit reports a "damage_split" decision whose shares the paired divided
// target ask already emitted at its own queue position. Emitting them again
// would duplicate every "^X=" answer.
func (r *answerRouting) ownsSplit(i int) bool {
	_, ok := r.splitTarget[i]
	return ok
}

// soleAltCost reports an AlternateAdditionalCost either-or ask that only one
// option of could be paid: XMage's OrCost poses no chooseUse for it.
func soleAltCost(d rules.OracleDecision) bool {
	return d.Kind == "choose_n" && len(d.Picks) == 1 && pickKind(d, 0) == "altaddcost" && d.AltPayable == 1
}

func sameAsker(a, b rules.OracleDecision) bool { return a.Step == b.Step && a.Seat == b.Seat }

// digBottomName is the card name of a Dig "Put X on bottom" label, else "".
func digBottomName(label string) string {
	if strings.HasPrefix(label, "Put ") && strings.HasSuffix(label, " on bottom") {
		return strings.TrimSuffix(strings.TrimPrefix(label, "Put "), " on bottom")
	}
	return ""
}

// declinedChoice is a generic effect ask ("choose up to N") gorge answered
// with nothing.
func declinedChoice(d rules.OracleDecision) bool {
	return d.Kind == "choose_n" && d.Resume == "choice" && d.Min == 0 && d.Max > 0 && len(d.Picks) == 0
}

// route returns the answers for decision i when this file owns it.
func (r *answerRouting) route(i int) (as []XAnswer, owned bool) {
	d := r.ds[i]
	if seat, ok := r.opponent[i]; ok {
		// The controller selects the opponent on the choice queue; the seat
		// is the asker of the clash's first decision, or the dig's first pick.
		asker := d.Seat
		if d.Resume == "dig" {
			asker = r.firstDigSeat(i)
		}
		as = append(as, XAnswer{asker, "choice", seatRef(seat)})
	}
	switch {
	case soleAltCost(d):
		return as, true
	case r.soleCost[i]:
		// XMage still asks the cost's own pick (TargetCardInHand "discard
		// cost", TargetSacrifice) through makeChoose, by name.
		return append(as, XAnswer{d.Seat, "choice", oraclediffRefName(d.ObjectPicks[0])}), true
	case d.Resume == "hidden_pick_confirm" && len(d.Picks) == 1 && pickKind(d, 0) == "yes" && r.upToPickFollows(i):
		// Forge's Optional$ confirmation before an "up to one" pick: XMage's
		// ReturnCardChosenFromGraveyardEffect has the up-to-one target only.
		return nil, true
	case d.Resume == "clash_placement" && len(d.PickKinds) == 1:
		// ClashTargetEffect's chooseUse "put back on top?": true is top.
		answer := "no"
		if d.PickKinds[0] == "top" {
			answer = "yes"
		}
		return append(as, XAnswer{d.Seat, "choice", answer}), true
	case d.Resume == "dig" && len(d.Picks) == 1 && digBottomName(d.Picks[0]) != "":
		// XMage's bottom pick is a card selection by name, not gorge's label.
		return append(as, XAnswer{d.Seat, "choice", digBottomName(d.Picks[0])}), true
	case declinedChoice(d) && !r.lookedFirst(i):
		// One makeChoose "up to" ask however many type slots gorge posed; its
		// own skip token ends it. (A boolean "no" is not an answer to it.)
		if i > 0 && declinedChoice(r.ds[i-1]) && sameAsker(r.ds[i-1], d) {
			return nil, true
		}
		return []XAnswer{{d.Seat, "choice", "[choice_skip]"}}, true
	}
	return nil, false
}

// lookedFirst reports a look_ack by the same seat earlier in decision i's
// step: a look-at-the-top "up to" pick (Zimone's Experiment, Break Out) whose
// declined answer is the measured "no" + target_skip pair, not this routing.
func (r *answerRouting) lookedFirst(i int) bool {
	for _, d := range r.ds[:i] {
		if d.Resume == "look_ack" && sameAsker(d, r.ds[i]) {
			return true
		}
	}
	return false
}

// firstDigSeat is the seat of the first dig decision of decision i's step.
func (r *answerRouting) firstDigSeat(i int) int {
	for _, d := range r.ds {
		if d.Step == r.ds[i].Step && d.Resume == "dig" {
			return d.Seat
		}
	}
	return r.ds[i].Seat
}

// upToPickFollows reports an optional (Min 0) hidden pick by the same seat
// right after decision i.
func (r *answerRouting) upToPickFollows(i int) bool {
	if i+1 >= len(r.ds) {
		return false
	}
	n := r.ds[i+1]
	return sameAsker(r.ds[i], n) && n.Resume == "hidden_pick" && n.Min == 0
}

func seatRef(seat int) string { return "p" + strconv.Itoa(seat) }
