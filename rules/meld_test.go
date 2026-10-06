package rules

// Meld (CR 701.42, 712.4): one melded permanent backed by two component
// cards. These drive the meld primitive effects.Meld directly with real
// corpus meld pairs; the api:Meld handler that calls it is separate work.

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// meldGame seats two players whose decks lead with the named corpus cards and
// drives to the first main phase. Nothing is advanced after it, so no ETB
// trigger of a card put onto the battlefield below ever resolves.
func meldGame(t *testing.T, reg *cards.Registry, seat0, seat1 []string) (*Engine, Config) {
	t.Helper()
	cs := func(names []string) []*cards.Card {
		out := make([]*cards.Card, 0, len(names))
		for _, n := range names {
			out = append(out, lookup(t, reg, n))
		}
		return out
	}
	return corpusEngineCfg(t, reg, cs(seat0), cs(seat1))
}

// meldPut moves owner's copy of the named card from library or hand onto the
// battlefield with a logged MoveZone and returns its id.
func meldPut(t *testing.T, e *Engine, name string, owner state.PlayerID) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, owner) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZBattlefield})
				if e.G.Obj(id).Zone != state.ZBattlefield {
					t.Fatalf("precondition: %s did not enter the battlefield", name)
				}
				return id
			}
		}
	}
	t.Fatalf("corpus card %q not in seat %d's hand or library", name, owner)
	return 0
}

// assertMelded checks the melded permanent's representation: exactly one
// battlefield object (the result card, on its meld face) under controller,
// the partner card parked off every zone list, and both components named.
func assertMelded(t *testing.T, e *Engine, res, partner state.ObjID, controller state.PlayerID, resultName string, power, toughness int32) {
	t.Helper()
	o := e.G.Obj(res)
	if o.Zone != state.ZBattlefield || o.Controller != controller {
		t.Fatalf("melded permanent zone=%s controller=%d, want battlefield under %d", o.Zone, o.Controller, controller)
	}
	if got := e.Name(res); got != resultName {
		t.Fatalf("melded permanent name = %q, want %q", got, resultName)
	}
	if p, tg := e.Power(res), e.Toughness(res); p != power || tg != toughness {
		t.Fatalf("melded permanent is %d/%d, want %d/%d", p, tg, power, toughness)
	}
	r, pt, ok := o.MeldComponents()
	if !ok || r != res || pt != partner {
		t.Fatalf("MeldComponents = %d,%d,%v; want %d,%d,true", r, pt, ok, res, partner)
	}
	p := e.G.Obj(partner)
	if p.Zone != state.ZCeased || p.Melded() {
		t.Fatalf("partner card zone=%s melded=%v, want parked in ceased", p.Zone, p.Melded())
	}
	for pl := range e.G.Players {
		for _, z := range []state.Zone{state.ZBattlefield, state.ZExile, state.ZGraveyard, state.ZHand, state.ZLibrary} {
			if slices.Contains(e.G.Zone(z, state.PlayerID(pl)), partner) {
				t.Fatalf("partner card %d is still listed in seat %d's %s", partner, pl, z)
			}
		}
	}
	n := 0
	for pl := range e.G.Players {
		for _, id := range e.G.Zone(state.ZBattlefield, state.PlayerID(pl)) {
			if id == res || id == partner {
				n++
			}
		}
	}
	if n != 1 {
		t.Fatalf("melded permanent occupies %d battlefield slots, want 1", n)
	}
}

// assertSplit checks that both component cards landed in `to`, each in its
// own owner's zone, back on their front faces and no longer melded.
func assertSplit(t *testing.T, e *Engine, to state.Zone, ids map[state.ObjID]string) {
	t.Helper()
	for id, front := range ids {
		o := e.G.Obj(id)
		if o.Zone != to || !slices.Contains(e.G.Zone(to, o.Owner), id) {
			t.Fatalf("%s (obj %d) is in %s, want its owner's (seat %d) %s", front, id, o.Zone, o.Owner, to)
		}
		if o.Melded() || o.Face() == nil || o.Face().Name != front {
			t.Fatalf("obj %d after the split: melded=%v face=%v, want unmelded front face %q", id, o.Melded(), o.Face(), front)
		}
		if to != state.ZLibrary && to != state.ZHand && o.EnteredFrom != state.ZBattlefield {
			t.Fatalf("%s entered %s from %s, want from the battlefield", front, to, o.EnteredFrom)
		}
	}
}

