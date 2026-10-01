package searchprobe

import (
	"reflect"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

func hiddenNames(e *rules.Engine, p state.PlayerID) []string {
	var out []string
	for _, zone := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(zone, p) {
			out = append(out, e.G.Obj(id).Card.Faces[0].Name)
		}
	}
	sort.Strings(out)
	return out
}

func zoneNames(e *rules.Engine, zone state.Zone, p state.PlayerID) []string {
	var out []string
	for _, id := range e.G.Zone(zone, p) {
		out = append(out, e.G.Obj(id).Card.Faces[0].Name)
	}
	return out
}

// TestRedealFallbackOnStarvedRealDeckSample: the bench fixture with too few
// attempts for its world count starves; the fallback supplies redealt worlds
// that pin every known card, keep each seat's hidden multiset and zone sizes,
// capture the same observation, play hypothetically, differ from one another,
// and leave the live engine untouched.
func TestRedealFallbackOnStarvedRealDeckSample(t *testing.T) {
	f := benchRoot(t)
	head, n := f.engine.L.Head(), len(f.engine.L.Events)
	opts := SampleOptions{Seed: 7, Attempts: 2, Worlds: 8, MaxSubmits: 5000}
	plain, err := Sample(f.setup, f.h, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Worlds) != 0 || plain.Redealt != 0 || plain.RedealRefused != "" {
		t.Fatalf("fixture does not starve without the fallback: %d worlds", len(plain.Worlds))
	}
	opts.Redeal = &RedealBase{Engine: f.engine, Observer: f.collector}
	res, err := Sample(f.setup, f.h, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.RedealRefused != "" || res.Redealt != 8 || len(res.Worlds) != 8 {
		t.Fatalf("redeal: refused %q, %d worlds", res.RedealRefused, len(res.Worlds))
	}
	known, err := ProjectKnownCards(f.h)
	if err != nil {
		t.Fatal(err)
	}
	distinct := make(map[string]bool)
	for _, w := range res.Worlds {
		if err := known.Holds(w); err != nil {
			t.Fatal(err)
		}
		for p := range w.Engine.G.Players {
			pid := state.PlayerID(p)
			if !reflect.DeepEqual(hiddenNames(w.Engine, pid), hiddenNames(f.engine, pid)) {
				t.Fatalf("player %d hidden multiset changed", p)
			}
			if len(w.Engine.G.Zone(state.ZHand, pid)) != len(f.engine.G.Zone(state.ZHand, pid)) || len(w.Engine.G.Zone(state.ZLibrary, pid)) != len(f.engine.G.Zone(state.ZLibrary, pid)) {
				t.Fatalf("player %d hidden zone sizes changed", p)
			}
		}
		if !reflect.DeepEqual(zoneNames(w.Engine, state.ZHand, 0), zoneNames(f.engine, state.ZHand, 0)) {
			t.Fatal("the actor's own hand was redealt")
		}
		frame, err := w.Observer.Clone().Capture(w.Engine, nil)
		if err != nil {
			t.Fatal(err)
		}
		if frame.Board.Sum != f.h.Frames[len(f.h.Frames)-1].Board.Sum {
			t.Fatal("redealt world changes the observed board")
		}
		key := ""
		for _, name := range zoneNames(w.Engine, state.ZHand, 1) {
			key += name + "|"
		}
		for _, name := range zoneNames(w.Engine, state.ZLibrary, 1) {
			key += name + "|"
		}
		distinct[key] = true
	}
	if len(distinct) < 2 {
		t.Fatal("redealt worlds are all the same deal")
	}
	// The worlds play: rollouts submit hypothetically on them.
	if _, err := TeacherChoice(res.Worlds[:2], benchCandidates(f.h.Frames[len(f.h.Frames)-1].Decision)[:2], TeacherOptions{Seed: 3, MaxSubmits: 5000}); err != nil {
		t.Fatal(err)
	}
	if f.engine.L.Head() != head || len(f.engine.L.Events) != n {
		t.Fatal("sampling or searching redealt worlds changed the live engine")
	}
	again, err := Sample(f.setup, f.h, opts)
	if err != nil {
		t.Fatal(err)
	}
	for i := range again.Worlds {
		if again.Worlds[i].Engine.L.Head() != res.Worlds[i].Engine.L.Head() {
			t.Fatal("redeal is not deterministic")
		}
	}
}

// TestRedealPinsKnownPositionsAndHands: on the synthetic rearrange and bounce
// fixtures, a forced fallback keeps the rearranged top and the bounced
// opponent cards exactly where the seat knows they are.
func TestRedealPinsKnownPositionsAndHands(t *testing.T) {
	for _, tc := range []struct {
		name, sa string
		kind     events.Kind
		to       state.Zone
	}{
		{"rearrange", "RearrangeTopOfLibrary | Defined$ You | NumCards$ 3", events.LibraryOrder, state.ZLibrary},
		{"bounce", "ChangeZoneAll | Origin$ Battlefield | Destination$ Hand | ChangeType$ Land", events.MoveZone, state.ZHand},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decks := effectDecks(t, tc.sa)
			decks[0] = uniqueLibraryCards(t, decks[0])
			g := playEffectGame(t, decks, effectTape(nil), tc.kind, tc.to)
			known, err := ProjectKnownCards(g.h)
			if err != nil {
				t.Fatal(err)
			}
			if known.Count() == 0 {
				t.Fatal("fixture learned nothing")
			}
			// More worlds than attempts: the rejection sampler must starve.
			res, err := Sample(g.setup, g.h, SampleOptions{Seed: 5, Attempts: 2, Worlds: 6, MaxSubmits: 5000, Redeal: &RedealBase{Engine: g.engine, Observer: g.collector}})
			if err != nil {
				t.Fatal(err)
			}
			if res.RedealRefused != "" || len(res.Worlds) != 6 {
				t.Fatalf("redeal refused %q (%d worlds)", res.RedealRefused, len(res.Worlds))
			}
			for _, w := range res.Worlds {
				if err := known.Holds(w); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// TestRedealIgnoresHiddenArrangement: two bases that differ only in where the
// opponent's never-revealed card sits redeal to identical hidden zones.
func TestRedealIgnoresHiddenArrangement(t *testing.T) {
	decks := effectDecks(t, "ChangeZoneAll | Origin$ Battlefield | Destination$ Hand | ChangeType$ Land")
	decks[1] = append([]*cards.Card(nil), decks[1]...)
	decks[1][0] = syntheticCard(t, "Name:Secret Relic\nManaCost:9\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:Fixture.\n")
	a := playEffectGame(t, decks, effectTape(func(int) int { return 0 }), events.MoveZone, state.ZHand)
	b := playEffectGame(t, decks, effectTape(func(n int) int {
		if n == 20 {
			return 1
		}
		return 0
	}), events.MoveZone, state.ZHand)
	if reflect.DeepEqual(zoneNames(a.engine, state.ZLibrary, 1), zoneNames(b.engine, state.ZLibrary, 1)) {
		t.Fatal("fixture needs different hidden arrangements")
	}
	opts := SampleOptions{Seed: 5, Attempts: 2, Worlds: 6, MaxSubmits: 5000}
	opts.Redeal = &RedealBase{Engine: a.engine, Observer: a.collector}
	ra, err := Sample(a.setup, a.h, opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.Redeal = &RedealBase{Engine: b.engine, Observer: b.collector}
	rb, err := Sample(b.setup, b.h, opts)
	if err != nil {
		t.Fatal(err)
	}
	if ra.RedealRefused != "" || rb.RedealRefused != "" || len(ra.Worlds) != 6 || len(rb.Worlds) != 6 {
		t.Fatalf("redeal refused: %q / %q", ra.RedealRefused, rb.RedealRefused)
	}
	for i := range ra.Worlds {
		for p := state.PlayerID(0); p < 2; p++ {
			for _, zone := range []state.Zone{state.ZHand, state.ZLibrary} {
				// Objects of the same name are interchangeable here (which
				// Mountain was played differs between the bases); names are
				// what a hidden arrangement could leak.
				if !reflect.DeepEqual(zoneNames(ra.Worlds[i].Engine, zone, p), zoneNames(rb.Worlds[i].Engine, zone, p)) {
					t.Fatalf("world %d player %d %v depends on the hidden arrangement", i, p, zone)
				}
			}
		}
	}
}
