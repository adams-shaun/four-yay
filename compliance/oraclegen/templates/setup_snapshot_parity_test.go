package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// Setup-and-snapshot parity rows (ticket cli-20261009T031409Z-a76de35a, the
// level-B "counters/speed in setup" and "saga lore at setup" classes): each
// subtest generates one row's item and replays it gorge-side, asserting the
// effect the scenario's comparison rests on. A subtest whose generator change
// regresses fails either at generation (the row's skip comes back) or at the
// replay assertion (the effect the scenario asserts vanishes).
//
//   - Extinguisher Battleship: a Spacecraft's station static is served by the
//     placed-card path; the card holds its gate counters and shows the granted
//     type and keyword.
//   - Training Regimen: an affected-permanent counter gate is served with the
//     probe holding the counters; the static's grant lands on the probe.
//   - The Astonishing Ant-Man: an announced SubCounter<X/P1P1> activation
//     announces X = 1, spends the seeded counter and creates that many tokens.
//   - Sunstar Chaplain: a SubCounter cost removing from a creature you control
//     is served with a fixture bearer; the payment removes its counter.
//   - The Legend of Kuruk: a back-face setup placement enters on its back
//     face, so the front face's Saga lore counter is not granted.

// requirementFor returns the named requirement key of the card's face 0, with
// loud preconditions: the card must exist and carry the requirement.
func requirementFor(t *testing.T, reg *cards.Registry, name, key string) levelb.Requirement {
	t.Helper()
	card, ok := reg.Lookup(name)
	if !ok || len(card.Faces) == 0 {
		t.Fatalf("precondition: %s missing from corpus", name)
	}
	for _, req := range levelb.Requirements(card) {
		if req.Key == key {
			return req
		}
	}
	t.Fatalf("precondition: %s has no %s requirement (the level-B census changed; re-pin this test)", name, key)
	panic("unreachable")
}

// generateServed generates one row's item, failing on a skip with the skip
// reason, and replays it gorge-side, failing on a replay error or a step fail.
func generateServed(t *testing.T, reg *cards.Registry, name, key string) (oraclegen.Item, rules.OracleResult) {
	t.Helper()
	req := requirementFor(t, reg, name, key)
	it, skip := GenerateB(reg, name, req)
	if skip != nil {
		t.Fatalf("static/activate row skip = %q, want a served scenario", skip.Reason)
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil {
		t.Fatalf("replay %s/%s: %v", name, key, err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("replay %s/%s failed: %v", name, key, res.Fails)
	}
	if len(res.Snapshots) == 0 {
		t.Fatalf("replay %s/%s produced no snapshots", name, key)
	}
	return it, res
}

func TestSetupSnapshotParity(t *testing.T) {
	reg := testutil.CorpusRegistry(t)

	t.Run("station gate on a Spacecraft with its own ETB", func(t *testing.T) {
		const name = "Extinguisher Battleship"
		_, res := generateServed(t, reg, name, "static#0.0")
		final := res.Snapshots[len(res.Snapshots)-1].Permanents
		var ship *rules.OracleSnapPerm
		for i := range final {
			if final[i].Name == name {
				ship = &final[i]
			}
		}
		if ship == nil {
			t.Fatal("precondition: the ship is not on the battlefield at the final checkpoint")
		}
		if ship.Counters["CHARGE"] < 5 {
			t.Fatalf("precondition: the gate counters are missing: %v", ship.Counters)
		}
		if ship.PT == "" {
			t.Fatal("the station static's AddType$ Creature did not make the ship a creature")
		}
		found := false
		for _, kw := range ship.Keywords {
			if kw == "Flying" {
				found = true
			}
		}
		if !found {
			t.Fatalf("the station static's Flying grant did not land; keywords %v", ship.Keywords)
		}
	})

	t.Run("affected-permanent counter gate", func(t *testing.T) {
		const name = "Training Regimen"
		_, res := generateServed(t, reg, name, "static#0.0")
		final := res.Snapshots[len(res.Snapshots)-1].Permanents
		countered := 0
		for i := range final {
			p := &final[i]
			if p.Name != "Grizzly Bears" {
				continue
			}
			if p.Counters["P1P1"] != 1 {
				t.Fatalf("precondition: a probe holds %v counters, want one P1P1", p.Counters)
			}
			countered++
			if p.Controller == 0 {
				found := false
				for _, kw := range p.Keywords {
					if kw == "Trample" {
						found = true
					}
				}
				if !found {
					t.Fatalf("the static's Trample grant did not land on the countered probe; keywords %v", p.Keywords)
				}
			} else {
				// The static is YouCtrl-only: the opponent's countered probe
				// must stay bare, or the comparison itself would diverge.
				if len(p.Keywords) != 0 {
					t.Fatalf("the static's grant crossed the controller line: %v", p.Keywords)
				}
			}
		}
		if countered != 2 {
			t.Fatalf("precondition: %d countered probes, want one on each seat", countered)
		}
	})

	t.Run("announced SubCounter X activation", func(t *testing.T) {
		const name = "The Astonishing Ant-Man"
		_, res := generateServed(t, reg, name, "activate#0.0")
		final := res.Snapshots[len(res.Snapshots)-1]
		tokens := 0
		for _, p := range final.Permanents {
			if p.Token {
				tokens++
			}
		}
		if tokens != 1 {
			t.Fatalf("the X = 1 activation created %d tokens, want exactly one Insect", tokens)
		}
		for _, p := range final.Permanents {
			if p.Name == name && p.Counters["P1P1"] != 0 {
				t.Fatalf("the activation did not spend the seeded +1/+1 counter: %v", p.Counters)
			}
		}
	})

	t.Run("counter removal from a creature you control", func(t *testing.T) {
		const name = "Sunstar Chaplain"
		_, res := generateServed(t, reg, name, "activate#0.0")
		final := res.Snapshots[len(res.Snapshots)-1]
		tapped := 0
		for _, p := range final.Permanents {
			if p.Name == "Grizzly Bears" && p.Counters["P1P1"] != 0 {
				t.Fatalf("the payment did not remove the bearer's +1/+1 counter: %v", p.Counters)
			}
			if p.Tapped {
				tapped++
			}
		}
		if tapped == 0 {
			t.Fatal("the ability's tap target never resolved tapped")
		}
	})

	t.Run("back-face placement enters on its back face", func(t *testing.T) {
		const name = "The Legend of Kuruk"
		card, ok := reg.Lookup(name)
		if !ok || len(card.Faces) < 2 {
			t.Fatal("precondition: the card is not a double-faced card")
		}
		_, front := cards.SagaChapters(card.Faces[0])
		_, back := cards.SagaChapters(card.Faces[1])
		if len(front) == 0 || len(back) != 0 {
			t.Fatal("precondition: face 0 is not the Saga and face 1 not a non-Saga")
		}
		_, res := generateServed(t, reg, name, "trigger#1.0")
		setup := res.Snapshots[0]
		if setup.Checkpoint != "setup" {
			t.Fatalf("precondition: first checkpoint is %q, want setup", setup.Checkpoint)
		}
		for _, p := range setup.Permanents {
			if p.Name != "Avatar Kuruk" {
				continue
			}
			if n := p.Counters["LORE"]; n != 0 {
				t.Fatalf("the back-face placement carries %d lore counters the entering face never earned", n)
			}
			return
		}
		t.Fatal("precondition: the transformed Avatar is not on the battlefield at setup")
	})
}
