package oraclediff

import (
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// wantEvidence pins the DESIGN section 8.1 "Evidence that closes it" text
// for every pattern, so a classifier that returns the right pattern with the
// wrong evidence fails too.
var wantEvidence = map[Pattern]string{
	Agree3:          "none",
	GorgeForgeX:     "Read the XMage diff for a driver cause first: if found, it is (b). Otherwise use Oracle text plus the CR: if they support gorge and Forge, it is (a); if they support XMage, it is (c)",
	ForgeXMageG:     "A one-line Oracle-text sanity check, plus the agreement. When the fix lands, a hand oracle scenario under rules/testdata/oracle/ pins it independently of both engines",
	GorgeXMageF:     "Optional. A driver artefact gets a fork ticket. A Forge engine bug gets an upstream Forge issue at low priority",
	AllDiffer:       "1. Rule out a harness cause on each side: strict_miss, leftover, decision-shape divergence. 2. A hand oracle scenario written from the Oracle text and the CR, using the W-series two-role authoring (2026-09-27-oracle-text-card-audit.md), is the arbiter",
	GorgeForgeNoX:   "It cannot count toward compliance (§3). An xmage_lacks card still needs its hand Oracle scenario (XMage design §8, lines 198-201)",
	GorgeNoForgeNoX: "Oracle text plus the CR. Most often a gorge bug, sometimes a Forge bug",
	ForgeHarness:    "none",
	Inconsistent:    "The three pairwise verdicts match no section 8.1 row: re-check the pair whose row is stale or harnessed before classifying.",
}

// allPatterns is every Pattern constant. Its length is held to wantEvidence
// so a new pattern cannot land without its section 8.1 text.
var allPatterns = []Pattern{
	Agree3, GorgeForgeX, ForgeXMageG, GorgeXMageF, AllDiffer,
	GorgeForgeNoX, GorgeNoForgeNoX, ForgeHarness, Inconsistent,
}

// TestThreeWay covers every row of DESIGN section 8.1, the harness and
// xmage_lacks states its H/L cells stand for, and the Inconsistent guard.
func TestThreeWay(t *testing.T) {
	tests := []struct {
		name         string
		gx, gf, fx   Verdict
		forgeHarness bool
		xmageLacks   bool
		want         Pattern
	}{
		{
			name: "AGREE3 all three agree",
			gx:   Verdict{Status: Agree}, gf: Verdict{Status: Agree}, fx: Verdict{Status: Agree},
			want: Agree3,
		},
		{
			name: "GF|X gorge = Forge != XMage",
			gx:   Verdict{Status: Diverge, Checkpoint: "step 1 (resolve)", Field: "p0.life"},
			gf:   Verdict{Status: Agree},
			fx:   Verdict{Status: Diverge, Field: "p0.life"},
			want: GorgeForgeX,
		},
		{
			name: "FX|G Forge = XMage != gorge",
			gx:   Verdict{Status: Diverge, Field: "p0.graveyard"},
			gf:   Verdict{Status: Diverge, Field: "p0.hand"},
			fx:   Verdict{Status: Agree},
			want: ForgeXMageG,
		},
		{
			name: "GX|F gorge = XMage != Forge",
			gx:   Verdict{Status: Agree},
			gf:   Verdict{Status: Diverge, Field: "permanents"},
			fx:   Verdict{Status: Diverge, Field: "permanents"},
			want: GorgeXMageF,
		},
		{
			name: "G|F|X all three differ",
			gx:   Verdict{Status: Diverge, Field: "p0.hand"},
			gf:   Verdict{Status: Diverge, Field: "p0.graveyard"},
			fx:   Verdict{Status: Diverge, Field: "p0.life"},
			want: AllDiffer,
		},
		{
			name: "GF|– XMage harness, gorge = Forge",
			gx:   Verdict{Status: Harness, Engine: "xmage", Msg: "strict_miss"},
			gf:   Verdict{Status: Agree},
			want: GorgeForgeNoX,
		},
		{
			name: "G|F|– XMage harness, gorge != Forge",
			gx:   Verdict{Status: Harness, Engine: "xmage", Msg: "strict_miss"},
			gf:   Verdict{Status: Diverge, Field: "p0.life"},
			want: GorgeNoForgeNoX,
		},
		{
			name: "GF|– XMage lacks status, gorge = Forge",
			gx:   Verdict{Status: XMageLacks, Engine: "xmage", Msg: "Couldn't find a card: X"},
			gf:   Verdict{Status: Agree},
			want: GorgeForgeNoX,
		},
		{
			name:       "G|F|– xmage_lacks flag, gorge != Forge",
			gx:         Verdict{Status: Harness, Engine: "xmage", Msg: "Couldn't find a card: X"},
			gf:         Verdict{Status: Diverge, Field: "p0.life"},
			xmageLacks: true,
			want:       GorgeNoForgeNoX,
		},
		{
			name:         "F harness, V(G,X) agree",
			gx:           Verdict{Status: Agree},
			gf:           Verdict{Status: Harness, Engine: "forge", Msg: "no Forge result"},
			forgeHarness: true,
			want:         ForgeHarness,
		},
		{
			name:         "F harness, V(G,X) diverge",
			gx:           Verdict{Status: Diverge, Field: "p0.life"},
			gf:           Verdict{Status: Harness, Engine: "forge", Msg: "unsupported op"},
			forgeHarness: true,
			want:         ForgeHarness,
		},
		{
			name:         "F harness, XMage harness too",
			gx:           Verdict{Status: Harness, Engine: "xmage"},
			gf:           Verdict{Status: Harness, Engine: "forge"},
			forgeHarness: true,
			want:         ForgeHarness,
		},
		{
			name: "F harness from a snapshot-count mismatch",
			gx:   Verdict{Status: Agree},
			gf:   Verdict{Status: Harness, Engine: "both", Msg: "3 gorge checkpoints, 2 xmage"},
			want: ForgeHarness,
		},
		{
			name: "F harness from an unrelabeled Forge row",
			gx:   Verdict{Status: Diverge, Field: "p0.life"},
			gf:   Verdict{Status: Harness, Engine: "xmage", Msg: "unsupported op"},
			want: ForgeHarness,
		},
		{
			name:         "F harness wins over xmage_lacks",
			gx:           Verdict{Status: XMageLacks, Engine: "xmage"},
			gf:           Verdict{Status: Harness, Engine: "forge"},
			forgeHarness: true, xmageLacks: true,
			want: ForgeHarness,
		},
		{
			name: "inconsistent A/A/D",
			gx:   Verdict{Status: Agree}, gf: Verdict{Status: Agree}, fx: Verdict{Status: Diverge},
			want: Inconsistent,
		},
		{
			name: "inconsistent A/D/A",
			gx:   Verdict{Status: Agree}, gf: Verdict{Status: Diverge}, fx: Verdict{Status: Agree},
			want: Inconsistent,
		},
		{
			name: "inconsistent D/A/A",
			gx:   Verdict{Status: Diverge}, gf: Verdict{Status: Agree}, fx: Verdict{Status: Agree},
			want: Inconsistent,
		},
		{
			name: "gorge harness on both legs",
			gx:   Verdict{Status: Harness, Engine: "gorge", Msg: "harness: panic"},
			gf:   Verdict{Status: Harness, Engine: "gorge", Msg: "harness: panic"},
			want: Inconsistent,
		},
		{
			name: "F-vs-X checkpoint harness",
			gx:   Verdict{Status: Diverge, Field: "p0.life"},
			gf:   Verdict{Status: Diverge, Field: "p0.life"},
			fx:   Verdict{Status: Harness, Engine: "both", Msg: `checkpoint "a" vs "b"`},
			want: Inconsistent,
		},
		{
			name: "XMage harness with a gorge harness on G/F",
			gx:   Verdict{Status: Harness, Engine: "xmage"},
			gf:   Verdict{Status: Harness, Engine: "gorge", Msg: "harness: panic"},
			want: Inconsistent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Preconditions: the row must carry the state its expected
			// pattern is defined on, so a case cannot pass for the wrong
			// reason (a missing F-vs-X leg, a non-XMage harness, or a
			// Forge harness the caller never reported).
			switch tt.want {
			case Agree3, GorgeForgeX, ForgeXMageG, GorgeXMageF, AllDiffer:
				if tt.fx.Status != Agree && tt.fx.Status != Diverge {
					t.Fatalf("precondition: %s needs a compared F-vs-X leg, fx = %+v", tt.name, tt.fx)
				}
			case GorgeForgeNoX, GorgeNoForgeNoX:
				if tt.fx != (Verdict{}) {
					t.Fatalf("precondition: %s must not carry an F-vs-X verdict, fx = %+v", tt.name, tt.fx)
				}
				if !(tt.xmageLacks || tt.gx.Status == XMageLacks || tt.gx.Status == Harness) {
					t.Fatalf("precondition: %s needs XMage harness or lacks, gx = %+v", tt.name, tt.gx)
				}
			case ForgeHarness:
				if !(tt.forgeHarness || (tt.gf.Status == Harness && tt.gf.Engine != "gorge")) {
					t.Fatalf("precondition: %s needs a reported or derivable Forge harness, gf = %+v", tt.name, tt.gf)
				}
			}

			got := ThreeWay(tt.gx, tt.gf, tt.fx, tt.forgeHarness, tt.xmageLacks)
			if got.Pattern != tt.want {
				t.Errorf("ThreeWay(gx=%+v, gf=%+v, fx=%+v, forgeHarness=%v, xmageLacks=%v) = %q, want %q",
					tt.gx, tt.gf, tt.fx, tt.forgeHarness, tt.xmageLacks, got.Pattern, tt.want)
			}
			if want := wantEvidence[tt.want]; got.Evidence != want {
				t.Errorf("%s evidence = %q, want %q", tt.want, got.Evidence, want)
			}
		})
	}
}

