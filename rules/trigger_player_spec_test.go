package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestMindsparkerSpellCastTriggerMatchesOpponentOnly(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	mindsparker, ok := reg.Lookup("Mindsparker")
	if !ok {
		t.Fatal("corpus fixture Mindsparker is missing")
	}

	for _, tc := range []struct {
		name   string
		caster state.PlayerID
		want   int
	}{
		{name: "own-controller casts", caster: 0, want: 0},
		{name: "opponent casts", caster: 1, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := layerEngine(t)
			o := e.G.AddObject(mindsparker, 0)
			o.Zone = state.ZBattlefield
			e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), o.ID))

			triggers := o.Face().Triggers
			if len(triggers) != 1 || triggers[0].Mode != "SpellCast" || triggers[0].Params["ValidActivatingPlayer"] != "Player.Opponent" {
				t.Fatalf("Mindsparker trigger fixture changed: %+v", triggers)
			}

			// The cast spell the trigger matches: a blue instant, so it clears
			// ValidCard$ Instant.Blue. It is cast by tc.caster and put on the
			// stack, so ValidActivatingPlayer$ is exercised against the caster.
			spell := card(t, "Name:TestBlueInstant\nManaCost:1 U\nTypes:Instant\nOracle:x\n")
			spellID := e.G.AddObject(spell, tc.caster).ID
			e.G.SetZone(state.ZHand, tc.caster, append(e.G.Zone(state.ZHand, tc.caster), spellID))

			e.emit(events.Event{Kind: events.PutOnStack, Obj: spellID, Player: tc.caster, From: state.ZHand, To: state.ZStack})

			got := 0
			for _, pending := range e.pendingTriggers {
				if pending.Source == o.ID {
					got++
				}
			}
			if got != tc.want {
				t.Fatalf("Mindsparker triggers when caster %d casts = %d, want %d", tc.caster, got, tc.want)
			}
		})
	}
}

func TestSatyrFiredancerDamageTriggerMatchesOpponentOnly(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	satyr, ok := reg.Lookup("Satyr Firedancer")
	if !ok {
		t.Fatal("corpus fixture Satyr Firedancer is missing")
	}

	for _, tc := range []struct {
		name   string
		target state.PlayerID
		want   int
	}{
		{name: "own-controller dealt damage", target: 0, want: 0},
		{name: "opponent dealt damage", target: 1, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := layerEngine(t)
			o := e.G.AddObject(satyr, 0)
			o.Zone = state.ZBattlefield
			e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), o.ID))

			triggers := o.Face().Triggers
			if len(triggers) != 1 || triggers[0].Mode != "DamageDone" || triggers[0].Params["ValidTarget"] != "Player.Opponent" {
				t.Fatalf("Satyr Firedancer trigger fixture changed: %+v", triggers)
			}

			// The damage-dealing spell a ValidSource$ Instant.YouCtrl source
			// must be: an instant the Satyr's controller controls, on the stack
			// so trigmatch.damageSource() finds it behind the Damage event.
			src := card(t, "Name:TestRedBolt\nManaCost:1 R\nTypes:Instant\nOracle:x\n")
			srcID := e.G.AddObject(src, 0).ID
			e.G.Stack = append(e.G.Stack, srcID)

			e.emit(events.Event{Kind: events.Damage, Player: tc.target, Amount: 3})

			got := 0
			for _, pending := range e.pendingTriggers {
				if pending.Source == o.ID {
					got++
				}
			}
			if got != tc.want {
				t.Fatalf("Satyr Firedancer triggers when player %d takes damage = %d, want %d", tc.target, got, tc.want)
			}
		})
	}
}

func TestMogisUpkeepTriggerMatchesOpponentOnly(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	mogis, ok := reg.Lookup("Mogis, God of Slaughter")
	if !ok {
		t.Fatal("corpus fixture Mogis, God of Slaughter is missing")
	}

	for _, tc := range []struct {
		name   string
		active state.PlayerID
		want   int
	}{
		{name: "controller", active: 0, want: 0},
		{name: "opponent", active: 1, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := layerEngine(t)
			o := e.G.AddObject(mogis, 0)
			e.G.SetZone(state.ZLibrary, 0, append(e.G.Zone(state.ZLibrary, 0), o.ID))
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})

			triggers := o.Face().Triggers
			if len(triggers) != 1 || triggers[0].Mode != "Phase" || triggers[0].Params["ValidPlayer"] != "Player.Opponent" {
				t.Fatalf("Mogis trigger fixture changed: %+v", triggers)
			}

			e.G.Active = tc.active
			e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})

			got := 0
			for _, pending := range e.pendingTriggers {
				if pending.Source == o.ID {
					got++
				}
			}
			if got != tc.want {
				t.Fatalf("Mogis triggers during active player %d's upkeep = %d, want %d", tc.active, got, tc.want)
			}
		})
	}
}
