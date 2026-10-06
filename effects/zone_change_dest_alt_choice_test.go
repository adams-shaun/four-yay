package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

type destinationTapeHost struct {
	*fakeHost
	answer int
	asked  *decision.Decision
}

func (h *destinationTapeHost) AskCount() uint64 { return 0 }
func (h *destinationTapeHost) TapeAnswer(d *decision.Decision) (decision.Intent, bool) {
	cp := *d
	cp.Options = append([]decision.Option(nil), d.Options...)
	h.asked = &cp
	return decision.Intent{Choices: []int{h.answer}}, true
}

func greenSunChangeZone(t *testing.T) (*cards.SA, map[string]string) {
	t.Helper()
	_, root, svars := corpusRiderSA(t, "Green Sun's Twilight", "")
	var find func(*cards.SA) *cards.SA
	find = func(sa *cards.SA) *cards.SA {
		if sa == nil {
			return nil
		}
		if sa.API == "ChangeZone" && sa.Params["DestAltSVar"] != "" {
			return sa
		}
		if found := find(sa.Sub); found != nil {
			return found
		}
		return nil
	}
	cz := find(root)
	if cz == nil {
		t.Fatal("precondition: Green Sun's Twilight has no conditional ChangeZone")
	}
	if cz.Params["Destination"] != "Hand" || cz.Params["DestinationAlternative"] != "Battlefield" || !strings.Contains(cz.Params["DestAltSVarCompare"], "GE5") {
		t.Fatalf("precondition: unexpected Green Sun destination parameters: %+v", cz.Params)
	}
	return cz, svars
}

func TestChangeZoneConditionalDestinationChoice(t *testing.T) {
	ability, cardSVars := greenSunChangeZone(t)
	for _, tc := range []struct {
		name string
		pick int
		want state.Zone
	}{
		{name: "primary", pick: 0, want: state.ZHand},
		{name: "alternate", pick: 1, want: state.ZBattlefield},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fh, c := fixtureHost(t)
			bearA := fh.g.AddObject(mkCard(t, riderBear), 0).ID
			bearB := fh.g.AddObject(mkCard(t, riderBear), 0).ID
			fh.g.SetZone(state.ZLibrary, 0, []state.ObjID{bearA, bearB})
			fh.g.Obj(bearA).Zone, fh.g.Obj(bearB).Zone = state.ZLibrary, state.ZLibrary
			h := &destinationTapeHost{fakeHost: fh, answer: tc.pick}
			c.Controller = 0
			c.Remembered = []state.Target{{Obj: bearA}, {Obj: bearB}}
			c.SVars = make(map[string]string, len(cardSVars)+1)
			for k, v := range cardSVars {
				c.SVars[k] = v
			}
			c.SVars["X"] = "Number$ 5"
			gotX, xOK := EvalCountOK(h, c, c.SVars["X"])
			if !xOK || gotX != 5 || len(c.Remembered) != 2 {
				t.Fatalf("precondition: X=%d (evaluated=%v) and remembered=%d, want X=5 and two selected cards", gotX, xOK, len(c.Remembered))
			}
			Resolve(h, c, ability)
			if h.asked == nil || h.asked.ResumeKind != "changezone_dest_alt" || len(h.asked.Options) != 2 {
				t.Fatalf("conditional destination decision not posed: %+v", h.asked)
			}
			for _, id := range []state.ObjID{bearA, bearB} {
				if got := h.g.Obj(id).Zone; got != tc.want {
					t.Fatalf("selected card %d zone=%s, want %s", id, got, tc.want)
				}
			}
		})
	}
}

func TestChangeZoneConditionalDestinationNoAskPaths(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params string
		x      string
		want   state.Zone
		ask    bool
	}{
		{name: "condition false", params: "DestAltSVar$ X | DestAltSVarCompare$ GE5", x: "Number$ 4", want: state.ZHand},
		{name: "mandatory", params: "DestAltSVar$ MANDATORY X | DestAltSVarCompare$ GE5 | DestinationAlternative$ Battlefield", x: "Number$ 5", want: state.ZBattlefield},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fh, c := fixtureHost(t)
			id := fh.g.AddObject(mkCard(t, riderBear), 0).ID
			fh.g.SetZone(state.ZLibrary, 0, []state.ObjID{id})
			fh.g.Obj(id).Zone = state.ZLibrary
			h := &destinationTapeHost{fakeHost: fh, answer: 0}
			c.Controller = 0
			c.Remembered = []state.Target{{Obj: id}}
			c.SVars = map[string]string{"X": tc.x}
			Resolve(h, c, sa(t, "DB$ ChangeZone | Defined$ Remembered | Destination$ Hand | "+tc.params))
			if got := h.g.Obj(id).Zone; got != tc.want {
				t.Fatalf("zone=%s, want %s", got, tc.want)
			}
			if (h.asked != nil) != tc.ask {
				t.Fatalf("asked=%v, want %v", h.asked, tc.ask)
			}
		})
	}
}

func TestChangeZoneConditionalDestinationNoHostUsesAlternate(t *testing.T) {
	h, c := fixtureHost(t)
	id := h.g.AddObject(mkCard(t, riderBear), 0).ID
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{id})
	c.Controller = 0
	c.Remembered = []state.Target{{Obj: id}}
	c.SVars = map[string]string{"X": "Number$ 5"}
	Resolve(h, c, sa(t, "DB$ ChangeZone | Defined$ Remembered | Destination$ Hand | DestinationAlternative$ Battlefield | DestAltSVar$ X | DestAltSVarCompare$ GE5"))
	if got := h.g.Obj(id).Zone; got != state.ZBattlefield {
		t.Fatalf("R-9 fallback zone=%s, want battlefield", got)
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "no destination answer") {
			return
		}
	}
	t.Fatalf("missing deterministic fallback Note: %+v", h.log)
}
