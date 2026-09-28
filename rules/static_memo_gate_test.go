package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// registerToken is the static-cold token fixture the two gate-flip tests
// share: a plain 1/1 Goblin token carrying no static of its own, so its entry
// is exactly what staticSafeSince otherwise admits.
func registerToken(t *testing.T, e *Engine, key, src string) {
	t.Helper()
	if e.G.Tokens == nil {
		e.G.Tokens = make(map[string]*cards.Card)
	}
	e.G.Tokens[key] = card(t, src)
}

// TestStaticMemoDoesNotReuseAcrossGateFlippingTokenCreates pins the bug
// agent-20260928T171650Z-006e2611 fixes: a static-cold token's ARRIVAL can
// flip an existing Continuous static's IsPresent$ gate, so the memo must not
// be re-stamped across the TokenCreate even though the token itself carries
// no static. Bolg's Company's haste grant is off while it is the only Goblin;
// a Goblin token entering must make the rescan emit it. Without the fix the
// re-stamp serves the stale 0-effect list (and, in the rules test binary,
// layerInertVerify panics on the disagreement).
func TestStaticMemoDoesNotReuseAcrossGateFlippingTokenCreates(t *testing.T) {
	e := layerEngine(t)
	registerToken(t, e, "gob", "Name:Plain Goblin token\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n")

	// Bolg's Company (inline fixture of .cards/cardsfolder/b/bolgs_company.txt):
	// its IsPresent$ Goblin.Other+YouCtrl gate fails while it is the only Goblin.
	onBoard(t, e, 0, "Name:Bolg's Company\nManaCost:B R\nTypes:Creature Goblin Soldier\nPT:2/2\n"+
		"S:Mode$ Continuous | Affected$ Card.Self | AddKeyword$ Haste | IsPresent$ Goblin.Other+YouCtrl | "+
		"Description$ This creature has haste as long as you control another Goblin.\nOracle:x\n")

	e.refreshStaticContinuous()
	if len(e.staticContinuous) != 0 {
		t.Fatalf("precondition: memo built with %d effects before any other Goblin, want 0", len(e.staticContinuous))
	}
	if !e.staticMemoGated {
		t.Fatal("precondition: the gate-carrying static was not recorded as gated")
	}
	builds := e.staticBuildSeq

	got := e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: "gob"})
	if got.Kind != events.TokenCreate {
		t.Fatalf("precondition: TokenCreate emitted as %v", got.Kind)
	}
	created := &e.G.Objs[len(e.G.Objs)-1]
	if created.Zone != state.ZBattlefield || objectStaticHotOn(created) {
		t.Fatal("precondition: created token is not a static-cold battlefield object")
	}

	// With the fix the re-stamp is refused (a full rescan runs) and the list
	// carries the haste effect; the stale-serving bug serves 0 forever here.
	if len(e.staticContinuous) != 1 {
		t.Fatalf("after gate-flipping token entry: memo serves %d effects, want 1", len(e.staticContinuous))
	}
	if e.staticBuildSeq <= builds {
		t.Fatalf("gate-carrying board reused the memo across a TokenCreate: builds stayed at %d", builds)
	}
}

// TestStaticMemoDoesNotReuseWhenTokenTurnsGateOff is the opposite direction: a
// "as long as you control exactly one creature" gate passes on the initial
// board and flips OFF when the token arrives, so a stale memo would keep
// applying a grant that should end.
func TestStaticMemoDoesNotReuseWhenTokenTurnsGateOff(t *testing.T) {
	e := layerEngine(t)
	registerToken(t, e, "gob", "Name:Plain Goblin token\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n")

	// PresentCompare$ LE1 over the count of creatures you control: on the
	// one-creature board below the gate holds, so the +1/+0 is emitted.
	onBoard(t, e, 0, "Name:Lone Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\n"+
		"S:Mode$ Continuous | Affected$ Card.Self | AddPower$ 1 | IsPresent$ Creature.YouCtrl | PresentCompare$ LE1 | "+
		"Description$ gets +1/+0 as long as you control exactly one creature.\nOracle:x\n")

	e.refreshStaticContinuous()
	if len(e.staticContinuous) != 1 {
		t.Fatalf("precondition: memo built with %d effects on the one-creature board, want 1", len(e.staticContinuous))
	}
	builds := e.staticBuildSeq

	got := e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: "gob"})
	if got.Kind != events.TokenCreate {
		t.Fatalf("precondition: TokenCreate emitted as %v", got.Kind)
	}
	created := &e.G.Objs[len(e.G.Objs)-1]
	if created.Zone != state.ZBattlefield || objectStaticHotOn(created) {
		t.Fatal("precondition: created token is not a static-cold battlefield object")
	}

	// The token breaks LE1, so a fresh rescan emits nothing.
	if len(e.staticContinuous) != 0 {
		t.Fatalf("after gate-flipping token entry: memo serves %d effects, want 0", len(e.staticContinuous))
	}
	if e.staticBuildSeq <= builds {
		t.Fatalf("gate-carrying board reused the memo across a TokenCreate: builds stayed at %d", builds)
	}
}
