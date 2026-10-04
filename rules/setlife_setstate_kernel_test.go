package rules

// Kernel-era restorations of effects/setlife_redistribute_test.go,
// effects/setstate_optional_test.go and effects/setstate_rememberchanged_test.go
// behaviour pins (deleted with the W3 legacy removal).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const kr3RedistributeSrc = "Name:Redistribute\nManaCost:W\nTypes:Sorcery\n" +
	"A:SP$ SetLife | PlayerChoices$ Player | ChoiceAmount$ Any | ChoicePrompt$ Choose any number of players | Redistribute$ True\nOracle:x\n"

// kr3LifeBoard is a 3-seat game at life totals 20/5/2 (logged LifeChange
// events) with the redistribute sorcery in seat 0's hand.
func kr3LifeBoard(t *testing.T, seed uint64) (*Engine, Config) {
	t.Helper()
	e, cfg := kr3Game(t, seed, kr3Cards(t, kr3RedistributeSrc), nil, nil)
	kr3Move(t, e, 0, "Redistribute", state.ZHand)
	for p, life := range []int32{20, 5, 2} {
		if d := life - e.G.Players[p].Life; d != 0 {
			e.emit(events.Event{Kind: events.LifeChange, Player: state.PlayerID(p), Amount: d})
		}
	}
	e.pending = nil
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 5 || e.G.Players[2].Life != 2 {
		t.Fatalf("precondition: lives = %d/%d/%d, want 20/5/2", e.G.Players[0].Life, e.G.Players[1].Life, e.G.Players[2].Life)
	}
	return e, cfg
}

func kr3OptionPlayer(t *testing.T, d *decision.Decision, p state.PlayerID) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == p {
			return o.Index
		}
	}
	t.Fatalf("seat %d not offered: %+v", p, d.Options)
	return -1
}

// TestSetLifeRedistributePermutation: the subset ask offers every player
// (Min 0, Max 3); choosing seats 0 and 2 then assigns totals one at a time
// over exactly the chosen seats. Seat 0 takes seat 2's 2 and seat 2 takes
// seat 0's 20; seat 1 is untouched, and nothing changes before the last
// answer. The deltas are -18 then +18.
func TestSetLifeRedistributePermutation(t *testing.T) {
	t.Parallel()
	e, cfg := kr3LifeBoard(t, 141)
	mark := len(e.L.Events)
	d := kr3Cast(t, e, "Redistribute", "W", -1)
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 || d.Min != 0 || d.Max != 3 ||
		d.Prompt != "Choose any number of players" || len(d.Options) != 3 {
		t.Fatalf("subset ask = %+v", d)
	}
	for i, o := range d.Options {
		if o.Kind != "player" || o.Player != state.PlayerID(i) {
			t.Fatalf("subset option %d = %+v", i, o)
		}
	}
	d = kr3Answer(t, e, kr3OptionPlayer(t, d, 0), kr3OptionPlayer(t, d, 2))
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 || len(d.Options) != 2 ||
		d.Options[0].Player != 0 || d.Options[1].Player != 2 {
		t.Fatalf("first assignment = %+v, want a pick over seats 0 and 2", d)
	}
	d = kr3Answer(t, e, kr3OptionPlayer(t, d, 2))
	if d == nil || len(d.Options) != 1 || d.Options[0].Player != 0 {
		t.Fatalf("remaining assignment = %+v, want seat 0 alone", d)
	}
	if e.G.Players[0].Life != 20 || e.G.Players[2].Life != 2 {
		t.Fatal("life changed before all answers")
	}
	if d := kr3Answer(t, e, 0); d != nil {
		t.Fatalf("unexpected follow-up %+v", d)
	}
	if e.G.Players[0].Life != 2 || e.G.Players[1].Life != 5 || e.G.Players[2].Life != 20 {
		t.Fatalf("lives = %d/%d/%d, want 2/5/20", e.G.Players[0].Life, e.G.Players[1].Life, e.G.Players[2].Life)
	}
	var changes []events.Event
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.LifeChange {
			changes = append(changes, ev)
		}
	}
	if len(changes) != 2 || changes[0].Player != 0 || changes[0].Amount != -18 || changes[1].Player != 2 || changes[1].Amount != 18 {
		t.Fatalf("life deltas = %+v, want seat 0 -18 then seat 2 +18", changes)
	}
	replayCheck(t, e, cfg)
}

