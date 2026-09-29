package rules

// CR 702.189 Firebending: the printed-keyword expansion
// (cards/kw_firebending.go) and the "until end of combat" mana lifetime the
// expansion's PersistentUntilEndOfCombat$ form gives. The set-audit pins on
// the two tla carriers live in rules/setaudit_tla_test.go; these cover the
// lifetime across combat steps and the corpus census.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestFirebendingManaSurvivesCombatStepsAndEmptiesAtEndOfCombat pins the third
// part of CR 702.189a: "Until end of combat, you don't lose this mana as steps
// and phases end." Fire Sages is declared attacking (adding one red mana), the
// combat phase continues through later steps (the mana must survive), and the
// phase ends (the mana must empty). A fix that added the mana as ORDINARY pool
// mana would empty it at the very first step boundary below -- and a fix that
// marked it persistent-until-end-of-turn would wrongly keep it into the
// postcombat main phase; the test pins both edges.
func TestFirebendingManaSurvivesCombatStepsAndEmptiesAtEndOfCombat(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _, _ := newFixtureDeck(t, 19, "Name:Placeholder\nManaCost:0\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
	fireSages := onBoardCard(t, e, 0, tlaCorpusCard(t, reg, "Fire Sages"))
	e.G.Obj(fireSages).SummonSick = false

	// Declare the Fire Sages attacking through the engine's REAL combat flow
	// (so the attack trigger fires from the same DeclareAttackers event a
	// game produces), then drive to the end of the combat phase.
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, fireSages)
	driveToStepAll(t, e, e.G.Turn, 0, state.StepEndCombat)

	// Precondition: the trigger really resolved and the unit is the
	// combat-persistent class, not ordinary pool mana. Without this a test
	// that asserted "0 at the end" could pass with the whole feature missing.
	if got := e.G.Players[0].Pool[state.MR]; got != 1 {
		t.Fatalf("precondition: Firebending 1 added R=%d, want 1 (CR 702.189a)", got)
	}
	if got := e.G.Players[0].CombatMana[state.MR]; got != 1 {
		t.Fatalf("precondition: combat-persistent tally R=%d, want 1", got)
	}

	// The mana survived every step boundary of the combat phase (the attack
	// step, blockers, combat damage) and is still there in the end-of-combat
	// step, where a player may still spend it.
	driveToStepAll(t, e, e.G.Turn, 0, state.StepMain2)
	if got := e.G.Players[0].Pool[state.MR]; got != 0 {
		t.Fatalf("Firebending mana survived the end of combat: R=%d, want 0", got)
	}
	if got := e.G.Players[0].CombatMana[state.MR]; got != 0 {
		t.Fatalf("combat-persistent tally survived the end of combat: R=%d, want 0", got)
	}
}

// TestFirebendingCorpusCensus names the mechanism class corpus-wide. It is the
// regression that keeps the keyword from silently falling out of the expanded
// head table: every printed K:Firebending carrier face must carry the expanded
// Mode$ Attacks trigger, the two Firebender token scripts must too, and the
// four keyword-grant carriers (Sozin's Comet and friends) must still name the
// grant -- the granted path is separate from the printed one.
func TestFirebendingCorpusCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)

	printed := map[string]bool{}
	var printedFaces int
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if !f.HasKeyword("Firebending") {
				continue
			}
			printed[f.Name] = true
			printedFaces++
			if !hasFirebendingTrigger(f) {
				t.Errorf("%s prints Firebending but has no expanded Mode$ Attacks trigger", f.Name)
			}
		}
	}
	if printedFaces != 26 {
		t.Errorf("corpus K:Firebending carrier faces = %d, want 26", printedFaces)
	}

	// The two Firebender token scripts expand through the same link path.
	tokens := map[string]bool{}
	for stem, c := range reg.Tokens {
		for _, f := range c.Faces {
			if f.HasKeyword("Firebending") {
				tokens[stem] = true
				if !hasFirebendingTrigger(f) {
					t.Errorf("token %s prints Firebending but has no expanded trigger", stem)
				}
			}
		}
	}
	for _, stem := range []string{"r_2_2_soldier_firebending_1", "r_4_4_dragon_flying_firebending_4"} {
		if !tokens[stem] {
			t.Errorf("token script %q missing from the Firebending census", stem)
		}
	}

	// The keyword-grant carriers: a KW$ Firebending:<N> on any ability is the
	// "also affected" class the printed expansion does not cover.
	grants := map[string]bool{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			for _, sa := range f.Abilities {
				for _, v := range sa.Params {
					if strings.HasPrefix(v, "Firebending:") {
						grants[f.Name] = true
					}
				}
			}
			for _, body := range f.SVars {
				if strings.Contains(body, "KW$ Firebending:") {
					grants[f.Name] = true
				}
			}
		}
	}
	for _, name := range []string{"Sozin's Comet", "Fire Nation Palace", "Fire Nation Turret"} {
		if !grants[name] {
			t.Errorf("%s no longer names a KW$ Firebending grant in the corpus census", name)
		}
	}

	if !printed["Fire Sages"] || !printed["Firebending Student"] {
		t.Fatalf("the two pinned tla carriers are missing from the census: printed=%v", printed)
	}
}

