package rules

// Count$YouScryThisTurn / Count$YouSurveilThisTurn: the number of times the
// resolving controller scried / surveilled this turn (Forge's per-turn scry
// and surveil tallies), read off the events.Scry / events.Surveil records
// since the last TurnChange. Proctor of Potential's graveyard ability
// ("Activate only if you've scried or surveilled this turn",
// `CheckSVar$ X` over `Count$YouScryThisTurn/Plus.Y`, Y =
// `Count$YouSurveilThisTurn`) is the behavioural reader.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// castLookSpell casts the named one-mana blue scry/surveil cantrip and drains
// the stack, keeping every looked-at card on top.
func castLookSpell(t *testing.T, e *Engine, name string) {
	t.Helper()
	addMana(t, e, 0, "U")
	castNamed(t, e, name)
	drainKeepTop(t, e, name)
}

// drainKeepTop resolves the stack, passing priority and keeping every card a
// scry/surveil looks at on top.
func drainKeepTop(t *testing.T, e *Engine, name string) {
	t.Helper()
	for range 40 {
		if len(e.G.Stack) == 0 {
			return
		}
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while resolving %s", name)
		}
		switch d.Kind {
		case decision.KArrange:
			keep := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				keep = append(keep, o.Index)
			}
			submitChoices(t, e, keep...)
		case decision.KPriority:
			submitChoices(t, e, passIndex(t, d))
		default:
			t.Fatalf("unexpected decision resolving %s: %+v", name, d)
		}
	}
	t.Fatalf("%s did not finish resolving", name)
}

func proctorFixture(t *testing.T, spell string) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Proctor of Potential"), lookup(t, reg, spell)}, nil)
	proctor := moveByName(t, e, 0, "Proctor of Potential", state.ZGraveyard)
	moveByName(t, e, 0, spell, state.ZHand)
	e.priorityRound()
	return e, cfg, proctor
}

func TestProctorOfPotentialReadsScryThisTurn(t *testing.T) {
	t.Parallel()
	for _, spell := range []string{"Opt", "Consider"} {
		t.Run(spell, func(t *testing.T) {
			t.Parallel()
			e, cfg, proctor := proctorFixture(t, spell)
			if n := e.ScriedThisTurn(0) + e.SurveilledThisTurn(0); n != 0 {
				t.Fatalf("precondition: seat 0 already scried/surveilled %d time(s) this turn", n)
			}
			addMana(t, e, 0, "WU")
			if _, ok := findAbilityOption(e, proctor, 0); ok {
				t.Fatal("Proctor of Potential's graveyard ability is offered before any scry or surveil this turn")
			}
			castLookSpell(t, e, spell)
			wantScry, wantSurveil := int32(1), int32(0)
			if spell == "Consider" {
				wantScry, wantSurveil = 0, 1
			}
			if got := e.ScriedThisTurn(0); got != wantScry {
				t.Fatalf("ScriedThisTurn(0) after %s = %d, want %d", spell, got, wantScry)
			}
			if got := e.SurveilledThisTurn(0); got != wantSurveil {
				t.Fatalf("SurveilledThisTurn(0) after %s = %d, want %d", spell, got, wantSurveil)
			}
			if got := e.ScriedThisTurn(1) + e.SurveilledThisTurn(1); got != 0 {
				t.Fatalf("seat 1 credited with %d scry/surveil it never performed", got)
			}
			addMana(t, e, 0, "WU")
			opt, ok := findAbilityOption(e, proctor, 0)
			if !ok {
				t.Fatalf("Proctor of Potential's graveyard ability is not offered after %s: %+v", spell, e.Pending())
			}
			submitChoices(t, e, opt.Index)
			// Proctor's own "whenever this creature ... enters, surveil 1"
			// fires on the return.
			drainKeepTop(t, e, "the Proctor activation")
			o := e.G.Obj(proctor)
			if o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("Proctor of Potential did not return to the battlefield: %+v", o)
			}
			if got := o.Counter("FINALITY"); got != 1 {
				t.Fatalf("Proctor of Potential returned with %d finality counters, want 1", got)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestScryThisTurnResetsAtTurnChange: the tally is per TURN -- the next
// turn's TurnChange clears it.
func TestScryThisTurnResetsAtTurnChange(t *testing.T) {
	t.Parallel()
	e, _, _ := proctorFixture(t, "Opt")
	castLookSpell(t, e, "Opt")
	if got := e.ScriedThisTurn(0); got != 1 {
		t.Fatalf("precondition: ScriedThisTurn(0) = %d, want 1", got)
	}
	turn := e.G.Turn
	for range 200 {
		if e.G.Turn != turn {
			break
		}
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while passing the turn")
		}
		if d.Kind == decision.KPriority {
			submitChoices(t, e, passIndex(t, d))
			continue
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	if e.G.Turn == turn {
		t.Fatal("the turn never ended")
	}
	if got := e.ScriedThisTurn(0); got != 0 {
		t.Fatalf("ScriedThisTurn(0) on the next turn = %d, want 0", got)
	}
}
