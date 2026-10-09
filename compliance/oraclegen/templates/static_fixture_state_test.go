package templates_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// stateFixtureServed generates static#0.0 for name and, when the row is
// served, replays the scenario and returns the final snapshot. ok is false
// when the row is still skipped.
func stateFixtureServed(t *testing.T, name string) (rules.OracleSnapshot, bool) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup(name)
	if !ok || len(card.Faces) == 0 {
		t.Fatalf("precondition: %s is in the corpus with faces", name)
	}
	for _, req := range levelb.Requirements(card) {
		if req.Key != "static#0.0" || req.Sub != "static.continuous" || req.Gap != "" {
			continue
		}
		it, skip := templates.GenerateB(reg, name, req)
		if skip != nil {
			return rules.OracleSnapshot{}, false
		}
		raw, err := json.Marshal(it.Scenario)
		if err != nil {
			t.Fatalf("%s: marshal scenario: %v", name, err)
		}
		res, err := rules.RunOracleScenarioJSON(reg, raw)
		if err != nil {
			t.Fatalf("%s: scenario does not replay: %v", name, err)
		}
		if len(res.Fails) != 0 || len(res.Snapshots) == 0 {
			t.Fatalf("%s: replay fails: %v", name, res.Fails)
		}
		return res.Snapshots[len(res.Snapshots)-1], true
	}
	t.Fatalf("precondition: %s has a static.continuous static#0.0 requirement", name)
	return rules.OracleSnapshot{}, false
}

// tokensOnP0 is the final snapshot's tokens on p0's battlefield whose name
// contains want (case-folded), as the snapshot's token rule matches refs.
func tokensOnP0(s rules.OracleSnapshot, want string) []rules.OracleSnapPerm {
	var out []rules.OracleSnapPerm
	for _, p := range s.Permanents {
		if p.Token && p.Controller == 0 && strings.Contains(strings.ToLower(p.Name), strings.ToLower(want)) {
			out = append(out, p)
		}
	}
	return out
}

// TestStateFixtureTokenRows pins the token fixture's served rows: the
// scenario casts Dragon Fodder into being the creature tokens the static
// selects, and the final snapshot shows the static's grant on them (ticket
// cli-20261009T031407Z-8f4b4f49, level-B class G7).
func TestStateFixtureTokenRows(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	goblin, ok := reg.Token("r_1_1_goblin")
	if !ok || len(goblin.Faces) == 0 {
		t.Fatal("precondition: the Dragon Fodder token script is in the registry")
	}
	printedPT := goblin.Faces[0].PT
	if printedPT != "1/1" {
		t.Fatalf("precondition: the Goblin token prints %s, want 1/1", printedPT)
	}
	for _, tc := range []struct {
		name      string
		wantN     int
		checkPT   string
		checkKW   string
		attacking bool
	}{
		// Toby: four or more creature tokens have flying.
		{name: "Toby, Beastie Befriender", wantN: 4, checkKW: "Flying"},
		// Gideon's Memorial: creature tokens get +1/+0 and have vigilance.
		{name: "Gideon's Memorial", wantN: 2, checkPT: "2/1", checkKW: "Vigilance"},
		// Pollen-Shield Hare: creature tokens get +1/+1.
		{name: "Pollen-Shield Hare", wantN: 2, checkPT: "2/2"},
		// Attacking tokens: the tokens attack on the turn after the prelude
		// cast (a token a prelude made is summoning sick) and gain the
		// keyword while attacking.
		{name: "Okoye, Dora Milaje Leader", wantN: 2, checkKW: "First Strike", attacking: true},
		{name: "Bone-Cairn Butcher", wantN: 2, checkKW: "Deathtouch", attacking: true},
		{name: "Starry-Eyed Skyrider", wantN: 2, checkKW: "Flying", attacking: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, served := stateFixtureServed(t, tc.name)
			if !served {
				t.Fatalf("static#0.0 is skipped, want the token fixture to serve it")
			}
			toks := tokensOnP0(s, "Goblin Token")
			if len(toks) < tc.wantN {
				t.Fatalf("p0 holds %d Goblin Tokens, want at least %d: %v", len(toks), tc.wantN, s.Permanents)
			}
			if tc.checkPT != "" && toks[0].PT != tc.checkPT {
				t.Fatalf("token P/T %s, want %s (printed %s)", toks[0].PT, tc.checkPT, printedPT)
			}
			if tc.checkKW != "" && !hasPermKeyword(toks[0], tc.checkKW) {
				t.Fatalf("token keywords %v, want %s", toks[0].Keywords, tc.checkKW)
			}
			if tc.attacking && !toks[0].Attacking {
				t.Fatalf("the tokens did not attack: %+v", toks[0])
			}
			// Every token the fixture made carries the grant, not just the
			// first: the static's filter selects all of them.
			for _, tok := range toks {
				if tc.checkPT != "" && tok.PT != tc.checkPT {
					t.Fatalf("token P/T %s on one token, %s on another", tok.PT, tc.checkPT)
				}
				if tc.checkKW != "" && !hasPermKeyword(tok, tc.checkKW) {
					t.Fatalf("keyword %s on one token, %v on another", tc.checkKW, tok.Keywords)
				}
				if tc.attacking && !tok.Attacking {
					t.Fatal("one of the tokens did not attack")
				}
			}
		})
	}
}

