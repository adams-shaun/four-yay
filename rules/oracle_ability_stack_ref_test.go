package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// Level-B ability-on-stack targets (ticket agent-20261009T085207Z-c99a362b):
// a scenario step must be able to NAME a pending ability object on the stack
// as a target ref -- `pN:ability:<source name>` (rules/oracle_run.go splitRef
// + resolve) -- and the engine's offer must admit the pending ability to the
// ask, so a CopySpellAbility activation copies it end to end.

// TestOracleAbilityOnStackRefActivated: Gogo, Master of Mimicry's
// "{X}{X}, {T}: Copy target activated or triggered ability you control X
// times" copies a PENDING Prodigal Pyromancer ping. The prelude activates
// the ping and leaves it unresolved; the Gogo step targets it through the
// ability ref; both the copy and the original resolve, so p1 takes the ping
// twice.
func TestOracleAbilityOnStackRefActivated(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const sc = `{"name":"ability-on-stack-activated-ref","cr":["115.5","701.10a"],
"why":"a pending activated ability on the stack is a legal target and the pN:ability ref resolves to it",
"setup":{"p0":{"battlefield":["Prodigal Pyromancer","Gogo, Master of Mimicry"]}},
"steps":[
 {"op":"activate","seat":0,"card":"p0:Prodigal Pyromancer","targets":["p1"]},
 {"op":"activate","seat":0,"card":"p0:Gogo, Master of Mimicry","mana":"CC","targets":["p0:ability:Prodigal Pyromancer"]},
 {"op":"resolve","seat":0},{"op":"resolve","seat":0},{"op":"resolve","seat":0}]}`
	res, err := RunOracleScenarioJSON(reg, []byte(sc))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	// Precondition: both probes are on p0's battlefield at setup.
	for _, ref := range []string{"p0:Prodigal Pyromancer", "p0:Gogo, Master of Mimicry"} {
		if p, ok := snapPerm(res.Snapshots[0], ref); !ok || p.Tapped {
			t.Fatalf("precondition: %s = %+v, want an untapped permanent on the battlefield", ref, p)
		}
	}
	// After the prelude the ping is PENDING on the stack (kind ability,
	// source the pyromancer); the Gogo step runs at that priority.
	pending := res.Snapshots[1].Stack
	if len(pending) != 1 || pending[0].Kind != "ability" || pending[0].Source != "p0:Prodigal Pyromancer" {
		t.Fatalf("precondition: stack after the prelude = %+v, want exactly the pending ping ability", pending)
	}
	// The Gogo ask OFFERED the pending ability (the option's ref is the
	// pN:ability ref this ticket introduced) and the step picked it.
	offered := false
	for _, d := range res.Decisions {
		if d.Step != 1 || d.Kind != "target" {
			continue
		}
		for _, r := range d.OptionRefs {
			if r == "p0:ability:Prodigal Pyromancer" {
				offered = true
			}
		}
		if len(d.PickRefs) != 1 || d.PickRefs[0] != "p0:ability:Prodigal Pyromancer" {
			t.Fatalf("Gogo target ask picked %v, want exactly p0:ability:Prodigal Pyromancer", d.PickRefs)
		}
	}
	if !offered {
		t.Fatalf("the Gogo ask never offered the pending ability; decisions: %+v", res.Decisions)
	}
	// The copy resolved: p1 took the 1-damage ping twice (copy + original).
	final := res.Snapshots[len(res.Snapshots)-1]
	if life := final.Players[1].Life; life != 18 {
		t.Fatalf("p1 life = %d, want 18 (the ping resolved twice)", life)
	}
	if len(final.Stack) != 0 {
		t.Fatalf("final stack = %+v, want empty", final.Stack)
	}
}

// TestOracleAbilityOnStackRefTriggered: Kirol, Attentive First-Year copies a
// PENDING Elvish Visionary ETB trigger. The prelude MOVES the Visionary onto
// the battlefield (setup placements fire no ETB triggers), whose entry fold
// pushes the draw trigger and stops at p0's priority; Kirol's
// tapXType<2/Creature> activation targets it through the ability ref and the
// copy draws a second card.
func TestOracleAbilityOnStackRefTriggered(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const sc = `{"name":"ability-on-stack-triggered-ref","cr":["115.5","603.3"],
"why":"a pending triggered ability on the stack is a legal target and the pN:ability ref resolves to it",
"setup":{"p0":{"hand":["Elvish Visionary"],"battlefield":["Kirol, Attentive First-Year","Grizzly Bears"]}},
"steps":[
 {"op":"move","seat":0,"card":"p0:Elvish Visionary","to":"battlefield"},
 {"op":"activate","seat":0,"card":"p0:Kirol, Attentive First-Year","targets":["p0:ability:Elvish Visionary"]},
 {"op":"resolve","seat":0},{"op":"resolve","seat":0},{"op":"resolve","seat":0}]}`
	res, err := RunOracleScenarioJSON(reg, []byte(sc))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	// Precondition: the Visionary is p0's whole OPENING hand (the runner
	// returns the dealt opening hand to the library and moves each named
	// card to its setup zone), so the move is what puts it on the
	// battlefield and fires the trigger.
	if got := len(res.Snapshots[0].Players[0].Hand); got != 1 {
		t.Fatalf("precondition: p0's opening hand has %d cards, want 1 (the Visionary)", got)
	}
	if _, ok := snapPerm(res.Snapshots[0], "p0:Elvish Visionary"); ok {
		t.Fatal("precondition: the Visionary is already on the battlefield at setup; the move fires nothing")
	}
	// After the move the ETB trigger is PENDING on the stack.
	pending := res.Snapshots[1].Stack
	if len(pending) != 1 || pending[0].Kind != "ability" || pending[0].Source != "p0:Elvish Visionary" {
		t.Fatalf("precondition: stack after the move = %+v, want exactly the pending ETB trigger", pending)
	}
	// The Kirol ask OFFERED the pending trigger and the step picked it.
	offered := false
	for _, d := range res.Decisions {
		if d.Step != 1 || d.Kind != "target" {
			continue
		}
		for _, r := range d.OptionRefs {
			if r == "p0:ability:Elvish Visionary" {
				offered = true
			}
		}
		if len(d.PickRefs) != 1 || d.PickRefs[0] != "p0:ability:Elvish Visionary" {
			t.Fatalf("Kirol target ask picked %v, want exactly p0:ability:Elvish Visionary", d.PickRefs)
		}
	}
	if !offered {
		t.Fatalf("the Kirol ask never offered the pending trigger; decisions: %+v", res.Decisions)
	}
	// The copied trigger resolved: p0 drew twice (the trigger and its copy).
	final := res.Snapshots[len(res.Snapshots)-1]
	if got := len(final.Players[0].Hand); got != 2 {
		t.Fatalf("p0's hand has %d cards, want 2 (empty after the move, +2 draws)", got)
	}
	if len(final.Stack) != 0 {
		t.Fatalf("final stack = %+v, want empty", final.Stack)
	}
	if _, ok := snapPerm(final, "p0:Elvish Visionary"); !ok {
		t.Fatal("the Visionary left the battlefield")
	}
}
