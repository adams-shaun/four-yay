// Tests for the two cumulative-upkeep remainders the AGENTS.md closing row
// named: a snow {S} upkeep cost must be a real, per-age-counter snow
// requirement (CR 702.24a / CR 107.4h), and a dynamically granted cumulative
// upkeep (a layer-6 AddKeyword$ or an A:AB$ Pump's KW$ grant) must run through
// the same rules/cumulative.go machinery a printed K:Cumulative upkeep line
// does. Kept in its own file so the ticket cannot conflict on a shared test
// file.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// coverOfWinterEngine seeds seat 0's library with the REAL corpus Cover of
// Winter (K:Cumulative upkeep:S) and returns the engine and its id once it is
// on the battlefield. The brief names no specific card; this is the corpus's
// snow cumulative-upkeep carrier.
func coverOfWinterEngine(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cover, ok := reg.Lookup("Cover of Winter")
	if !ok {
		t.Fatal("corpus fixture: Cover of Winter missing")
	}
	e := New(Config{Seed: 7, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append([]*cards.Card{cover}, mountainDeck(t, 39)...), mountainDeck(t, 40)}})
	e.Advance()
	toMain1(t, e)
	id := moveByName(t, e, 0, "Cover of Winter", state.ZBattlefield)
	return e, id
}

// resolveUpkeepCumulative drives one beginning-of-upkeep StepChange, places
// the queued cumulative-upkeep trigger on the stack and resolves it, leaving
// the engine at the resolution-time payment ask. It fails if no
// CumulativeUpkeep ability reached the stack, so a missing trigger can never
// masquerade as a resolved one.
func resolveUpkeepCumulative(t *testing.T, e *Engine) {
	t.Helper()
	e.G.Active, e.G.Priority = 0, 0
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	idx := -1
	for i, sid := range e.G.Stack {
		if o := e.G.Obj(sid); o != nil && o.Ability != nil && o.Ability.API == "CumulativeUpkeep" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("cumulative upkeep was not placed as a triggered ability: stack=%v", e.G.Stack)
	}
	e.resolveTop()
}

// cumulativeGrantStatic is a layer-6 AddKeyword$ Cumulative upkeep:2 grant --
// the Breath of Dreams / Mana Chains shape. The granting static is an
// Enchantment on the battlefield; the affected creature is a separate object.
const cumulativeGrantStatic = "Name:Cumulus Cover\nManaCost:0\nTypes:Enchantment\n" +
	"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddKeyword$ Cumulative upkeep:2 | Description$ Creatures you control have cumulative upkeep {2}.\nOracle:x\n"

// cumulativeGrantPump is an activated A:AB$ Pump with a KW$ Cumulative
// upkeep:1 grant -- the Balduvian Shaman / Dreams of the Dead shape (the brief
// names KW$ Cumulative upkeep:...). It grants to its own source so a test can
// activate it without a target decision.
const cumulativeGrantPump = "Name:Cumulus Idol\nManaCost:0\nTypes:Artifact\n" +
	"A:AB$ Pump | Cost$ 0 | Defined$ Self | KW$ Cumulative upkeep:1 | Duration$ Permanent | SpellDescription$ CARDNAME gains cumulative upkeep {1}.\nOracle:x\n"

// TestGrantedCumulativeUpkeepStaticGrantTriggers is the grant half of the row:
// a creature granted Cumulative upkeep by a layer-6 AddKeyword$ must accrue an
// age counter and pose the pay/sacrifice ask at its controller's upkeep,
// exactly as a printed K:Cumulative upkeep line does. Before the fix the
// keyword sat in the derived list with no Phase trigger to carry it, so the
// upkeep simply passed.
func TestGrantedCumulativeUpkeepStaticGrantTriggers(t *testing.T) {
	t.Parallel()
	const bear = "Name:Testbear\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e := handEngine(t)
	_ = onBoard(t, e, 0, cumulativeGrantStatic)
	bearID := onBoard(t, e, 0, bear)
	// Precondition: the grant is live in the DERIVED keyword list while the
	// creature's printed face does NOT carry it -- so the trigger below can
	// only come from the synthesis, never the printed expansion.
	if !e.HasKeyword(bearID, "Cumulative upkeep") {
		t.Fatal("precondition: granted cumulative upkeep is not in the derived keyword list")
	}
	if e.G.Obj(bearID).Face().HasKeyword("Cumulative upkeep") {
		t.Fatal("precondition: the test creature prints cumulative upkeep; it must only be granted")
	}
	resolveUpkeepCumulative(t, e)
	if got := e.G.Obj(bearID).Counter("AGE"); got != 1 {
		t.Fatalf("granted cumulative upkeep placed %d age counters, want 1", got)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no cumulative pay/sacrifice ask for the granted keyword: %+v", d)
	}
	if optionKinds(d)["cumulative_sac"] != 1 {
		t.Fatalf("sacrifice option missing from the ask: %+v", d.Options)
	}
}
