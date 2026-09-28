package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The planar die (CR 901.3, task rollplanar1) pinned end to end on the REAL
// corpus cards: Fractured Powerstone's activated roll and Ichor Elixir's
// roll-plus-one-ignore-one replacement. These are the only two carriers in
// the corpus (measured with /usr/bin/grep over .cards/cardsfolder: 1 AB$
// RollPlanarDice line, 1 R:Event$ RollPlanarDice line), and neither is in
// any repo deck, so no chain head depends on them.
//
// Helpers come from token_replacement_test.go (tokenReplGame, moveSeededCard)
// and activate_test.go/cast_test.go (abilityOption, submitChoices, addMana,
// passUntilStackEmpty, replayCheck) — all the same package.

// planarRolls collects the log's PlanarRoll events in order.
func planarRolls(e *Engine) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.PlanarRoll {
			out = append(out, ev)
		}
	}
	return out
}

// planarDieNotes collects the log's per-die roll Notes.
func planarDieNotes(e *Engine) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "rolls the planar die") {
			out = append(out, ev)
		}
	}
	return out
}

// rollThePlanarDie activates the stone's second ability (index 1 — index 0
// is the {T}: Add {C} mana ability) and drains the stack. Cost$ T is paid by
// the activation itself, exactly like activateTokenForge's shape.
func rollThePlanarDie(t *testing.T, e *Engine, stone state.ObjID) {
	t.Helper()
	addMana(t, e, 0, "") // toMain1 + a fresh priority decision for seat 0
	opt := abilityOption(t, e, stone, 1)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
}

// assertSingleRoll checks the one PlanarRoll event the activation produced:
// one event per activation (never the "unimplemented API" Note), the
// post-replacement count in Amount, the kept results in IDs and the ignored
// count in Counter.
func assertSingleRoll(t *testing.T, e *Engine, stone state.ObjID, wantCount, wantKept int32, wantIgnore string) events.Event {
	t.Helper()
	rolls := planarRolls(e)
	if len(rolls) != 1 {
		t.Fatalf("got %d PlanarRoll events, want 1", len(rolls))
	}
	ev := rolls[0]
	if ev.Player != 0 || ev.Obj != stone {
		t.Fatalf("roll player=%d obj=%d, want seat 0 / stone %d", ev.Player, ev.Obj, stone)
	}
	if ev.Amount != wantCount {
		t.Fatalf("rolled %d dice, want %d", ev.Amount, wantCount)
	}
	if len(ev.IDs) != int(wantKept) {
		t.Fatalf("kept %d results (%v), want %d", len(ev.IDs), ev.IDs, wantKept)
	}
	for _, r := range ev.IDs {
		if r < 1 || r > 6 {
			t.Fatalf("planar die result %d outside 1..6", r)
		}
	}
	if ev.Counter != wantIgnore {
		t.Fatalf("ignored-count marker %q, want %q", ev.Counter, wantIgnore)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented") {
			t.Fatalf("the roll still hits the unimplemented fallback: %q", ev.Text)
		}
	}
	return ev
}

// TestFracturedPowerstoneRollsThePlanarDie is the activation: {T}: Roll the
// planar die, one PlanarRoll event per activation, one die rolled, its
// 1-6 result kept, no ignore marker and no replacement (no Ichor in play).
func TestFracturedPowerstoneRollsThePlanarDie(t *testing.T) {
	t.Parallel()
	stone := tokenReplCorpusCard(t, "Fractured Powerstone")
	e, cfg := tokenReplGame(t, 9204, stone)
	id := moveSeededCard(t, e, 0, stone, state.ZBattlefield)
	rollThePlanarDie(t, e, id)

	ev := assertSingleRoll(t, e, id, 1, 1, "")
	notes := planarDieNotes(e)
	if len(notes) != 1 {
		t.Fatalf("got %d die-roll Notes, want 1", len(notes))
	}
	face := planarDieFaceName(int32(ev.IDs[0]))
	if face != "blank" && face != "planeswalk" && face != "chaos" {
		t.Fatalf("die face %q is not a planar-die face", face)
	}
	if face == "blank" && !strings.Contains(notes[0].Text, "blank") {
		t.Fatalf("die-roll Note %q does not name the rolled face", notes[0].Text)
	}
	replayCheck(t, e, cfg)
}

// TestIchorElixirRollsOneMoreAndIgnoresOne is the replacement: with Ichor
// Elixir on its controller's battlefield, the stone's roll becomes two dice
// with one ignored — Amount 2 (the rewrite), exactly one kept result in IDs,
// the ignored count recorded on the event and the log carrying both die-roll
// Notes plus the ignore Note.
func TestIchorElixirRollsOneMoreAndIgnoresOne(t *testing.T) {
	t.Parallel()
	stone := tokenReplCorpusCard(t, "Fractured Powerstone")
	elixir := tokenReplCorpusCard(t, "Ichor Elixir")
	e, cfg := tokenReplGame(t, 9205, stone, elixir)
	id := moveSeededCard(t, e, 0, stone, state.ZBattlefield)
	moveSeededCard(t, e, 0, elixir, state.ZBattlefield)
	rollThePlanarDie(t, e, id)

	assertSingleRoll(t, e, id, 2, 1, "1")
	notes := planarDieNotes(e)
	if len(notes) != 2 {
		t.Fatalf("got %d die-roll Notes, want 2", len(notes))
	}
	ignoreNotes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "ignores 1 planar-dice result") {
			ignoreNotes++
		}
	}
	if ignoreNotes != 1 {
		t.Fatalf("got %d ignore Notes, want 1", ignoreNotes)
	}
	replayCheck(t, e, cfg)
}

// TestIchorElixirValidPlayerScopesTheRoll pins the R: line's ValidPlayer$ You
// gate: Ichor Elixir under seat 1 does not rewrite seat 0's roll — the event
// stays a bare one-die roll with no ignore marker.
func TestIchorElixirValidPlayerScopesTheRoll(t *testing.T) {
	t.Parallel()
	stone := tokenReplCorpusCard(t, "Fractured Powerstone")
	elixir := tokenReplCorpusCard(t, "Ichor Elixir")
	e, cfg := tokenReplGameSeats(t, 9206, []*cards.Card{stone}, []*cards.Card{elixir})
	id := moveSeededCard(t, e, 0, stone, state.ZBattlefield)
	moveSeededCard(t, e, 1, elixir, state.ZBattlefield)
	rollThePlanarDie(t, e, id)

	assertSingleRoll(t, e, id, 1, 1, "")
	replayCheck(t, e, cfg)
}
