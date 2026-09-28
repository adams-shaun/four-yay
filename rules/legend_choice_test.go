// Ticket cli-20260922T225140Z-68ca4d95: the CR 704.5j legend rule now poses a
// decision instead of keeping the battlefield-order first duplicate. An SBA
// that needs a controller choice has a decision channel: destroyLethalDamage
// parks the whole batch (the duplicate set, the lethal-damage casualties found
// in the same pass, the pre-batch look-back board) and asks the set's
// controller which member to keep, exactly as the CR 903.9 commander-zone
// replacement parks its move and asks its owner from inside the same pass.
// The unchosen members go to their owners' graveyards as legend-rule
// departures (placement, not destruction); a KEPT member keeps its own
// lethal-damage destruction path, so a regeneration shield can still save it.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// legendPairGame fields TWO identically-named legendary commanders for seat 0
// (the board CR 704.5j forbids -- here that is the test's premise, not a
// broken fixture) and returns the engine, both battlefield ids in battlefield
// order, and their shared name. FormatConstructed keeps the CR 903.9
// commander-zone replacement out of the way: binning a commander in a
// Commander-format game would park a second, unrelated decision on top of the
// one this file is testing.
func legendPairGame(t *testing.T, name string) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	src := "Name:" + name + "\nManaCost:2 G\nTypes:Legendary Creature Bear\nPT:5/5\nOracle:x\n"
	e, _ := commanderGame(t, commanderDamageSeed, FormatConstructed, 40,
		[][]string{{src, src}, {}})
	// Capture the pristine command-zone order first: fieldCommanderByID is
	// safe for several fieldings in any order, fieldCommander's live-zone
	// index is not.
	cz := e.G.Zone(state.ZCommand, 0)
	id1 := fieldCommanderByID(t, e, cz[0])
	id2 := fieldCommanderByID(t, e, cz[1])
	// Precondition the rule reads: both on the battlefield, both legendary,
	// both the same name -- otherwise there is no duplicate set to choose in.
	for _, id := range []state.ObjID{id1, id2} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("fixture: commander %d not on the battlefield (zone %v)", id, o)
		}
		if !o.Face().IsLegendary() {
			t.Fatalf("fixture: %s is not legendary; the legend rule cannot fire", o.Face().Name)
		}
		if o.Face().Name != name {
			t.Fatalf("fixture: name %q, want %q", o.Face().Name, name)
		}
	}
	if id1 == id2 {
		t.Fatalf("fixture: the two permanents share one id; nothing is duplicated")
	}
	return e, id1, id2
}

// legendPending asserts the engine is holding exactly the CR 704.5j choice for
// the given controller over the given duplicate set, offered in battlefield
// order, and returns the pending decision.
func legendPending(t *testing.T, e *Engine, p state.PlayerID, ids ...state.ObjID) *decision.Decision {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatalf("no decision pending: the legend rule kept the scan-order survivor instead of asking its controller")
	}
	if d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 {
		t.Fatalf("pending decision %s Min %d Max %d, want a KChoose Min 1 Max 1", d.Kind, d.Min, d.Max)
	}
	if d.Player != p {
		t.Fatalf("legend choice asked seat %d, want the duplicates' controller seat %d", d.Player, p)
	}
	if len(d.Options) != len(ids) {
		t.Fatalf("legend choice offers %d options, want one per duplicate (%d)", len(d.Options), len(ids))
	}
	for i, o := range d.Options {
		if o.Kind != "keep" {
			t.Fatalf("legend option %d Kind %q, want \"keep\"", i, o.Kind)
		}
		if o.Obj != ids[i] {
			t.Fatalf("legend option %d names obj %d, want %d (battlefield order)", i, o.Obj, ids[i])
		}
	}
	return d
}

// submitKeep answers the pending legend choice, keeping the option at the
// given index.
func submitKeep(t *testing.T, e *Engine, d *decision.Decision, keep int) {
	t.Helper()
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{keep}}); err != nil {
		t.Fatalf("Submit(keep %d): %v", keep, err)
	}
}

