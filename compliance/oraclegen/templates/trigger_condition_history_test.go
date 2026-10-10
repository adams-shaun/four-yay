package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestTriggerConditionHistoryPreludes: an intervening-if that counts this
// turn's events is served with n distinct events, and gorge puts the trigger on
// the stack; the two gates with no cause keep a named skip.
func TestTriggerConditionHistoryPreludes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key string
		// want checks the served scenario's steps carry the events.
		want func(t *testing.T, sc oraclegen.Scenario)
	}{
		{"Bloodline Recollector // Ancestral Craving", "trigger#0.0", func(t *testing.T, sc oraclegen.Scenario) {
			// GE3: three removal spells on three distinct creatures.
			castsDistinct(t, sc, 3, "Murder", "Doom Blade", "Go for the Throat")
		}},
		{"Darklight Phoenix", "trigger#0.0", func(t *testing.T, sc oraclegen.Scenario) {
			castsDistinct(t, sc, 2, "Murder", "Doom Blade")
		}},
		{"Lasting Tarfire", "trigger#0.0", func(t *testing.T, sc oraclegen.Scenario) { castsDistinct(t, sc, 1, "Battlegrowth") }},
		{"Fractal Tender", "trigger#0.0", func(t *testing.T, sc oraclegen.Scenario) {
			// Card.Self: the counter goes on the source itself.
			castsDistinct(t, sc, 1, "Battlegrowth")
			for _, st := range sc.Steps {
				if st.Op == "cast" && (len(st.Targets) != 1 || st.Targets[0] != "p0:Fractal Tender") {
					t.Fatalf("Battlegrowth target = %v, want the source", st.Targets)
				}
			}
		}},
		{"Primary Research", "trigger#0.1", func(t *testing.T, sc oraclegen.Scenario) { castsDistinct(t, sc, 1, "Raise Dead", "Disentomb") }},
		{"Living History", "trigger#0.1", func(t *testing.T, sc oraclegen.Scenario) {
			castsDistinct(t, sc, 1, "Raise Dead", "Disentomb")
			// The attacker and the graveyard card are different names.
			for _, g := range sc.Setup["p0"].Graveyard {
				if containsString(sc.Setup["p0"].Battlefield, g) {
					t.Fatalf("%s is in both the graveyard and on the battlefield", g)
				}
			}
			if len(sc.Setup["p0"].Graveyard) == 0 {
				t.Fatal("precondition: no card in the graveyard to leave it")
			}
		}},
		{"Lunar Convocation", "trigger#0.1", func(t *testing.T, sc oraclegen.Scenario) {
			// "if you gained and lost life this turn": Angel's Mercy then Shock.
			castsDistinct(t, sc, 2, "Angel's Mercy", "Shock")
		}},
		{"Avengers Assemble!", "trigger#0.0", func(t *testing.T, sc oraclegen.Scenario) {
			castsDistinct(t, sc, 1, "Brave Brawler", "Pet Avengers", "Guerrilla Gorilla")
		}},
		{"Ennis, Debate Moderator", "trigger#0.1", func(t *testing.T, sc oraclegen.Scenario) {
			castsDistinct(t, sc, 1, "Swords to Plowshares", "Path to Exile")
		}},
		{"Stromkirk Bloodthief", "trigger#0.0", func(t *testing.T, sc oraclegen.Scenario) {
			castsDistinct(t, sc, 1, "Shock", "Lightning Bolt", "Lava Spike")
		}},
		{"Case of the Gateway Express", "trigger#0.1", func(t *testing.T, sc oraclegen.Scenario) {
			atk := attackStep(t, sc.Steps).Attackers
			if len(atk) < 3 {
				t.Fatalf("attackers = %v, want three (SVarCompare GE3)", atk)
			}
		}},
		{"Reverberating Summons", "trigger#0.0", func(t *testing.T, sc oraclegen.Scenario) {
			// GE2: two spells, never the same name twice.
			castsDistinct(t, sc, 2, "Grizzly Bears", "Llanowar Elves", "Shock", "Lightning Bolt", "Divination")
		}},
		{"Tunnel Tipster", "trigger#0.0", func(t *testing.T, sc oraclegen.Scenario) {
			// "if a face-down creature entered the battlefield under your
			// control this turn": the cause face-down casts a Disguise probe
			// for {3}; the entry itself is the face-down permanent.
			castsFaceDown(t, sc, "Bolrac-Clan Basher", "disguised")
		}},
		{"Spider-Man 2099", "trigger#0.0", func(t *testing.T, sc oraclegen.Scenario) {
			// "if you've ... cast a spell this turn from anywhere other than
			// your hand": the probe's own static grants a cast from exile.
			if len(sc.Setup["p0"].Exile) == 0 {
				t.Fatal("precondition: no card in exile for a cast-from-exile cause")
			}
			castsDistinct(t, sc, 1, "Misthollow Griffin", "Eternal Scourge")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerItem(t, reg, tc.name, tc.key)
			tc.want(t, it.Scenario)
		})
	}
}