func TestMeldVanilleFangRagnarok(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	const vanille, fang, ragnarok = "Vanille, Cheerful l'Cie", "Fang, Fearless l'Cie", "Ragnarok, Divine Deliverance"
	for _, to := range []state.Zone{state.ZGraveyard, state.ZHand, state.ZExile, state.ZLibrary} {
		t.Run(to.String(), func(t *testing.T) {
			t.Parallel()
			e, cfg := meldGame(t, reg, []string{vanille, fang}, nil)
			v, f := meldPut(t, e, vanille, 0), meldPut(t, e, fang, 0)
			if e.Power(v) != 3 || e.Power(f) != 2 || e.HasKeyword(v, "Trample") {
				t.Fatalf("precondition: Vanille %d/%d, Fang %d/%d", e.Power(v), e.Toughness(v), e.Power(f), e.Toughness(f))
			}
			// Secondary first: the primitive finds the result card itself.
			res, ok := effects.Meld(e, 0, f, v, state.MeldOptions{ResultName: ragnarok, PrimaryName: vanille, SecondaryName: fang})
			if !ok || res != v {
				t.Fatalf("Meld = %d,%v; want Vanille's object %d", res, ok, v)
			}
			assertMelded(t, e, v, f, 0, ragnarok, 7, 6)
			for _, kw := range []string{"Vigilance", "Menace", "Trample", "Reach", "Haste"} {
				if !e.HasKeyword(v, kw) {
					t.Fatalf("Ragnarok lacks %s", kw)
				}
			}
			if !e.IsCreature(v) {
				t.Fatal("Ragnarok is not a creature")
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: v, From: state.ZBattlefield, To: to})
			assertSplit(t, e, to, map[state.ObjID]string{v: vanille, f: fang})
			if diff := diffGames(e.G, replayFromLog(t, cfg, e.L.Events)); diff != "" {
				t.Fatalf("log-only replay differs:\n%s", diff)
			}
		})
	}
}

func TestMeldGiselaBrunaBrisela(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	const gisela, bruna, brisela = "Gisela, the Broken Blade", "Bruna, the Fading Light", "Brisela, Voice of Nightmares"
	for _, to := range []state.Zone{state.ZGraveyard, state.ZHand, state.ZExile} {
		t.Run(to.String(), func(t *testing.T) {
			t.Parallel()
			e, cfg := meldGame(t, reg, []string{gisela, bruna}, nil)
			g, b := meldPut(t, e, gisela, 0), meldPut(t, e, bruna, 0)
			if e.Power(g) != 4 || e.Power(b) != 5 {
				t.Fatalf("precondition: Gisela %d power, Bruna %d power", e.Power(g), e.Power(b))
			}
			res, ok := effects.Meld(e, 0, g, b, state.MeldOptions{ResultName: brisela})
			if !ok || res != g {
				t.Fatalf("Meld = %d,%v; want Gisela's object %d", res, ok, g)
			}
			assertMelded(t, e, g, b, 0, brisela, 9, 10)
			for _, kw := range []string{"Flying", "First Strike", "Vigilance", "Lifelink"} {
				if !e.HasKeyword(g, kw) {
					t.Fatalf("Brisela lacks %s", kw)
				}
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: g, From: state.ZBattlefield, To: to})
			assertSplit(t, e, to, map[state.ObjID]string{g: gisela, b: bruna})
			if diff := diffGames(e.G, replayFromLog(t, cfg, e.L.Events)); diff != "" {
				t.Fatalf("log-only replay differs:\n%s", diff)
			}
		})
	}
}

// TestMeldTwoOwners melds a pair whose cards have different owners (seat 1's
// Bruna under seat 0's control): the melded permanent is seat 0's, and when
// it leaves each card goes to its OWN owner's zone.
func TestMeldTwoOwners(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	const gisela, bruna, brisela = "Gisela, the Broken Blade", "Bruna, the Fading Light", "Brisela, Voice of Nightmares"
	for _, to := range []state.Zone{state.ZGraveyard, state.ZHand, state.ZExile} {
		t.Run(to.String(), func(t *testing.T) {
			t.Parallel()
			e, cfg := meldGame(t, reg, []string{gisela}, []string{bruna})
			g, b := meldPut(t, e, gisela, 0), meldPut(t, e, bruna, 1)
			e.emit(events.Event{Kind: events.ControlChange, Obj: b, Player: 0, Text: "GainControl"})
			if e.G.Obj(b).Owner != 1 || e.G.Obj(b).Controller != 0 {
				t.Fatalf("precondition: Bruna owner=%d controller=%d, want 1/0", e.G.Obj(b).Owner, e.G.Obj(b).Controller)
			}
			res, ok := effects.Meld(e, 0, g, b, state.MeldOptions{ResultName: brisela})
			if !ok || res != g {
				t.Fatalf("Meld = %d,%v; want Gisela's object %d", res, ok, g)
			}
			assertMelded(t, e, g, b, 0, brisela, 9, 10)
			e.emit(events.Event{Kind: events.MoveZone, Obj: g, From: state.ZBattlefield, To: to})
			assertSplit(t, e, to, map[state.ObjID]string{g: gisela, b: bruna})
			if e.G.Obj(g).Owner == e.G.Obj(b).Owner {
				t.Fatal("precondition: the two cards must have different owners")
			}
			if diff := diffGames(e.G, replayFromLog(t, cfg, e.L.Events)); diff != "" {
				t.Fatalf("log-only replay differs:\n%s", diff)
			}
		})
	}
}

