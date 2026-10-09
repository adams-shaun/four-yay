package paymirror

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

// authored parses an authored minimal IR fixture -- never Forge script text,
// which must not be committed.
func authored(t testing.TB, src string) *cards.Card {
	t.Helper()
	c, d := cards.ParseBytes("fixture.txt", []byte(src))
	if len(d) != 0 {
		t.Fatalf("fixture diags: %v", d)
	}
	c.Link()
	for _, f := range c.Faces {
		f.ApplyIntrinsics()
	}
	return c
}

// authoredDecks is a basic-land R/G deck against a basic-land U/B deck:
// creatures, a mana dork, a two-mana rock, a targeted burn spell and a draw
// spell, so planned casts cover generic and coloured pips, fixed
// multi-output production, creature sources and targets chosen before
// payment. Every land has exactly one basic land type.
func authoredDecks(t testing.TB) [][]*cards.Card {
	mountain := authored(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	forest := authored(t, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n")
	island := authored(t, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	swamp := authored(t, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n")
	bear := authored(t, "Name:Fixture Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	ogre := authored(t, "Name:Fixture Ogre\nManaCost:2 R R\nTypes:Creature Ogre\nPT:4/3\nOracle:x\n")
	elf := authored(t, "Name:Fixture Elf\nManaCost:G\nTypes:Creature Elf\nPT:1/1\nA:AB$ Mana | Cost$ T | Produced$ G | SpellDescription$ Add {G}.\nOracle:x\n")
	rock := authored(t, "Name:Fixture Rock\nManaCost:3\nTypes:Artifact\nA:AB$ Mana | Cost$ T | Produced$ C | Amount$ 2 | SpellDescription$ Add {C}{C}.\nOracle:x\n")
	shock := authored(t, "Name:Fixture Shock\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 2 | SpellDescription$ Deal 2 damage to any target.\nOracle:x\n")
	study := authored(t, "Name:Fixture Study\nManaCost:2 U\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 2 | SpellDescription$ Draw two cards.\nOracle:x\n")
	drake := authored(t, "Name:Fixture Drake\nManaCost:1 U U\nTypes:Creature Drake\nPT:2/3\nK:Flying\nOracle:x\n")
	shade := authored(t, "Name:Fixture Shade\nManaCost:2 B\nTypes:Creature Shade\nPT:3/2\nOracle:x\n")
	fill := func(n int, c *cards.Card) []*cards.Card {
		out := make([]*cards.Card, n)
		for i := range out {
			out[i] = c
		}
		return out
	}
	var rg, ub []*cards.Card
	rg = append(rg, fill(9, mountain)...)
	rg = append(rg, fill(8, forest)...)
	for _, c := range []*cards.Card{bear, ogre, elf, rock, shock} {
		rg = append(rg, fill(5, c)...)
	}
	ub = append(ub, fill(9, island)...)
	ub = append(ub, fill(8, swamp)...)
	for _, c := range []*cards.Card{study, drake, shade, rock, shock} {
		ub = append(ub, fill(5, c)...)
	}
	return [][]*cards.Card{rg, ub}
}

func requireAllEquivalent(t *testing.T, g GameResult, minPlanned int) {
	t.Helper()
	if g.Err != "" {
		t.Fatalf("game %+v: game-level error %s", g.Spec, g.Err)
	}
	if len(g.Reports) < minPlanned {
		t.Fatalf("game %+v: %d planned casts checked, want at least %d (the check must not be vacuous)", g.Spec, len(g.Reports), minPlanned)
	}
	floats := 0
	for _, r := range g.Reports {
		st, key := r.Verdict()
		if st != Equivalent {
			t.Errorf("game %+v seq %d %q: %s %s (routes %+v)", g.Spec, r.Seq, r.Card, st, key, r.Routes)
		}
		if r.Control == nil || r.Control.Status != Equivalent {
			t.Errorf("game %+v seq %d %q: control %+v", g.Spec, r.Seq, r.Card, r.Control)
		}
		for _, rr := range r.Routes {
			if rr.Route == RouteFloat && rr.Status == Equivalent {
				floats++
			}
		}
	}
	if floats == 0 {
		t.Fatalf("game %+v: no float-then-cast route was mirrored", g.Spec)
	}
	t.Logf("MEASURED %v seed %d: %d planned casts, %d float-route equivalents, %d turns", g.Spec.Decks, g.Spec.Seed, len(g.Reports), floats, g.Turns)
}

// TestPayMirrorAuthoredFixtureEquivalent is corpus-free: two fixed seeds of
// authored basic-land decks, every planned cast mirrored manually and
// compared for equivalence.
func TestPayMirrorAuthoredFixtureEquivalent(t *testing.T) {
	for _, seed := range []uint64{11, 12} {
		cfg := rules.Config{Seed: seed, Names: []string{"rg", "ub"}, Decks: authoredDecks(t), Tokens: map[string]*cards.Card{}}
		g := PlayConfig(cfg, GameSpec{Seed: seed, Decks: []string{"authored-rg", "authored-ub"}, Policy: "bot"},
			DriverOptions{Control: true, MaxTurns: 40})
		requireAllEquivalent(t, g, 10)
	}
}

// TestPayMirrorRepoDecksFixedSeedsEquivalent plays fixed-seed auto-pay games
// over the mono-colour repo decks (no dual lands, no additional-cost
// spells), where every planned cast is mirrored equivalently. Known
// mismatch shapes (multi-type lands, additional/contribution costs) are NOT
// skipped here: they are simply not in these decks, and each has a story.
func TestPayMirrorRepoDecksFixedSeedsEquivalent(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	d, err := LoadDecks(reg)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []GameSpec{
		{Seed: 1, Decks: []string{"mono-green-stompy", "mono-red-goblins"}, Policy: "bot"},
		{Seed: 3, Decks: []string{"mono-black-aggro", "mono-white-equipment"}, Policy: "bot"},
		{Seed: 2, Decks: []string{"mono-blue-tempo", "mono-red-prowess"}, Policy: "lethal"},
	} {
		requireAllEquivalent(t, PlayGame(d, spec, DriverOptions{Control: true}), 10)
	}
}

// TestPayMirrorDetectsPerturbation proves the comparison is not vacuous: a
// mirror that taps one extra land after its route must be reported as a
// state mismatch naming the tapped flag and the pool.
func TestPayMirrorDetectsPerturbation(t *testing.T) {
	cfg := rules.Config{Seed: 11, Names: []string{"rg", "ub"}, Decks: authoredDecks(t), Tokens: map[string]*cards.Card{}}
	e := rules.NewStartingPlayerChoice(cfg)
	bots := []*seat.Bot{seat.NewBot(1).EnableAutoPayMana(), seat.NewBot(2).EnableAutoPayMana()}
	answer := func(e *rules.Engine, d *decision.Decision) (decision.Intent, error) {
		if bots[d.Player].WantsPaymentActions() {
			e.EnsurePaymentActions()
		}
		return bots[d.Player].DecideBoard(context.Background(), botpolicy.BoardFromGame(e.G, e, d.Player), *d)
	}
	e.AskStartingPlayer()
	e.Advance()
	perturbed := 0
	for n := 0; n < 3000 && !e.G.Over && perturbed == 0; n++ {
		d := e.Pending()
		in, err := answer(e, d)
		if err != nil {
			t.Fatal(err)
		}
		if in.Payment == nil {
			if err := e.Submit(in); err != nil {
				t.Fatal(err)
			}
			continue
		}
		tapped := false
		opt := Options{AfterRoute: func(b *rules.Engine) {
			bd := b.Pending()
			if bd == nil || bd.Kind != decision.KPriority {
				return
			}
			for _, o := range bd.Options {
				if o.Kind == "activate" {
					if err := b.Submit(decision.Intent{Seq: bd.Seq, Player: bd.Player, Choices: []int{o.Index}}); err == nil {
						tapped = true
					}
					return
				}
			}
		}}
		rep := Check(e, in, answer, opt)
		if !tapped {
			// No untapped source left to perturb with; play on.
			if err := e.Submit(in); err != nil {
				t.Fatal(err)
			}
			continue
		}
		perturbed++
		st, _ := rep.Verdict()
		if st != Mismatch {
			t.Fatalf("perturbed mirror verdict = %s, want mismatch (%+v)", st, rep.Routes)
		}
		var paths []string
		for _, df := range rep.Routes[0].Diffs {
			paths = append(paths, df.Path)
		}
		joined := strings.Join(paths, " ")
		if !strings.Contains(joined, ".Tapped") || !strings.Contains(joined, ".Pool[") {
			t.Fatalf("perturbation diffs %v, want a Tapped flag and a pool slot", paths)
		}
	}
	if perturbed == 0 {
		t.Fatal("no planned cast with a spare source to perturb")
	}
}

// TestExclusionTableNamesEngineFields keeps the documented exclusions honest:
// every entry must name a real rules.Engine field (found through promotion,
// so a field living on an anonymous embedded cluster still counts), and it
// must resolve to a field the differ's walk can actually reach — an entry
// that resolves nowhere is silently compared again. That second check is
// what the vacuous FieldByName lookup could not catch: after the engine_struct
// embedding moved 85 of these fields onto cluster structs, every route pair
// mismatched on engineScratch.derivedMemo / engineDerivedTables.types* before
// excludedAt resolved the table through the embedding.
func TestExclusionTableNamesEngineFields(t *testing.T) {
	typ := reflect.TypeOf(rules.Engine{})
	for key := range excluded {
		if key.owner != "rules.Engine" {
			t.Errorf("exclusion %v: unexpected owner", key)
			continue
		}
		if _, ok := typ.FieldByName(key.field); !ok {
			t.Errorf("exclusion %v names no rules.Engine field", key)
		}
	}
	resolved := map[excludedField]bool{}
	for _, fields := range engineExcludedByType {
		for _, key := range fields {
			resolved[key] = true
		}
	}
	for key := range excluded {
		if !resolved[key] {
			t.Errorf("exclusion %v resolves to no field the walk reaches through Engine's embedding", key)
		}
	}
}

// TestExcludedAtResolvesThroughTheClusters pins the walk's resolution: an
// Engine field that moved onto an anonymous embedded cluster is excluded
// under the documented table key, a field the table does not name is still
// compared, and a zero-value walk of the cluster records the hit. Fails if
// the cluster layout changes without the exclusion table following, or if
// the walk stops consulting the resolver.
func TestExcludedAtResolvesThroughTheClusters(t *testing.T) {
	eng := reflect.TypeOf(rules.Engine{})
	f, ok := eng.FieldByName("engineScratch")
	if !ok || !f.Anonymous || f.Type.Kind() != reflect.Struct {
		t.Fatalf("rules.Engine has no anonymous engineScratch cluster to resolve through")
	}
	sf, ok := f.Type.FieldByName("derivedMemo")
	if !ok {
		t.Fatalf("engineScratch has no derivedMemo field to resolve")
	}
	key, ok := excludedAt(f.Type, sf)
	if !ok || key != (excludedField{"rules.Engine", "derivedMemo"}) {
		t.Errorf("excludedAt(engineScratch.derivedMemo) = (%v, %v), want the documented table key", key, ok)
	}
	sf, ok = f.Type.FieldByName("secretVoteBallots")
	if !ok {
		t.Fatalf("engineScratch has no secretVoteBallots field")
	}
	if key, ok := excludedAt(f.Type, sf); ok {
		t.Errorf("excludedAt(engineScratch.secretVoteBallots) = %v, want compared", key)
	}
	// The walk itself must consult the resolver: a zero-value comparison of
	// the cluster records the exclusion hits even though nothing differs.
	df := newDiffer()
	z := reflect.Zero(f.Type)
	df.walk("", z, z)
	for _, name := range []string{"derivedMemo", "legalOptBuf", "foreachBuf"} {
		if df.excludedHits[excludedField{"rules.Engine", name}] == 0 {
			t.Errorf("the walk did not exclude engineScratch.%s under the documented key", name)
		}
	}
	// And the float route's path matchers read the promotion-flat spelling.
	if got := engineFieldPath("engineResolution.damageSourceLKI{7}"); got != "damageSourceLKI{7}" {
		t.Errorf("engineFieldPath(cluster-prefixed) = %q, want the flat spelling", got)
	}
	if got := engineFieldPath("G.Objs[3].Tapped"); got != "G.Objs[3].Tapped" {
		t.Errorf("engineFieldPath(non-cluster path) = %q, want unchanged", got)
	}
}

func TestLabelProductionParsesManaOptions(t *testing.T) {
	for _, tc := range []struct {
		label string
		want  decision.ManaAmount
		any   bool
		combo int
	}{
		{"Add R", decision.ManaAmount{0, 0, 0, 1, 0, 0}, false, 0},
		{"Add CC", decision.ManaAmount{0, 0, 0, 0, 0, 2}, false, 0},
		{"Sacrifice a creature: Add BB", decision.ManaAmount{0, 0, 2, 0, 0, 0}, false, 0},
		{"Add any color", decision.ManaAmount{}, true, 0},
		{"Add three mana of any one color", decision.ManaAmount{}, true, 0},
		{"Add three mana in any combination of colors", decision.ManaAmount{}, true, 0},
		{"Add 21 mana of any one color", decision.ManaAmount{}, true, 0},
		{"Add 22 mana in any combination of colors", decision.ManaAmount{}, true, 0},
		{"Add W, U or B", decision.ManaAmount{}, false, 3},
	} {
		amt, any, combo, ok := labelProduction(tc.label)
		if !ok || amt != tc.want || any != tc.any || len(combo) != tc.combo {
			t.Errorf("labelProduction(%q) = %v %v %v %v", tc.label, amt, any, combo, ok)
		}
	}
}
