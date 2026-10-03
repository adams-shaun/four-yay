package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestDruidOfPurificationStartingWith(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name           string
		definedReorder bool
	}{
		{name: "printed trigger"},
		// Defined$ Player currently happens to enumerate from the controller,
		// so the printed trigger alone cannot distinguish StartingWith$ from
		// its absence. Force a conflicting Defined order on a copy of the REAL
		// corpus ChooseCard SA to exercise the order modifier (and its resume
		// path) without changing the corpus or global Defined$ semantics.
		{name: "conflicting Defined order", definedReorder: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := New(Config{Seed: 812, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{
				mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40),
			}})
			if got := e.G.AliveFrom(0); len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
				t.Fatalf("turn order = %v, want [0 1 2]", got)
			}
			druid := e.G.AddObject(mustCorpusCard(t, reg, "Druid of Purification"), 1)
			sa := cards.ResolveSVar(druid.Face().SVars, "TrigChoice")
			if sa == nil || sa.API != "ChooseCard" || sa.Params["Defined"] != "Player" || sa.Params["StartingWith"] != "You" || sa.Sub == nil || sa.Sub.API != "Destroy" || sa.Sub.Params["Defined"] != "ChosenCard" {
				t.Fatalf("corpus Druid ChooseCard/Destroy chain changed: %+v", sa)
			}
			// One differently owned permanent per seat. "You" in the choice
			// filter is the Druid's CONTROLLER (seat 1) for every chooser --
			// Forge validates Choices$ against the activating player -- so
			// every seat is offered seat 0's and seat 2's permanents (a
			// chooser may pick its own) and seat 1's never. Seat 1 chooses
			// seat 2's, then seat 2 chooses seat 0's, then seat 0 chooses
			// seat 2's again.
			ids := make([]state.ObjID, 3)
			for p, script := range []string{
				"Name:Seat Zero Relic\nTypes:Artifact\nOracle:x\n",
				"Name:Seat One Relic\nTypes:Artifact\nOracle:x\n",
				"Name:Seat Two Ward\nTypes:Enchantment\nOracle:x\n",
			} {
				o := e.G.AddObject(card(t, script), state.PlayerID(p))
				e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
				ids[p] = o.ID
			}
			for p, id := range ids {
				o := e.G.Obj(id)
				if o == nil || o.Zone != state.ZBattlefield || o.Controller != state.PlayerID(p) {
					t.Fatalf("seat %d eligible permanent not on battlefield under its own control: %+v", p, o)
				}
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: druid.ID, From: state.ZLibrary, To: state.ZBattlefield})
			if druid.Zone != state.ZBattlefield || druid.Controller != 1 || len(e.pendingTriggers) != 1 {
				t.Fatalf("Druid ETB precondition: source=%+v pending triggers=%d", druid, len(e.pendingTriggers))
			}
			if tc.definedReorder {
				// Opponent & You produces [2, 0, 1] from controller 1.
				// Only Defined$ is changed; the choices and chained destruction
				// still come from Druid's real, linked corpus SA.
				copySA := *sa
				copySA.Params = make(map[string]string, len(sa.Params))
				for k, v := range sa.Params {
					copySA.Params[k] = v
				}
				copySA.Params["Defined"] = "Opponent & You"
				effects.Resolve(e, &effects.Ctx{Source: druid.ID, Controller: 1, SVars: druid.Face().SVars}, &copySA)
			} else {
				e.putTriggersOnStack()
				e.resolveTop()
			}
			for _, ask := range []struct {
				seat state.PlayerID
				pick state.ObjID
			}{{1, ids[2]}, {2, ids[0]}, {0, ids[2]}} {
				d := e.Pending()
				if d == nil || d.Kind != decision.KChoose || d.Player != ask.seat || d.Source != druid.ID {
					t.Fatalf("wanted Druid's seat %d ChooseCard ask, got %+v", ask.seat, d)
				}
				if len(d.Options) != 2 {
					t.Fatalf("seat %d must have the two permanents the Druid's controller does not control to choose from: %+v", ask.seat, d.Options)
				}
				index := -1
				for _, o := range d.Options {
					if o.Obj == ids[1] {
						t.Fatalf("seat %d offered the Druid controller's own permanent: %+v", ask.seat, d.Options)
					}
					if o.Obj == ask.pick {
						index = o.Index
					}
				}
				if index < 0 {
					t.Fatalf("seat %d not offered chosen permanent %d: %+v", ask.seat, ask.pick, d.Options)
				}
				submitChoices(t, e, index)
			}
			if !tc.definedReorder {
				passUntilStackEmpty(t, e, 30)
			}
			for p, id := range ids {
				want := state.ZGraveyard
				if p == 1 {
					want = state.ZBattlefield // the controller's own: never choosable
				}
				if z := e.G.Obj(id).Zone; z != want {
					t.Fatalf("seat %d's permanent zone = %s, want %s", p, z, want)
				}
			}
		})
	}
}
