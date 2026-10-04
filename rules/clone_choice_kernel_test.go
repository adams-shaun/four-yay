package rules

// Restored from effects/clone_choicezone_test.go and clone_optional_test.go
// (W3 legacy removal): Clone's Choices$ pick, ChoiceOptional$ decline,
// ExcludeChosen$ and Optional$ "you may" election, answered through the
// resolution kernel on a real engine.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func kr0ClonePermanents(evs []events.Event, id state.ObjID) int {
	n := 0
	for _, ev := range evs {
		if ev.Kind == events.ClonePermanent && ev.Obj == id {
			n++
		}
	}
	return n
}

// kr0Token puts a token creature named name on p's battlefield.
func kr0Token(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	id := kr0Src(t, e, p, "Name:"+name+"\nManaCost:0\nTypes:Creature Myr\nPT:1/1\nOracle:x\n", state.ZBattlefield)
	e.G.Obj(id).IsToken = true
	return id
}

// TestCloneExcludeChosenSkipsThePickedSourceKernel: Sakashima's Will --
// choose a creature you control; each OTHER creature you control becomes a
// copy of it. The chosen creature takes no self-copy.
func TestCloneExcludeChosenSkipsThePickedSourceKernel(t *testing.T) {
	t.Parallel()
	will := kr0Corpus(t, "Sakashima's Will")
	body := kr0SVar(t, will, "DBClone")
	if body.Params["ExcludeChosen"] != "True" || body.Params["Choices"] != "Creature.YouCtrl" || body.Params["CloneTarget"] != "Valid Creature.YouCtrl" {
		t.Fatalf("precondition: unexpected Sakashima's Will DBClone %+v", body.Params)
	}
	e := kr0Engine(t, 2)
	chosen := kr0Src(t, e, 0, "Name:Fixture Exclusion Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n", state.ZBattlefield)
	other := kr0Src(t, e, 0, "Name:Fixture Exclusion Elk\nManaCost:1 G\nTypes:Creature Elk\nPT:3/3\nOracle:x\n", state.ZBattlefield)
	start := len(e.L.Events)
	d := kr0Run(t, e, body, func() *effects.Ctx {
		return &effects.Ctx{Source: chosen, Controller: 0, SVars: will.Faces[0].SVars}
	}, nil)
	if d == nil || d.ResumeKind != "clone_choice" {
		t.Fatalf("Sakashima's Will posed %+v, want the clone_choice pick", d)
	}
	kr0Answer(t, e, kr0Opt(t, d, chosen))
	evs := kr0Since(e, start)
	if n := kr0ClonePermanents(evs, chosen); n != 0 {
		t.Fatalf("the CHOSEN creature received %d self-copy ClonePermanent events", n)
	}
	if n := kr0ClonePermanents(evs, other); n != 1 {
		t.Fatalf("the OTHER creature received %d ClonePermanent events, want 1", n)
	}
	if e.G.Obj(chosen).CopyFace != nil {
		t.Fatal("the chosen creature copied itself")
	}
	if f := e.G.Obj(other).Face(); f == nil || f.Name != "Fixture Exclusion Bear" {
		t.Fatalf("the other creature's face = %v, want a copy of the bear", f)
	}
}

