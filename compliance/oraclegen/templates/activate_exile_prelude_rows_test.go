// The four activate rows whose level-B scenario needs an extra prelude the
// bare fixture cannot supply (ticket agent-20261010T044009Z-fb50a4ce):
//
//   - SPM Urban Retreat: a "Return<1/Creature.tapped>" cost's creature is
//     tapped by a real tap spell, because setup's Tapped list is undone by
//     the genesis untap step;
//   - FIN Phoenix Down: an activated AB$ Charm's mode/target are chosen at
//     RESOLUTION, so the fixture seeds a legal target for a mode and the
//     activate step carries none;
//   - LCI Pit of Offerings: the reflect ability reads the cards a REAL ETB
//     trigger exiled, so the source is played from hand and untapped;
//   - LCI Sunbird Standard: a back-face requirement is reached by crafting
//     the front face, so the ExiledWith set is populated.
//
// Each subtest asserts the PRECONDITION its effect depends on, then that the
// scenario plays through gorge with zero fails and the final snapshot shows
// the effect. Kept in its own file so the ticket cannot conflict on a shared
// test file.
package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// activateExilePreludeRow is one row this file pins: its card, requirement key
// and the sub-family the requirement must land in.
type activateExilePreludeRow struct {
	name, key, sub string
}

func TestActivateExilePreludeRows(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	gen := func(t *testing.T, row activateExilePreludeRow) oraclegen.Item {
		t.Helper()
		c, ok := reg.Lookup(row.name)
		if !ok {
			t.Fatalf("precondition: %q is not in the corpus", row.name)
		}
		for _, r := range levelb.Requirements(c) {
			if r.Key != row.key {
				continue
			}
			if r.Family != "activate" {
				t.Fatalf("precondition: %s %s family = %q, want activate", row.name, row.key, r.Family)
			}
			if r.Sub != row.sub {
				t.Fatalf("precondition: %s %s sub = %q, want %q", row.name, row.key, r.Sub, row.sub)
			}
			it, skip := GenerateB(reg, row.name, r)
			if skip != nil {
				t.Fatalf("%s %s skipped: %s", row.name, row.key, skip.Reason)
			}
			return it
		}
		t.Fatalf("precondition: %s has no %s requirement", row.name, row.key)
		return oraclegen.Item{}
	}
	// play drives the scenario and returns the final snapshot, failing when
	// gorge cannot play it.
	play := func(t *testing.T, row activateExilePreludeRow, it oraclegen.Item) rules.OracleSnapshot {
		t.Helper()
		res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
		if !ok || len(res.Fails) != 0 {
			t.Fatalf("%s %s does not play through gorge: ok=%v fails=%v", row.name, row.key, ok, res.Fails)
		}
		if len(res.Snapshots) == 0 {
			t.Fatalf("%s %s produced no snapshots", row.name, row.key)
		}
		return res.Snapshots[len(res.Snapshots)-1]
	}
	// hasName reports whether names holds want.
	hasName := func(names []string, want string) bool {
		for _, n := range names {
			if n == want {
				return true
			}
		}
		return false
	}
	// perm returns the named battlefield permanent, ok=false when absent.
	perm := func(snap rules.OracleSnapshot, name string) (rules.OracleSnapPerm, bool) {
		for _, p := range snap.Permanents {
			if p.Name == name {
				return p, true
			}
		}
		return rules.OracleSnapPerm{}, false
	}
	// activateSteps lists every activate step in order.
	activateSteps := func(it oraclegen.Item) []oraclegen.Step {
		var out []oraclegen.Step
		for _, st := range it.Scenario.Steps {
			if st.Op == "activate" {
				out = append(out, st)
			}
		}
		return out
	}

	t.Run("SPM Urban Retreat: a real tap spell taps the Return cost's creature", func(t *testing.T) {
		row := activateExilePreludeRow{"Urban Retreat", "activate#0.1", "activate.hand"}
		it := gen(t, row)
		p0 := it.Scenario.Setup["p0"]
		// Precondition: the source starts in hand and the Return cost's
		// catalogue creature is on the battlefield but NOT in the setup Tapped
		// list -- that list is undone by the genesis untap step, which is the
		// bug this prelude works around.
		if !hasName(p0.Hand, "Urban Retreat") {
			t.Fatalf("precondition: Urban Retreat not in p0's hand: %v", p0.Hand)
		}
		if hasName(p0.Tapped, "Grizzly Bears") {
			t.Fatalf("precondition: Grizzly Bears still in p0's setup Tapped list %v", p0.Tapped)
		}
		if !hasName(p0.Hand, tapSpellProbe) {
			t.Fatalf("precondition: the tap spell %q not in p0's hand: %v", tapSpellProbe, p0.Hand)
		}
		castTap := false
		for _, st := range it.Scenario.Steps {
			if st.Op == "cast" && st.Card == "p0:"+tapSpellProbe {
				castTap = true
			}
		}
		if !castTap {
			t.Fatalf("precondition: no %s cast prelude in %v", tapSpellProbe, it.Scenario.Steps)
		}
		snap := play(t, row, it)
		// The ability put Urban Retreat onto the battlefield and the Return
		// cost returned the tapped creature to hand.
		if _, ok := perm(snap, "Urban Retreat"); !ok {
			t.Fatalf("Urban Retreat not on the battlefield: %v", snap.Permanents)
		}
		if !hasName(snap.Players[0].Hand, "Grizzly Bears") {
			t.Fatalf("the returned Grizzly Bears not in p0's hand: %v", snap.Players[0].Hand)
		}
	})

	t.Run("FIN Phoenix Down: an activated charm seeds a mode target", func(t *testing.T) {
		row := activateExilePreludeRow{"Phoenix Down", "activate#0.0", "activate.battlefield"}
		it := gen(t, row)
		p0 := it.Scenario.Setup["p0"]
		// Precondition: a legal creature card for the Return mode sits in p0's
		// graveyard, and the activate step carries no targets (the mode and
		// target are chosen at resolution).
		if !hasName(p0.Graveyard, "Grizzly Bears") {
			t.Fatalf("precondition: no legal creature card in p0's graveyard: %v", p0.Graveyard)
		}
		acts := activateSteps(it)
		if len(acts) != 1 {
			t.Fatalf("precondition: want one activate step, got %v", it.Scenario.Steps)
		}
		if len(acts[0].Targets) != 0 {
			t.Fatalf("precondition: the activate step carries targets %v; an activated charm chooses them at resolution", acts[0].Targets)
		}
		snap := play(t, row, it)
		// The chosen mode (1) returned the graveyard creature to the
		// battlefield tapped, and the artifact exiled itself as its cost.
		if !hasName(snap.Players[0].Exile, "Phoenix Down") {
			t.Fatalf("Phoenix Down not in p0's exile: %v", snap.Players[0].Exile)
		}
		bears, ok := perm(snap, "Grizzly Bears")
		if !ok || bears.Controller != 0 || !bears.Tapped {
			t.Fatalf("the returned Grizzly Bears = %+v, want a tapped creature controlled by p0", bears)
		}
	})

	t.Run("LCI Pit of Offerings: a real ETB exile feeds the reflect ability", func(t *testing.T) {
		row := activateExilePreludeRow{"Pit of Offerings", "activate#0.1", "activate.battlefield"}
		it := gen(t, row)
		p0 := it.Scenario.Setup["p0"]
		// Precondition: the source starts in hand (so it can be played for
		// real) with a graveyard card, and the play step targets that card.
		if !hasName(p0.Hand, "Pit of Offerings") {
			t.Fatalf("precondition: Pit of Offerings not in p0's hand: %v", p0.Hand)
		}
		if !hasName(p0.Graveyard, "Grizzly Bears") {
			t.Fatalf("precondition: no graveyard card for the ETB to exile: %v", p0.Graveyard)
		}
		playStep := false
		passTo := 0
		for _, st := range it.Scenario.Steps {
			if st.Op == "play" && st.Card == "p0:Pit of Offerings" {
				if !hasName(st.Targets, "p0:Grizzly Bears") {
					t.Fatalf("precondition: the play step does not target the graveyard card: %v", st.Targets)
				}
				playStep = true
			}
			if st.Op == "pass_to" {
				passTo++
			}
		}
		if !playStep {
			t.Fatalf("precondition: no play step for Pit of Offerings in %v", it.Scenario.Steps)
		}
		if passTo < 2 {
			t.Fatalf("precondition: want the two pass_to steps that untap the land, got %d", passTo)
		}
		snap := play(t, row, it)
		// The ETB exiled the green creature; the reflect ability then added a
		// green mana of its colour.
		if !hasName(snap.Players[0].Exile, "Grizzly Bears") {
			t.Fatalf("the exiled Grizzly Bears not in p0's exile: %v", snap.Players[0].Exile)
		}
		if snap.Players[0].Pool == "" || !containsRune(snap.Players[0].Pool, 'G') {
			t.Fatalf("p0's pool = %q, want it to hold the exiled card's colour G", snap.Players[0].Pool)
		}
	})

	t.Run("LCI Sunbird Standard: the Craft prelude populates the ExiledWith set", func(t *testing.T) {
		row := activateExilePreludeRow{"Sunbird Standard", "activate#1.0", "activate.mana"}
		it := gen(t, row)
		p0 := it.Scenario.Setup["p0"]
		// Precondition: the card is NOT marked as a back-face setup (that
		// shortcut never ran the Craft), a craft material sits in the
		// graveyard, and a craft activate step precedes the ability under test.
		if hasName(p0.BackFace, "Sunbird Standard") {
			t.Fatalf("precondition: Sunbird Standard is still a back-face setup %v", p0.BackFace)
		}
		if !hasName(p0.Graveyard, "Colossal Dreadmaw") {
			t.Fatalf("precondition: no craft material in p0's graveyard: %v", p0.Graveyard)
		}
		acts := activateSteps(it)
		if len(acts) != 2 || acts[0].Card != "p0:Sunbird Standard" || acts[1].Card != "p0:Sunbird Standard" {
			t.Fatalf("precondition: want the craft activation then the ability under test, got %v", it.Scenario.Steps)
		}
		if xab := it.XAbility[0]; xab == "" {
			t.Fatalf("precondition: the craft activation has no XMage prefix: %v", it.XAbility)
		}
		snap := play(t, row, it)
		// The crafted card transformed; the back face's ability added a green
		// mana of the exiled material's colour.
		if eff, ok := perm(snap, "Sunbird Effigy"); !ok {
			t.Fatalf("the transformed Sunbird Effigy is not on the battlefield: %v", snap.Permanents)
		} else if eff.PT != "1/1" {
			t.Fatalf("Sunbird Effigy P/T = %q, want 1/1 (one colour among the exiled material)", eff.PT)
		}
		if !hasName(snap.Players[0].Exile, "Colossal Dreadmaw") {
			t.Fatalf("the craft material not in p0's exile: %v", snap.Players[0].Exile)
		}
		if snap.Players[0].Pool == "" || !containsRune(snap.Players[0].Pool, 'G') {
			t.Fatalf("p0's pool = %q, want it to hold the exiled material's colour G", snap.Players[0].Pool)
		}
	})
}

// containsRune reports whether s holds r.
func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
