package rules

// api:GenericChoice `AtRandom$ True` on real corpus carriers. Forge's
// ChooseGenericEffect picks an AtRandom$ choice itself (Aggregates.random),
// never asking the chooser; the engine draws the pick from its seeded rng
// at resolution so a log-only replay re-derives it.
//
//   - Face to Face: the three Notify GenericChoices carry ONE choice each
//     and AtRandom$ True (Forge's way of announcing the caster's throw to
//     the opponent, ShowChoice$ ExceptSelf). They must never be posed to the
//     caster as a one-option ask.
//   - Item Crate: four token choices, AtRandom$ True -- the random pick
//     decides which token is created.
//
// The same engine-random discipline on the other APIs whose AtRandom$ was
// unread: api:ChangeZone's hidden public-origin pick (Make a Wish) and
// library pick (Geist of Regret), and api:ChooseType's as-enters body
// (Camato Scout).

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func modeOptionByLabel(d *decision.Decision, label string) int {
	for _, o := range d.Options {
		if o.Label == label {
			return o.Index
		}
	}
	return -1
}

// TestFaceToFaceNotifyIsNotAsked casts Face to Face at seat 1, throws Rock
// against Scissors twice (two wins), and asserts the match never poses the
// one-option Notify GenericChoice to anyone, the 5 damage lands, and the
// log replays.
func TestFaceToFaceNotifyIsNotAsked(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Face to Face")}, nil)
	ensureInHand(t, e, 0, "Face to Face")
	addMana(t, e, 0, "1R")
	castCardNow(t, e, "Face to Face")
	d := passToTargetAsk(t, e)
	idx := -1
	for _, o := range d.Options {
		if o.Player == 1 && o.Obj == 0 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("seat 1 not offered as Face to Face's target: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	throws, notifyAsks := 0, 0
	for i := 0; i < 200 && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while Face to Face is on the stack")
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KModes:
			if len(d.Options) == 1 {
				notifyAsks++
				t.Errorf("one-option AtRandom$ GenericChoice posed to seat %d: %+v", d.Player, d.Options)
				submitChoices(t, e, d.Options[0].Index)
				continue
			}
			want := "Rock"
			if d.Player == 1 {
				want = "Scissors"
			}
			j := modeOptionByLabel(d, want)
			if j < 0 {
				t.Fatalf("seat %d throw ask has no %s: %+v", d.Player, want, d.Options)
			}
			throws++
			submitChoices(t, e, j)
		default:
			t.Fatalf("unexpected decision during Face to Face: %+v", d)
		}
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("Face to Face never finished resolving: stack %v", e.G.Stack)
	}
	if throws != 4 {
		t.Fatalf("throw asks = %d, want 4 (two rounds, both seats)", throws)
	}
	if notifyAsks != 0 {
		t.Fatalf("Notify GenericChoice asked %d times", notifyAsks)
	}
	if got := e.G.Players[1].Life; got != 15 {
		t.Fatalf("seat 1 life = %d, want 15 (two Rock-beats-Scissors wins)", got)
	}
	replayCheck(t, e, cfg)
}

// TestItemCrateCreatesARandomTokenWithoutAsking activates the real Item
// Crate and asserts the four-way GenericChoice is never asked, exactly one
// tapped token from the listed four is created, the pick is recorded as a
// Note, and the log replays (the rng draw is re-derived).
func TestItemCrateCreatesARandomTokenWithoutAsking(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	seen := map[string]bool{}
	_, base := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Item Crate")}, nil)
	for _, seed := range []uint64{1, 20, 40, 60, 80, 100, 120, 140} {
		cfg := base
		cfg.Seed = seed
		cfg = seatZeroStart(cfg)
		e := New(cfg)
		e.Advance()
		toMain1(t, e)
		crate := moveByName(t, e, 0, "Item Crate", state.ZBattlefield)
		addMana(t, e, 0, "R")
		submitChoices(t, e, abilityOption(t, e, crate, 0).Index)
		for i := 0; i < 40 && len(e.G.Stack) > 0; i++ {
			d := e.Pending()
			if d == nil {
				t.Fatal("no decision while Item Crate's ability is on the stack")
			}
			if d.Kind != decision.KPriority {
				t.Fatalf("seed %d: Item Crate's AtRandom$ GenericChoice posed %+v", seed, d)
			}
			submitPass(t, e)
		}
		if len(e.G.Stack) != 0 {
			t.Fatalf("seed %d: ability never resolved", seed)
		}
		var tokens []string
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			o := e.G.Obj(id)
			if o == nil || !o.IsToken || o.Face() == nil {
				continue
			}
			if !o.Tapped {
				t.Fatalf("seed %d: token %s entered untapped", seed, o.Face().Name)
			}
			tokens = append(tokens, o.Face().Name)
		}
		if len(tokens) != 1 {
			t.Fatalf("seed %d: tokens created = %v, want exactly one", seed, tokens)
		}
		switch tokens[0] {
		case "Banana", "Shell", "Star", "Best Shell":
		default:
			t.Fatalf("seed %d: token %q is not one of Item Crate's four", seed, tokens[0])
		}
		seen[tokens[0]] = true
		if countEvents(e, func(ev events.Event) bool {
			return ev.Kind == events.Note && ev.Obj == crate &&
				strings.HasPrefix(ev.Text, "chose a mode at random: ") && strings.Contains(ev.Text, tokens[0]+" with")
		}) != 1 {
			t.Fatalf("seed %d: no random-pick Note naming %s", seed, tokens[0])
		}
		replayCheck(t, e, cfg)
	}
	if len(seen) < 2 {
		t.Fatalf("eight seeds all created %v: the pick is not random", seen)
	}
}