// TestStateFixtureEquipRows pins the equip fixture's served rows: the
// scenario attaches Bonesplitter (+2/+0, its only printed effect) and the
// static's grant shows on the equipped permanent beyond that baseline.
func TestStateFixtureEquipRows(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	bs, ok := reg.Lookup("Bonesplitter")
	if !ok || len(bs.Faces) == 0 {
		t.Fatal("precondition: Bonesplitter is in the corpus")
	}
	if got := bs.Faces[0].PT; got != "" {
		t.Fatalf("precondition: Bonesplitter prints a P/T %q", got)
	}
	cases := []struct {
		name     string
		probe    string
		wantPT   string
		wantKW   []string
		probeCtl int
	}{
		// Firion: equipped creatures you control have haste -- the Bear.
		{name: "Firion, Wild Rose Warrior", probe: "Grizzly Bears", wantPT: "4/2", wantKW: []string{"Haste"}, probeCtl: 0},
		// Blacksmith's Talent (Class level 3): during your turn, equipped
		// creatures you control have double strike and haste.
		{name: "Blacksmith's Talent", probe: "Grizzly Bears", wantPT: "4/2", wantKW: []string{"Double Strike", "Haste"}, probeCtl: 0},
		// Cloud, Planet's Champion: as long as it is equipped, it has double
		// strike and indestructible -- the card itself wears Bonesplitter.
		{name: "Cloud, Planet's Champion", probe: "Cloud, Planet's Champion", wantPT: "6/4", wantKW: []string{"Double Strike", "Indestructible"}, probeCtl: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, served := stateFixtureServed(t, tc.name)
			if !served {
				t.Fatalf("static#0.0 is skipped, want the equip fixture to serve it")
			}
			var p *rules.OracleSnapPerm
			for i := range s.Permanents {
				if s.Permanents[i].Name == tc.probe && s.Permanents[i].Controller == tc.probeCtl {
					p = &s.Permanents[i]
					break
				}
			}
			if p == nil {
				t.Fatalf("precondition: %s is on p%d's final battlefield", tc.probe, tc.probeCtl)
			}
			if p.PT != tc.wantPT {
				t.Fatalf("%s P/T %s, want %s (printed 2/2 or 4/4 plus Bonesplitter's +2/+0)", tc.probe, p.PT, tc.wantPT)
			}
			for _, kw := range tc.wantKW {
				if !hasPermKeyword(*p, kw) {
					t.Fatalf("%s keywords %v, want %s", tc.probe, p.Keywords, kw)
				}
			}
		})
	}
	// The control: p1's Bear is untouched (a you-control static never
	// reaches it), so the grant is p0's equipment plus the static, not the
	// equipment alone.
	s, served := stateFixtureServed(t, "Firion, Wild Rose Warrior")
	if !served {
		t.Fatal("precondition: Firion serves")
	}
	for _, p := range s.Permanents {
		if p.Name == "Grizzly Bears" && p.Controller == 1 {
			if p.PT != "2/2" || hasPermKeyword(p, "Haste") {
				t.Fatalf("p1's Bear moved: P/T %s keywords %v", p.PT, p.Keywords)
			}
		}
	}
}