// TestSetLifeRedistributeZeroAndOne: choosing no players, or one player
// (whose only assignment is its own total), changes nothing and emits no
// LifeChange or Note.
func TestSetLifeRedistributeZeroAndOne(t *testing.T) {
	t.Parallel()
	for i, pick := range [][]state.PlayerID{nil, {1}} {
		e, _ := kr3LifeBoard(t, uint64(142+i))
		mark := len(e.L.Events)
		d := kr3Cast(t, e, "Redistribute", "W", -1)
		if d == nil || d.Max != 3 {
			t.Fatalf("subset ask = %+v", d)
		}
		var choices []int
		for _, p := range pick {
			choices = append(choices, kr3OptionPlayer(t, d, p))
		}
		d = kr3Answer(t, e, choices...)
		asks := 1
		if len(pick) == 1 {
			if d == nil || len(d.Options) != 1 || d.Options[0].Player != 1 {
				t.Fatalf("one-player assignment = %+v", d)
			}
			asks++
			d = kr3Answer(t, e, 0)
		}
		if d != nil {
			t.Fatalf("pick %v: unexpected follow-up %+v (after %d asks)", pick, d, asks)
		}
		if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 5 || e.G.Players[2].Life != 2 {
			t.Fatalf("pick %v changed life: %d/%d/%d", pick, e.G.Players[0].Life, e.G.Players[1].Life, e.G.Players[2].Life)
		}
		for _, ev := range e.L.Events[mark:] {
			if ev.Kind == events.LifeChange || (ev.Kind == events.Note && ev.Obj != 0 && ev.Text != "") {
				if ev.Kind == events.LifeChange {
					t.Fatalf("pick %v: unexpected %+v", pick, ev)
				}
			}
		}
	}
}

// kr3TransformSrc is a two-faced permanent with a free activated Optional$
// transform (Dowsing Dagger's DBTransform shape) chained to a gain-1 rider,
// plus a RememberChanged$ variant gated on Remembered.
func kr3TransformSrc(name, extra, sub string) string {
	return "Name:" + name + "\nManaCost:1\nTypes:Artifact\n" +
		"A:AB$ SetState | Cost$ 0 | Defined$ Self | Mode$ Transform | Optional$ True" + extra + " | SubAbility$ " + sub + "\n" +
		"SVar:DBGain:DB$ GainLife | Defined$ You | LifeAmount$ 1\n" +
		"SVar:DBRem:DB$ GainLife | Defined$ You | LifeAmount$ 5 | ConditionDefined$ Remembered | ConditionPresent$ Card | ConditionCompare$ GE1\n" +
		"AlternateMode:DoubleFaced\nOracle:x\n\nALTERNATE\n\nName:" + name + " Back\nTypes:Artifact\nOracle:x\n"
}

// kr3Activate activates obj's first ability from the pending priority
// decision and returns the next non-priority decision.
func kr3Activate(t *testing.T, e *Engine, obj state.ObjID) *decision.Decision {
	t.Helper()
	e.pending = nil
	e.priorityRound()
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == obj {
			submitChoices(t, e, o.Index)
			return kr3Next(t, e)
		}
	}
	t.Fatalf("no ability option for %d: %+v", obj, d.Options)
	return nil
}

