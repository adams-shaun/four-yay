package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestBotRequiredBlockTeamFitsCombinedPhyrexianTax is the review-r2 MAJOR's
// end-to-end reproduction on the block side: the combined non-mana block
// charge must be solved WITH the team minima, never by filtering a finished
// team pair by pair. A Watchdog (CR 509.1c "must block if able", published as
// Required) faces a Menace attacker (MinBlockers 2), every blocker costs one
// Phyrexian pip (a CantBlockUnless static), and the defender is at two life.
// Each pair is individually affordable (one pip = 2 life) and a lone blocker
// is illegal against Menace, but the two-blocker team costs four life -- more
// than the defender has -- so NO legal team can discharge the requirement.
// The quota must then be 0 and the bot's own declaration must be the legal
// empty one; the old pair-by-pair filter returned a lone Watchdog singleton,
// which is not a legal declaration at all and which Submit rejects (turning
// into a crashed table).
func TestBotRequiredBlockTeamFitsCombinedPhyrexianTax(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	// A CantBlockUnless static that charges every blocker one Phyrexian pip.
	onBoardCard(t, e, 0, card(t, blockPhyrexianFixture))
	watchdog := onBoardCard(t, e, 0, mshCorpusCard(t, "Watchdog"))
	helper := onBoardCard(t, e, 0, card(t, "Name:Block Helper\nTypes:Creature Bear\nPT:1/2\nOracle:x\n"))
	attacker := onBoardCard(t, e, 1, card(t, "Name:Menace Tax Attacker\nTypes:Creature Bear\nPT:2/2\nK:Menace\nOracle:x\n"))
	attackSeat0(t, e, attacker)
	e.G.Players[0].Life = 2

	// Precondition: both blockers are eligible and each pair carries exactly
	// one pip; the Menace floor is two.
	if e.G.Obj(watchdog).Zone != state.ZBattlefield || !e.canBlock(watchdog, attacker) {
		t.Fatal("precondition: Watchdog is not an eligible blocker")
	}
	if e.G.Obj(helper).Zone != state.ZBattlefield || !e.canBlock(helper, attacker) {
		t.Fatal("precondition: the helper is not an eligible blocker")
	}
	if ch := e.blockPairCharge(watchdog, attacker); len(ch.phyrexian) != 1 || ch.mana != 0 || ch.life != 0 {
		t.Fatalf("precondition: Watchdog block charge = %+v, want one pip", ch)
	}
	if ch := e.blockPairCharge(helper, attacker); len(ch.phyrexian) != 1 || ch.mana != 0 || ch.life != 0 {
		t.Fatalf("precondition: helper block charge = %+v, want one pip", ch)
	}

	d := askBlockersFresh(t, e)
	if d == nil || d.Kind != decision.KBlockers || d.Player != 0 {
		t.Fatalf("pending = %+v, want seat 0's blockers", d)
	}
	watch := findBlockOption(d, watchdog, attacker)
	help := findBlockOption(d, helper, attacker)
	if watch == nil || help == nil {
		t.Fatalf("precondition: missing offered pairs: %+v", d.Options)
	}
	if !watch.BlockMust {
		t.Fatalf("precondition: Watchdog's MustBlock pair is not flagged: %+v", watch)
	}
	if watch.MinBlockers != 2 || help.MinBlockers != 2 {
		t.Fatalf("precondition: Menace floor not published (min %d/%d)", watch.MinBlockers, help.MinBlockers)
	}
	if watch.CostPhyrexian != 1 || help.CostPhyrexian != 1 {
		t.Fatalf("precondition: pip not published on the wire: %+v / %+v", watch, help)
	}
	if d.PayerLife != 2 {
		t.Fatalf("decision.PayerLife = %d, want 2 (the bound only binds there)", d.PayerLife)
	}
	// The two-blocker team is legal but costs four life -- unpayable at two --
	// while a lone blocker is illegal (verified against the engine below), so
	// the pair-by-pair filter's singleton is exactly the trap this test pins.
	if d.ChargeOptionsFit([]int{watch.Index, help.Index}) {
		t.Fatal("fixture wrong: two pips (4 life) must not fit a 2-life bound")
	}
	if !d.ChargeOptionsFit([]int{watch.Index}) {
		t.Fatal("fixture wrong: each pair must be individually affordable so it is offered")
	}
	// The engine rejects the lone required blocker against Menace's two-blocker
	// floor, so a solo singleton is not a legal declaration at all. A clone
	// proves it without consuming the live decision.
	if err := e.Clone().Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{watch.Index}}); err == nil {
		t.Fatal("fixture wrong: a lone Watchdog against Menace was accepted")
	}
	if d.RequiredQuota() != 0 {
		t.Fatalf("RequiredQuota = %d, want 0 (no payable legal team satisfies the requirement)", d.RequiredQuota())
	}

	// The bot's own answer is the legal empty declaration and Submit accepts
	// it; a singleton (or any partial team) would be rejected.
	in := newTestBot(7).answer(e, d)
	if len(in.Choices) != 0 {
		t.Fatalf("bot declared %v, want the legal empty declaration (no payable legal team exists)", in.Choices)
	}
	if err := e.Submit(in); err != nil {
		t.Fatalf("production policy declared %v: Submit rejected it: %v", in.Choices, err)
	}
}