// TestStateFixtureManaPoolRow pins Ozai, the Phoenix King: the card's own
// cast is paid with six mana more than it needs and the pool keeps the rest,
// so Count$ManaPool:All reads six and flying/indestructible is live.
func TestStateFixtureManaPoolRow(t *testing.T) {
	s, served := stateFixtureServed(t, "Ozai, the Phoenix King")
	if !served {
		t.Fatal("static#0.0 is skipped, want the mana-pool fixture to serve it")
	}
	var pool string
	for _, pl := range s.Players {
		if pl.Seat == 0 {
			pool = pl.Pool
		}
	}
	// The precondition the static's gate reads: at least six unspent mana.
	if len(pool) < 6 {
		t.Fatalf("p0's pool %q, want at least six unspent mana", pool)
	}
	for _, p := range s.Permanents {
		if p.Name == "Ozai, the Phoenix King" && p.Controller == 0 {
			if !hasPermKeyword(p, "Flying") || !hasPermKeyword(p, "Indestructible") {
				t.Fatalf("Ozai keywords %v, want flying and indestructible", p.Keywords)
			}
			return
		}
	}
	t.Fatal("precondition: Ozai is on p0's final battlefield")
}

// TestStateFixtureAttackCountRow pins Deepway Navigator: the fixture attacks
// with three Merfolk before the card is cast, so Count$AttackersDeclared
// reads three and the Merfolk pump is live.
func TestStateFixtureAttackCountRow(t *testing.T) {
	s, served := stateFixtureServed(t, "Deepway Navigator")
	if !served {
		t.Fatal("static#0.0 is skipped, want the attack-count fixture to serve it")
	}
	merfolk := 0
	shifted := false
	for _, p := range s.Permanents {
		if p.Name == "Coral Merfolk" && p.Controller == 0 {
			merfolk++
			if p.PT == "3/1" {
				shifted = true
			}
		}
	}
	if merfolk < 3 {
		t.Fatalf("p0 holds %d Coral Merfolk, want three (the raid count)", merfolk)
	}
	if !shifted {
		t.Fatal("no Coral Merfolk shows the +1/+0 the raid count grants")
	}
}

// TestStateFixtureClassTokenRow pins Caretaker's Talent: the token fixture
// casts the tokens the level-3 static pumps, and the class prelude raises
// the Class to level 3.
func TestStateFixtureClassTokenRow(t *testing.T) {
	s, served := stateFixtureServed(t, "Caretaker's Talent")
	if !served {
		t.Fatal("static#0.0 is skipped, want the token fixture to serve it")
	}
	toks := tokensOnP0(s, "Goblin Token")
	if len(toks) == 0 {
		t.Fatal("p0 holds no Goblin Tokens for the level-3 static to pump")
	}
	for _, tok := range toks {
		if tok.PT != "3/3" {
			t.Fatalf("token P/T %s, want 3/3 (printed 1/1 plus the level-3 +2/+2)", tok.PT)
		}
	}
}

func hasPermKeyword(p rules.OracleSnapPerm, kw string) bool {
	for _, k := range p.Keywords {
		if strings.EqualFold(k, kw) {
			return true
		}
	}
	return false
}