// grantedFirebendingStatic is a layer-6 AddKeyword$ Firebending:3 grant --
// the Sozin's Comet / Fire Nation Palace shape, but on a durable static so a
// test can declare the granted creature attacking without first resolving a
// spell or activation. The granting static is a separate Enchantment.
const grantedFirebendingStatic = "Name:Blazing Banner\nManaCost:0\nTypes:Enchantment\n" +
	"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddKeyword$ Firebending:3 | Description$ Creatures you control have firebending 3.\nOracle:x\n"

// TestGrantedFirebendingAddsManaOnAttack is the granted half of CR 702.189a:
// a creature that does NOT print K:Firebending but is given it in layer 6
// (Sozin's Comet's `KW$ Firebending:5`, Fire Nation Palace's targeted grant,
// Iroh, Dragon of the West's pump-all) has the same rules text as a printed
// keyword, so attacking must add the granted N red. The printed expansion
// (cards/kw_firebending.go) only covers printed lines, so before
// checkGrantedFirebendingTriggers the grant sat in the derived list with no
// attack trigger to carry it. The preconditions keep this from passing with
// the whole granted path missing: the keyword must be in the DERIVED list and
// absent from the printed face, and the mana must arrive both in the pool and
// in the combat-persistent tally (so an ordinary-pool fix cannot satisfy it).
func TestGrantedFirebendingAddsManaOnAttack(t *testing.T) {
	e, _, _ := newFixtureDeck(t, 23, "Name:Placeholder\nManaCost:0\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
	_ = onBoard(t, e, 0, grantedFirebendingStatic)
	bear := onBoard(t, e, 0, "Name:Testbear\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Obj(bear).SummonSick = false

	// Precondition: the grant is live in the DERIVED keyword list while the
	// creature's printed face does NOT carry it -- so the mana below can only
	// come from the granted synthesis, never the printed expansion.
	if !e.HasKeyword(bear, "Firebending") {
		t.Fatal("precondition: granted Firebending is not in the derived keyword list")
	}
	if e.G.Obj(bear).Face() != nil && e.G.Obj(bear).Face().HasKeyword("Firebending") {
		t.Fatal("precondition: the test creature prints Firebending; it must only be granted")
	}

	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, bear)
	driveToStepAll(t, e, e.G.Turn, 0, state.StepEndCombat)

	if got := e.G.Players[0].Pool[state.MR]; got != 3 {
		t.Fatalf("granted Firebending 3 added R=%d, want 3 (CR 702.189a)", got)
	}
	if got := e.G.Players[0].CombatMana[state.MR]; got != 3 {
		t.Fatalf("granted Firebending 3 combat-persistent tally R=%d, want 3", got)
	}
}

// hasFirebendingTrigger reports whether f carries the trigger
// cards/kw_firebending.go adds: an Attacks self-trigger tagged
// Keyword$ Firebending whose Execute$ body is a DB$ Mana producing red.
func hasFirebendingTrigger(f *cards.Face) bool {
	for _, tg := range f.Triggers {
		if tg.Params["Keyword"] != "Firebending" {
			continue
		}
		if tg.Mode != "Attacks" || tg.Params["ValidCard"] != "Card.Self" {
			continue
		}
		body, ok := f.SVars[tg.Params["Execute"]]
		if !ok || !strings.Contains(body, "DB$ Mana") ||
			!strings.Contains(body, "PersistentUntilEndOfCombat$ True") {
			continue
		}
		return true
	}
	return false
}
