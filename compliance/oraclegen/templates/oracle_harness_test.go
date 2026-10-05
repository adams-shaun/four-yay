package templates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// oracleHarnessCorpus loads the pinned card corpus the generator needs.
func oracleHarnessCorpus(t *testing.T) *cards.Registry {
	t.Helper()
	reg, err := cards.LoadRegistry(cards.CachePath(filepath.Join("..", "..", "..", ".cards")))
	if err != nil {
		t.Fatalf("the generator needs the corpus (make fetch-cards compile-cards): %v", err)
	}
	return reg
}

// oracleCorpusCarriers returns the catalogue name of every script under
// .cards/cardsfolder whose body contains marker. A script names each face
// ("Name:Front", "ALTERNATE", "Name:Back"), and the catalogue -- what a
// deck list, a manifest and a scenario name it by -- is the faces joined
// with " // ". That is the spelling the setup bug tripped over, so the split
// census must use it, not the front face alone.
func oracleCorpusCarriers(t *testing.T, marker string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "..", ".cards", "cardsfolder", "*", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, fp := range files {
		b, err := os.ReadFile(fp)
		if err != nil || !strings.Contains(string(b), marker) {
			continue
		}
		var faces []string
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "Name:") {
				faces = append(faces, strings.TrimSpace(line[len("Name:"):]))
			}
		}
		if len(faces) > 0 {
			out = append(out, strings.Join(faces, " // "))
		}
	}
	return out
}

// TestOracleSplitRoomCensusBinds is the corpus-wide ratchet for the
// AlternateMode:Split / Room setup bug: every carrier is seeded into a seat's
// hand, and the runner must bind it back to the dealt object. A new Split/Room
// card changes the pinned count and, until it binds, names itself here.
//
// The binding was by exact printed name, so a scenario naming "Bottomless Pool
// // Locker Room" (the catalogue spelling) failed with "was not dealt" because
// the dealt object is the front face "Bottomless Pool". cards.NormalizeName
// folds the two.
func TestOracleSplitRoomCensusBinds(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	carriers := oracleCorpusCarriers(t, "AlternateMode:Split")
	// Pinned 2026-10-04 against FORGE_REF 95f04e8a. A corpus pin bump may
	// legitimately move this; update it and say so.
	if len(carriers) != 126 {
		t.Fatalf("AlternateMode:Split carriers = %d, want the pinned 126; a new carrier must be reviewed", len(carriers))
	}
	for _, name := range carriers {
		b, _ := json.Marshal(oraclegen.Scenario{Setup: map[string]oraclegen.Seat{"p0": {Hand: []string{name}}}})
		res, err := rules.RunOracleScenarioJSON(reg, b)
		if err != nil {
			t.Errorf("%s: runner refused the setup: %v", name, err)
			continue
		}
		for _, f := range res.Fails {
			if strings.Contains(f, "was not dealt") || strings.Contains(f, "not in the corpus") {
				t.Errorf("%s: setup did not bind: %s", name, f)
			}
		}
	}
}

// TestOracleOpeningHandCensusDeclines is the corpus-wide ratchet for the
// K:MayEffectFromOpeningHand bug: every carrier must generate a scenario that
// declines the "begin the game with this card" ask. Otherwise the runner's
// setup fallback takes option 0 ("Yes"), the card starts on the battlefield,
// and the scenario's cast step is "not offered". A new carrier changes the
// pinned count and, until it declines, names itself here.
func TestOracleOpeningHandCensusDeclines(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	carriers := oracleCorpusCarriers(t, "MayEffectFromOpeningHand")
	// Pinned 2026-10-04 against FORGE_REF 95f04e8a.
	if len(carriers) != 30 {
		t.Fatalf("MayEffectFromOpeningHand carriers = %d, want the pinned 30; a new carrier must be reviewed", len(carriers))
	}
	for _, name := range carriers {
		it, skip := Generate(reg, name)
		if skip != nil {
			t.Errorf("%s: no scenario: %s", name, skip.Reason)
			continue
		}
		if len(it.SetupAnswers) == 0 {
			t.Errorf("%s: no setup_answers, so the fallback starts it on the battlefield", name)
			continue
		}
		a := it.SetupAnswers[0]
		if a.Kind != "choose" || len(a.Pick) != 1 || a.Pick[0] != "no" {
			t.Errorf("%s: setup answer %+v, want {choose [no]}", name, a)
		}
	}
}

