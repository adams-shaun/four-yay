package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// stolenUniformScenario casts Stolen Uniform (a Pump root whose
// RememberObjects$ ThisTargetedCard hands the chosen creature to the chain's
// Defined$ Remembered Attach), then passes into the opponent's turn so the
// end-of-turn control reversion and the delayed unattach both run.
const stolenUniformScenario = `{
  "name": "stolen-uniform", "cr": ["608.2c", "701.3a"], "why": "Pump RememberObjects$",
  "setup": {
    "p0": {"hand": ["Stolen Uniform"], "battlefield": ["Grizzly Bears"]},
    "p1": {"battlefield": ["Accorder's Shield", "Grizzly Bears"]}
  },
  "steps": [
    {"op": "cast", "seat": 0, "card": "p0:Stolen Uniform", "mana": "U",
     "targets": ["p0:Grizzly Bears", "p1:Accorder's Shield"]},
    {"op": "resolve"},
    {"op": "pass_to", "step": "main1", "active": "p1"}
  ],
  "expect": []
}`

// TestStolenUniformAttachesTheStolenEquipment pins Pump's RememberObjects$:
// the root Pump remembers its own target (ThisTargetedCard), so the chain's
// Attach (Defined$ Remembered) puts the gained Equipment on that creature.
// When control reverts at end of turn, the delayed ChangesController trigger
// unattaches it.
func TestStolenUniformAttachesTheStolenEquipment(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(stolenUniformScenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	if got := len(res.Snapshots); got != 4 {
		t.Fatalf("%d snapshots, want 4 (setup + 3 steps)", got)
	}
	setup, resolved, later := res.Snapshots[0], res.Snapshots[2], res.Snapshots[3]

	// Preconditions: the Shield starts on p1's side, unattached; p0's
	// Bears are a plain 2/2.
	shield, ok := snapPerm(setup, "p1:Accorder's Shield")
	if !ok || shield.Controller != 1 || shield.AttachedTo != "" {
		t.Fatalf("precondition: setup shield = %+v, %v", shield, ok)
	}
	bears, ok := snapPerm(setup, "p0:Grizzly Bears")
	if !ok || bears.PT != "2/2" || bears.Controller != 0 {
		t.Fatalf("precondition: setup p0 bears = %+v, %v", bears, ok)
	}

	shield, ok = snapPerm(resolved, "p1:Accorder's Shield")
	if !ok || shield.Controller != 0 {
		t.Fatalf("after resolve: shield = %+v, %v; want controlled by p0", shield, ok)
	}
	if shield.AttachedTo != "p0:Grizzly Bears" {
		t.Fatalf("after resolve: shield attached_to = %q, want p0:Grizzly Bears\n%s",
			shield.AttachedTo, strings.Join(res.Transcript, "\n"))
	}
	if bears, _ = snapPerm(resolved, "p0:Grizzly Bears"); bears.PT != "2/5" {
		t.Fatalf("after resolve: p0 bears pt = %q, want 2/5 (Accorder's Shield +0/+3)", bears.PT)
	}

	// The control effect ends at cleanup; the delayed trigger then
	// unattaches the Equipment from p0's creature.
	shield, ok = snapPerm(later, "p1:Accorder's Shield")
	if !ok || shield.Controller != 1 {
		t.Fatalf("next turn: shield = %+v, %v; want controlled by p1 again", shield, ok)
	}
	if shield.AttachedTo != "" {
		t.Fatalf("next turn: shield still attached to %q; the delayed trigger should unattach it\n%s",
			shield.AttachedTo, strings.Join(res.Transcript, "\n"))
	}
	if bears, _ = snapPerm(later, "p0:Grizzly Bears"); bears.PT != "2/2" {
		t.Fatalf("next turn: p0 bears pt = %q, want 2/2", bears.PT)
	}
}