// TestSetStateOptionalAskShapeAndAnsweredReEntries: an Optional$ True
// SetState poses the controller a Min == Max == 1 setstate_optional KChoose
// with yes first and flips nothing while posed; "no" leaves the front face,
// "yes" flips exactly once.
func TestSetStateOptionalAskShapeAndAnsweredReEntries(t *testing.T) {
	t.Parallel()
	for i, yes := range []bool{false, true} {
		name := "no"
		if yes {
			name = "yes"
		}
		t.Run(name, func(t *testing.T) {
			src := kr3TransformSrc("Dagger Shape", "", "DBGain")
			e, cfg := kr3Game(t, uint64(151+i), kr3Cards(t, src), nil)
			obj := kr3Move(t, e, 0, "Dagger Shape", state.ZBattlefield)
			if o := e.G.Obj(obj); o.FaceIdx != 0 || len(o.Card.Faces) != 2 {
				t.Fatalf("precondition: want the front face of a two-faced permanent, got %+v", o)
			}
			life := e.G.Players[0].Life
			mark := len(e.L.Events)
			d := kr3Activate(t, e, obj)
			if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 || d.Player != 0 || d.ResumeKind != "setstate_optional" {
				t.Fatalf("election = %+v, want seat 0's Min==Max==1 setstate_optional", d)
			}
			if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
				t.Fatalf("options = %+v, want yes then no", d.Options)
			}
			if n := kr3Count(e, mark, events.FlipFace); n != 0 {
				t.Fatalf("flipped %d time(s) before the answer", n)
			}
			if d := kr3Answer(t, e, kr3OptionKind(t, d, name)); d != nil {
				t.Fatalf("unexpected follow-up %+v", d)
			}
			want, flips := uint8(0), 0
			if yes {
				want, flips = 1, 1
			}
			if n := kr3Count(e, mark, events.FlipFace); n != flips {
				t.Fatalf("%s flipped %d time(s), want %d", name, n, flips)
			}
			if got := e.G.Obj(obj).FaceIdx; got != want {
				t.Fatalf("%s left FaceIdx %d, want %d", name, got, want)
			}
			// TestSetStateOptionalChainedSubAbilityRunsOnBothAnswers: the
			// chained SubAbility$ runs on either answer.
			if got := e.G.Players[0].Life; got != life+1 {
				t.Fatalf("%s: life = %d, want %d (the chained sub-ability must run on both answers)", name, got, life+1)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestSetStateRememberChangedOptionalDeclineRemembersNothing: an Optional$
// RememberChanged$ SetState answered "no" neither flips nor remembers (Forge
// remembers the objects whose state CHANGED), so the Remembered-gated rider
// does not fire; answered "yes" it remembers the flipped permanent.
func TestSetStateRememberChangedOptionalDeclineRemembersNothing(t *testing.T) {
	t.Parallel()
	for i, yes := range []bool{false, true} {
		name := "no"
		if yes {
			name = "yes"
		}
		t.Run(name, func(t *testing.T) {
			src := kr3TransformSrc("Megatron Shape", " | RememberChanged$ True", "DBRem")
			e, cfg := kr3Game(t, uint64(161+i), kr3Cards(t, src), nil)
			obj := kr3Move(t, e, 0, "Megatron Shape", state.ZBattlefield)
			life := e.G.Players[0].Life
			d := kr3Activate(t, e, obj)
			if d == nil || d.ResumeKind != "setstate_optional" {
				t.Fatalf("election = %+v, want setstate_optional", d)
			}
			if d := kr3Answer(t, e, kr3OptionKind(t, d, name)); d != nil {
				t.Fatalf("unexpected follow-up %+v", d)
			}
			wantFace, wantLife := uint8(0), life
			if yes {
				wantFace, wantLife = 1, life+5
			}
			if got := e.G.Obj(obj).FaceIdx; got != wantFace {
				t.Fatalf("FaceIdx = %d, want %d", got, wantFace)
			}
			if got := e.G.Players[0].Life; got != wantLife {
				t.Fatalf("life = %d, want %d (Remembered must hold only a CHANGED object)", got, wantLife)
			}
			replayCheck(t, e, cfg)
		})
	}
}
