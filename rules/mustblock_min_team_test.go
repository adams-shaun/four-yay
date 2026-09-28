package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestMustBlockMinTeamBotAnswerNeverLivelocks exercises the real corpus
// Watchdog and Underworld Cerberus with exactly enough defenders for Min$ 3.
// A required lone block is illegal; a team including two ordinary blockers
// is the maximum legal declaration and must be the bot's own answer.
func TestMustBlockMinTeamBotAnswerNeverLivelocks(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	watchdog := onBoardCard(t, e, 0, mshCorpusCard(t, "Watchdog"))
	bear := onBoardCard(t, e, 0, card(t, "Name:First Helper\nTypes:Creature Bear\nPT:1/2\nOracle:x\n"))
	other := onBoardCard(t, e, 0, card(t, "Name:Second Helper\nTypes:Creature Bear\nPT:1/2\nOracle:x\n"))
	cerberus := onBoardCard(t, e, 1, mshCorpusCard(t, "Underworld Cerberus"))
	attackSeat0(t, e, cerberus)
	min, _, ok, _, _ := e.minMaxBlockerBounds(cerberus)
	if !ok || min != 3 || e.legalBlockerCount(cerberus, 0) != 3 {
		t.Fatalf("precondition: expected exactly three eligible blockers for Min$ 3, got min %d, count %d", min, e.legalBlockerCount(cerberus, 0))
	}
	for _, id := range []state.ObjID{watchdog, bear, other} {
		if e.G.Obj(id).Zone != state.ZBattlefield || !e.canBlock(id, cerberus) {
			t.Fatalf("precondition: %d is not an eligible battlefield blocker", id)
		}
	}
	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("precondition: missing block decision")
	}
	watch := findBlockOption(d, watchdog, cerberus)
	if watch == nil || !watch.Required || watch.MinBlockers != 3 {
		t.Fatalf("Watchdog requirement or minimum missing: %+v", d.Options)
	}
	if d.RequiredQuota() != 1 {
		t.Fatalf("legal full team must satisfy one requirement; quota %d", d.RequiredQuota())
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{watch.Index}}); err == nil {
		t.Fatal("lone Watchdog block illegally accepted against Min$ 3")
	}
	in := newTestBot(7).answer(e, d)
	if len(in.Choices) != 3 {
		t.Fatalf("bot did not assemble the legal full team: %v", in.Choices)
	}
	if err := e.Submit(in); err != nil {
		t.Fatalf("bot's own answer %v rejected (livelock): %v", in.Choices, err)
	}
	if !blockCommitted(t, e, cerberus, watchdog) || !blockCommitted(t, e, cerberus, bear) || !blockCommitted(t, e, cerberus, other) {
		t.Fatalf("the full team did not commit: %v", e.G.Obj(cerberus).BlockedBy)
	}
}

