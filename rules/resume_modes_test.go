package rules

// Ctx.Modes is a ONE-SHOT answer: the first mode reader a resolution walk
// reaches (effCharm, the per-player GenericChoice loop, effVillainousChoice,
// a KWChoice$ Pump) consumes it and clears it. A resumed resolution builds a
// fresh Ctx, so the resume must leave Modes non-nil ONLY when the answer it
// is binding IS a mode answer for the SA it re-enters (or the stack object's
// announced ChosenModes for the ability's own root). Anything else is stale:
// it is consumed by the first reader the walk reaches, which runs names that
// were never chosen for it, silently skips its own ask, or -- for the
// per-player GenericChoice, which reads the chooser at index-1 of a cursor it
// has only just built -- panics with index out of range [-1].
//
// The cardfuzz panic (seed 18319407183030670816, Forbidden Ritual) was the
// RepeatOptional$ "Repeat this process?" yes/no answer falling into the
// "modes" default arm of resumeAnswerBindingRest, which wrote
// modeChoiceNames(Repeat SA, [yes]) = [""] into Ctx.Modes. The tests below
// pin each member of that class the census found.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// answerUntil answers every non-priority decision with option 0 (the unless
// asks with "Don't pay") until one with the given ResumeKind is pending, and
// returns it.
func answerUntil(t *testing.T, e *Engine, d *decision.Decision, kind string) *decision.Decision {
	t.Helper()
	for i := 0; d != nil && d.ResumeKind != kind; i++ {
		if i > 12 {
			t.Fatalf("no %q decision after %d answers; last %+v", kind, i, d)
		}
		pick := 0
		for _, o := range d.Options {
			if o.Label == "Don't pay" {
				pick = o.Index
			}
		}
		submitChoices(t, e, pick)
		d = e.Pending()
	}
	if d == nil {
		t.Fatalf("no %q decision pending", kind)
	}
	return d
}

// noPanic turns an engine panic into a test failure naming the step, so the
// regression reports instead of killing the parallel test binary.
func noPanic(t *testing.T, step string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s panicked: %v", step, r)
		}
	}()
	f()
}

// TestForbiddenRitualRepeatYesAsksTheNextChooser is the cardfuzz panic
// end to end on the real corpus card. Seat 0 keeps exactly two nontoken
// permanents, so the first iteration's sacrifice is a real pick and the
// second iteration's is forced (one candidate, no ask): after the "Repeat"
// answer the second iteration runs straight from the election's re-entry
// into the GenericChoice, with no intervening ask to rebuild the Ctx. The
// targeted opponent must be asked its choice for that iteration -- the
// election's yes is not a mode answer.
func TestForbiddenRitualRepeatYesAsksTheNextChooser(t *testing.T) {
	t.Parallel()
	e, cfg, id := forbiddenRitualFixture(t, 6205)
	bf := append([]state.ObjID(nil), e.G.Zone(state.ZBattlefield, 0)...)
	for _, oid := range bf[2:] {
		e.emit(events.Event{Kind: events.MoveZone, Obj: oid, From: state.ZBattlefield, To: state.ZGraveyard})
	}
	if got := len(e.G.Zone(state.ZBattlefield, 0)); got != 2 {
		t.Fatalf("precondition failed: seat 0 controls %d permanents, want 2", got)
	}
	e.pending = nil
	e.priorityRound()
	addMana(t, e, 0, "BBCC")

	d := answerUntil(t, e, castFixture(t, e, id, 1), "repeat_optional")
	if got := countSacrifices(e); got != 1 {
		t.Fatalf("precondition failed: first iteration sacrificed %d permanents, want 1", got)
	}
	yes := -1
	for _, o := range d.Options {
		if o.Kind == "yes" {
			yes = o.Index
		}
	}
	noPanic(t, "the Repeat answer", func() { submitChoices(t, e, yes) })

	d = e.Pending()
	if got := countSacrifices(e); got != 2 {
		t.Fatalf("second iteration sacrificed %d permanents in total, want 2 (the forced pick)", got)
	}
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "generic_players" || d.Player != 1 {
		t.Fatalf("after the Repeat answer, pending = %+v; want the targeted opponent's GenericChoice ask", d)
	}
	noPanic(t, "draining the rest", func() {
		d = answerUntil(t, e, d, "repeat_optional")
		submitStop(t, e, d)
		passUntilStackEmpty(t, e, 30)
	})
	if o := e.G.Obj(id); o != nil && o.Zone == state.ZStack {
		t.Fatal("Forbidden Ritual is still on the stack")
	}
	replayCheck(t, e, cfg)
}