// zoneText returns the Text of the object's one battlefield-departure
// MoveZone ("legend rule", "lethal damage", ...), failing if there is none.
func legendDepartureText(t *testing.T, e *Engine, id state.ObjID) string {
	t.Helper()
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.MoveZone && ev.Obj == id &&
			ev.From == state.ZBattlefield && ev.To == state.ZGraveyard {
			return ev.Text
		}
	}
	t.Fatalf("no battlefield->graveyard MoveZone for obj %d", id)
	return ""
}

// TestLegendRuleAsksControllerWhichDuplicateToKeep is the row's headline: the
// duplicate set's controller is asked which member survives, and keeping the
// SECOND leaves it on the battlefield while the FIRST is binned as a
// legend-rule departure. The pre-decision build binned the scan-order SECOND
// unconditionally, so this fails with the fix reverted.
func TestLegendRuleAsksControllerWhichDuplicateToKeep(t *testing.T) {
	t.Parallel()
	e, id1, id2 := legendPairGame(t, "Legend Twin")
	e.checkStateBased()
	d := legendPending(t, e, 0, id1, id2)
	if e.legendBatch == nil || e.legendBatch.group.ids[0] != id1 {
		t.Fatalf("the batch is not parked with the asked set")
	}
	submitKeep(t, e, d, 1)
	if o := e.G.Obj(id2); o.Zone != state.ZBattlefield {
		t.Fatalf("kept duplicate left the battlefield (zone %v); the controller's choice did not hold", o.Zone)
	}
	if o := e.G.Obj(id1); o.Zone != state.ZGraveyard {
		t.Fatalf("unchosen duplicate stayed in %v; want its owner's graveyard", o.Zone)
	}
	if got := legendDepartureText(t, e, id1); got != "legend rule" {
		t.Fatalf("unchosen duplicate departed with Text %q, want \"legend rule\" (placement, not destruction)", got)
	}
	kept := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && ev.Counter == "legend_keep" && ev.Obj == id2 {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("no Choose \"legend_keep\" event recording the kept permanent %d", id2)
	}
	if e.legendBatch != nil {
		t.Fatalf("legend batch still parked after the answer")
	}
}

// TestLegendRuleSettlesEachDuplicateSetInTurn: two duplicate sets (two names)
// are parked in one pass's discovery but only ONE decision is ever pending;
// the Submit tail's re-scan poses the second set's choice after the first is
// settled. Both settles hold.
func TestLegendRuleSettlesEachDuplicateSetInTurn(t *testing.T) {
	t.Parallel()
	src := func(name string) string {
		return "Name:" + name + "\nManaCost:2 G\nTypes:Legendary Creature Bear\nPT:5/5\nOracle:x\n"
	}
	e, _ := commanderGame(t, commanderDamageSeed, FormatConstructed, 40,
		[][]string{{src("Legend Alpha"), src("Legend Alpha"), src("Legend Beta"), src("Legend Beta")}, {}})
	cz := e.G.Zone(state.ZCommand, 0)
	a1 := fieldCommanderByID(t, e, cz[0])
	a2 := fieldCommanderByID(t, e, cz[1])
	b1 := fieldCommanderByID(t, e, cz[2])
	b2 := fieldCommanderByID(t, e, cz[3])
	e.checkStateBased()
	// Battlefield order is Alpha, Alpha, Beta, Beta: the first asked set is
	// the Alpha pair.
	d := legendPending(t, e, 0, a1, a2)
	submitKeep(t, e, d, 1)
	// The second set's ask was posed by the answer's own SBA re-scan.
	d2 := legendPending(t, e, 0, b1, b2)
	submitKeep(t, e, d2, 0)
	for _, tc := range []struct {
		id      state.ObjID
		want    state.Zone
		binned  bool // the unchosen ones are the legend-rule departures
		setName string
	}{{a1, state.ZGraveyard, true, "Legend Alpha"}, {a2, state.ZBattlefield, false, "Legend Alpha"},
		{b1, state.ZBattlefield, false, "Legend Beta"}, {b2, state.ZGraveyard, true, "Legend Beta"}} {
		if o := e.G.Obj(tc.id); o.Zone != tc.want {
			t.Errorf("%s duplicate %d in %v, want %v", tc.setName, tc.id, o.Zone, tc.want)
		}
		if tc.binned {
			if got := legendDepartureText(t, e, tc.id); got != "legend rule" {
				t.Errorf("%s unchosen duplicate departed with Text %q, want \"legend rule\"", tc.setName, got)
			}
		}
	}
	if e.pending != nil && e.pending.Kind == decision.KChoose && len(e.pending.Options) > 0 && e.pending.Options[0].Kind == "keep" {
		t.Fatalf("a legend choice is still pending after every duplicate set was settled")
	}
	if e.legendBatch != nil {
		t.Fatalf("legend batch still parked after every duplicate set was settled")
	}
}

