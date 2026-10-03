package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The bounded mid-resolution ChooseNumber ask (task bounded1, absorbing
// agent-20260922T091245Z-d777f1b6): a card's own Max$ bound, resolved against
// the current resolution context, and its ListTitle$ prompt now shape the
// number ask -- no more hard-coded 0..12 offer under a literal "Choose a
// number". The primitive-level pins live effects-side
// (effects/choose_number_bounds_test.go); these are the REAL-corpus end-to-end
// pins on the carriers the brief names: Pia Nalaar, Chief Mechanic (direct
// Max$ Count$YourCountersEnergy, driving the full trigger -> ask -> pay ->
// X/X token chain), Localized Destruction and Aether Refinery (the Creative
// Energy may-pay carriers, absorbing the d777f1b6 ticket: one KChoose bounded
// by the current energy with each card's exact ListTitle$ prompt, the
// answered value folded into one Choose{number} event), and Rampaging
// Aetherhood (the SVar-indirect Max$ Max spelling).

// numberEventsIn counts the Choose events a log carries whose Counter is
// "number" -- the one event the answered ask records the pick with.
func numberEventsIn(e *Engine) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && ev.Counter == "number" {
			out = append(out, ev)
		}
	}
	return out
}

// TestPiaNalaarBoundedEnergyChoiceMakesTheToken is the flagship end-to-end
// pin: Pia Nalaar, Chief Mechanic's end-step trigger poses its ChooseNumber
// ask bounded by the controller's ACTUAL energy (Max$
// Count$YourCountersEnergy) under the card's ListTitle$ prompt; answering 2
// with 3 {E} on the table pays 2 {E} through the token body's UnlessCost$
// Mandatory PayEnergy<X> and creates the 2/2 Nalaar Aetherjet. The answered
// number (2) is within the OLD fixed 0..12 list too, so the token half alone
// cannot prove the fix -- the option-shape assertions (exactly 0..3, the
// ListTitle prompt) are what the old list fails.
func TestPiaNalaarBoundedEnergyChoiceMakesTheToken(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := ateotEngine(t, reg, "Pia Nalaar, Chief Mechanic")
	pia := ateotFind(t, e, "Pia Nalaar, Chief Mechanic", 0)
	ateotTo(t, e, pia, state.ZLibrary, state.ZBattlefield)
	if o := e.G.Obj(pia); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Pia Nalaar is not on the battlefield: %+v", o)
	}
	// The real face carries exactly the parameters under test.
	face := e.G.Obj(pia).Face()
	askSA := resolveSVarOf(t, face, "TrigChooseNumber")
	if askSA.Params["Max"] != "Count$YourCountersEnergy" {
		t.Fatalf("precondition: TrigChooseNumber Max$ = %q", askSA.Params["Max"])
	}
	if askSA.Params["ListTitle"] != "amount of energy to pay" {
		t.Fatalf("precondition: TrigChooseNumber ListTitle$ = %q", askSA.Params["ListTitle"])
	}
	seedPlayerCounter(t, e, 0, "ENERGY", 3)
	ateotDriveToStep(t, e, 1, 0, state.StepEnd)

	// Pass priority until the trigger's number ask is pending.
	d := passUntilAskKind(t, e, decision.KChoose, 400)
	if d == nil || d.ResumeKind != "choosenumber" {
		t.Fatalf("expected the bounded number ask, got %+v", d)
	}
	if d.Player != 0 || d.Min != 1 || d.Max != 1 {
		t.Fatalf("ask meta = player %d min %d max %d, want seat 0 and a one-pick ask", d.Player, d.Min, d.Max)
	}
	want := e.G.Players[0].Counter("ENERGY")
	if want != 3 {
		t.Fatalf("precondition: seat 0's energy at ask time = %d, want the seeded 3", want)
	}
	if got := d.Prompt; got != "amount of energy to pay" {
		t.Fatalf("prompt = %q, want the card's ListTitle$ verbatim", got)
	}
	if len(d.Options) != int(want)+1 {
		t.Fatalf("options = %+v (%d), want exactly 0..%d -- the card's own bound, not the fixed 0..12", d.Options, len(d.Options), want)
	}
	for i, o := range d.Options {
		if o.Kind != "number" || o.Amount != i {
			t.Fatalf("option %d = %+v, want the ascending number %d", i, o, i)
		}
	}
	two := -1
	for _, o := range d.Options {
		if o.Kind == "number" && o.Amount == 2 {
			two = o.Index
		}
	}
	if two < 0 {
		t.Fatalf("precondition: the bounded list offers no 2: %+v", d.Options)
	}
	submitChoices(t, e, two)

	// The chain continues to DBToken: its UnlessCost$ Mandatory PayEnergy<X>
	// poses the unless-pay election ("Pay ..." is option 0); answer pay.
	for i := 0; i < 100; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision pending while waiting for the unless-pay ask")
		}
		if d.ResumeKind == "unless_pay" {
			if e.G.Players[0].Counter("ENERGY") < 2 {
				t.Fatalf("precondition: the payer holds %d {E}, cannot pay 2", e.G.Players[0].Counter("ENERGY"))
			}
			submitChoices(t, e, 0)
			break
		}
		if d.Kind == decision.KPriority {
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			submitChoices(t, e, idx)
			continue
		}
		t.Fatalf("unexpected decision while waiting for the unless-pay ask: %+v", d)
	}

	// Drain the rest of the stack, then assert the outcome.
	passUntilStackEmpty(t, e, 100)
	var jet state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Nalaar Aetherjet" {
			jet = id
		}
	}
	if jet == 0 {
		t.Fatalf("answered 2 of 3 {E}: no Nalaar Aetherjet token was created (battlefield %+v)", e.G.Zone(state.ZBattlefield, 0))
	}
	if pt := e.Derived(jet); pt.Power != 2 || pt.Toughness != 2 {
		t.Fatalf("Nalaar Aetherjet P/T = %d/%d, want the answered 2/2", pt.Power, pt.Toughness)
	}
	if got := e.G.Players[0].Counter("ENERGY"); got != 1 {
		t.Fatalf("energy after the pay = %d, want 3-2=1", got)
	}
	evs := numberEventsIn(e)
	if len(evs) != 1 || evs[0].Amount != 2 {
		t.Fatalf("Choose{number} events = %+v, want exactly one recording the answered 2", evs)
	}
	replayCheck(t, e, cfg)
}
