package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestOffStackManaTargetAskDoesNotResolveTheStack(t *testing.T) {
	e, _, bear := newFixtureDeck(t, 9340, "Name:Stack Bear\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	engine := onBoard(t, e, 0, "Name:Engine Test\nTypes:Artifact\nA:AB$ Mana | Cost$ T | Produced$ B | Amount$ 4 | SubAbility$ DBGive | SpellDescription$ Engine fixture.\nSVar:DBGive:DB$ GainControl | Defined$ Self | ValidTgts$ Opponent | TgtPrompt$ Choose target\nOracle:x\n")
	if e.G.Obj(engine).Zone != state.ZBattlefield {
		t.Fatal("mana source is not on the battlefield")
	}
	toMain1(t, e)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "G", Amount: 1})
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bear {
			cast = o.Index
		}
	}
	if cast < 0 {
		t.Fatalf("no cast option for the bear in %#v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{cast}}); err != nil {
		t.Fatal(err)
	}
	if z := e.G.Obj(bear).Zone; z != state.ZStack {
		t.Fatalf("bear zone = %s, want stack after casting", z)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("pending = %#v, want the caster's priority with the bear on the stack", d)
	}
	act := -1
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == engine {
			act = o.Index
		}
	}
	if act < 0 {
		t.Fatalf("no activate option for the engine in %#v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{act}}); err != nil {
		t.Fatal(err)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "tgts" {
		t.Fatalf("mana rider ask = %#v, want the target decision", d)
	}
	if len(d.Options) != 1 || d.Options[0].Player != 1 {
		t.Fatalf("target options = %#v, want the opponent", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
		t.Fatal(err)
	}
	if z := e.G.Obj(bear).Zone; z != state.ZStack {
		t.Errorf("bear zone = %s after the mana ability's target answer, want stack: nobody passed priority", z)
	}
	if got := e.G.Obj(engine).Controller; got != 1 {
		t.Errorf("engine controller = %d, want the targeted opponent 1", got)
	}
	if got := e.G.Players[0].Pool[state.ManaIndex('B')]; got != 4 {
		t.Errorf("black mana in pool = %d, want 4", got)
	}
}

func TestOffStackManaModesAskResumesManaRider(t *testing.T) {
	e, _, bear := newFixtureDeck(t, 9341, "Name:Stack Bear\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	engine := onBoard(t, e, 0, "Name:Engine Charm\nTypes:Artifact\n"+
		"A:AB$ Mana | Cost$ T | Produced$ B | Amount$ 4 | SubAbility$ DBPick | SpellDescription$ Engine fixture.\n"+
		"SVar:DBPick:DB$ Charm | Choices$ DBGain,DBLose | CharmNum$ 1\n"+
		"SVar:DBGain:DB$ GainLife | LifeAmount$ 3 | Defined$ You\n"+
		"SVar:DBLose:DB$ LoseLife | LifeAmount$ 3 | Defined$ You\nOracle:x\n")
	if e.G.Obj(engine).Zone != state.ZBattlefield {
		t.Fatal("mana source is not on the battlefield")
	}
	toMain1(t, e)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "G", Amount: 1})
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bear {
			cast = o.Index
		}
	}
	if cast < 0 {
		t.Fatalf("no cast option for bear in %#v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{cast}}); err != nil {
		t.Fatal(err)
	}
	if z := e.G.Obj(bear).Zone; z != state.ZStack {
		t.Fatalf("bear zone = %s after cast, want stack", z)
	}
	if e.G.Players[0].Life != 20 {
		t.Fatalf("fixture life = %d, want 20 before rider", e.G.Players[0].Life)
	}
	d = e.Pending()
	act := -1
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == engine {
			act = o.Index
		}
	}
	if act < 0 {
		t.Fatalf("no activate option for engine in decision %#v, bear zone %s", d, e.G.Obj(bear).Zone)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{act}}); err != nil {
		t.Fatal(err)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("mana rider ask = %#v, want modes", d)
	}
	gain := -1
	for _, o := range d.Options {
		if o.Kind == "mode" && o.Label == "DBGain" {
			gain = o.Index
		}
	}
	if gain < 0 {
		t.Fatalf("gain-life mode missing from %#v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{gain}}); err != nil {
		t.Fatal(err)
	}
	if z := e.G.Obj(bear).Zone; z != state.ZStack {
		t.Errorf("bear zone = %s after mode answer, want stack", z)
	}
	if got := e.G.Players[0].Life; got != 23 {
		t.Errorf("life = %d, want 23 after selected mode resolves", got)
	}
	if got := e.G.Players[0].Pool[state.ManaIndex('B')]; got != 4 {
		t.Errorf("black mana in pool = %d, want 4", got)
	}
	modeRecorded := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.ModeChosen && ev.Player == 0 {
			modeRecorded = true
		}
	}
	if !modeRecorded {
		t.Error("modal rider answer did not emit ModeChosen")
	}
}