// TestCloneChoiceOptionalDeclineMakesNoCopyKernel: Brudiclad's
// ChoiceOptional$ pick is Min 0 with the token offered and an explicit
// decline; the decline makes no copy and no malformed-answer Note.
func TestCloneChoiceOptionalDeclineMakesNoCopyKernel(t *testing.T) {
	t.Parallel()
	brud := kr0Corpus(t, "Brudiclad, Telchor Engineer")
	body := kr0SVar(t, brud, "DBClone")
	if body.Params["ChoiceOptional"] != "True" || body.Params["ExcludeChosen"] != "True" {
		t.Fatalf("precondition: Brudiclad DBClone must carry ChoiceOptional$/ExcludeChosen$ True: %+v", body.Params)
	}
	e := kr0Engine(t, 2)
	brudID := kr0Place(t, e, 0, brud, state.ZBattlefield)
	tok := kr0Token(t, e, 0, "Fixture Myr Token")
	start := len(e.L.Events)
	d := kr0Run(t, e, body, func() *effects.Ctx {
		return &effects.Ctx{Source: brudID, Controller: 0, SVars: brud.Faces[0].SVars}
	}, nil)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "clone_choice" || d.Min != 0 {
		t.Fatalf("Brudiclad posed %+v, want a Min-0 clone_choice", d)
	}
	sawToken := false
	for _, o := range d.Options {
		sawToken = sawToken || o.Obj == tok
	}
	if !sawToken {
		t.Fatalf("the token is not offered in %+v", d.Options)
	}
	kr0Answer(t, e, kr0Kind(t, d, "decline"))
	evs := kr0Since(e, start)
	if n := kr0Count(evs, events.ClonePermanent); n != 0 {
		t.Fatalf("an answered decline still copied (%d ClonePermanent)", n)
	}
	for _, ev := range evs {
		if ev.Kind == events.Note && ev.Text == "Clone Choices$ answer named no object; no copy" {
			t.Fatal("a choice-optional DECLINE emitted the malformed-answer Note")
		}
	}
}

// TestCloneChoiceOptionalExcludesChosenFromBecomeKernel: when Brudiclad's
// pick IS made, the chosen token does not copy itself while the other token
// copies it.
func TestCloneChoiceOptionalExcludesChosenFromBecomeKernel(t *testing.T) {
	t.Parallel()
	brud := kr0Corpus(t, "Brudiclad, Telchor Engineer")
	body := kr0SVar(t, brud, "DBClone")
	e := kr0Engine(t, 2)
	brudID := kr0Place(t, e, 0, brud, state.ZBattlefield)
	a := kr0Token(t, e, 0, "Fixture Token Alpha")
	b := kr0Token(t, e, 0, "Fixture Token Beta")
	start := len(e.L.Events)
	d := kr0Run(t, e, body, func() *effects.Ctx {
		return &effects.Ctx{Source: brudID, Controller: 0, SVars: brud.Faces[0].SVars}
	}, nil)
	if d == nil || d.ResumeKind != "clone_choice" {
		t.Fatalf("Brudiclad posed %+v, want the clone_choice pick", d)
	}
	kr0Answer(t, e, kr0Opt(t, d, a))
	evs := kr0Since(e, start)
	if n := kr0ClonePermanents(evs, a); n != 0 {
		t.Fatalf("the chosen token received %d self-copy events", n)
	}
	if n := kr0ClonePermanents(evs, b); n != 1 {
		t.Fatalf("the other token received %d ClonePermanent events, want 1", n)
	}
	if e.G.Obj(a).CopyFace != nil {
		t.Fatal("the chosen token copied itself")
	}
}

// kr0CloneOptionalBoard: a Sarkhan-like permanent (the become operand) and a
// Dragon (the copy source, bound as the remembered triggered card).
func kr0CloneOptionalBoard(t *testing.T) (*Engine, func() *effects.Ctx, state.ObjID) {
	t.Helper()
	e := kr0Engine(t, 2)
	sark := kr0Src(t, e, 0, "Name:Sarkhan, Soul Aflame\nManaCost:1 U R\nTypes:Legendary Creature Human Shaman\nPT:2/4\nOracle:x\n", state.ZBattlefield)
	dragon := kr0Src(t, e, 0, "Name:Dragon Hatchling\nManaCost:1 R\nTypes:Creature Dragon\nPT:0/1\nK:Flying\nOracle:x\n", state.ZBattlefield)
	return e, func() *effects.Ctx {
		return &effects.Ctx{Controller: 0, Source: sark, Remembered: []state.Target{{Obj: dragon}}}
	}, sark
}

