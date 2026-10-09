package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// The d8b-zones level-B rows (ticket cli-20261009T105445Z-3420f4d1) whose
// gorge half this round fixed. Each test generates the same level-B
// requirement the diverge row carries and plays it through
// rules.RunOracleScenarioJSON, asserting the state the comparator compares:
// the card the row names moved where the Oracle text sends it.
//
//   - Ojer Pakpatiq, Deepest Epoch (trigger#0.0/#0.1): the resolving instant
//     is exiled, not graveyarded -- its cast trigger pumps the Rebound
//     keyword onto the stack spell (CR 702.95a).
//   - Goliath Daydreamer (trigger#0.0): the resolving instant is exiled with
//     a dream counter instead -- an Effect-delivered Event$ Moved redirect
//     (Fizzle$ False, ReplaceWith$ a ChangeZone-to-exile body).
//   - Hedge Shredder (trigger#0.0): the land cards its attack trigger milled
//     from the library into the graveyard go onto the battlefield tapped --
//     ChangeZoneAll's ChangeType$ Card.TriggeredCards referent.

// d8bPlay generates the requirement's scenario and replays it, requiring a
// clean run with at least one snapshot.
func d8bPlay(t *testing.T, reg *cards.Registry, name, key string) rules.OracleResult {
	t.Helper()
	it, skip := GenerateB(reg, name, d8Req(t, reg, name, key))
	if skip != nil {
		t.Fatalf("%s %s: %s", name, key, skip.Reason)
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		t.Fatalf("%s %s does not replay: err=%v fails=%v", name, key, err, res.Fails)
	}
	return res
}

// d8bCastStep asserts the spell's cast step left the stack holding the spell
// and its cast trigger (the precondition every exile assertion below reads:
// the spell really resolved, so its resting zone is the effect's doing).
func d8bCastStep(t *testing.T, res rules.OracleResult, card string) {
	t.Helper()
	var cast rules.OracleSnapshot
	for _, s := range res.Snapshots {
		if len(s.Stack) > 0 {
			cast = s
		}
	}
	if len(cast.Stack) == 0 {
		t.Fatalf("precondition: %s never held a stack item at any checkpoint", card)
	}
}

// TestD8BReboundGrantExilesTheSpell covers Ojer Pakpatiq's two trigger rows:
// the resolving instant must reach exile, never the graveyard.
func TestD8BReboundGrantExilesTheSpell(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, key, spell string
	}{
		{"Ojer Pakpatiq, Deepest Epoch", "trigger#0.0", "Shock"},
		{"Ojer Pakpatiq, Deepest Epoch", "trigger#0.1", "Murder"},
	} {
		res := d8bPlay(t, reg, tc.name, tc.key)
		d8bCastStep(t, res, tc.name)
		last := res.Snapshots[len(res.Snapshots)-1]
		for _, p := range last.Players {
			if p.Seat != 0 {
				continue
			}
			if len(p.Graveyard) != 0 {
				t.Errorf("%s %s: p0.graveyard = %v, want empty (the spell gained rebound)", tc.name, tc.key, p.Graveyard)
			}
			found := false
			for _, e := range p.Exile {
				if e == tc.spell {
					found = true
				}
			}
			if !found {
				t.Errorf("%s %s: p0.exile = %v, want it to hold the exiled %s", tc.name, tc.key, p.Exile, tc.spell)
			}
		}
	}
}

// TestD8BDreamCounterRedirectExilesTheSpell covers Goliath Daydreamer's
// trigger row: the resolving instant is exiled with a dream counter via the
// Effect-delivered Moved redirect, never graveyarded.
func TestD8BDreamCounterRedirectExilesTheSpell(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name, key, spell = "Goliath Daydreamer", "trigger#0.0", "Shock"
	res := d8bPlay(t, reg, name, key)
	d8bCastStep(t, res, name)
	last := res.Snapshots[len(res.Snapshots)-1]
	for _, p := range last.Players {
		if p.Seat != 0 {
			continue
		}
		if len(p.Graveyard) != 0 {
			t.Errorf("%s %s: p0.graveyard = %v, want empty (the spell was exiled instead)", name, key, p.Graveyard)
		}
		found := false
		for _, e := range p.Exile {
			if e == spell {
				found = true
			}
		}
		if !found {
			t.Errorf("%s %s: p0.exile = %v, want it to hold the exiled %s", name, key, p.Exile, spell)
		}
	}
	if last.Players[1].Life != 18 {
		t.Errorf("%s %s: p1.life = %d, want 18 (the Shock resolved for its damage)", name, key, last.Players[1].Life)
	}
}

// TestD8BMilledLandsEnterTapped covers Hedge Shredder's trigger row: the two
// milled land cards end the attack on the battlefield, tapped, and the
// graveyard stays empty.
func TestD8BMilledLandsEnterTapped(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name, key = "Hedge Shredder", "trigger#0.0"
	res := d8bPlay(t, reg, name, key)
	d8bCastStep(t, res, name)
	last := res.Snapshots[len(res.Snapshots)-1]
	if got := last.Players[0].LibraryCount; got != 36 {
		t.Errorf("%s %s: p0.library_count = %d, want 36 (the attack trigger milled two of the 38)", name, key, got)
	}
	if got := last.Players[0].Graveyard; len(got) != 0 {
		t.Errorf("%s %s: p0.graveyard = %v, want empty (the milled lands entered the battlefield)", name, key, got)
	}
	milled := 0
	for _, p := range last.Permanents {
		if p.Name != "Wastes" {
			continue
		}
		milled++
		if !p.Tapped {
			t.Errorf("%s %s: milled %s entered untapped", name, key, p.Name)
		}
		if p.Controller != 0 {
			t.Errorf("%s %s: milled %s is under p%d, want p0", name, key, p.Name, p.Controller)
		}
	}
	if milled != 2 {
		t.Errorf("%s %s: %d milled Wastes on the battlefield, want 2", name, key, milled)
	}
}
