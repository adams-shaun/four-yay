package rules

// setaudit_ecl_test.go — the Lorwyn Eclipsed (ecl) set audit.
//
// This file is an AUDIT product: each guarded test names a real engine defect
// found by auditing how correctly the engine plays the set. The guard
// (GORGE_SET_AUDIT) keeps a failing finding out of the ordinary suite while the
// follow-up ticket fixes the root cause; an UNGUARDED test is regression
// coverage for behaviour verified correct.
//
// Card data comes from the corpus (.cards/cardsfolder) via the ordinary corpus
// helpers; no Forge script text is committed here.

import (
	"os"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// eclGuard skips a failing finding unless GORGE_SET_AUDIT is set, naming the
// defect and its follow-up ticket title.
func eclGuard(t *testing.T, defect, ticket string) {
	t.Helper()
	if os.Getenv("GORGE_SET_AUDIT") == "" {
		t.Skip("set-audit finding (ecl): " + defect + ". Follow-up: " + ticket)
	}
}

// eclEngine builds a two-seat engine from named corpus cards (seat 0's deck
// first) padded with Mountains, with the real token scripts, and drives it to
// seat 0's turn 1 main 1.
func eclEngine(t *testing.T, seed uint64, seat0, seat1 []string) (*Engine, Config) {
	t.Helper()
	reg := searchTestRegistry(t)
	mountain := searchCorpusCard(t, reg, "Mountain")
	fill := func(names []string) []*cards.Card {
		out := make([]*cards.Card, 0, 40)
		for _, n := range names {
			out = append(out, searchCorpusCard(t, reg, n))
		}
		for len(out) < 40 {
			out = append(out, mountain)
		}
		return out
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"p0", "p1"},
		Decks: [][]*cards.Card{fill(seat0), fill(seat1)}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// eclTokenCreates counts TokenCreate events whose Text names the token script
// key.
func eclTokenCreates(e *Engine, key string) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TokenCreate && ev.Text == key {
			n++
		}
	}
	return n
}

// eclPassAll answers pending priority decisions with "pass", pending library
// search/choose/mode asks with their first option, for up to limit decisions
// or until no decision remains.
func eclPassAll(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit; i++ {
		d := e.Pending()
		if d == nil {
			return
		}
		switch d.Kind {
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d.Options)
			}
			submitChoices(t, e, idx)
		case decision.KChoose:
			if len(d.Options) == 0 {
				t.Fatalf("choose with no options: %+v", d)
			}
			submitChoices(t, e, d.Options[0].Index)
		default:
			return
		}
	}
}

// eclDriveToStep is driveToStep generalised to answer a mid-drive ask
// (Mornsong Aria's draw-step trigger searches every player).
func eclDriveToStep(t *testing.T, e *Engine, turn int32, active state.PlayerID, step state.Step) {
	t.Helper()
	for i := 0; i < 2000; i++ {
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == step {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before reaching turn %d seat %d step %s", turn, active, step)
		}
		eclPassAll(t, e, 1)
	}
	t.Fatalf("did not reach turn %d seat %d step %s", turn, active, step)
}

// TestSetAudit_ecl_MornsongAria_CantDrawStopsDrawStep pins CR 121.6 and the
// CantDraw static: Mornsong Aria is "Players can't draw cards or gain life."
// The draw-step draw (CR 504.1) must not happen while it is on the
// battlefield. `S:Mode$ CantDraw` is now implemented (rules/replacement.go
// drawForbidden, consulted by applyReplacements), so this is ordinary
// regression coverage rather than a guarded finding.
func TestSetAudit_ecl_MornsongAria_CantDrawStopsDrawStep(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	aria := mustCorpusCard(t, reg, "Mornsong Aria")

	sawCantDraw := false
	for _, f := range aria.Faces {
		for _, st := range f.Statics {
			if st.Mode == "CantDraw" {
				sawCantDraw = true
			}
		}
	}
	if !sawCantDraw {
		t.Fatalf("precondition: Mornsong Aria carries no CantDraw static")
	}

	e, cfg := eclEngine(t, 616, []string{"Mornsong Aria"}, nil)
	blightMove(t, e, 0, "Mornsong Aria", state.ZBattlefield)
	e.pending = nil
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)

	// Drive to just BEFORE seat 1's draw step, mark the log, then cross into
	// the step: the draw-step draw (CR 504.1) is a turn-based action on entry.
	eclDriveToStep(t, e, 2, 1, state.StepUpkeep)
	mark := len(e.L.Events)
	eclDriveToStep(t, e, 2, 1, state.StepDraw)
	eclPassAll(t, e, 12)
	drew := 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw && ev.Player == 1 {
			drew++
		}
	}
	if drew != 0 {
		t.Fatalf("seat 1 drew %d cards on entering its draw step, want 0 (CantDraw)", drew)
	}
	replayCheck(t, e, cfg)
}