// TestOracleNamedHarnessCardsGenerate pins the cards this ticket fixed: each
// must now produce a level-A scenario instead of landing in the .skips.jsonl.
func TestOracleNamedHarnessCardsGenerate(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	names := []string{
		// DSK Rooms, all AlternateMode:Split.
		"Bottomless Pool // Locker Room", "Charred Foyer // Warped Space",
		"Dazzling Theater // Prop Room", "Defiled Crypt // Cadaver Lab",
		"Derelict Attic // Widow's Walk", "Dollmaker's Shop // Porcelain Gallery",
		"Funeral Room // Awakening Hall", "Glassworks // Shattered Yard",
		"Grand Entryway // Elegant Rotunda", "Greenhouse // Rickety Gazebo",
		"Meat Locker // Drowned Diner", "Mirror Room // Fractured Realm",
		"Moldering Gym // Weight Room", "Painter's Studio // Defaced Gallery",
		"Restricted Office // Lecture Hall", "Roaring Furnace // Steaming Sauna",
		"Surgical Suite // Hospital Room", "Ticket Booth // Tunnel of Hate",
		"Underwater Tunnel // Slimy Aquarium", "Unholy Annex // Ritual Chamber",
		"Walk-In Closet // Forgotten Cellar",
		// MKM splits. Push // Pull is excluded: its only half targets a tapped
		// creature, which no fixture can put on the battlefield (there is no
		// setup-tap facility), so it needs its own ticket.
		"Cease // Desist", "Flotsam // Jetsam", "Fuss // Bother",
		// Player-target zone bug.
		"Cruelclaw's Heist", "Ruthless Negotiation", "Soul Search", "Aggressive Negotiations",
		// Counter template's later target slot.
		"Sokka's Haiku",
		// Unposed spree modes answer.
		"Phantom Interference", "Shifting Grift",
		// Opening-hand ask.
		"Quicksilver, Brash Blur", "Leyline of Hope", "Leyline of Mutation",
		"Leyline of Resonance", "Leyline of Transformation", "Leyline of the Void",
		"Leyline of the Guildpact",
	}
	for _, name := range names {
		if _, skip := Generate(reg, name); skip != nil {
			t.Errorf("%s: %s", name, skip.Reason)
		}
	}
}

// TestOraclePlayerTargetKeepsNoZone pins the fix for ValidTgts$ Opponent with
// Origin$ Hand: the zone qualifier belongs to the effect, not the player
// target, so "Opponent@Hand" must never appear. Each named card's generated
// scenario targets a player ref ("p1"), not a card in hand.
func TestOraclePlayerTargetKeepsNoZone(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	for _, name := range []string{"Cruelclaw's Heist", "Ruthless Negotiation", "Soul Search", "Aggressive Negotiations"} {
		it, skip := Generate(reg, name)
		if skip != nil {
			t.Errorf("%s: %s", name, skip.Reason)
			continue
		}
		found := false
		for _, st := range it.Steps {
			for _, tg := range st.Targets {
				if strings.Contains(tg, "@") {
					t.Errorf("%s: target %q carries a zone qualifier on a player head", name, tg)
				}
				if tg == "p1" || tg == "p0" {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("%s: no player target in %+v", name, it.Steps)
		}
	}
}

// TestOracleSnapshotNamesFaceDownAndSetName pins the snapshot naming fixes:
// Honest Work's SetName$ Humble Merchant must be what the permanent reports
// (layer 3), and a face-down permanent reports no name at all (CR 708.2a).
func TestOracleSnapshotNamesFaceDownAndSetName(t *testing.T) {
	reg := oracleHarnessCorpus(t)

	it, skip := Generate(reg, "Honest Work")
	if skip != nil {
		t.Fatalf("Honest Work: %s", skip.Reason)
	}
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok {
		t.Fatalf("Honest Work scenario did not play through: %v", res.Fails)
	}
	last := res.Snapshots[len(res.Snapshots)-1]
	sawEnchanted := false
	for _, p := range last.Permanents {
		if strings.Contains(p.Ref, "Grizzly Bears") {
			sawEnchanted = true
			if p.Name != "Humble Merchant" {
				t.Errorf("enchanted creature name = %q, want the layer-3 %q", p.Name, "Humble Merchant")
			}
		}
	}
	if !sawEnchanted {
		t.Fatalf("the enchanted Grizzly Bears is not on the battlefield: %+v", last.Permanents)
	}

	// Cryptic Coat makes a face-down 3/2 creature; its snapshot name is empty.
	it, skip = Generate(reg, "Cryptic Coat")
	if skip != nil {
		t.Fatalf("Cryptic Coat: %s", skip.Reason)
	}
	res, ok = oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok {
		t.Fatalf("Cryptic Coat scenario did not play through: %v", res.Fails)
	}
	last = res.Snapshots[len(res.Snapshots)-1]
	sawFaceDown := false
	for _, p := range last.Permanents {
		if p.FaceDown {
			sawFaceDown = true
			if p.Name != "" {
				t.Errorf("a face-down permanent reported name %q, want empty (CR 708.2a)", p.Name)
			}
		}
	}
	if !sawFaceDown {
		t.Fatalf("no face-down permanent in the snapshot: %+v", last.Permanents)
	}
}
