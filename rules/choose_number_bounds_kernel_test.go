package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
)

// kr4NumberOption returns the index of the number option with amount n.
func kr4NumberOption(t *testing.T, d *decision.Decision, n int) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == "number" && o.Amount == n {
			return o.Index
		}
	}
	t.Fatalf("number %d not offered: %+v", n, d.Options)
	return -1
}

// TestEnergyMayPayCarriersPoseTheBoundedAskKernel: Localized Destruction's
// and Aether Refinery's `ChooseNumber | Max$ Count$YourCountersEnergy` pose a
// real ask bounded by the controller's energy (exactly 0..energy) under the
// card's ListTitle$; the answer is one Choose{number} event read back by
// Count$ChosenNumber, and the chained body's UnlessCost$ PayEnergy<X> then
// asks the controller.
func TestEnergyMayPayCarriersPoseTheBoundedAskKernel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		prompt string
		seed   int32
	}{
		{name: "Localized Destruction", prompt: "Choose amount of energy to pay", seed: 3},
		{name: "Aether Refinery", prompt: "amount of energy to pay", seed: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := layerEngine(t)
			id := onBoardCard(t, e, 0, corpusCard(t, tc.name))
			face := e.G.Obj(id).Face()
			sa := resolveSVarOf(t, face, "DBChooseNumber")
			if sa.Params["Max"] != "Count$YourCountersEnergy" || sa.Params["ListTitle"] != tc.prompt {
				t.Fatalf("precondition: %s DBChooseNumber params %v", tc.name, sa.Params)
			}
			if n, ok := effects.EvalCountOK(e, &effects.Ctx{Controller: 0, Source: id}, "Count$ChosenNumber"); !ok || n != 0 {
				t.Fatalf("unbound Count$ChosenNumber = %d (ok %v), want 0", n, ok)
			}
			seedPlayerCounter(t, e, 0, "ENERGY", tc.seed)
			want := e.G.Players[0].Counter("ENERGY")
			if (want != tc.seed && want != 2*tc.seed) || want <= 0 || want > 12 {
				t.Fatalf("precondition: board energy = %d", want)
			}
			kr4Resolve(e, func() *effects.Ctx { return &effects.Ctx{Controller: 0, Source: id, SVars: face.SVars} }, sa)
			d := e.Pending()
			if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choosenumber" {
				t.Fatalf("expected the bounded number ask, got %+v", d)
			}
			if d.Player != 0 || d.Min != 1 || d.Max != 1 || d.Prompt != tc.prompt {
				t.Fatalf("ask meta = player %d min %d max %d prompt %q", d.Player, d.Min, d.Max, d.Prompt)
			}
			if len(d.Options) != int(want)+1 {
				t.Fatalf("options = %+v, want exactly 0..%d", d.Options, want)
			}
			for i, o := range d.Options {
				if o.Kind != "number" || o.Amount != i {
					t.Fatalf("option %d = %+v, want the ascending number %d", i, o, i)
				}
			}
			submitChoices(t, e, kr4NumberOption(t, d, int(want)))
			evs := numberEventsIn(e)
			if len(evs) != 1 || evs[0].Amount != want {
				t.Fatalf("Choose{number} events = %+v, want exactly one recording %d", evs, want)
			}
			if o := e.G.Obj(id); o.ChosenNumber != want {
				t.Fatalf("recorded answer = %d, want %d", o.ChosenNumber, want)
			}
			if n, ok := effects.EvalCountOK(e, &effects.Ctx{Controller: 0, Source: id, SVars: face.SVars}, "Count$ChosenNumber"); !ok || n != want {
				t.Fatalf("Count$ChosenNumber after the answer = %d (ok %v), want %d", n, ok, want)
			}
			d2 := e.Pending()
			if d2 == nil || d2.ResumeKind != "unless_pay" || d2.Player != 0 {
				t.Fatalf("the chained body did not pose seat 0's unless-pay ask, got %+v", d2)
			}
		})
	}
}

// TestRampagingAetherhoodSVarIndirectBoundKernel: `ChooseNumber | Max$ Max`
// resolves the named SVar (Count$YourCountersEnergy), offering exactly
// 0..energy under the ListTitle$, and records the answer.
func TestRampagingAetherhoodSVarIndirectBoundKernel(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	id := onBoardCard(t, e, 0, corpusCard(t, "Rampaging Aetherhood"))
	face := e.G.Obj(id).Face()
	sa := resolveSVarOf(t, face, "DBChooseNumber")
	if sa.Params["Max"] != "Max" || svarBodyOf(t, face, "Max") != "Count$YourCountersEnergy" || sa.Params["ListTitle"] != "amount of energy to pay" {
		t.Fatalf("precondition: DBChooseNumber params %v", sa.Params)
	}
	seedPlayerCounter(t, e, 0, "ENERGY", 4)
	if want := e.G.Players[0].Counter("ENERGY"); want != 4 {
		t.Fatalf("precondition: board energy = %d, want 4", want)
	}
	kr4Resolve(e, func() *effects.Ctx { return &effects.Ctx{Controller: 0, Source: id, SVars: face.SVars} }, sa)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choosenumber" || d.Prompt != "amount of energy to pay" {
		t.Fatalf("expected the bounded number ask, got %+v", d)
	}
	if len(d.Options) != 5 {
		t.Fatalf("options = %+v, want exactly 0..4", d.Options)
	}
	for i, o := range d.Options {
		if o.Kind != "number" || o.Amount != i {
			t.Fatalf("option %d = %+v, want the ascending number %d", i, o, i)
		}
	}
	submitChoices(t, e, kr4NumberOption(t, d, 1))
	evs := numberEventsIn(e)
	if len(evs) != 1 || evs[0].Amount != 1 {
		t.Fatalf("Choose{number} events = %+v, want exactly one recording 1", evs)
	}
	if o := e.G.Obj(id); o.ChosenNumber != 1 {
		t.Fatalf("recorded answer = %d, want 1", o.ChosenNumber)
	}
}
