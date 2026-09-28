package rules

// The generic (non-cross-mode) Charm mode loop's mid-mode suspension
// contract (task cli-20260922T225138Z-a850f8be, closing the AGENTS.md
// "effCharm's inner Resolve has no Suspended() guard" row).
//
// effCharm runs the chosen Choices$ sub-abilities in order. When a mode's
// own chain poses a mid-resolution ask, the walk must STOP there and report
// the remaining modes as a charm_rest continuation -- never run the next
// mode while a decision is pending (Engine.ask panics on the overwrite) and
// never re-run the modes that already ran. The cross-mode TargetUnique
// family (rules/charm_cross_mode_test.go) already pins its own loop; this
// file pins the GENERIC loop a plain Charm (no TargetUnique$) uses.
//
// The carrier is synthetic (.cards files are GPL and may never be committed):
// a Charm whose chosen modes include a hidden graveyard pick (the ChangeZone
// shape that suspends the resolution with a KChoose ask) and an observable
// LoseLife. If the generic loop lost its post-Resolve Suspended() guard, an
// answered pick's re-entry would run an already-run mode again.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// genericCharmScript is the synthetic carrier: NO TargetUnique$ anywhere, so
// CharmCrossModeShape classifies it None and effCharm takes the generic
// shared-target loop rather than charmCrossModeRun.
func genericCharmScript() string {
	return "Name:GenCharm\nManaCost:1 U\nTypes:Creature Human Wizard\nPT:2/2\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigCharm | TriggerDescription$ When NICKNAME enters, ABILITY\n" +
		"SVar:TrigCharm:DB$ Charm | CharmNum$ 2 | Choices$ MReturn,MLife\n" +
		"SVar:MReturn:DB$ ChangeZone | Hidden$ True | Mandatory$ True | ChangeType$ Creature.YouOwn | ChangeTypeDesc$ creature card | ChangeNum$ 1 | Origin$ Graveyard | Destination$ Hand | SpellDescription$ Return a creature card from your graveyard to your hand.\n" +
		"SVar:MLife:DB$ LoseLife | Defined$ You | LifeAmount$ 3 | SpellDescription$ You lose 3 life.\n" +
		"Oracle:x\n"
}

// genericCharmAtPick drives the carrier to the placement modes ask, answers
// the given modes, drains priority until the hidden graveyard pick pends, and
// returns the engine with that pick pending plus the seat-0 life recorded
// immediately before the modes were answered. m0/m1 are the mode indices in
// the chosen order.
func genericCharmAtPick(t *testing.T, seed uint64, m0, m1 int) (*Engine, int32) {
	t.Helper()
	gen := genericCharmScript()
	e, _ := charmTwoSeatDeck(t, seed, gen)
	putCreature(t, e, 0, gen)
	// Precondition: the hidden graveyard pick has exactly one eligible card
	// (seat 0's own graveyard), asserted before the charm resolves.
	if id := addToGraveyard(t, e, 0, vanillaCreatureScript); id == 0 {
		t.Fatal("precondition: the graveyard creature was not seeded")
	}
	if n := countZoneName(t, e, 0, state.ZGraveyard, "Vanilla"); n != 1 {
		t.Fatalf("precondition: seat 0 graveyard Vanillas = %d, want 1", n)
	}
	e.putTriggersOnStack()
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("pending = %+v, want the placement modes ask", d)
	}
	lifeBefore := e.G.Players[0].Life
	submitChoices(t, e, m0, m1)
	for i := 0; i < 10; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatal("no decision pending while the charm should resolve")
		}
		if d.Kind != decision.KPriority {
			break
		}
		pass := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pass = o.Index
			}
		}
		submitChoices(t, e, pass)
	}
	d = e.Pending()
	if d == nil || d.ResumeKind != "hidden_pick" {
		t.Fatalf("pending = %+v, want the ChangeZone hidden graveyard pick (the mode's own ask)", d)
	}
	if len(d.Options) != 1 {
		t.Fatalf("hidden-pick options = %d, want exactly seat 0's one graveyard creature", len(d.Options))
	}
	return e, lifeBefore
}

// drainCharm counts any further hidden graveyard pick and drains the rest of
// the resolution. It returns the number of EXTRA hidden-pick asks seen (a
// non-zero count means a mode re-ran) and the number of "no sub-ability
// recorded" degradation Notes the resume emitted (SuspendCharmRest suppresses
// the plain continuation report in favour of the Charm re-entry, so neither a
// non-empty nor an empty remainder emits one).
func drainCharm(t *testing.T, e *Engine) (extraPicks, degradedResumes int) {
	t.Helper()
	for i := 0; i < 30; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if d.ResumeKind == "hidden_pick" {
			extraPicks++
		}
		if d.Kind == decision.KPriority {
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			submitChoices(t, e, pass)
			continue
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "mid-resolution answer resumed with no sub-ability recorded" {
			degradedResumes++
		}
	}
	return extraPicks, degradedResumes
}

