package effects_test

import (
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
)

// repoDeckFilterStrings collects every parameter and SVar value of every card
// in the repo decks, plus each whitespace-separated field of those values
// (so `Count$Valid Creature.YouCtrl+Other` contributes its spec). Most of the
// strings are not filters at all; that is deliberate -- the compiled form must
// agree with the textual oracle on ANY string, including the unknown and
// malformed ones whose contract is to fail closed.
func repoDeckFilterStrings(t *testing.T, reg *cards.Registry) []string {
	t.Helper()
	set := map[string]bool{}
	add := func(v string) {
		set[v] = true
		for _, f := range strings.Fields(v) {
			set[f] = true
		}
		for _, f := range strings.Split(v, "|") {
			set[strings.TrimSpace(f)] = true
		}
	}
	addParams := func(m map[string]string) {
		for _, v := range m {
			add(v)
		}
	}
	var addSA func(sa *cards.SA)
	addSA = func(sa *cards.SA) {
		for ; sa != nil; sa = sa.Sub {
			addParams(sa.Params)
		}
	}
	for _, name := range testutil.RepoDeckNames() {
		deck, err := testutil.LoadRepoDeck(reg, name)
		if err != nil {
			t.Fatalf("deck %s: %v", name, err)
		}
		for _, c := range deck {
			for _, f := range c.Faces {
				if f == nil {
					continue
				}
				for _, sa := range f.Abilities {
					addSA(sa)
				}
				for _, tr := range f.Triggers {
					addParams(tr.Params)
					addSA(tr.Effect)
				}
				for _, st := range f.Statics {
					addParams(st.Params)
				}
				for _, r := range f.Repls {
					addParams(r.Params)
					addSA(r.With)
				}
				addParams(f.SVars)
			}
		}
	}
	// Hand-written shapes the deck text may not carry: EACH, CARDNAME,
	// negated bases, the contextual sameName forms, bare '!', keyword
	// predicates under an ExtraKeywords binding, numeric RHS through Resolve.
	for _, s := range []string{
		"", " ", ",", "Creature.", "Creature.+", "Creature.!", "Creature.!!YouCtrl",
		"EACH Forest & Plains", "EACH Card.Creature & Land.YouCtrl", "EACH A & & B",
		"CARDNAME", "CARDNAME.YouCtrl", "nonCARDNAME", "nonnonCreature", "non",
		"nonLand.YouCtrl", "nonCreature.nonToken+!tapped", "Permanent.nonLand",
		"Remembered.sameName", "Targeted.Permanent+sameName", "Triggered.sameName",
		"Card.Permanent", "Any.YouCtrl", "Affinity", "PermanentCard", "SpellAbility.OppCtrl",
		"Creature.withFlying", "Creature.withoutFlying+YouCtrl", "Card.cmcLEX", "Card.powerGEX",
		"Creature.powerLTtoughness", "Creature.counters_GE1_P1P1", "Creature.counters_EQX_P1P1",
		"Card.namedGrizzly Bears", "Card.notnamedGrizzly Bears", "Card.namedKorlash, Heir to Blackblade",
		"Card.IsRemembered", "Card.TriggeredCard", "Card.TriggeredCards", "Card.ChosenCard", "Card.nonChosenCard",
		"Creature.ControlledBy TriggeredPlayer", "Creature.ControlledBy NextOpponentToYourLeft", "Creature.greatestPower", "Card.lowestCMC",
		"Creature.ChosenColor", "Creature.DefenderCtrl", "Creature.NotDefinedTargeted",
		"Creature.Goblin", "Creature.nonGoblin", "Creature.Elf,Creature.Goblin",
		"Creature.wasCastFromYourHandByYou", "Creature.!wasCastFromYourHandByYou",
	} {
		set[s] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// midGameStates plays a few seeded bot games between repo decks and returns
// their games at several intent counts.
func midGameStates(t *testing.T, reg *cards.Registry) []*state.Game {
	t.Helper()
	names := testutil.LegacyDeckNames()
	var games []*state.Game
	for i, pair := range [][2]int{{0, 1}, {4, 7}} {
		if pair[1] >= len(names) {
			break
		}
		decks := [][]*cards.Card{
			testutil.RepoDeck(t, reg, names[pair[0]]),
			testutil.RepoDeck(t, reg, names[pair[1]]),
		}
		for _, n := range []int{80, 250, 500} {
			seed := uint64(7000 + i)
			cfg := rules.Config{Seed: seed, Names: []string{"a", "b"}, Decks: decks, Tokens: reg.Tokens}
			seats := []seat.Seat{seat.NewBot(seed ^ 1), seat.NewBot(seed ^ 2)}
			_, e, err := bench.PlayGame(cfg, seats, 0, n, bench.Hooks{})
			if err != nil {
				t.Fatalf("game %d@%d: %v", i, n, err)
			}
			games = append(games, e.G)
		}
	}
	return games
}

func pickObjs(g *state.Game) []state.ObjID {
	var ids []state.ObjID
	for i := range g.Objs {
		o := &g.Objs[i]
		if o.ID != 0 {
			ids = append(ids, o.ID)
		}
	}
	return ids
}

// contextsFor builds a spread of evaluation contexts over g: bare, sourced,
// a fully bound resolution/trigger context, and a layer-walk context with
// ExtraTypes/ExtraKeywords, DerivedTypes and EffectiveNames.
func contextsFor(g *state.Game) []effects.SpecContext {
	ids := pickObjs(g)
	at := func(k int) state.ObjID {
		if len(ids) == 0 {
			return 0
		}
		return ids[k%len(ids)]
	}
	resolve := func(name string) (int32, bool) {
		if name == "X" {
			return 2, true
		}
		return 0, false
	}
	rich := effects.SpecContext{You: 0, Source: at(3), Resolving: true, Resolve: resolve,
		Remembered:        []state.Target{{Obj: at(5)}, {IsPlayer: true, Player: 1}},
		ResolutionTargets: []state.Target{{Obj: at(7)}},
		Chosen:            []state.Target{{Obj: at(5)}}, ChosenValid: true,
		ExtraKeywords: []string{"Flying", "Trample"}, ExtraKeywordsOwner: at(2),
	}
	rich.TriggerCard = at(9)
	rich.TriggerPlayer = state.Target{IsPlayer: true, Player: 1}
	rich.DefendingPlayer = state.Target{IsPlayer: true, Player: 0}
	rich.TriggerTarget = state.Target{Obj: at(11)}
	layer := effects.SpecContext{You: 1, Source: at(13),
		ExtraTypes:      []string{"Goblin", "Creature"},
		ExtraTypesOwner: at(2),
		Layers: effects.LayerTables{
			DerivedTypes:   []effects.ObjectTypes{{ID: at(2), Types: []string{"Creature", "Elf"}}},
			EffectiveNames: []effects.ObjectName{{ID: at(4), Name: "Grizzly Bears"}},
			StaticGoads:    map[state.ObjID]bool{at(6): true},
		},
	}
	derived := layer
	derived.ExtraTypes = nil
	return []effects.SpecContext{
		{You: 0},
		{You: 1, Source: at(1)},
		rich,
		layer,
		derived,
	}
}

// TestCompiledFilterMatchesTextualOracle holds the compiled filter form
// exactly equal to the textual evaluator it replaced, over every parameter
// string of the repo decks' cards, every object of several mid-game states,
// and a spread of bound and unbound evaluation contexts -- for both the
// ordinary matcher and the zone-aware off-battlefield matcher.
func TestCompiledFilterMatchesTextualOracle(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	specs := repoDeckFilterStrings(t, reg)
	games := midGameStates(t, reg)
	if len(specs) < 1000 || len(games) == 0 {
		t.Fatalf("sample too small: %d specs, %d games", len(specs), len(games))
	}
	contexts := make([][]effects.SpecContext, len(games))
	for i, g := range games {
		contexts[i] = contextsFor(g)
	}
	zones := []state.Zone{state.ZGraveyard, state.ZHand, state.ZExile, state.ZLibrary}
	checks, mismatches, matched := 0, 0, 0
	report := func(format string, args ...any) {
		mismatches++
		if mismatches <= 20 {
			t.Errorf(format, args...)
		}
	}
	for _, spec := range specs {
		match, matchZone := effects.CompiledSpecForTest(spec)
		for gi, g := range games {
			ids := pickObjs(g)
			for ci, sc := range contexts[gi] {
				for _, id := range ids {
					o := g.Obj(id)
					want := effects.MatchesObjectTextOracle(g, spec, o, sc)
					got := match(g, o, sc)
					checks++
					if want {
						matched++
					}
					if got != want {
						report("game %d ctx %d spec %q obj %d (%s, zone %v): oracle %v compiled %v",
							gi, ci, spec, id, faceName(o), o.Zone, want, got)
					}
					if ci == 0 {
						if cached := effects.MatchesObjectCompiledCached(g, spec, o, sc); cached != want {
							report("cached: game %d spec %q obj %d: oracle %v cached %v", gi, spec, id, want, cached)
						}
					}
					if o.IsCopy && o.Zone != state.ZStack && o.Zone != state.ZBattlefield {
						continue
					}
					for _, z := range zones {
						want := effects.MatchesZoneTextOracle(g, spec, o, sc, z)
						checks++
						if got := matchZone(g, o, sc, z); got != want {
							report("zone %v game %d ctx %d spec %q obj %d: oracle %v compiled %v",
								z, gi, ci, spec, id, want, got)
						}
					}
				}
			}
		}
	}
	if matched == 0 {
		t.Fatal("no spec matched any object: the sample exercises nothing")
	}
	t.Logf("%d specs, %d states, %d comparisons (%d oracle matches), %d mismatches",
		len(specs), len(games), checks, matched, mismatches)
}

// TestCompiledSpecCacheConcurrent exercises the process-wide cache from many
// goroutines at once (run it under -race): every reader must see the same
// answer the oracle gives, whether it compiled the spec, raced another
// compiler, or read a promoted snapshot.
func TestCompiledSpecCacheConcurrent(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	specs := repoDeckFilterStrings(t, reg)
	g := midGameStates(t, reg)[1]
	ids := pickObjs(g)
	sc := effects.SpecContext{You: 0, Source: ids[0]}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for k := range specs {
				spec := specs[(k*7+w*131)%len(specs)]
				id := ids[(k+w)%len(ids)]
				o := g.Obj(id)
				if effects.MatchesObjectCompiledCached(g, spec, o, sc) != effects.MatchesObjectTextOracle(g, spec, o, sc) {
					t.Errorf("worker %d spec %q obj %d disagrees", w, spec, id)
					return
				}
			}
		}(w)
	}
	wg.Wait()
}

func faceName(o *state.Object) string {
	if f := o.Face(); f != nil {
		return f.Name
	}
	return "<no face>"
}