// TestNameAnswerDoesNotChooseTheChainedCharmsMode pins the "name" member
// (Liar's Pendulum's shape: NameCard, then a GenericChoice on the same
// chain). The name answer resumed through the "modes" default arm too, so the
// Charm after it read [""] (or an empty non-nil list for a later name) as its
// already-chosen modes and resolved nothing, never posing its own ask.
func TestNameAnswerDoesNotChooseTheChainedCharmsMode(t *testing.T) {
	t.Parallel()
	spell := "Name:NameThenCharm\nManaCost:R\nTypes:Sorcery\n" +
		"A:SP$ NameCard | Defined$ You | SubAbility$ DBCharm\n" +
		"SVar:DBCharm:DB$ Charm | Choices$ DoGain,DoLose\n" +
		"SVar:DoGain:DB$ GainLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Gain 5 life\n" +
		"SVar:DoLose:DB$ LoseLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Lose 5 life\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 6206, spell)
	addMana(t, e, 0, "R")
	life := e.G.Players[0].Life
	d := castFixture(t, e, id, -1)
	if d == nil || d.ResumeKind != "name" {
		t.Fatalf("pending = %+v, want the NameCard ask", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "modes" {
		t.Fatalf("after the name answer, pending = %+v; want the chained Charm's own mode ask", d)
	}
	submitChoices(t, e, 1) // Lose 5 life
	if got := e.G.Players[0].Life; got != life-5 {
		t.Fatalf("life = %d, want %d (the chosen mode ran)", got, life-5)
	}
	replayCheck(t, e, cfg)
}

// modalLeakScript is a modal trigger whose chosen mode suspends (a hidden
// graveyard pick) and then reaches a NESTED Charm on its own chain. The
// trigger's announced ChosenModes ([MReturn]) belong to the root Charm only;
// the hidden pick's resume re-enters MReturn, not the root, so the nested
// Charm must pose its own ask rather than re-run the root's mode names.
const modalLeakScript = "Name:LeakCharm\nManaCost:1 U\nTypes:Creature Human Wizard\nPT:2/2\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigCharm | TriggerDescription$ When NICKNAME enters, ABILITY\n" +
	"SVar:TrigCharm:DB$ Charm | Choices$ MReturn,MLife\n" +
	"SVar:MReturn:DB$ ChangeZone | Hidden$ True | Mandatory$ True | ChangeType$ Creature.YouOwn | ChangeTypeDesc$ creature card | ChangeNum$ 1 | Origin$ Graveyard | Destination$ Hand | SubAbility$ DBInner | SpellDescription$ Return a creature card from your graveyard to your hand.\n" +
	"SVar:DBInner:DB$ Charm | Choices$ InGain,InLose\n" +
	"SVar:InGain:DB$ GainLife | Defined$ You | LifeAmount$ 2 | SpellDescription$ Inner gain 2 life\n" +
	"SVar:InLose:DB$ LoseLife | Defined$ You | LifeAmount$ 3 | SpellDescription$ Inner lose 3 life\n" +
	"SVar:MLife:DB$ LoseLife | Defined$ You | LifeAmount$ 1 | SpellDescription$ You lose 1 life.\n" +
	"Oracle:x\n"

// TestModalTriggerModesDoNotLeakPastTheirRoot pins the ability-branch
// member: resumeResolution seeded the stack object's ChosenModes on EVERY
// resumed frame of an ability, so the nested Charm reached after the mode's
// own ask consumed the root's [MReturn] as its answer.
func TestModalTriggerModesDoNotLeakPastTheirRoot(t *testing.T) {
	t.Parallel()
	e, cfg := charmTwoSeatDeck(t, 6207, modalLeakScript)
	putCreature(t, e, 0, modalLeakScript)
	// Two graveyard creatures: a leaked re-run of MReturn would pose a
	// second hidden pick instead of the nested Charm's ask.
	for i := 0; i < 2; i++ {
		if id := addToGraveyard(t, e, 0, vanillaCreatureScript); id == 0 {
			t.Fatal("precondition: the graveyard creature was not seeded")
		}
	}
	e.putTriggersOnStack()
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("pending = %+v, want the placement modes ask", d)
	}
	submitChoices(t, e, 0) // MReturn
	d = passUntilNonPriority(t, e, 10)
	if d == nil || d.ResumeKind != "hidden_pick" {
		t.Fatalf("pending = %+v, want MReturn's hidden graveyard pick", d)
	}
	life := e.G.Players[0].Life
	noPanic(t, "the hidden pick", func() { submitChoices(t, e, d.Options[0].Index) })
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes || len(d.Options) != 2 ||
		!strings.Contains(d.Options[0].Label, "Inner gain") {
		t.Fatalf("after the pick, pending = %+v; want the nested Charm's own mode ask", d)
	}
	submitChoices(t, e, 1) // Inner lose 3 life
	if got := e.G.Players[0].Life; got != life-3 {
		t.Fatalf("life = %d, want %d (only the nested Charm's chosen mode ran)", got, life-3)
	}
	replayCheck(t, e, cfg)
}

// modeAnswerKinds are the resume kinds whose answer IS a mode selection of
// the SA the frame re-enters (or, for charm_rest, the remaining announced
// modes of the Charm re-entering itself). Only their arms may write a
// non-nil Ctx.Modes. Adding a kind here is a claim that its decision is a
// KModes pick of rp.sa -- a yes/no, a name, a card pick or a pure
// continuation is not.
var modeAnswerKinds = map[string]bool{
	"modes":           true,
	"villainous":      true,
	"generic_players": true,
	"charm_rest":      true,
}

// TestResumeModesBoundOnlyByModeAnswers is the class ratchet for the stale
// Ctx.Modes defect: in resumeResolution and its two answer-binding switches,
// every assignment of a non-nil value to ctx.Modes must sit in a case arm (or
// an `rp.kind == "..."` branch) whose kinds are all modeAnswerKinds, under
// an isModeAnswerKind guard, or be the root-scoped announcement seed
// resumeChosenModes. A bare default arm may only clear it. A new resume kind
// therefore cannot inherit a mode binding by falling through to a default --
// the way "repeat_optional" and "name" did -- and isModeAnswerKind itself
// may admit no resume kind the engine poses outside modeAnswerKinds.
func TestResumeModesBoundOnlyByModeAnswers(t *testing.T) {
	t.Parallel()
	funcs := map[string]bool{"resumeResolution": true, "resumeAnswerBinding": true, "resumeAnswerBindingRest": true}
	files := []string{"resolution.go", "resolution_answer.go", "resolution_answer_rest.go"}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	assignments := 0
	var bad []string
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || !funcs[fd.Name.Name] {
				continue
			}
			seen[fd.Name.Name] = true
			var path []ast.Node
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if n == nil {
					path = path[:len(path)-1]
					return true
				}
				path = append(path, n)
				as, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for i, lhs := range as.Lhs {
					sel, ok := lhs.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Modes" {
						continue
					}
					if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "ctx" {
						continue
					}
					assignments++
					rhs := as.Rhs[0]
					if len(as.Rhs) == len(as.Lhs) {
						rhs = as.Rhs[i]
					}
					if id, ok := rhs.(*ast.Ident); ok && id.Name == "nil" {
						continue
					}
					if call, ok := rhs.(*ast.CallExpr); ok {
						if fn, ok := call.Fun.(*ast.Ident); ok && fn.Name == "resumeChosenModes" {
							continue
						}
					}
					if kinds, ok := enclosingKinds(path); !ok || !allModeAnswers(kinds) {
						bad = append(bad, fset.Position(as.Pos()).String()+" (kinds "+strings.Join(kinds, ",")+")")
					}
				}
				return true
			})
		}
	}
	for fn := range funcs {
		if !seen[fn] {
			t.Fatalf("%s not found: the ratchet no longer covers the resume answer binding", fn)
		}
	}
	if assignments == 0 {
		t.Fatal("no ctx.Modes assignment found: the ratchet is measuring nothing")
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Errorf("ctx.Modes bound outside a mode-answer kind at %s", b)
	}

	// The guard's own vocabulary: every resume kind literal the engine
	// poses (rules and effects sources) that isModeAnswerKind admits must be
	// a mode answer.
	kinds := resumeKindLiterals(t)
	if !kinds["repeat_optional"] || !kinds["name"] || !kinds["modes"] {
		t.Fatalf("resume kind census lost its known members (got %d kinds)", len(kinds))
	}
	for k := range kinds {
		if isModeAnswerKind(k) && !modeAnswerKinds[k] {
			t.Errorf("isModeAnswerKind admits %q, which is not a mode answer", k)
		}
	}
}

