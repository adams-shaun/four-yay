package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestETBEvenOddAsEnters(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		choice string
	}{
		{name: "Lavabrink Venturer", choice: "Odd"},
		{name: "Ashling's Prerogative", choice: "Even"},
		{name: "Gollum, Riddle Master", choice: "Even"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := handEngine(t, corpusAlternativeCard(t, tc.name))
			id := e.G.Zone(state.ZHand, 0)[0]
			if got := e.G.Obj(id).Zone; got != state.ZHand {
				t.Fatalf("precondition: %s zone = %s, want hand", tc.name, got)
			}

			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
			d := e.Pending()
			if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" || len(d.Options) != 2 {
				t.Fatalf("entry ask = %+v, want a two-option ETB even/odd choice", d)
			}
			if got := e.G.Obj(id).Zone; got != state.ZHand {
				t.Fatalf("precondition after parked move: %s zone = %s, want hand", tc.name, got)
			}
			odd, even := optionByLabel(d.Options, "Odd"), optionByLabel(d.Options, "Even")
			if odd < 0 || even < 0 || odd == even {
				t.Fatalf("entry options = %+v, want distinct Odd and Even options", d.Options)
			}
			for _, opt := range d.Options {
				if opt.Kind != "evenodd" {
					t.Fatalf("entry option = %+v, want kind evenodd", opt)
				}
			}
			selected := optionByLabel(d.Options, tc.choice)
			if selected < 0 {
				t.Fatalf("no %q option in entry ask %+v", tc.choice, d.Options)
			}
			unchosen := "Odd"
			if tc.choice == "Odd" {
				unchosen = "Even"
			}
			if tc.choice == unchosen {
				t.Fatalf("precondition: selected %q must differ from unchosen %q", tc.choice, unchosen)
			}
			wantType, unchosenType := "odd", "even"
			if tc.choice == "Even" {
				wantType, unchosenType = "even", "odd"
			}
			if wantType == unchosenType {
				t.Fatalf("precondition: expected type %q must differ from unchosen type %q", wantType, unchosenType)
			}

			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{selected}}); err != nil {
				t.Fatalf("submit %s choice: %v", tc.choice, err)
			}
			o := e.G.Obj(id)
			if o.Zone != state.ZBattlefield || o.ChosenType != wantType {
				t.Fatalf("after entry answer: zone=%s ChosenType=%q, want battlefield and %q (not %q)", o.Zone, o.ChosenType, wantType, unchosenType)
			}
			if next := e.Pending(); next != nil && next.ResumeKind == "chooseevenodd" {
				t.Fatalf("entry body posed a second chooseevenodd ask: %+v", next)
			}
		})
	}
}