// TestMeldEntersTappedAndAttacking covers Mishra's "It enters tapped and
// attacking" riders on the primitive.
func TestMeldEntersTappedAndAttacking(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	const mishra, engine, lost = "Mishra, Claimed by Gix", "Phyrexian Dragon Engine", "Mishra, Lost to Phyrexia"
	t.Run("attacking", func(t *testing.T) {
		t.Parallel()
		e, cfg := meldGame(t, reg, []string{mishra, engine}, nil)
		m, d := meldPut(t, e, mishra, 0), meldPut(t, e, engine, 0)
		res, ok := effects.Meld(e, 0, m, d, state.MeldOptions{ResultName: lost, Tapped: true, Attacking: true, Defender: 1})
		if !ok {
			t.Fatal("Mishra did not meld")
		}
		assertMelded(t, e, res, d, 0, lost, 9, 9)
		o := e.G.Obj(res)
		if !o.Tapped || !o.IsAttacking || o.Attacking != 1 {
			t.Fatalf("Mishra, Lost to Phyrexia tapped=%v attacking=%v defender=%d, want tapped and attacking seat 1", o.Tapped, o.IsAttacking, o.Attacking)
		}
		if diff := diffGames(e.G, replayFromLog(t, cfg, e.L.Events)); diff != "" {
			t.Fatalf("log-only replay differs:\n%s", diff)
		}
	})
	t.Run("tapped", func(t *testing.T) {
		t.Parallel()
		e, _ := meldGame(t, reg, []string{mishra, engine}, nil)
		m, d := meldPut(t, e, mishra, 0), meldPut(t, e, engine, 0)
		res, ok := effects.Meld(e, 0, m, d, state.MeldOptions{Tapped: true})
		if !ok {
			t.Fatal("Mishra did not meld")
		}
		if o := e.G.Obj(res); !o.Tapped || o.IsAttacking {
			t.Fatalf("tapped=%v attacking=%v, want tapped and not attacking", o.Tapped, o.IsAttacking)
		}
	})
}

// TestMeldRefusesANonPair is CR 701.42c: two permanents that are not a meld
// pair are exiled and stay exiled; nothing enters.
func TestMeldRefusesANonPair(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cases := []struct {
		name string
		a, b string
		opts state.MeldOptions
	}{
		{"wrong partner", "Gisela, the Broken Blade", "Fang, Fearless l'Cie",
			state.MeldOptions{PrimaryName: "Gisela, the Broken Blade", SecondaryName: "Bruna, the Fading Light"}},
		{"two result cards", "Gisela, the Broken Blade", "Vanille, Cheerful l'Cie", state.MeldOptions{}},
		{"wrong result name", "Gisela, the Broken Blade", "Bruna, the Fading Light",
			state.MeldOptions{ResultName: "Ragnarok, Divine Deliverance"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, cfg := meldGame(t, reg, []string{tc.a, tc.b}, nil)
			a, b := meldPut(t, e, tc.a, 0), meldPut(t, e, tc.b, 0)
			if _, ok := effects.Meld(e, 0, a, b, tc.opts); ok {
				t.Fatal("a non-pair melded")
			}
			for _, id := range []state.ObjID{a, b} {
				if o := e.G.Obj(id); o.Zone != state.ZExile || o.Melded() || o.FaceIdx != 0 {
					t.Fatalf("obj %d zone=%s melded=%v face=%d, want exiled on its front face", id, o.Zone, o.Melded(), o.FaceIdx)
				}
			}
			if diff := diffGames(e.G, replayFromLog(t, cfg, e.L.Events)); diff != "" {
				t.Fatalf("log-only replay differs:\n%s", diff)
			}
		})
	}
}
