package rules

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// A subset put-back consumes chance draws, but is NOT a whole-library shuffle
// epoch. The next real Shuffle must still be the planner's first post-genesis
// shuffle and the resulting event stream must replay from the chance tape.
func TestGreenSunSubsetRandomOrderDoesNotAdvancePlannedShuffleEpoch(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	green := searchCorpusCard(t, reg, "Green Sun's Twilight")
	if green == nil || len(green.Faces) == 0 {
		t.Fatal("precondition: real Green Sun's Twilight corpus card")
	}
	vars := green.Faces[0].SVars
	chosen := cards.ResolveSVar(vars, "DBChangeZone")
	rest := cards.ResolveSVar(vars, "RestBottom")
	if chosen == nil || chosen.Params["SubAbility"] != "RestBottom" ||
		rest == nil || rest.Params["RandomOrder"] != "True" || rest.Params["NoShuffle"] != "True" {
		t.Fatalf("precondition: paired corpus chain = %+v / %+v", chosen, rest)
	}
	deck := []*cards.Card{green}
	for i := 0; i < 6; i++ {
		deck = append(deck, searchCorpusCard(t, reg, "Grizzly Bears"))
	}
	deck = append(deck, mountainDeck(t, 40-len(deck))...)
	cfg := Config{Seed: 73, Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}}
	plannedCalls := 0
	planned, err := NewHypotheticalPlanned(cfg, nil, func(ctx ShuffleContext) ([]state.ObjID, error) {
		if ctx.Player != 0 || ctx.Ordinal == 0 {
			return nil, nil // genesis shuffle
		}
		if ctx.Ordinal != 1 || len(ctx.Library) < 3 {
			return nil, fmt.Errorf("subset consumed a whole-library shuffle epoch: %+v", ctx)
		}
		plannedCalls++
		out := make([]state.ObjID, len(ctx.Library))
		for i, card := range ctx.Library {
			out[len(out)-1-i] = card.ID
		}
		return out, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Choose two distinct cards still in the library as the deferred rest,
	// and a third as the chosen Remembered card. Leave the other cards untouched.
	prepare := func(e *Engine) (state.ObjID, []state.ObjID) {
		t.Helper()
		lib := e.G.Zone(state.ZLibrary, 0)
		if len(lib) < 5 || lib[0] == lib[1] || lib[1] == lib[2] ||
			e.G.Obj(lib[0]).Zone != state.ZLibrary || e.G.Obj(lib[1]).Zone != state.ZLibrary {
			t.Fatalf("precondition: distinct chosen, imprinted and untouched library cards: %v", lib)
		}
		var source state.ObjID
		for _, zone := range []state.Zone{state.ZHand, state.ZLibrary} {
			for _, id := range e.G.Zone(zone, 0) {
				if e.G.Obj(id).Face().Name == "Green Sun's Twilight" {
					source = id
					break
				}
			}
		}
		if source == 0 {
			t.Fatal("precondition: corpus source in hand or library")
		}
		var selected []state.ObjID
		for _, id := range lib {
			if id != source && len(selected) < 3 {
				selected = append(selected, id)
			}
		}
		if len(selected) != 3 || selected[0] == selected[1] || selected[1] == selected[2] {
			t.Fatalf("precondition: three distinct deferred cards excluding source: %v", selected)
		}
		return source, selected
	}
	resolve := func(e *Engine) {
		t.Helper()
		source, ids := prepare(e)
		e.Emit(events.Event{Kind: events.Imprint, Obj: source, IDs: ids[1:]})
		if !reflect.DeepEqual(e.G.Obj(source).Imprinted, ids[1:]) {
			t.Fatalf("precondition: imprinted library pair = %v", e.G.Obj(source).Imprinted)
		}
		ctx := &effects.Ctx{Source: source, Controller: 0, SVars: vars,
			Remembered: []state.Target{{Obj: ids[0]}}}
		effects.Resolve(e, ctx, chosen)
		lib := e.G.Zone(state.ZLibrary, 0)
		if len(lib) < 2 || lib[len(lib)-1] == lib[len(lib)-2] ||
			(lib[len(lib)-2] != ids[1] && lib[len(lib)-2] != ids[2]) {
			t.Fatalf("precondition/result: distinct imprinted pair returned to bottom: %v", lib)
		}
		for _, ev := range e.L.Events {
			if ev.Kind == events.Shuffle && ev.Player == 0 && reflect.DeepEqual(ev.IDs, ids[1:]) {
				t.Fatalf("subset was emitted as whole-library Shuffle: %+v", ev)
			}
		}
		order := e.ShuffleLibrary(0, lib)
		e.Emit(events.Event{Kind: events.Shuffle, Player: 0, IDs: order, Secret: true})
	}
	resolve(planned)
	if plannedCalls != 1 {
		t.Fatalf("post-Green Sun planner calls = %d, want one real shuffle", plannedCalls)
	}
	tape := planned.ChanceTranscript()
	replay, err := NewHypothetical(cfg, tape)
	if err != nil {
		t.Fatal(err)
	}
	resolve(replay)
	if replay.L.Head() != planned.L.Head() || replay.RNGDraws() != planned.RNGDraws() ||
		!reflect.DeepEqual(replay.G.Zone(state.ZLibrary, 0), planned.G.Zone(state.ZLibrary, 0)) {
		t.Fatalf("planned Green Sun/Shuffle replay diverged: heads %s / %s, draws %d / %d",
			planned.L.Head(), replay.L.Head(), planned.RNGDraws(), replay.RNGDraws())
	}
}
