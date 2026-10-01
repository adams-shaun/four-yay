package searchprobe

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// redealPlayerReference is redealPlayer as it was before the canonical deal
// order was precomputed per boundary (it sorted the library pool by name on
// every deal). TestRedealMatchesTheReferenceDeal holds the two to the same
// event stream, so the precomputation changes no world.
func redealPlayerReference(w *rules.Engine, plan redealPlan, r *rand.Rand) string {
	pinHand := make(map[state.ObjID]bool)
	for _, id := range plan.pins {
		pinHand[id] = true
	}
	deal := append([]state.ObjID(nil), plan.unknown...)
	r.Shuffle(len(deal), func(i, j int) { deal[i], deal[j] = deal[j], deal[i] })
	if plan.handFree < 0 || plan.handFree > len(deal) {
		return fmt.Sprintf("player %d hand does not fit its pool", plan.player)
	}
	newHand := deal[:plan.handFree]
	free := append(append([]state.ObjID(nil), deal[plan.handFree:]...), plan.loose...)
	sortByName(w, free)
	r.Shuffle(len(free), func(i, j int) { free[i], free[j] = free[j], free[i] })
	lib := make([]state.ObjID, plan.libLen)
	for i, id := range plan.top {
		lib[i] = id
	}
	for i, id := range plan.bot {
		at := plan.libLen - len(plan.bot) + i
		if lib[at] != 0 && lib[at] != id {
			return fmt.Sprintf("player %d known top and bottom disagree", plan.player)
		}
		lib[at] = id
	}
	next := 0
	for i := range lib {
		if lib[i] != 0 {
			continue
		}
		if next >= len(free) {
			return fmt.Sprintf("player %d library does not fit its pool", plan.player)
		}
		lib[i] = free[next]
		next++
	}
	if next != len(free) {
		return fmt.Sprintf("player %d library does not fit its pool", plan.player)
	}
	if plan.handFree > 0 {
		for _, id := range plan.hand {
			events.Emit(w.G, w.L, events.Event{Kind: events.MoveZone, Player: plan.player, Obj: id, From: state.ZHand, To: state.ZLibrary, Secret: true})
		}
		var pins []state.ObjID
		for id := range pinHand {
			pins = append(pins, id)
		}
		sort.Slice(pins, func(i, j int) bool { return pins[i] < pins[j] })
		for _, id := range append(pins, newHand...) {
			events.Emit(w.G, w.L, events.Event{Kind: events.MoveZone, Player: plan.player, Obj: id, From: state.ZLibrary, To: state.ZHand, Secret: true})
		}
	}
	events.Emit(w.G, w.L, events.Event{Kind: events.LibraryOrder, Player: plan.player, IDs: lib, Secret: true})
	if got := w.G.Zone(state.ZHand, plan.player); len(got) != len(plan.hand) || !slices.Equal(w.G.Zone(state.ZLibrary, plan.player), lib) {
		return fmt.Sprintf("player %d redeal did not land", plan.player)
	}
	return ""
}

