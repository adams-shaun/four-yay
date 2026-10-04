package rules

// Kernel-era restorations of the offstack_mana_ask_test.go behaviour tests the W3
// legacy removal deleted: the same scenarios, driven through the resolution
// kernel (rules/resolve) instead of the suspend/resume protocol.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestOffStackManaTargetAskDoesNotResolveTheStack(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// TestOffStackManaNameAskCommitsOnce pins the decision-specific name binding
// on the off-stack mana continuation: a SubAbility$ NameCard answer must be
// interpreted exactly as handleChoose interprets the same KChoose "name"
// answer (option 0's label becomes the chosen name), not re-posed forever.
// main 7fb66ee5f re-asked "name" immediately after a valid answer because
// answerManaColor resumed the rider without that binding, and the resumed
// NameCard saw an empty Ctx.NameChoice and asked again.
func TestOffStackManaNameAskCommitsOnce(t *testing.T) {
	t.Parallel()
	e, _, bear := newFixtureDeck(t, 9343, "Name:Stack Bear\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	engine := onBoard(t, e, 0, "Name:Engine Namer\nTypes:Artifact\n"+
		"A:AB$ Mana | Cost$ T | Produced$ B | Amount$ 4 | SubAbility$ DBName | SpellDescription$ Engine fixture.\n"+
		"SVar:DBName:DB$ NameCard\nOracle:x\n")
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
	d = e.Pending()
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
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "name" {
		t.Fatalf("mana rider ask = %#v, want the NameCard decision", d)
	}
	if len(d.Options) == 0 {
		t.Fatalf("NameCard decision offered no names: %#v", d)
	}
	name := d.Options[0].Label
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
		t.Fatal(err)
	}
	// The precondition of the assertion below: the answer must not re-pose
	// the same NameCard ask. A second name decision here is the livelock.
	if next := e.Pending(); next != nil && next.Kind == decision.KChoose && next.ResumeKind == "name" {
		t.Fatalf("NameCard re-asked after a valid answer: %#v", next)
	}
	namesChosen := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && ev.Counter == "name" {
			namesChosen++
			if ev.Text != name {
				t.Errorf("chosen name = %q, want %q", ev.Text, name)
			}
		}
	}
	if namesChosen != 1 {
		t.Errorf("name Choose events = %d, want exactly 1", namesChosen)
	}
	if z := e.G.Obj(bear).Zone; z != state.ZStack {
		t.Errorf("bear zone = %s after the mana ability's name answer, want stack", z)
	}
	if got := e.G.Players[0].Pool[state.ManaIndex('B')]; got != 4 {
		t.Errorf("black mana in pool = %d, want 4", got)
	}
}

// TestOffStackManaArrangeAskResumesManaRider pins the KArrange half of the
// off-stack mana continuation: a SubAbility$ Scry/RearrangeTopOfLibrary
// rider's ordered answer must resume the mana ability's own chain, not be
// dropped (handleArrange's no-suspended-resolution Note) and not re-enter
// the unrelated spell on top of the stack. main 7fb66ee5f had no off-stack
// KArrange routing at all, so the answer landed on the stack top; the fix
// parks and re-enters the rider under the mana frame (rules/arrange.go).
func TestOffStackManaArrangeAskResumesManaRider(t *testing.T) {
	t.Parallel()
	const bearSrc = "Name:Stack Bear\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, _, bear := newFixtureDeck(t, 9344, bearSrc)
	engine := onBoard(t, e, 0, "Name:Engine Seer\nTypes:Artifact\n"+
		"A:AB$ Mana | Cost$ T | Produced$ B | Amount$ 4 | SubAbility$ DBScry | SpellDescription$ Engine fixture.\n"+
		"SVar:DBScry:DB$ Scry | ScryNum$ 3\nOracle:x\n")
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
	d = e.Pending()
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
	if d == nil || d.Kind != decision.KArrange {
		t.Fatalf("mana rider ask = %#v, want the arrange decision", d)
	}
	if len(d.Options) != 3 {
		t.Fatalf("arrange options = %d, want the top 3", len(d.Options))
	}
	// A non-identity permutation: the reverse of the offered order.
	top := []state.ObjID{d.Options[0].Obj, d.Options[1].Obj, d.Options[2].Obj}
	choices := []int{d.Options[2].Index, d.Options[1].Index, d.Options[0].Index}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatal(err)
	}
	// Precondition: the answer must not re-pose the same arrange ask.
	if next := e.Pending(); next != nil && next.Kind == decision.KArrange {
		t.Fatalf("arrange re-asked after a valid answer: %#v", next)
	}
	if z := e.G.Obj(bear).Zone; z != state.ZStack {
		t.Errorf("bear zone = %s after the mana ability's arrange answer, want stack", z)
	}
	if got := e.G.Players[0].Pool[state.ManaIndex('B')]; got != 4 {
		t.Errorf("black mana in pool = %d, want 4", got)
	}
	libAfter := e.G.Zone(state.ZLibrary, 0)
	want := []state.ObjID{top[2], top[1], top[0]}
	for i := 0; i < 3; i++ {
		if libAfter[i] != want[i] {
			t.Fatalf("arrange answer not applied: library top[%d] = %v, want %v", i, libAfter[i], want[i])
		}
	}
}

