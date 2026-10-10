package shape

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
)

// TestMustAttackPhaseEntryRuling pins the shape ruling for the four level-B
// static#0.0 MustAttack rows (Ares, God of War; Flamewake Phoenix;
// Juggernaut; Red Herring): XMage's engine force-declares and taps an
// Attacks-each-combat creature when it enters DECLARE_ATTACKERS
// (checkAttackRequirements before selectAttackers), while gorge leaves the
// declaration to the active player (CR 508.1d), so the two snapshots cannot
// agree at the pending decision. The ruling freezes gorge's side.
func TestMustAttackPhaseEntryRuling(t *testing.T) {
	const rulingID = "must-attack-force-declared-at-phase-entry"
	const wantKey = "static | pass_to | permanents | $CARD gorge+[] xmage+[attacking, tapped]"

	rs, err := LoadRulings(filepath.Join("..", "..", RulingDir))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rs {
		if r.ID == rulingID {
			found = true
		}
	}
	if !found {
		t.Fatalf("ruling %s not loaded from %s", rulingID, RulingDir)
	}

	cases := []struct {
		card       string
		gorge      string
		xmage      string
		wantReview string
	}{
		{"Ares, God of War",
			"c0 o0 Ares, God of War [creature god legendary villain warrior] {BR} 4/3",
			"c0 o0 Ares, God of War [creature god legendary villain warrior] {BR} 4/3 tapped attacking",
			""},
		{"Flamewake Phoenix",
			"c0 o0 Flamewake Phoenix [creature phoenix] {R} 2/2",
			"c0 o0 Flamewake Phoenix [creature phoenix] {R} 2/2 tapped attacking",
			""},
		{"Juggernaut",
			"c0 o0 Juggernaut [artifact creature juggernaut] {} 5/3",
			"c0 o0 Juggernaut [artifact creature juggernaut] {} 5/3 tapped attacking",
			ReviewPending},
		{"Red Herring",
			"c0 o0 Red Herring [artifact clue creature fish] {R} 2/2",
			"c0 o0 Red Herring [artifact clue creature fish] {R} 2/2 tapped attacking",
			ReviewPending},
	}
	for _, c := range cases {
		row := compliance.VerdictRow{
			Card:     c.card,
			Template: "static#0.0",
			Status:   compliance.StatusDiverge,
			Detail:   fmt.Sprintf("step 0 (pass_to) permanents: gorge %q, xmage %q", c.gorge, c.xmage),
		}
		s, ok := Of(row)
		if !ok {
			t.Fatalf("%s: no shape from detail %q", c.card, row.Detail)
		}
		if s.Key() != wantKey {
			t.Fatalf("%s: shape %q, want %q", c.card, s.Key(), wantKey)
		}
		m, overlap, changed := Classify(&row, rs, "-")
		if m == nil || !changed {
			t.Fatalf("%s: ruling did not apply: %+v", c.card, row)
		}
		if len(overlap) != 0 {
			t.Fatalf("%s: overlapping rulings %v", c.card, overlap)
		}
		if row.Status != compliance.StatusXMageWrong || row.RulingID != rulingID {
			t.Fatalf("%s: status %q ruling %q, want xmage_wrong/%s", c.card, row.Status, row.RulingID, rulingID)
		}
		if !strings.Contains(row.Ruling, "CR 508.1d") {
			t.Errorf("%s: ruling text %q does not cite CR 508.1d", c.card, row.Ruling)
		}
		if row.Review != c.wantReview {
			t.Errorf("%s: review %q, want %q", c.card, row.Review, c.wantReview)
		}
	}
}
