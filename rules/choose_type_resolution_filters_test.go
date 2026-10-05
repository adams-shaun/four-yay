package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// TestResolutionChooseTypeCreatureHonoursValidTypes pins the real resolution-
// time ChooseType path through Host.TypeChoices, rather than only testing the
// engine's type vocabulary directly.
func TestResolutionChooseTypeCreatureHonoursValidTypes(t *testing.T) {
	t.Parallel()
	e, _, mk := kr0ChooseSource(t)
	all := e.TypeChoices(0, "Creature", "", "")
	if len(all) <= 100 {
		t.Fatalf("precondition: unfiltered creature vocabulary has %d options, want >100", len(all))
	}
	if len(e.TypeChoices(0, "Creature", "Elf,Goblin", "")) != 2 {
		t.Fatal("precondition: ValidTypes$ Elf,Goblin must differ from the full vocabulary")
	}

	d := kr0Run(t, e, kr0SA(t, "SP$ ChooseType | Defined$ You | Type$ Creature | ValidTypes$ Elf,Goblin"), mk, nil)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choosetype" {
		t.Fatalf("resolution-time ChooseType ask = %+v, want a choosetype decision", d)
	}
	if len(d.Options) != 2 || d.Options[0].Label != "Elf" || d.Options[1].Label != "Goblin" {
		t.Fatalf("ValidTypes$ Elf,Goblin options = %+v, want exactly [Elf Goblin]", d.Options)
	}
}

// TestResolutionChooseTypeCreatureHonoursInvalidTypes pins the corresponding
// resolution-time exclusion path and guards against a vacuous empty offer.
func TestResolutionChooseTypeCreatureHonoursInvalidTypes(t *testing.T) {
	t.Parallel()
	e, _, mk := kr0ChooseSource(t)
	all := e.TypeChoices(0, "Creature", "", "")
	if len(all) <= 100 {
		t.Fatalf("precondition: unfiltered creature vocabulary has %d options, want >100", len(all))
	}
	unfiltered := e.TypeChoices(0, "Creature", "", "Elf,Goblin")
	if len(unfiltered) != len(all)-2 {
		t.Fatalf("precondition: InvalidTypes$ removes %d entries, want exactly 2", len(all)-len(unfiltered))
	}

	d := kr0Run(t, e, kr0SA(t, "SP$ ChooseType | Defined$ You | Type$ Creature | InvalidTypes$ Elf,Goblin"), mk, nil)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choosetype" {
		t.Fatalf("resolution-time ChooseType ask = %+v, want a choosetype decision", d)
	}
	if len(d.Options) != len(all)-2 {
		t.Fatalf("InvalidTypes$ Elf,Goblin options = %d, want %d", len(d.Options), len(all)-2)
	}
	for _, o := range d.Options {
		if o.Label == "Elf" || o.Label == "Goblin" {
			t.Fatalf("InvalidTypes$ leaked %q into resolution-time options", o.Label)
		}
	}
}