// castsDistinct asserts the scenario casts n spells, each of the named pool,
// no name twice (a second cast of one name is an ambiguous reference).
func castsDistinct(t *testing.T, sc oraclegen.Scenario, n int, pool ...string) {
	t.Helper()
	seen := map[string]bool{}
	for _, st := range sc.Steps {
		if st.Op != "cast" {
			continue
		}
		if seen[st.Card] {
			t.Fatalf("%s is cast twice: %+v", st.Card, sc.Steps)
		}
		seen[st.Card] = true
	}
	in := 0
	for _, p := range pool {
		if seen["p0:"+p] {
			in++
		}
	}
	if in < n {
		t.Fatalf("scenario casts %v, want %d of %v", seen, n, pool)
	}
}

// castsFaceDown asserts the scenario face-down casts card for {3} with mode
// (a morph/disguise cast), the cause a face-down entry count reads.
func castsFaceDown(t *testing.T, sc oraclegen.Scenario, card, mode string) {
	t.Helper()
	for _, st := range sc.Steps {
		if st.Op != "cast" || st.Card != "p0:"+card {
			continue
		}
		if st.CastMode != mode || st.Mana != faceDownCastPool {
			t.Fatalf("cast %s mode=%q mana=%q, want %q for %q", card, st.CastMode, st.Mana, mode, faceDownCastPool)
		}
		return
	}
	t.Fatalf("scenario does not face-down cast %s: %+v", card, sc.Steps)
}

// TestHistoryPreludeDistinctNames: n events never repeat a probe name, the
// opponent's creatures are the victims of an OppCtrl head, and a pool too short
// for n yields no candidate instead of a repeated name.
func TestHistoryPreludeDistinctNames(t *testing.T) {
	reg := loadGenRegistry(t)
	pres := historyPreludes(reg, "Source", "Count$ThisTurnEntered_Graveyard_from_Battlefield_Creature.OppCtrl", 3)
	if len(pres) != 1 {
		t.Fatalf("OppCtrl deaths: %d preludes, want 1", len(pres))
	}
	p := pres[0]
	if len(p.opponentBattlefield) != 3 || len(p.battlefield) != 0 {
		t.Fatalf("victims p1=%v p0=%v, want three on p1", p.opponentBattlefield, p.battlefield)
	}
	for i, st := range p.steps {
		if st.Op == "cast" && !strings.HasPrefix(st.Targets[0], "p1:") {
			t.Fatalf("step %d targets %v, want a p1 creature", i, st.Targets)
		}
	}
	if got := historyPreludes(reg, "Source", "Count$ThisTurnEntered_Exile_Card.!token", 3); len(got) != 0 {
		t.Fatalf("three exiles from a two-spell pool: %+v", got)
	}
	faceDown := historyPreludes(reg, "Source", "Count$ThisTurnEntered_Battlefield_Creature.faceDown+YouCtrl", 1)
	if len(faceDown) != 1 {
		t.Fatalf("face-down entry offered %+v, want one candidate", faceDown)
	}
	if !preludeFaceDownCast(faceDown[0]) {
		t.Fatalf("face-down candidate is not a face-down cast: %+v", faceDown[0])
	}
}

// preludeFaceDownCast reports a prelude whose steps face-down cast a probe for
// {3} (the cause a face-down entry count reads).
func preludeFaceDownCast(p conditionPrelude) bool {
	for _, st := range p.steps {
		if st.Op == "cast" && st.CastMode != "" && st.Mana == faceDownCastPool {
			return true
		}
	}
	return false
}