// TestOffStackManaTargetThenNameAskCommitsOnce pins the COMBINED targeted
// rider: a SubAbility$ that asks a target and THEN asks another question
// (DB$ NameCard | ValidTgts$ Opponent) must keep the answered target while
// the second question is pending. The prior round bound the NameCard name
// answer but dropped the recorded target when the name ask parked: the
// per-resolution cleanup at the end of resumeResolution tested only
// e.resume, while an off-stack mana ask parks on the mana activation
// (e.choosing == chooseManaColor), so the target record was forgotten and
// the re-entry re-posed the target -- the two asks alternated
// tgts/name/tgts/... forever with no completion.
//
// Each assertion's precondition is asserted first: the mana source is on the
// battlefield, the bear reaches the stack, the target ask offers exactly the
// one opponent, and the name ask offers real names. Without those a vacuous
// setup would pass.
func TestOffStackManaTargetThenNameAskCommitsOnce(t *testing.T) {
	t.Parallel()
	e, _, bear := newFixtureDeck(t, 9345, "Name:Stack Bear\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	engine := onBoard(t, e, 0, "Name:Engine Namer\nTypes:Artifact\n"+
		"A:AB$ Mana | Cost$ T | Produced$ B | Amount$ 4 | SubAbility$ DBName | SpellDescription$ Engine fixture.\n"+
		"SVar:DBName:DB$ NameCard | ValidTgts$ Opponent\nOracle:x\n")
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
	// Ask 1: the rider's target; it must offer the single opponent.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "tgts" {
		t.Fatalf("first rider ask = %#v, want the target decision", d)
	}
	if len(d.Options) != 1 || d.Options[0].Player != 1 {
		t.Fatalf("target options = %#v, want exactly the opponent", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
		t.Fatal(err)
	}
	// Ask 2: the name. Answering it must not re-pose the target.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "name" {
		t.Fatalf("second rider ask = %#v, want the NameCard decision after the target", d)
	}
	if len(d.Options) == 0 {
		t.Fatal("NameCard decision offered no names")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
		t.Fatal(err)
	}
	if next := e.Pending(); next != nil && next.Kind == decision.KChoose &&
		(next.ResumeKind == "tgts" || next.ResumeKind == "name") {
		t.Fatalf("rider re-asked after a valid answer: %#v", next)
	}
	namesChosen := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && ev.Counter == "name" {
			namesChosen++
		}
	}
	if namesChosen != 1 {
		t.Errorf("name Choose events = %d, want exactly 1", namesChosen)
	}
	if z := e.G.Obj(bear).Zone; z != state.ZStack {
		t.Errorf("bear zone = %s after the rider answered, want stack", z)
	}
	if got := e.G.Players[0].Pool[state.ManaIndex('B')]; got != 4 {
		t.Errorf("black mana in pool = %d, want 4", got)
	}
}