// TestLegendRuleKeptSurvivorKeepsLethalDamagePath: the scan-order FIRST
// duplicate has lethal damage, and the controller keeps the SECOND. The
// second therefore SURVIVES -- under the pre-decision build the scan-order
// first was kept and the second was binned unconditionally, so this fails
// with the fix reverted. It also pins the batch discipline: the unchosen
// (damaged) member is serialized by the legend rule, not by its own lethal
// damage, matching the single-serialization discipline the ordinary batch
// uses for a member that is both.
func TestLegendRuleKeptSurvivorKeepsLethalDamagePath(t *testing.T) {
	t.Parallel()
	e, id1, id2 := legendPairGame(t, "Legend Twin")
	// Mark the FIRST duplicate lethally damaged (5/5, six damage).
	e.emit(events.Event{Kind: events.Damage, Obj: id1, Amount: 6})
	if o := e.G.Obj(id1); o.Damage != 6 {
		t.Fatalf("fixture: marked damage %d, want 6", o.Damage)
	}
	e.checkStateBased()
	d := legendPending(t, e, 0, id1, id2)
	submitKeep(t, e, d, 1)
	if o := e.G.Obj(id2); o.Zone != state.ZBattlefield {
		t.Fatalf("kept duplicate in %v; want on the battlefield -- the controller kept the healthy one", o.Zone)
	}
	if o := e.G.Obj(id1); o.Zone != state.ZGraveyard {
		t.Fatalf("damaged unchosen duplicate in %v, want graveyard", o.Zone)
	}
	if got := legendDepartureText(t, e, id1); got != "legend rule" {
		t.Fatalf("damaged unchosen duplicate departed with Text %q, want \"legend rule\"", got)
	}
}

// TestLegendRuleBotAnswerKeepsBattlefieldOrderFirst runs the deterministic
// bot's own answer through the validator on a board where the choice binds:
// botpolicy has no scored arm for a "keep" ballot, so its clamp fallback
// takes option 0 -- the battlefield-order first duplicate, exactly the
// survivor the pre-decision build always picked. The intent must validate
// (no livelock: an illegal answer would re-submit forever) and the settle
// must hold.
func TestLegendRuleBotAnswerKeepsBattlefieldOrderFirst(t *testing.T) {
	t.Parallel()
	e, id1, id2 := legendPairGame(t, "Legend Twin")
	e.checkStateBased()
	d := legendPending(t, e, 0, id1, id2)
	bot := newTestBot(commanderDamageSeed)
	in := bot.answer(e, d)
	if err := d.Validate(in); err != nil {
		t.Fatalf("the bot's own answer does not validate: %v", err)
	}
	if len(in.Choices) != 1 || in.Choices[0] != 0 {
		t.Fatalf("bot answer %v, want option 0 (the battlefield-order first duplicate)", in.Choices)
	}
	if err := e.Submit(in); err != nil {
		t.Fatalf("Submit(bot answer): %v", err)
	}
	if o := e.G.Obj(id1); o.Zone != state.ZBattlefield {
		t.Fatalf("bot-kept first duplicate in %v, want battlefield", o.Zone)
	}
	if o := e.G.Obj(id2); o.Zone != state.ZGraveyard {
		t.Fatalf("bot-kept board: second duplicate in %v, want graveyard", o.Zone)
	}
}
