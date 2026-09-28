package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const oftNabbedGoatOfferFixture = `Name:Oft-Nabbed Goat
ManaCost:1 B
Types:Creature Goat
PT:0/5
A:AB$ Draw | Cost$ 1 | Activator$ Player.Opponent | NumCards$ 1 | SorcerySpeed$ True | SpellDescription$ Draw a card.
Oracle:Only your opponents may activate this ability and only as a sorcery.
`

func hasAbilityOptionFor(options []decision.Option, id state.ObjID) bool {
	for _, o := range options {
		if o.Kind == "ability" && o.Obj == id && o.Ability == 0 {
			return true
		}
	}
	return false
}

func TestOftNabbedGoatActivatorOffer(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 8601, oftNabbedGoatOfferFixture)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	driveToStep(t, e, 1, 0, state.StepMain1)
	if got := e.controllerOf(id); got != 0 || e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("fixture precondition: controller=%d zone=%s", got, e.G.Obj(id).Zone)
	}
	if !e.sorcerySpeed(0) {
		t.Fatal("fixture precondition: controller check is not in a sorcery window")
	}
	if hasAbilityOptionFor(e.legalActions(0), id) {
		t.Fatal("Oft-Nabbed Goat offered its opponent-only ability to its controller")
	}

	// On the opponent's own turn they have a sorcery window and the same
	// battlefield ability must be offered to them, not merely withheld.
	driveToStep(t, e, 2, 1, state.StepMain1)
	if e.G.Active != 1 || !e.sorcerySpeed(1) {
		t.Fatalf("fixture precondition: active=%d sorcery=%v", e.G.Active, e.sorcerySpeed(1))
	}
	if got := e.controllerOf(id); got != 0 || e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("fixture changed before opponent offer: controller=%d zone=%s", got, e.G.Obj(id).Zone)
	}
	addMana(t, e, 1, "B")
	if !hasAbilityOptionFor(e.legalActions(1), id) {
		t.Fatalf("opponent was not offered the Goat ability: %+v", e.legalActions(1))
	}
}

func TestActivatorOfferDefaultsAndSelectors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		activator string
		player    state.PlayerID
		want      bool
	}{
		{name: "absent_preserves_controller", player: 0, want: true},
		{name: "player_you_means_controller", activator: "Player.You", player: 0, want: true},
		{name: "player_opponent_means_opponent", activator: "Player.Opponent", player: 1, want: true},
		{name: "unknown_fails_closed", activator: "Player.UnhandledActivator", player: 1, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "Name:Activator Probe\nManaCost:0\nTypes:Artifact\n" +
				"A:AB$ Draw | Cost$ 0 | NumCards$ 1 | SpellDescription$ Draw a card.\n"
			if tc.activator != "" {
				src = "Name:Activator Probe\nManaCost:0\nTypes:Artifact\n" +
					"A:AB$ Draw | Cost$ 0 | Activator$ " + tc.activator + " | NumCards$ 1 | SpellDescription$ Draw a card.\n"
			}
			e, _, id := newFixtureDeck(t, 8602, src)
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
			if tc.player == 0 {
				driveToStep(t, e, 1, 0, state.StepMain1)
			} else {
				driveToStep(t, e, 2, 1, state.StepMain1)
			}
			if e.G.Obj(id).Zone != state.ZBattlefield || e.controllerOf(id) != 0 {
				t.Fatalf("fixture precondition: object=%+v controller=%d", e.G.Obj(id), e.controllerOf(id))
			}
			if !e.sorcerySpeed(tc.player) {
				t.Fatalf("fixture precondition: player %d is not in a sorcery window", tc.player)
			}
			got := hasAbilityOptionFor(e.legalActions(tc.player), id)
			if got != tc.want {
				t.Fatalf("ability offered to player %d = %v, want %v", tc.player, got, tc.want)
			}
		})
	}
}
