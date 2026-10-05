package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestValgavothsOnslaughtManifestDread exercises the compiled X, repeated
// ManifestDread, remembered objects, and counter rider from the real card.
func TestValgavothsOnslaughtManifestDread(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Valgavoth's Onslaught")
	spell := searchMoveByName(t, e, "Valgavoth's Onslaught", state.ZHand)
	lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	const x = int32(2)
	if len(lib) < int(2*x) {
		t.Fatalf("test precondition: library has %d cards, need %d for X=%d", len(lib), 2*x, x)
	}
	for i := 0; i < int(2*x); i++ {
		if lib[i] == 0 || e.G.Obj(lib[i]) == nil || e.G.Obj(lib[i]).Zone != state.ZLibrary {
			t.Fatalf("test precondition: library object %d (%d) is not in the library", i, lib[i])
		}
		for j := 0; j < i; j++ {
			if lib[i] == lib[j] {
				t.Fatalf("test precondition: library positions %d and %d are not distinct objects", j, i)
			}
		}
	}

	addMana(t, e, 0, "GGGGG") // X X G at X=2
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected priority to cast the sorcery, got %+v", d)
	}
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spell {
			cast = o.Index
			break
		}
	}
	if cast < 0 {
		t.Fatalf("Valgavoth's Onslaught %d is not offered to cast", spell)
	}
	submitChoices(t, e, cast)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("expected cast-X decision, got %+v", d)
	}
	xChoice := -1
	for _, o := range d.Options {
		if o.Label == "X = 2" {
			xChoice = o.Index
			break
		}
	}
	if xChoice < 0 {
		t.Fatalf("X=2 is not an offered value: %+v", d.Options)
	}
	submitChoices(t, e, xChoice)
	if o := e.G.Obj(spell); o.Zone != state.ZStack || o.X != x {
		t.Fatalf("cast spell zone=%s X=%d, want stack/X=%d", o.Zone, o.X, x)
	}

	manifested := make([]state.ObjID, 0, x)
	graveyard := make([]state.ObjID, 0, x)
	for steps := 0; steps < 80 && e.G.Obj(spell).Zone == state.ZStack; steps++ {
		d = e.Pending()
		if d == nil {
			t.Fatal("no decision while resolving Valgavoth's Onslaught")
		}
		switch {
		case d.Kind == decision.KChoose && d.ResumeKind == "manifest_dread":
			if len(d.Options) != 2 {
				t.Fatalf("ManifestDread offered %d cards, want a pair: %+v", len(d.Options), d.Options)
			}
			chosen, other := d.Options[0].Obj, d.Options[1].Obj
			if chosen == other || e.G.Obj(chosen) == nil || e.G.Obj(other) == nil || e.G.Obj(chosen).Zone != state.ZLibrary || e.G.Obj(other).Zone != state.ZLibrary {
				t.Fatalf("test precondition: offered objects %d/%d are not distinct library cards", chosen, other)
			}
			manifested = append(manifested, chosen)
			graveyard = append(graveyard, other)
			submitChoices(t, e, d.Options[0].Index)
		case d.Kind == decision.KPriority:
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
					break
				}
			}
			if pass < 0 {
				t.Fatalf("no pass option while resolving spell: %+v", d.Options)
			}
			submitChoices(t, e, pass)
		default:
			t.Fatalf("unexpected decision while resolving spell: %+v", d)
		}
	}
	if got := len(manifested); got != int(x) {
		t.Fatalf("ManifestDread repetitions = %d, want X=%d", got, x)
	}
	if len(graveyard) != int(x) {
		t.Fatalf("paired graveyard cards = %d, want X=%d", len(graveyard), x)
	}
	if e.G.Obj(spell).Zone == state.ZStack {
		t.Fatal("spell did not finish resolving within 80 decisions")
	}
	for i, id := range manifested {
		o := e.G.Obj(id)
		if o.Zone != state.ZBattlefield || !o.FaceDown {
			t.Fatalf("manifested object %d zone=%s faceDown=%v, want face-down battlefield", id, o.Zone, o.FaceDown)
		}
		if got := o.Counter("P1P1"); got != x {
			t.Fatalf("manifested object %d (%d) has %d +1/+1 counters, want X=%d", i, id, got, x)
		}
	}
	for _, id := range graveyard {
		if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
			t.Fatalf("paired object %d is in %s, want graveyard", id, got)
		}
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(strings.ToLower(ev.Text), "unimplemented") {
			t.Fatalf("fallback/unimplemented note recorded: %q", ev.Text)
		}
	}
	replayCheck(t, e, cfg)
}