// TestSetAudit_ecl_OptionalCostIsRegistered pins the coverage-report contract:
// the optional self additional cost (stat:OptionalCost) IS implemented
// (rules/optional_cost_test.go drives Burning Curiosity's Blight<1> and
// Voltage Surge's Sac<1/Artifact> end to end), but the primitive is absent
// from effects.Supported(), so the coverage ratchet counts five ecl cards --
// Pyrrhic Strike, Requiting Hex, Cinder Strike, Celestial Reunion, Burning
// Curiosity -- as needing a primitive the build already has.
func TestSetAudit_ecl_OptionalCostIsRegistered(t *testing.T) {
	t.Parallel()
	if effects.Supported()["stat:OptionalCost"] {
		return
	}
	eclGuard(t, "stat:OptionalCost is implemented but never registered, so the coverage ratchet under-reports support",
		"Register stat:OptionalCost in effects.Supported() so the coverage ratchet stops counting it as missing")
	t.Fatal("effects.Supported() lacks stat:OptionalCost even though the primitive is implemented")
}

// TestSetAudit_ecl_VinebredBrawler_MustBeBlockedKeyword is regression coverage
// for the attacker-oriented CR 509.1c requirement "This creature must be
// blocked if able." Vinebred Brawler spells it `K:CARDNAME must be blocked if
// able.` (the same raw form 16 corpus files use); the engine reads it through
// the derived hidden-keyword list, so the requirement is enforced despite the
// primitive's name not being registered.
func TestSetAudit_ecl_VinebredBrawler_MustBeBlockedKeyword(t *testing.T) {
	t.Parallel()
	e, cfg := eclEngine(t, 617, []string{"Vinebred Brawler"}, nil)
	id := blightMove(t, e, 0, "Vinebred Brawler", state.ZBattlefield)
	if !e.hasMustBeBlockedKeyword(id) {
		t.Fatalf("Vinebred Brawler's derived keywords do not carry the must-be-blocked requirement: %q",
			e.Derived(id).Keywords)
	}
	replayCheck(t, e, cfg)
}