// TestRedealMatchesTheReferenceDeal: on the real-deck bench root and the
// pinned-hand / known-top fixtures, every Deal lands exactly the event stream
// (log head) and hidden zones the reference deal does from the same seed.
func TestRedealMatchesTheReferenceDeal(t *testing.T) {
	type root struct {
		name  string
		setup PublicGame
		h     History
		e     *rules.Engine
		c     *Collector
	}
	f := benchRoot(t)
	roots := []root{{"bench", f.setup, f.h, f.engine, f.collector}}
	for _, tc := range []struct {
		name, sa string
		kind     events.Kind
		to       state.Zone
	}{
		{"rearrange", "RearrangeTopOfLibrary | Defined$ You | NumCards$ 3", events.LibraryOrder, state.ZLibrary},
		{"bounce", "ChangeZoneAll | Origin$ Battlefield | Destination$ Hand | ChangeType$ Land", events.MoveZone, state.ZHand},
	} {
		decks := effectDecks(t, tc.sa)
		decks[0] = uniqueLibraryCards(t, decks[0])
		g := playEffectGame(t, decks, effectTape(nil), tc.kind, tc.to)
		roots = append(roots, root{tc.name, g.setup, g.h, g.engine, g.collector})
	}
	for _, rt := range roots {
		known, err := ProjectKnownCards(rt.h)
		if err != nil {
			t.Fatal(err)
		}
		r, refused := NewRedealer(rt.setup, rt.h, known, RedealBase{Engine: rt.e, Observer: rt.c})
		if refused != "" {
			t.Fatalf("%s: refused %q", rt.name, refused)
		}
		pinned, freeHand := 0, 0
		for _, p := range r.plans {
			pinned += len(p.pins) + len(p.top) + len(p.loose)
			freeHand += p.handFree
		}
		for _, p := range r.plans {
			t.Logf("%s p%d pins %d handFree %d top %d loose %d unknown %d", rt.name, p.player, len(p.pins), p.handFree, len(p.top), len(p.loose), len(p.unknown))
		}
		if freeHand == 0 {
			t.Fatalf("%s: nothing is dealt into a hand", rt.name)
		}
		if rt.name != "bench" && pinned == 0 {
			t.Fatalf("%s: fixture pins nothing", rt.name)
		}
		var sp rules.Spare
		var prev *rules.Engine
		for i := 0; i < 64; i++ {
			seed := [2]uint64{uint64(i)*0x9e3779b97f4a7c15 + 1, uint64(i) ^ 0x5151}
			if prev != nil {
				sp = prev.Release()
			}
			w, reason := r.Deal(seed, &sp)
			if reason != "" {
				t.Fatalf("%s seed %d: %s", rt.name, i, reason)
			}
			prev = w
			ref := rt.e.CloneHypotheticalInto(seed[1], nil)
			rng := rand.New(rand.NewPCG(seed[0], seed[1]))
			for _, plan := range r.plans {
				if reason := redealPlayerReference(ref, plan, rng); reason != "" {
					t.Fatalf("%s seed %d reference: %s", rt.name, i, reason)
				}
			}
			if w.L.Head() != ref.L.Head() || len(w.L.Events) != len(ref.L.Events) {
				t.Fatalf("%s seed %d: deal diverges from the reference", rt.name, i)
			}
			for p := range w.G.Players {
				for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
					if !slices.Equal(w.G.Zone(z, state.PlayerID(p)), ref.G.Zone(z, state.PlayerID(p))) {
						t.Fatalf("%s seed %d: player %d %v differs from the reference", rt.name, i, p, z)
					}
				}
			}
		}
	}
}

// TestRedealPlayerMatchesTheReferenceOnPinnedShapes covers the plan shapes no
// played fixture reaches: several pinned hand cards beside free hand slots,
// position-less known library members, and a known top and bottom, all on
// the bench root's opponent.
func TestRedealPlayerMatchesTheReferenceOnPinnedShapes(t *testing.T) {
	f := benchRoot(t)
	e := f.engine
	p := state.PlayerID(1)
	hand, lib := e.G.Zone(state.ZHand, p), e.G.Zone(state.ZLibrary, p)
	if len(hand) < 4 || len(lib) < 10 {
		t.Fatalf("fixture too small: hand %d library %d", len(hand), len(lib))
	}
	for _, shape := range []struct{ pins, top, bot, loose int }{{2, 0, 0, 0}, {3, 2, 1, 3}, {0, 1, 0, 4}, {len(hand), 0, 2, 2}} {
		plan := redealPlan{player: p, hand: slices.Clone(hand), libLen: len(lib)}
		plan.pins = slices.Clone(hand[:shape.pins])
		slices.Sort(plan.pins)
		plan.top = slices.Clone(lib[:shape.top])
		plan.bot = slices.Clone(lib[len(lib)-shape.bot:])
		plan.loose = slices.Clone(lib[shape.top : shape.top+shape.loose])
		known := make(map[state.ObjID]bool)
		for _, ids := range [][]state.ObjID{plan.pins, plan.top, plan.bot, plan.loose} {
			for _, id := range ids {
				known[id] = true
			}
		}
		for _, id := range append(slices.Clone(hand), lib...) {
			if !known[id] {
				plan.unknown = append(plan.unknown, id)
			}
		}
		sortByName(e, plan.unknown)
		plan.handFree = len(hand) - len(plan.pins)
		plan.index(e)
		var sc dealScratch
		for i := 0; i < 32; i++ {
			seed := [2]uint64{uint64(i) + 11, uint64(i) * 7}
			w := e.CloneHypotheticalInto(seed[1], nil)
			ref := e.CloneHypotheticalInto(seed[1], nil)
			got := redealPlayer(w, &plan, rand.New(rand.NewPCG(seed[0], seed[1])), &sc)
			want := redealPlayerReference(ref, plan, rand.New(rand.NewPCG(seed[0], seed[1])))
			if (got == "") != (want == "") {
				t.Fatalf("shape %+v seed %d: %q, reference %q", shape, i, got, want)
			}
			if w.L.Head() != ref.L.Head() || !slices.Equal(w.G.Zone(state.ZHand, p), ref.G.Zone(state.ZHand, p)) || !slices.Equal(w.G.Zone(state.ZLibrary, p), ref.G.Zone(state.ZLibrary, p)) {
				t.Fatalf("shape %+v seed %d: deal diverges from the reference", shape, i)
			}
		}
	}
}