// TestThreeWayEvidence holds every Pattern constant to its section 8.1
// evidence text and to the map above, so a new pattern without a text fails
// the build.
func TestThreeWayEvidence(t *testing.T) {
	if len(allPatterns) != len(wantEvidence) {
		t.Fatalf("%d patterns, %d evidence texts: a pattern constant is missing its text", len(allPatterns), len(wantEvidence))
	}
	for _, p := range allPatterns {
		want, ok := wantEvidence[p]
		if !ok {
			t.Errorf("pattern %q has no pinned evidence text", p)
			continue
		}
		if got := p.Evidence(); got != want {
			t.Errorf("%s.Evidence() = %q, want %q", p, got, want)
		}
	}
}

// TestThreeWayForgeOracleResult pins the F-vs-X conversion: the Forge row's
// snapshots become the left-hand OracleResult with no Fails or Decisions,
// and the unchanged comparator then diffs Forge against XMage.
func TestThreeWayForgeOracleResult(t *testing.T) {
	forgeSnap := rules.OracleSnapshot{
		Checkpoint: "step 1 (resolve)",
		Turn:       1,
		Step:       "main1",
		Players:    []rules.OracleSnapPlayer{{Seat: 0, Life: 20}},
	}
	xmageSnap := rules.OracleSnapshot{
		Checkpoint: "step 1 (resolve)",
		Turn:       1,
		Step:       "main1",
		Players:    []rules.OracleSnapPlayer{{Seat: 0, Life: 20}},
	}

	got := ForgeOracleResult(XResult{Name: "Shock", Snapshots: []rules.OracleSnapshot{forgeSnap}})
	// Precondition: the conversion kept the snapshot, so the comparator
	// assertions below are about a real F-vs-X leg, not an empty result.
	if len(got.Snapshots) != 1 {
		t.Fatalf("converted snapshots = %d, want 1", len(got.Snapshots))
	}
	if len(got.Fails) != 0 || len(got.Decisions) != 0 {
		t.Errorf("converted result carries Fails %v / Decisions %v, want neither", got.Fails, got.Decisions)
	}
	if v := CompareOpts(got, nil, XResult{Snapshots: []rules.OracleSnapshot{xmageSnap}}, nil); v.Status != Agree {
		t.Errorf("Forge vs XMage on equal snapshots = %+v, want AGREE", v)
	}

	xmageSnap.Players[0].Life = 19
	if v := CompareOpts(got, nil, XResult{Snapshots: []rules.OracleSnapshot{xmageSnap}}, nil); v.Status != Diverge || v.Field != "p0.life" {
		t.Errorf("Forge vs XMage on a life difference = %+v, want DIVERGE at p0.life", v)
	}
}
