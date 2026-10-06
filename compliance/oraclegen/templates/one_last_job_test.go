package templates

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// TestOneLastJobKeepsItsAuraEquipmentModeUnserved pins the generator's
// deliberate skip of One Last Job's third (Aura/Equipment) Spree mode.
//
// Serving that mode is NOT deterministic across engines at this fixture: the
// mode returns an Aura attached to a creature, and XMage's OneLastJobEffect
// poses an unhinted, non-targeting TargetPermanent attach ask that consumes the
// cast's scripted mana-ability choice ("Activate Llanowar Elves for mana")
// instead of a target, failing the strict run with "Found wrong choice command
// (invalid target or miss skip command)". The failure text varies run to run
// (a random object_id appears in it), so the row is not replayable and a
// level-A card may not be declared against it. Main served the creature mode
// only, and that row replays deterministically and agrees; keep it.
//
// The scenario sha is main's recorded scenario_sha for
// "One Last Job/cast-resolve/v1"; a generator change that re-serves the
// Aura/Equipment mode produces a different scenario and fails here first.
func TestOneLastJobKeepsItsAuraEquipmentModeUnserved(t *testing.T) {
	reg := loadGenRegistry(t)
	it, skip := Generate(reg, "One Last Job")
	if skip != nil {
		t.Fatalf("One Last Job: %s", skip.Reason)
	}
	cast := castStep(t, it, "One Last Job")

	// Precondition: the fixture really does offer the spell a mode decision.
	var picked []string
	for _, answer := range cast.Answers {
		if answer.Kind == "modes" {
			picked = answer.Pick
		}
	}
	if len(picked) == 0 {
		t.Fatalf("One Last Job cast answers %v have no mode pick; the fixture changed shape", cast.Answers)
	}
	for _, mode := range picked {
		if strings.Contains(mode, "Aura or Equipment") {
			t.Fatalf("One Last Job fixture selected the Aura/Equipment mode %q; it is not deterministically replayable (see the test doc)", mode)
		}
	}

	// The scenario bytes must be exactly main's deterministically-agreeing row.
	b, err := json.Marshal(it)
	if err != nil {
		t.Fatalf("marshal One Last Job item: %v", err)
	}
	sum := sha256.Sum256(b)
	const wantSHA = "36552f0d3157e0da755686c5365ad97dab34312b129a8c846008865432e4ad70"
	if got := hex.EncodeToString(sum[:]); got != wantSHA {
		t.Fatalf("One Last Job scenario sha = %s, want %s (main's deterministic row); the generator re-served a mode", got, wantSHA)
	}
}
