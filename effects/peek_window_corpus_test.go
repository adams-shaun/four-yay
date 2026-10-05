package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// peekCorpusSA resolves an actual ability/SVar from the pinned compiled card.
// Missing cards or changed script shape are errors, not corpus-dependent skips.
func peekCorpusSA(t *testing.T, cardName, svarName string) (*cards.Card, *cards.SA) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup(cardName)
	if !ok {
		t.Fatalf("corpus pin missing %s", cardName)
	}
	for _, face := range card.Faces {
		if svarName == "" {
			for _, sa := range face.Abilities {
				if sa.API == "PeekAndReveal" {
					return card, sa
				}
			}
			continue
		}
		if sa := cards.ResolveSVar(face.SVars, svarName); sa != nil {
			return card, sa
		}
	}
	t.Fatalf("corpus pin moved: %s has no expected PeekAndReveal SA/SVar %q", cardName, svarName)
	return nil, nil
}

func TestGatheringStonePeekAndRevealWindow(t *testing.T) {
	card, sa := peekCorpusSA(t, "Gathering Stone", "TrigPeek")
	if sa.API != "PeekAndReveal" || sa.Params["PeekAmount"] != "1" ||
		sa.Params["RevealValid"] != "Card.ChosenType" || sa.Params["RevealOptional"] != "True" {
		t.Fatalf("corpus pin moved: Gathering Stone TrigPeek = API %q params %+v; want PeekAndReveal, PeekAmount 1, RevealValid Card.ChosenType, RevealOptional True", sa.API, sa.Params)
	}
	face := card.Faces[0]
	// Both boards use the actual script SA; inline cards make the top/window
	// ordering explicit and keep Forge text out of tracked fixtures.
	for _, tc := range []struct {
		name          string
		library       []string
		wantTopReveal bool
	}{
		{name: "matching card below window is not revealed", library: []string{peekIsle, peekBear}},
		{name: "matching card in window is revealed", library: []string{peekBear, peekIsle}, wantTopReveal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, src, lib := riderBoard(t, tc.library...)
			h.g.Obj(src).ChosenType = "Creature"
			if !peekContainsID(h.g.Zone(state.ZBattlefield, 0), src) || h.g.Zone(state.ZLibrary, 0)[0] != lib[0] || h.g.Obj(src).ChosenType == "" {
				t.Fatal("precondition: library top/order or Gathering Stone chosen type missing")
			}
			if matchesCreature(h.g.Obj(lib[0])) != tc.wantTopReveal {
				t.Fatalf("precondition: top card creature status does not match expected reveal for %s", tc.name)
			}
			ctx := &Ctx{Source: src, Controller: 0, SVars: face.SVars}
			effReveal(h, ctx, sa)
			var revealed bool
			for _, e := range h.log {
				if e.Kind != events.Note || e.Secret {
					continue
				}
				for _, id := range e.IDs {
					if id == lib[1] && !tc.wantTopReveal {
						t.Fatalf("matching creature below PeekAmount$ window was revealed: %+v", e)
					}
					if id == lib[0] {
						revealed = true
					}
				}
			}
			if tc.wantTopReveal != revealed {
				t.Fatalf("top card reveal = %v, want %v; log=%+v", revealed, tc.wantTopReveal, h.log)
			}
		})
	}
}

func peekContainsID(ids []state.ObjID, want state.ObjID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestPortentOfCalamityImprintRevealed(t *testing.T) {
	_, sa := peekCorpusSA(t, "Portent of Calamity", "")
	if sa.API != "PeekAndReveal" || sa.Params["PeekAmount"] != "X" || sa.Params["ImprintRevealed"] != "True" || sa.Params["Look"] != "" {
		t.Fatalf("corpus pin moved: Portent main SA = API %q params %+v; want PeekAndReveal, PeekAmount X, ImprintRevealed True, no Look$", sa.API, sa.Params)
	}
	h, src, lib := riderBoard(t, peekBolt, peekIsle, peekBear)
	if !peekContainsID(h.g.Zone(state.ZBattlefield, 0), src) || len(h.g.Zone(state.ZLibrary, 0)) != 3 || lib[0] == lib[1] {
		t.Fatal("precondition: Portent source must be on the battlefield with distinct ordered library cards")
	}
	ctx := &Ctx{Source: src, Controller: 0, X: 2}
	effReveal(h, ctx, sa)
	want := []state.ObjID{lib[0], lib[1]}
	var found *events.Event
	for i := range h.log {
		e := &h.log[i]
		if e.Kind == events.Imprint && e.Obj == src && e.Text == "seek-found" {
			if found != nil {
				t.Fatalf("multiple seek-found imprint events: %+v", h.log)
			}
			found = e
		}
	}
	if found == nil || len(found.IDs) != len(want) || found.IDs[0] != want[0] || found.IDs[1] != want[1] {
		t.Fatalf("seek-found Imprint event = %+v, want IDs %v", found, want)
	}
	got := h.g.Obj(src).SeekFound
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("event-folded source SeekFound = %v, want %v", got, want)
	}
	for _, id := range want {
		if h.g.Obj(id).Zone != state.ZLibrary {
			t.Fatalf("revealed card %d moved from library; test seam unexpectedly resolved Portent continuation", id)
		}
	}

	// The ImprintRevealed rider is public-reveal-only. Exercise the same
	// compiled SA with the private Look$ mode enabled and keep that boundary.
	lookSA := *sa
	lookSA.Params = make(map[string]string, len(sa.Params)+1)
	for k, v := range sa.Params {
		lookSA.Params[k] = v
	}
	lookSA.Params["Look"] = "True"
	lookHost, lookSrc, lookLib := riderBoard(t, peekBolt, peekIsle)
	effReveal(lookHost, &Ctx{Source: lookSrc, Controller: 0, X: 2}, &lookSA)
	privateLook := false
	for _, e := range lookHost.log {
		if e.Kind == events.Imprint && e.Obj == lookSrc && e.Text == "seek-found" {
			t.Fatalf("private Look$ emitted public seek-found imprint: %+v", e)
		}
		if e.Kind == events.Note && len(e.IDs) > 0 {
			if !e.Secret {
				t.Fatalf("private Look$ emitted a public reveal Note: %+v", e)
			}
			privateLook = true
			if len(e.IDs) != 2 || e.IDs[0] != lookLib[0] || e.IDs[1] != lookLib[1] {
				t.Fatalf("private Look$ Note IDs = %v, want the two looked-at cards", e.IDs)
			}
		}
	}
	if !privateLook {
		t.Fatalf("Look$ did not emit its private information record; log=%+v", lookHost.log)
	}
}
