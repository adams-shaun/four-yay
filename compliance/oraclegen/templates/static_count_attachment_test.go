package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// The four computed-count rows the static template's count fixtures serve
// (ticket levelb-static-count-attachments, class G7 "static effect
// unobservable"): a count-bearing amount the bare turn leaves at zero. Each
// row's fixture supplies the state the count reads and the serving loop
// asserts a real change on top of the fixture's own contribution, so a row
// the engine stopped counting still fails here. TestStaticNotObservableShapes
// pins the earlier paths' shapes; this file pins the attachment, crew and
// Craft preludes.
func TestStaticCountAttachmentFixtures(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	gen := func(t *testing.T, card, key string) oraclegen.Item {
		t.Helper()
		c, ok := reg.Lookup(card)
		if !ok {
			t.Fatalf("%s absent from the corpus", card)
		}
		for _, r := range levelb.Requirements(c) {
			if r.Key == key && r.Sub == "static.continuous" {
				it, skip := GenerateB(reg, card, r)
				if skip != nil {
					t.Fatalf("%s %s skipped: %s", card, key, skip.Reason)
				}
				return it
			}
		}
		t.Fatalf("%s: no static.continuous requirement %s", card, key)
		return oraclegen.Item{}
	}
	// replay runs the scenario and returns the final snapshot's permanents.
	replay := func(t *testing.T, card, key string, it oraclegen.Item) []rules.OracleSnapPerm {
		t.Helper()
		res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
		if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
			t.Fatalf("%s %s does not replay: %v fails=%v", card, key, err, res.Fails)
		}
		return res.Snapshots[len(res.Snapshots)-1].Permanents
	}
	find := func(ps []rules.OracleSnapPerm, name string) (rules.OracleSnapPerm, bool) {
		for _, p := range ps {
			if p.Name == name {
				return p, true
			}
		}
		return rules.OracleSnapPerm{}, false
	}
	// stepOf finds the i-th step whose op matches, ok=false when none.
	stepOf := func(it oraclegen.Item, op string, i int) (oraclegen.Step, bool) {
		n := 0
		for _, st := range it.Scenario.Steps {
			if st.Op != op {
				continue
			}
			if n == i {
				return st, true
			}
			n++
		}
		return oraclegen.Step{}, false
	}

	t.Run("The Last Ride: the crew makes the -X/-X life static print", func(t *testing.T) {
		it := gen(t, "The Last Ride", "static#0.0")
		// Precondition: the fixture set p0's starting life below the printed
		// power, so the count the static subtracts (12) leaves 1/1 and the
		// scenario poses the crew activation. Without the life the crewed
		// Vehicle would be 13/13 (unobservable) or -7/-7 (swept).
		life := it.Scenario.Setup["p0"].Life
		if life == nil || *life != 12 {
			t.Fatalf("precondition: p0 life = %v, want 12", life)
		}
		crew, ok := stepOf(it, "activate", 0)
		if !ok || crew.AbilityIndex == nil || crew.Card != "p0:The Last Ride" {
			t.Fatalf("precondition: no crew activation step in %v", it.Scenario.Steps)
		}
		// The XMage rule-text prefix labels the activation; without it the
		// driver's replay cannot match the crew.
		if got := xabAt(it, len(it.Scenario.Steps)-2); got != "Crew 2" {
			t.Fatalf("the crew activation's XMage prefix = %q, want \"Crew 2\"", got)
		}
		ps := replay(t, "The Last Ride", "static#0.0", it)
		ride, ok := find(ps, "The Last Ride")
		if !ok {
			t.Fatalf("the crewed Vehicle is not on the battlefield: %v", ps)
		}
		if got, want := ride.PT, "1/1"; got != want {
			t.Fatalf("the crewed Vehicle's P/T = %q, want %q (printed 13/13 minus life 12)", got, want)
		}
	})

	t.Run("Winter Soldier: the attached-Equipment count pumps the card", func(t *testing.T) {
		it := gen(t, "Winter Soldier, Icy Assassin", "static#0.0")
		attach, ok := stepOf(it, "attach", 0)
		if !ok || attach.Card != "p0:Bonesplitter" || attach.AttachedTo != "p0:Winter Soldier, Icy Assassin" {
			t.Fatalf("precondition: no Bonesplitter attach onto the card in %v", it.Scenario.Steps)
		}
		ps := replay(t, "Winter Soldier, Icy Assassin", "static#0.0", it)
		ws, ok := find(ps, "Winter Soldier, Icy Assassin")
		if !ok {
			t.Fatalf("the card is not on the battlefield: %v", ps)
		}
		// 2/2 printed + 2/0 the Equipment itself grants + 2/0 the static's
		// Count$Valid Equipment.Attached/Times.2 adds: 6/2. A count the
		// engine stopped reading leaves 4/2 and fails here.
		if got, want := ws.PT, "6/2"; got != want {
			t.Fatalf("the equipped card's P/T = %q, want %q", got, want)
		}
	})

	t.Run("Kellan: the attached-Equipment count pumps the other creatures", func(t *testing.T) {
		it := gen(t, "Kellan, the Fae-Blooded", "static#0.0")
		attach, ok := stepOf(it, "attach", 0)
		if !ok || attach.Card != "p0:Bonesplitter" || attach.AttachedTo != "p0:Kellan, the Fae-Blooded" {
			t.Fatalf("precondition: no Bonesplitter attach onto the card in %v", it.Scenario.Steps)
		}
		ps := replay(t, "Kellan, the Fae-Blooded", "static#0.0", it)
		bear, ok := find(ps, "Grizzly Bears")
		if !ok {
			t.Fatalf("precondition: p0's probe Bear is not on the battlefield: %v", ps)
		}
		// The static's count is the ONE Equipment attached to CARDNAME (not
		// Times.2), so the other creature you control is +1/+0: 3/2.
		if got, want := bear.PT, "3/2"; got != want {
			t.Fatalf("the other creature's P/T = %q, want %q", got, want)
		}
		kellan, ok := find(ps, "Kellan, the Fae-Blooded")
		if !ok {
			t.Fatalf("the card is not on the battlefield: %v", ps)
		}
		// Kellan himself is out of the static's Affected$ (Other): the
		// equipment's own +2/+0 alone, 4/2.
		if got, want := kellan.PT, "4/2"; got != want {
			t.Fatalf("the card's own P/T = %q, want %q", got, want)
		}
	})

	t.Run("Sunbird Standard: the Craft prelude populates the ExiledWith colour count", func(t *testing.T) {
		it := gen(t, "Sunbird Standard", "static#1.0")
		c, _ := reg.Lookup("Sunbird Standard")
		if len(c.Faces) < 2 {
			t.Fatal("precondition: Sunbird Standard is not double-faced")
		}
		// Precondition: the scenario casts the front face and activates its
		// Craft ({5} plus the material exile).
		if got := xabAt(it, len(it.Scenario.Steps)-2); got == "" || !strings.Contains(strings.ToLower(got), "craft") {
			t.Fatalf("the craft activation's XMage prefix = %q, want a Craft line", got)
		}
		craft, ok := stepOf(it, "activate", 0)
		if !ok || craft.Mana != "CCCCC" {
			t.Fatalf("precondition: no {5} craft activation in %v", it.Scenario.Steps)
		}
		ps := replay(t, "Sunbird Standard", "static#1.0", it)
		eff, ok := find(ps, "Sunbird Effigy")
		if !ok {
			// A zero colour count reads 0/0 and the SBA sweeps the Effigy:
			// the missing permanent IS the failing assertion.
			t.Fatalf("the transformed Effigy is not on the battlefield: %v", ps)
		}
		// One colour among the exiled craft material (the green catalogue
		// permanent) and the colorless source: 1/1.
		if got, want := eff.PT, "1/1"; got != want {
			t.Fatalf("the Effigy's CDA P/T = %q, want %q", got, want)
		}
	})
}

// xabAt is the item's XMage rule-text prefix for step i ("" outside the
// table).
func xabAt(it oraclegen.Item, i int) string {
	if i < 0 || i >= len(it.XAbility) {
		return ""
	}
	return it.XAbility[i]
}
