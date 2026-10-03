package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// CR 601.2c: every target of a spell -- the root declaration's and each
// SubAbility$ link's -- is announced as it is cast, and a TargetUnique$ link
// ("another target creature") must name a target no earlier declaration of
// the same spell named. The offer census (castTargetsAvailable, which both
// the priority walk and the payment planner's candidate walk read) skipped
// every TargetUnique$ link and never walked a Charm mode's chain at all, so
// it offered casts whose announcement the cast flow (subTargetAsk) then
// reversed with "no legal target for a chained ability" -- cardfuzz
// fuzz-1003's planrev class: Stand Together, Rookie Mistake, Incremental
// Growth (too few distinct creatures for the unique chain), Swift Kick (no
// creature its unique link may name at all) and Compel Brutality (a Charm
// mode whose chained link has no candidate).
//
// Each case: a board on which the full announcement is impossible (not
// offered, not a planner candidate), then one more creature that makes it
// possible (offered again).
func TestChainTargetCensusIsJoint(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		spell string
		mana  string
		// seat 0's and seat 1's battlefield creatures on the infeasible board
		mine, theirs []string
		// the creature (and its seat) that makes the announcement feasible
		addSeat state.PlayerID
		add     string
	}{
		// "target creature and another target creature": one creature on
		// the whole table cannot fill both.
		{name: "Stand Together", spell: "Stand Together", mana: "GG111",
			theirs: []string{"Grizzly Bears"}, addSeat: 1, add: "Craw Wurm"},
		{name: "Rookie Mistake", spell: "Rookie Mistake", mana: "U",
			theirs: []string{"Grizzly Bears"}, addSeat: 0, add: "Craw Wurm"},
		// three distinct creatures over three unique links: two are not
		// enough.
		{name: "Incremental Growth", spell: "Incremental Growth", mana: "GG111",
			mine: []string{"Grizzly Bears"}, theirs: []string{"Centaur Courser"}, addSeat: 1, add: "Craw Wurm"},
		// the unique link's own pool (a creature you don't control) is
		// empty: the skip hid even that.
		{name: "Swift Kick", spell: "Swift Kick", mana: "R111",
			mine: []string{"Grizzly Bears"}, addSeat: 1, add: "Craw Wurm"},
		// a Charm: the creature mode's chained "target creature or
		// planeswalker an opponent controls" has no candidate, the
		// planeswalker mode has no root candidate.
		{name: "Compel Brutality", spell: "Compel Brutality", mana: "G1",
			mine: []string{"Grizzly Bears"}, addSeat: 1, add: "Craw Wurm"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			seat0 := map[string]state.Zone{tc.spell: state.ZHand}
			seat1 := map[string]state.Zone{}
			for _, n := range tc.mine {
				seat0[n] = state.ZBattlefield
			}
			for _, n := range tc.theirs {
				seat1[n] = state.ZBattlefield
			}
			if tc.addSeat == 0 {
				seat0[tc.add] = state.ZHand
			} else {
				seat1[tc.add] = state.ZHand
			}
			e, cfg, mine, _ := cr601Board(t, uint64(60900+i), seat0, seat1)
			spell := mine[tc.spell]
			addMana(t, e, 0, tc.mana)
			edrSeatZeroPriority(t, e)
			if cr601Offered(t, e, spell) {
				t.Fatalf("%s is offered although no legal announcement of all its targets exists", tc.spell)
			}
			if e.paymentPlanCastCandidate(0, spell) {
				t.Fatalf("%s is a payment-plan candidate although no legal announcement of all its targets exists", tc.spell)
			}
			moveByName(t, e, tc.addSeat, tc.add, state.ZBattlefield)
			e.pending = nil
			e.priorityRound()
			edrSeatZeroPriority(t, e)
			if !cr601Offered(t, e, spell) {
				t.Fatalf("%s is not offered once %s is on the battlefield", tc.spell, tc.add)
			}
			if !e.paymentPlanCastCandidate(0, spell) {
				t.Fatalf("%s is not a payment-plan candidate once %s is on the battlefield", tc.spell, tc.add)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestDistinctTargetsFeasible pins the joint assignment the chain census
// runs: each slot takes `need` candidates of its own pool, and no candidate
// serves two slots.
func TestDistinctTargetsFeasible(t *testing.T) {
	t.Parallel()
	obj := func(ids ...state.ObjID) []targetCandidate {
		out := make([]targetCandidate, 0, len(ids))
		for _, id := range ids {
			out = append(out, targetCandidate{kind: "card", obj: id})
		}
		return out
	}
	player := targetCandidate{kind: "player", player: 1}
	for _, tc := range []struct {
		name  string
		slots []uniqueTargetSlot
		want  bool
	}{
		{"empty", nil, true},
		{"one creature, two slots", []uniqueTargetSlot{{obj(7), 1}, {obj(7), 1}}, false},
		{"two creatures, two slots", []uniqueTargetSlot{{obj(7, 8), 1}, {obj(7, 8), 1}}, true},
		{"two creatures, three slots", []uniqueTargetSlot{{obj(7, 8), 1}, {obj(7, 8), 1}, {obj(7, 8), 1}}, false},
		// greedy would give slot 0 the 7 and strand slot 1; the augmenting
		// path moves slot 0 to the 8.
		{"augmenting path", []uniqueTargetSlot{{obj(7, 8), 1}, {obj(7), 1}}, true},
		{"need two of a shared pool", []uniqueTargetSlot{{obj(7, 8), 2}, {obj(8), 1}}, false},
		{"zero-need slot", []uniqueTargetSlot{{obj(7), 1}, {nil, 0}}, true},
		// a player and an object with the same numeric id are different
		// targets.
		{"player vs object", []uniqueTargetSlot{{[]targetCandidate{player}, 1}, {obj(1), 1}}, true},
		{"empty pool", []uniqueTargetSlot{{nil, 1}}, false},
	} {
		if got := distinctTargetsFeasible(tc.slots); got != tc.want {
			t.Errorf("%s: distinctTargetsFeasible = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestRootTargetLeavingUniqueChainEmptyIsWithheld is the ask half of the
// class: Band Together ("up to two target creatures you control each deal
// damage equal to their power to another target creature") with one creature
// on the table. Naming it as a root target leaves "another target creature"
// nothing, so the cast could only reverse (CR 733.1); the root ask withholds
// it, the root announces no target and the chain names the creature.
func TestRootTargetLeavingUniqueChainEmptyIsWithheld(t *testing.T) {
	t.Parallel()
	e, cfg, mine, _ := cr601Board(t, 60950,
		map[string]state.Zone{"Band Together": state.ZHand, "Grizzly Bears": state.ZBattlefield},
		map[string]state.Zone{})
	spell, bears := mine["Band Together"], mine["Grizzly Bears"]
	addMana(t, e, 0, "G11")
	edrSeatZeroPriority(t, e)
	cr601Cast(t, e, spell, "")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the chained target ask", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != bears || d.ResumeKind != "cast_sub" {
		t.Fatalf("ask = %q resume %q options %+v, want the chained ask offering only Grizzly Bears", d.Prompt, d.ResumeKind, d.Options)
	}
	answerCastSubObj(t, e, bears)
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack {
		t.Fatalf("Band Together is not on the stack after its announcement: %+v", o)
	}
	replayCheck(t, e, cfg)
}