// TestSetAudit_ecl_ChampionOfTheClachan_BeholdExileReturns pins the
// BeholdExile additional cost and its leave-the-battlefield rider: Champion
// of the Clachan's oracle is "As an additional cost to cast this spell,
// behold a Kithkin and exile it. ... When this creature leaves the
// battlefield, return the exiled card to its owner's hand." (CR 701.56 behold
// plus the CR 607.2d linked ability). Both halves must work.
func TestSetAudit_ecl_ChampionOfTheClachan_BeholdExileReturns(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	champ := mustCorpusCard(t, reg, "Champion of the Clachan")

	// Precondition: the script carries the BeholdExile raise cost.
	sawBeholdExile := false
	for _, f := range champ.Faces {
		for _, st := range f.Statics {
			if st.Mode == "RaiseCost" && strings.Contains(st.Params["Cost"], "BeholdExile") {
				sawBeholdExile = true
			}
		}
	}
	if !sawBeholdExile {
		t.Fatalf("precondition: Champion of the Clachan carries no BeholdExile cost")
	}

	e, cfg := eclEngine(t, 619,
		[]string{"Champion of the Clachan", "Timid Shieldbearer"}, nil)
	kith := blightMove(t, e, 0, "Timid Shieldbearer", state.ZBattlefield)
	champID := searchMoveByName(t, e, "Champion of the Clachan", state.ZHand)
	addMana(t, e, 0, "CCCW")

	opt := castByName(t, e, 0, "Champion of the Clachan")
	if opt == nil {
		t.Fatalf("Champion of the Clachan not castable: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	// The BeholdExile cost asks which Kithkin to behold and exile.
	eclPassAll(t, e, 8)
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(kith).Zone; got != state.ZExile {
		t.Fatalf("beheld Kithkin is in %s, want exile", got)
	}
	if got := e.G.Obj(champID).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition: Champion of the Clachan is in %s, want battlefield", got)
	}

	// It leaves the battlefield: the exiled card returns to its owner's hand.
	e.emit(events.Event{Kind: events.MoveZone, Obj: champID, From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	e.priorityRound()
	passUntilStackEmpty(t, e, 30)
	if got := e.G.Obj(kith).Zone; got != state.ZHand {
		t.Fatalf("beheld Kithkin is in %s after Champion left, want hand", got)
	}
	replayCheck(t, e, cfg)
}

// TestSetAudit_ecl_HighPerfectMorcant_BlightsEachOpponent pins CR 701.60 on
// the set's own Blight mechanic: "Whenever CARDNAME or another Elf you
// control enters, each opponent blights 1." Each opponent who controls a
// creature puts one -1/-1 counter on a creature they control.
func TestSetAudit_ecl_HighPerfectMorcant_BlightsEachOpponent(t *testing.T) {
	t.Parallel()
	e, cfg := eclEngine(t, 620,
		[]string{"High Perfect Morcant"}, []string{"Grizzly Bears"})
	bear := blightMove(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	searchMoveByName(t, e, "High Perfect Morcant", state.ZHand)
	addMana(t, e, 0, "BBGG")
	opt := castByName(t, e, 0, "High Perfect Morcant")
	if opt == nil {
		t.Fatalf("High Perfect Morcant not castable: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	eclPassAll(t, e, 8)
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(bear).Counter("M1M1"); got != 1 {
		t.Fatalf("-1/-1 counters on the opponent's creature = %d, want 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestSetAudit_ecl_SaplingNursery_AffinityAndLandfall pins the set's Affinity
// (CR 702.42) and Landfall mechanics together: Sapling Nursery is
// "{6}{G}{G} ... Affinity for Forests ... Landfall — Whenever a land you
// control enters, create a 3/4 green Treefolk creature token with reach."
// Six Forests make the affinity reduction 6, so the spell is castable for
// {G}{G}, and a land entering fires the token trigger.
func TestSetAudit_ecl_SaplingNursery_AffinityAndLandfall(t *testing.T) {
	t.Parallel()
	deck := []string{"Sapling Nursery"}
	for i := 0; i < 6; i++ {
		deck = append(deck, "Forest")
	}
	e, cfg := eclEngine(t, 621, deck, nil)
	nursery := searchMoveByName(t, e, "Sapling Nursery", state.ZHand)
	for i := 0; i < 6; i++ {
		blightMove(t, e, 0, "Forest", state.ZBattlefield)
	}
	if got := reduceOf(t, e, 0, nursery); got != 6 {
		t.Fatalf("affinity reduction with 6 Forests = %d, want 6", got)
	}
	addMana(t, e, 0, "GG")
	opt := castByName(t, e, 0, "Sapling Nursery")
	if opt == nil {
		t.Fatalf("Sapling Nursery not castable for {G}{G} with 6 Forests: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 30)

	// Landfall: a land entering creates the 3/4 reach Treefolk token.
	before := eclTokenCreates(e, "g_3_4_treefolk_reach")
	blightMove(t, e, 0, "Mountain", state.ZBattlefield)
	e.pending = nil
	e.priorityRound()
	passUntilStackEmpty(t, e, 30)
	if got := eclTokenCreates(e, "g_3_4_treefolk_reach"); got != before+1 {
		t.Fatalf("Treefolk tokens after a land entered = %d, want %d (Landfall)", got, before+1)
	}
	replayCheck(t, e, cfg)
}