// kr0CloneElect resolves an Optional$ Clone body, answers its yes/no with
// kind, and returns the events the resolution logged.
func kr0CloneElect(t *testing.T, line, kind string) (*Engine, state.ObjID, []events.Event) {
	t.Helper()
	e, mk, sark := kr0CloneOptionalBoard(t)
	start := len(e.L.Events)
	d := kr0Run(t, e, kr0SA(t, line), mk, nil)
	if d == nil || d.ResumeKind != "clone" || len(d.Options) != 2 {
		t.Fatalf("Optional$ Clone posed %+v, want the clone yes/no", d)
	}
	kr0Answer(t, e, kr0Kind(t, d, kind))
	return e, sark, kr0Since(e, start)
}

// TestCloneOptionalElectionKernel: Sarkhan Soul Aflame's "you may" copy --
// a decline copies nothing; an acceptance copies with the overridden name
// (the old answer-field consume/clear test's behaviour half).
func TestCloneOptionalElectionKernel(t *testing.T) {
	t.Parallel()
	const line = "DB$ Clone | Defined$ TriggeredCardLKICopy | NewName$ Sarkhan, Soul Aflame | AddTypes$ Legendary | Duration$ UntilEndOfTurn | Optional$ True"
	e, sark, _ := kr0CloneElect(t, line, "no")
	if e.G.Obj(sark).CopyFace != nil {
		t.Fatal("the answered decline still cloned")
	}
	e2, sark2, _ := kr0CloneElect(t, line, "yes")
	if e2.G.Obj(sark2).CopyFace == nil {
		t.Fatal("the accepted copy recorded no copy basis")
	}
	if f := e2.G.Obj(sark2).Face(); f == nil || f.Name != "Sarkhan, Soul Aflame" {
		t.Fatalf("accepted copy name %v, want the overridden name", f)
	}
}

// TestCloneOptionalAcceptedAnswerEmitsTheUnreadRiderNoteKernel: the accepted
// may-copy still emits the combined unread-rider Note (AddSVars$); the
// decline copies nothing and emits no such Note.
func TestCloneOptionalAcceptedAnswerEmitsTheUnreadRiderNoteKernel(t *testing.T) {
	t.Parallel()
	const line = "DB$ Clone | Defined$ TriggeredCardLKICopy | NewName$ Kimahri, Valiant Guardian | GainThisAbility$ True | Optional$ True | AddSVars$ RonsoCounter,RonsoTap"
	const want = "Clone does not read: AddSVars$ RonsoCounter,RonsoTap"
	has := func(evs []events.Event) bool {
		for _, ev := range evs {
			if ev.Kind == events.Note && ev.Text == want {
				return true
			}
		}
		return false
	}
	e, sark, evs := kr0CloneElect(t, line, "yes")
	if f := e.G.Obj(sark).Face(); f == nil || f.Name != "Kimahri, Valiant Guardian" {
		t.Fatalf("accepted copy name %v, want Kimahri", f)
	}
	if !has(evs) {
		t.Fatalf("accepted clone dropped the unread-rider diagnostic %q: %+v", want, evs)
	}
	e2, sark2, evs2 := kr0CloneElect(t, line, "no")
	if e2.G.Obj(sark2).CopyFace != nil {
		t.Fatal("the decline copied")
	}
	if has(evs2) {
		t.Fatal("the declined clone emitted the unread-rider diagnostic")
	}
}

// TestCloneOptionalAcceptedAnswerRegistersDurationKernel: an accepted
// election with Duration$ UntilFacedown copies without a duration warning.
func TestCloneOptionalAcceptedAnswerRegistersDurationKernel(t *testing.T) {
	t.Parallel()
	e, sark, evs := kr0CloneElect(t, "DB$ Clone | Defined$ TriggeredCardLKICopy | Optional$ True | Duration$ UntilFacedown", "yes")
	if e.G.Obj(sark).CopyFace == nil {
		t.Fatal("accepted copy recorded no copy basis")
	}
	for _, ev := range evs {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "UntilFacedown") {
			t.Fatalf("supported duration emitted a warning: %s", ev.Text)
		}
	}
}