// Two MustBlock creatures can both block the same attacker. A pairwise
// attacker-capacity matching counts only one and accepts an illegal omission.
func TestMustBlockTwoWatchdogsShareAttacker(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	first := onBoardCard(t, e, 0, mshCorpusCard(t, "Watchdog"))
	second := onBoardCard(t, e, 0, mshCorpusCard(t, "Watchdog"))
	attacker := onBoardCard(t, e, 1, card(t, "Name:Plain Attack\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	attackSeat0(t, e, attacker)
	if e.G.Obj(first).Zone != state.ZBattlefield || e.G.Obj(second).Zone != state.ZBattlefield || !e.canBlock(first, attacker) || !e.canBlock(second, attacker) {
		t.Fatal("precondition: both Watchdogs must be able to block the battlefield attacker")
	}
	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("precondition: missing block decision")
	}
	if got := d.RequiredQuota(); got != 2 {
		t.Fatalf("two simultaneous satisfiable requirements, quota %d, want 2", got)
	}
	one := findBlockOption(d, first, attacker)
	two := findBlockOption(d, second, attacker)
	if one == nil || two == nil {
		t.Fatalf("precondition: both Watchdog pairs must be offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{one.Index}}); err == nil {
		t.Fatal("one Watchdog omitted the other satisfiable block")
	}
	in := newTestBot(7).answer(e, d)
	if err := e.Submit(in); err != nil {
		t.Fatalf("bot's own answer %v rejected: %v", in.Choices, err)
	}
	if !blockCommitted(t, e, attacker, first) || !blockCommitted(t, e, attacker, second) {
		t.Fatalf("both Watchdogs should block: %v", e.G.Obj(attacker).BlockedBy)
	}
}

// Two individually affordable duties competing for one pool can be met by
// either creature. The wire highlights one answer, but the other legal
// maximum must be accepted as well.
func TestMustBlockAlternateAffordableRequirement(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	priced := strings.Replace(pricedMustBlockSrc, "Cost$ 3", "Cost$ 2", 1)
	first := onBoardCard(t, e, 0, card(t, priced))
	second := onBoardCard(t, e, 0, card(t, strings.Replace(priced, "PT:1/2\n", "PT:1/2\nK:Flying\n", 1)))
	ground := onBoardCard(t, e, 1, card(t, "Name:Ground Attack\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	flying := onBoardCard(t, e, 1, card(t, "Name:Flying Attack\nTypes:Creature Bird\nPT:2/2\nK:Flying\nOracle:x\n"))
	attackSeat0(t, e, ground, flying)
	floatMana(t, e, 0, "RR")
	if e.G.Obj(first).Zone != state.ZBattlefield || e.G.Obj(second).Zone != state.ZBattlefield || e.blockManaBudget(0) != 2 || e.blockPairCharge(first, ground).mana != 2 || e.blockPairCharge(second, flying).mana != 2 || e.canBlock(first, flying) {
		t.Fatal("precondition: two distinct, individually affordable {2} duties and only {2} total")
	}
	d := askBlockersFresh(t, e)
	if d == nil || findBlockOption(d, first, ground) == nil || findBlockOption(d, second, flying) == nil || d.RequiredQuota() != 1 {
		t.Fatalf("precondition: missing two candidate blocks with quota 1: %+v", d)
	}
	for _, pick := range []struct{ blocker, attacker state.ObjID }{{first, ground}, {second, flying}} {
		o := findBlockOption(d, pick.blocker, pick.attacker)
		copy := e.Clone()
		if err := copy.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
			t.Fatalf("alternate legal maximum with blocker %d rejected: %v", pick.blocker, err)
		}
		if !blockCommitted(t, copy, pick.attacker, pick.blocker) {
			t.Fatalf("blocker %d was not committed", pick.blocker)
		}
	}
}

// A required blocker with a choice of attackers should use the unbounded
// pair instead of forcing an illegal singleton onto a Min$ 3 attacker.
func TestMustBlockPrefersLegalSingletonOverMinTeam(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	watchdog := onBoardCard(t, e, 0, mshCorpusCard(t, "Watchdog"))
	bear := onBoardCard(t, e, 0, card(t, "Name:Helper 1\nTypes:Creature Bear\nPT:1/2\nOracle:x\n"))
	other := onBoardCard(t, e, 0, card(t, "Name:Helper 2\nTypes:Creature Bear\nPT:1/2\nOracle:x\n"))
	cerberus := onBoardCard(t, e, 1, mshCorpusCard(t, "Underworld Cerberus"))
	plain := onBoardCard(t, e, 1, card(t, "Name:Plain Attacker\nTypes:Creature Bear\nPT:1/2\nOracle:x\n"))
	attackSeat0(t, e, cerberus, plain)
	if e.G.Obj(watchdog).Zone != state.ZBattlefield || !e.canBlock(watchdog, plain) || !e.canBlock(watchdog, cerberus) || e.legalBlockerCount(cerberus, 0) != 3 {
		t.Fatal("precondition: Watchdog needs both offered pairs and a reachable three-blocker team")
	}
	d := askBlockersFresh(t, e)
	if d == nil || findBlockOption(d, watchdog, plain) == nil || findBlockOption(d, watchdog, cerberus) == nil {
		t.Fatalf("precondition: missing Watchdog pairs: %+v", d)
	}
	// The requirement is on Watchdog, not on one particular attacker. A
	// different legal full team must also discharge it even though the
	// deterministic bot prefers the singleton.
	var team []int
	for _, bid := range []state.ObjID{watchdog, bear, other} {
		o := findBlockOption(d, bid, cerberus)
		if o == nil {
			t.Fatalf("precondition: %d cannot join the Cerberus team", bid)
		}
		team = append(team, o.Index)
	}
	copy := e.Clone()
	if err := copy.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: team}); err != nil {
		t.Fatalf("alternate full team rejected: %v", err)
	}
	in := newTestBot(7).answer(e, d)
	if err := e.Submit(in); err != nil {
		t.Fatalf("bot's own answer %v rejected: %v", in.Choices, err)
	}
	if !blockCommitted(t, e, plain, watchdog) {
		t.Fatalf("bot did not choose the legal singleton over the Min$ 3 team: plain %v, Cerberus %v",
			e.G.Obj(plain).BlockedBy, e.G.Obj(cerberus).BlockedBy)
	}
}