// castNoAsk casts seat 0's named corpus card with the given pool and drains
// the stack, failing on ANY decision other than priority: every AtRandom$
// pick below is the engine's, never a player's.
func castNoAsk(t *testing.T, e *Engine, name, mana string) {
	t.Helper()
	ensureInHand(t, e, 0, name)
	addMana(t, e, 0, mana)
	castCardNow(t, e, name)
	for i := 0; i < 60 && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while %s's stack drains", name)
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("%s: an AtRandom$ pick posed %+v", name, d)
		}
		submitPass(t, e)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("%s never finished resolving: stack %v", name, e.G.Stack)
	}
}

func zoneNames(e *Engine, z state.Zone, p state.PlayerID) []string {
	var out []string
	for _, id := range e.G.Zone(z, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil {
			out = append(out, o.Face().Name)
		}
	}
	return out
}

// TestMakeAWishReturnsTwoRandomCards: api:ChangeZone `Hidden$ True |
// AtRandom$ True` from a public origin (the graveyard) is the engine's
// random pick of ChangeNum$ cards, never a hidden-pick ask.
func TestMakeAWishReturnsTwoRandomCards(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	yard := []string{"Grizzly Bears", "Llanowar Elves", "Giant Growth", "Lightning Bolt"}
	extras := []*cards.Card{lookup(t, reg, "Make a Wish")}
	for _, n := range yard {
		extras = append(extras, lookup(t, reg, n))
	}
	_, base := corpusEngineCfg(t, reg, extras, nil)
	picks := map[string]bool{}
	for _, seed := range []uint64{1, 20, 40, 60, 80, 100} {
		cfg := base
		cfg.Seed = seed
		cfg = seatZeroStart(cfg)
		e := New(cfg)
		e.Advance()
		toMain1(t, e)
		for _, n := range yard {
			moveByName(t, e, 0, n, state.ZGraveyard)
		}
		castNoAsk(t, e, "Make a Wish", "GGGG")
		var back []string
		for _, n := range zoneNames(e, state.ZHand, 0) {
			for _, y := range yard {
				if n == y {
					back = append(back, n)
				}
			}
		}
		if len(back) != 2 {
			t.Fatalf("seed %d: returned %v, want exactly two of %v", seed, back, yard)
		}
		if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 3 {
			t.Fatalf("seed %d: graveyard holds %d, want 3 (two left + Make a Wish)", seed, got)
		}
		picks[strings.Join(back, "+")] = true
		replayCheck(t, e, cfg)
	}
	if len(picks) < 2 {
		t.Fatalf("six seeds all returned %v: the pick is not random", picks)
	}
}

// TestGeistOfRegretMillsRandomInstantAndSorcery: the library-origin twin
// (`Origin$ Library | Hidden$ True | AtRandom$ True | NoLooking$ True`) on
// the ETB trigger: one instant and one sorcery reach the graveyard with no
// search ask.
func TestGeistOfRegretMillsRandomInstantAndSorcery(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Geist of Regret"),
		lookup(t, reg, "Lightning Bolt"), lookup(t, reg, "Giant Growth"), lookup(t, reg, "Divination")}, nil)
	for _, n := range []string{"Lightning Bolt", "Giant Growth", "Divination"} {
		for _, id := range e.G.Zone(state.ZHand, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == n {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
			}
		}
	}
	castNoAsk(t, e, "Geist of Regret", "UUUUU")
	yard := zoneNames(e, state.ZGraveyard, 0)
	instants, sorceries := 0, 0
	for _, n := range yard {
		switch n {
		case "Lightning Bolt", "Giant Growth":
			instants++
		case "Divination":
			sorceries++
		}
	}
	if instants != 1 || sorceries != 1 {
		t.Fatalf("graveyard %v, want exactly one instant and Divination", yard)
	}
	replayCheck(t, e, cfg)
}

// TestCamatoScoutChoosesARandomLandType: api:ChooseType `AtRandom$ True` on
// the as-enters body (K:ETBReplacement:Other) records a basic land type the
// engine drew, with no entry ask.
func TestCamatoScoutChoosesARandomLandType(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	_, base := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Camato Scout")}, nil)
	seen := map[string]bool{}
	for _, seed := range []uint64{1, 20, 40, 60, 80, 100, 120, 140} {
		cfg := base
		cfg.Seed = seed
		cfg = seatZeroStart(cfg)
		e := New(cfg)
		e.Advance()
		toMain1(t, e)
		castNoAsk(t, e, "Camato Scout", "UUU")
		var scout *state.Object
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Camato Scout" {
				scout = o
			}
		}
		if scout == nil {
			t.Fatalf("seed %d: Camato Scout not on the battlefield", seed)
		}
		switch scout.ChosenType {
		case "Plains", "Island", "Swamp", "Mountain", "Forest":
		default:
			t.Fatalf("seed %d: chosen type %q is not a basic land type", seed, scout.ChosenType)
		}
		seen[scout.ChosenType] = true
		replayCheck(t, e, cfg)
	}
	if len(seen) < 2 {
		t.Fatalf("eight seeds all chose %v: the pick is not random", seen)
	}
}