// resumeKindLiterals collects the string literals assigned as a resume kind
// in the non-test rules and effects sources: `ResumeKind: "x"`,
// `ResumeKind = "x"`, `kind: "x"` in a composite literal and `.kind = "x"`.
func resumeKindLiterals(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, dir := range []string{".", "../effects"} {
		pkgs, err := parser.ParseDir(token.NewFileSet(), dir, func(fi fs.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, pkg := range pkgs {
			for _, f := range pkg.Files {
				ast.Inspect(f, func(n ast.Node) bool {
					var key ast.Expr
					var val ast.Expr
					switch x := n.(type) {
					case *ast.KeyValueExpr:
						key, val = x.Key, x.Value
					case *ast.AssignStmt:
						if len(x.Lhs) != 1 || len(x.Rhs) != 1 {
							return true
						}
						key, val = x.Lhs[0], x.Rhs[0]
					default:
						return true
					}
					name := ""
					switch k := key.(type) {
					case *ast.Ident:
						name = k.Name
					case *ast.SelectorExpr:
						name = k.Sel.Name
					}
					if name != "ResumeKind" && name != "kind" {
						return true
					}
					if lit, ok := val.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						v, _ := strconv.Unquote(lit.Value)
						out[v] = true
					}
					return true
				})
			}
		}
	}
	return out
}

// enclosingKinds returns the resume kinds guarding the innermost case
// clause or `rp.kind == "..."` if-branch on path. ok is false when the
// assignment is guarded by neither, or by a default clause.
func enclosingKinds(path []ast.Node) (kinds []string, ok bool) {
	for i := len(path) - 1; i >= 0; i-- {
		switch n := path[i].(type) {
		case *ast.CaseClause:
			if n.List == nil {
				return []string{"default"}, false
			}
			for _, e := range n.List {
				lit, isLit := e.(*ast.BasicLit)
				if !isLit || lit.Kind != token.STRING {
					return nil, false
				}
				k, _ := strconv.Unquote(lit.Value)
				kinds = append(kinds, k)
			}
			return kinds, true
		case *ast.IfStmt:
			if i+1 < len(path) && path[i+1] != n.Body {
				continue // inside the condition or the else branch
			}
			if call, isCall := n.Cond.(*ast.CallExpr); isCall {
				if fn, isID := call.Fun.(*ast.Ident); isID && fn.Name == "isModeAnswerKind" {
					// The guard's vocabulary is checked against the
					// engine's resume kinds separately.
					return []string{"modes", "villainous"}, true
				}
			}
			be, isBin := n.Cond.(*ast.BinaryExpr)
			if !isBin || be.Op != token.EQL {
				continue
			}
			sel, isSel := be.X.(*ast.SelectorExpr)
			lit, isLit := be.Y.(*ast.BasicLit)
			if !isSel || sel.Sel.Name != "kind" || !isLit || lit.Kind != token.STRING {
				continue
			}
			k, _ := strconv.Unquote(lit.Value)
			return []string{k}, true
		}
	}
	return nil, false
}

func allModeAnswers(kinds []string) bool {
	for _, k := range kinds {
		if !modeAnswerKinds[k] {
			return false
		}
	}
	return len(kinds) > 0
}