// TestGenericCharmModesSurviveTheMidModeSuspension pins the generic loop:
// choosing MReturn (the hidden graveyard pick) then MLife must suspend ONCE
// on the pick, and the answer's resume must run ONLY MLife -- the pick is not
// re-posed and the LoseLife lands exactly once.
func TestGenericCharmModesSurviveTheMidModeSuspension(t *testing.T) {
	t.Parallel()
	e, life0 := genericCharmAtPick(t, 6411, 0, 1) // MReturn, MLife
	d := e.Pending()
	submitChoices(t, e, d.Options[0].Index)
	extra, degraded := drainCharm(t, e)
	if extra != 0 {
		t.Fatalf("%d extra hidden graveyard pick(s): the generic loop re-ran the first mode", extra)
	}
	if degraded != 0 {
		t.Fatalf("%d degradation Note(s): the guard did not report the rest via SuspendCharmRest", degraded)
	}
	if e.G.Players[0].Life != life0-3 {
		t.Fatalf("seat 0 life = %d, want %d: MLife did not run exactly once",
			e.G.Players[0].Life, life0-3)
	}
	if n := countZoneName(t, e, 0, state.ZHand, "Vanilla"); n != 1 {
		t.Fatalf("seat 0 hand Vanillas = %d, want 1 (the pick returned it)", n)
	}
	if n := countZoneName(t, e, 0, state.ZGraveyard, "Vanilla"); n != 0 {
		t.Fatalf("seat 0 graveyard Vanillas = %d, want 0 (the pick moved it)", n)
	}
}

// TestGenericCharmSuspendsAfterAnEarlierModeAlreadyRan is the mirror-order
// pin the row's "ask N times" wording names directly: the OBSERVABLE mode
// runs first, the SUSPENDING mode second. With the guard, MLife lands once,
// MReturn's pick suspends, and the answered pick completes the charm without
// re-running MLife -- and, because this suspended mode was the LAST chosen
// one, with no "no sub-ability recorded" degradation Note either (the empty
// remainder is reported at the Charm level through SuspendCharmRest).
func TestGenericCharmSuspendsAfterAnEarlierModeAlreadyRan(t *testing.T) {
	t.Parallel()
	e, lifeBefore := genericCharmAtPick(t, 6412, 1, 0) // MLife, MReturn
	d := e.Pending()
	lifeAfterFirstMode := e.G.Players[0].Life
	// Precondition: MLife ran before the suspension, so the value the
	// post-resume assertion compares against really did move by 3 (and so the
	// MLife-ran-once assertion cannot pass vacuously against an unmoved base).
	if lifeAfterFirstMode != lifeBefore-3 {
		t.Fatalf("precondition: life after MLife = %d, want %d (one 3-life loss)",
			lifeAfterFirstMode, lifeBefore-3)
	}
	// Precondition: the hidden graveyard pick this test drains is really
	// pending, so the mode that suspends is the one whose completion the
	// note-count assertion below covers.
	if d.ResumeKind != "hidden_pick" {
		t.Fatalf("precondition: pending = %+v, want the hidden graveyard pick", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	extra, degraded := drainCharm(t, e)
	if extra != 0 {
		t.Fatalf("%d extra hidden graveyard pick(s) after the second mode", extra)
	}
	if degraded != 0 {
		t.Fatalf("%d degradation Note(s): the last-mode suspension must report an empty remainder through SuspendCharmRest", degraded)
	}
	if e.G.Players[0].Life != lifeAfterFirstMode {
		t.Fatalf("seat 0 life moved after the suspending mode completed: %d -> %d, want no MLife re-run",
			lifeAfterFirstMode, e.G.Players[0].Life)
	}
	if n := countZoneName(t, e, 0, state.ZHand, "Vanilla"); n != 1 {
		t.Fatalf("seat 0 hand Vanillas = %d, want 1", n)
	}
}

// TestGenericCharmCarrierIsNotCrossMode asserts the PRECONDITION both tests
// above depend on: the synthetic carrier classifies as CharmUniqueNone, so
// effCharm really does take the generic loop rather than charmCrossModeRun.
// Without this the tests could silently exercise the cross-mode runner and
// leave the row they exist to close untested.
func TestGenericCharmCarrierIsNotCrossMode(t *testing.T) {
	t.Parallel()
	gen := card(t, genericCharmScript())
	svars := gen.Faces[0].SVars
	trig := cards.ResolveSVar(svars, "TrigCharm")
	if trig == nil {
		t.Fatal("precondition: the carrier's TrigCharm SVar did not resolve")
	}
	var choices []string
	for _, p := range strings.Split(trig.Params["Choices"], ",") {
		choices = append(choices, strings.TrimSpace(p))
	}
	if len(choices) != 2 {
		t.Fatalf("precondition: carrier Choices$ = %v, want two modes", choices)
	}
	if status, why := effects.CharmCrossModeShape(svars, choices); status != effects.CharmUniqueNone {
		t.Fatalf("carrier classifies %v (%q), want CharmUniqueNone so the generic loop runs", status, why)
	}
}
