package main

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/shape"
)

// marina is the message XMage's TestPlayer raised for Marina Vendrell's
// activate#0.0 (DSK), as the oracle cache holds it.
const marina = "AssertionError: PlayerA - Targets list was setup by addTarget with [@p0:Bottomless Pool, [target_skip]], but not used\n" +
	"Card: Marina Vendrell, C, MarinaVendrell, DSK:221::0, 3/5\n" +
	"Ability: ActivateAsSorceryActivatedAbility ({T}: Lock or unlock a door of target Room you control. Activate only as a sorcery.)\n" +
	"Target: selected 0, more possible 1, TargetPermanent (Select a Room you control)\n" +
	"You must implement target class support in TestPlayer, \"filter instanceof\", or setup good targets"

// TestHarnessDetailKeepsTheFailingAsk: the first line of an unused-targets
// failure names the leftover answers; the Card/Ability/Target lines name the
// ask that found none of them, which the next replay needs to tell a
// mis-bound alias from a queue ordered against the ask sequence.
func TestHarnessDetailKeepsTheFailingAsk(t *testing.T) {
	got := harnessDetail(marina)
	for _, want := range []string{
		"Targets list was setup by addTarget with [@p0:Bottomless Pool, [target_skip]], but not used",
		"Card: Marina Vendrell",
		"Ability: ActivateAsSorceryActivatedAbility",
		"Target: selected 0, more possible 1, TargetPermanent (Select a Room you control)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("detail %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "\n") || strings.Contains(got, "You must implement") {
		t.Errorf("detail %q keeps a newline or the boilerplate tail", got)
	}
	// Any other harness message is unchanged: its first line only.
	other := "AssertionError: Found wrong choice command (invalid target or miss skip command):\nBottomless Pool\nTarget: selected 0"
	if got := harnessDetail(other); got != "AssertionError: Found wrong choice command (invalid target or miss skip command):" {
		t.Errorf("other message detail = %q, want its first line", got)
	}
}

// TestHarnessDetailLeavesTheShapeAlone: the appended ask lines must not
// scatter one root cause across a shape per card -- the shape of the long
// detail is the shape of the first line alone.
func TestHarnessDetailLeavesTheShapeAlone(t *testing.T) {
	long := compliance.VerdictRow{Card: "Marina Vendrell", Template: "activate#0.0",
		Status: compliance.StatusHarness, Detail: "xmage: " + harnessDetail(marina)}
	short := long
	short.Detail = "xmage: " + firstLine(marina)
	if long.Detail == short.Detail {
		t.Fatalf("precondition: the long detail adds nothing to the first line: %q", long.Detail)
	}
	a, ok1 := shape.Of(long)
	b, ok2 := shape.Of(short)
	if !ok1 || !ok2 || a != b {
		t.Errorf("shape.Of(long) = %+v (%v), shape.Of(short) = %+v (%v)", a, ok1, b, ok2)
	}
}
