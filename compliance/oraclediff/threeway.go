package oraclediff

// The three-way adjudication core of the Forge oracle (design
// .ds4/forge-oracle/DESIGN.md section 8.1, plan P3-1). Everything here is
// pure: the caller supplies the three pairwise comparisons and the two
// states the comparator cannot report, and gets back one of the section 8.1
// patterns with the evidence that closes it. Nothing here runs an engine,
// reads a cache or writes a file; cmd/oraclediff/adjudicate joins the rows
// and calls this.

import "github.com/adams-shaun/gorge/rules"

// Pattern is one of the three-way patterns of DESIGN section 8.1. The
// values are the section's own labels, so a ledger row carries them
// verbatim.
type Pattern string

const (
	// Agree3: all three engines agree ("AGREE3").
	Agree3 Pattern = "AGREE3"
	// GorgeForgeX: gorge = Forge, both differ from XMage ("GF|X"): an XMage
	// engine bug, an XMage driver artefact, or a shared script bug.
	GorgeForgeX Pattern = "GF|X"
	// ForgeXMageG: Forge = XMage, both differ from gorge ("FX|G"): a gorge
	// engine bug, and the fast path for a gorge fix ticket.
	ForgeXMageG Pattern = "FX|G"
	// GorgeXMageF: gorge = XMage, both differ from Forge ("GX|F"): a
	// Forge-only bug or a Forge driver artefact. Recorded only.
	GorgeXMageF Pattern = "GX|F"
	// AllDiffer: no two engines agree ("G|F|X"): no engine is a reference.
	AllDiffer Pattern = "G|F|X"
	// GorgeForgeNoX: XMage harness or lacks; gorge = Forge ("GF|–"). A
	// script-faithful triage hint only; it cannot count toward compliance.
	GorgeForgeNoX Pattern = "GF|–"
	// GorgeNoForgeNoX: XMage harness or lacks; gorge ≠ Forge ("G|F|–").
	// Gorge diverges from the script's reference interpreter.
	GorgeNoForgeNoX Pattern = "G|F|–"
	// ForgeHarness: the Forge driver could not express the scenario ("F
	// harness"). A fork driver ticket, never a verdict on the card.
	ForgeHarness Pattern = "F harness"
	// Inconsistent is not a section 8.1 pattern: the inputs match no row of
	// it (a harness on gorge's own side, or a pairwise combination the
	// transitivity premise says cannot occur). A caller must not treat it
	// as a classification.
	Inconsistent Pattern = "INCONSISTENT"
)

// ThreeWayVerdict is one scenario's section 8.1 classification: the pattern
// and the section's "Evidence that closes it" text.
type ThreeWayVerdict struct {
	Pattern  Pattern
	Evidence string
}

// ThreeWay classifies one scenario's three pairwise comparisons.
//
// gx, gf and fx are the CompareOpts verdicts for gorge/XMage, gorge/Forge
// and Forge/XMage. fx is the zero Verdict when the F-vs-X leg was not
// compared: the comparison needs both engines' snapshots, so it is skipped
// when XMage is harness or lacks, or when the Forge row harnessed.
//
// forgeHarness reports that the Forge row itself is a driver gap
// (XResult.Harness != ""). It is a parameter rather than a read of
// gf.Engine because forge-diff relabels a Forge harness verdict's engine
// from "xmage" to "forge" (cmd/oraclediff/forge.go), and the classification
// must not depend on that relabel. xmageLacks reports XMage's card-database
// miss (XMageLacksCard): CompareOpts reports it as HARNESS, and only the
// caller, which knows the scenario card, can tell the two apart.
//
// A harness on gorge's own side matches no section 8.1 pattern and returns
// Inconsistent; the caller should report the gorge failure instead.
func ThreeWay(gx, gf, fx Verdict, forgeHarness, xmageLacks bool) ThreeWayVerdict {
	// A Forge harness is a driver gap whatever the other pairs say
	// (section 8.1 "F harness": V(G,X) any, V(G,F) H, V(F,X) –). The x side
	// of the G/F leg is Forge, so a harness verdict there with an engine
	// other than gorge is Forge's too; the explicit flag covers a caller
	// that did not compute gf at all.
	if forgeHarness || (gf.Status == Harness && gf.Engine != "gorge") {
		return threeWay(ForgeHarness)
	}
	// Gorge could not run the scenario: neither comparison says anything
	// about an engine, and section 8.1 has no pattern for it. CompareOpts
	// reports it with engine "gorge" on the G/X leg; the same run failed on
	// the G/F leg, so a harness there is gorge's too.
	if gf.Status == Harness || (gx.Status == Harness && gx.Engine == "gorge") {
		return threeWay(Inconsistent)
	}
	// XMage harness or lacks: only gorge vs Forge remains (section 8.1
	// "GF|–" / "G|F|–"; V(F,X) is not computed).
	if xmageLacks || gx.Status == XMageLacks || gx.Status == Harness {
		switch gf.Status {
		case Agree:
			return threeWay(GorgeForgeNoX)
		case Diverge:
			return threeWay(GorgeNoForgeNoX)
		}
		return threeWay(Inconsistent)
	}
	// All three legs compared: the five-pattern table. Equality of the
	// normalized fields is transitive, so only these five combinations can
	// occur.
	switch {
	case gx.Status == Agree && gf.Status == Agree && fx.Status == Agree:
		return threeWay(Agree3)
	case gx.Status == Diverge && gf.Status == Agree && fx.Status == Diverge:
		return threeWay(GorgeForgeX)
	case gx.Status == Diverge && gf.Status == Diverge && fx.Status == Agree:
		return threeWay(ForgeXMageG)
	case gx.Status == Agree && gf.Status == Diverge && fx.Status == Diverge:
		return threeWay(GorgeXMageF)
	case gx.Status == Diverge && gf.Status == Diverge && fx.Status == Diverge:
		return threeWay(AllDiffer)
	}
	return threeWay(Inconsistent)
}

// threeWay pairs a pattern with its evidence text.
func threeWay(p Pattern) ThreeWayVerdict {
	return ThreeWayVerdict{Pattern: p, Evidence: p.Evidence()}
}

// Evidence is the DESIGN section 8.1 "Evidence that closes it" text for the
// pattern.
func (p Pattern) Evidence() string {
	switch p {
	case Agree3:
		return "none"
	case GorgeForgeX:
		return "Read the XMage diff for a driver cause first: if found, it is (b). Otherwise use Oracle text plus the CR: if they support gorge and Forge, it is (a); if they support XMage, it is (c)"
	case ForgeXMageG:
		return "A one-line Oracle-text sanity check, plus the agreement. When the fix lands, a hand oracle scenario under rules/testdata/oracle/ pins it independently of both engines"
	case GorgeXMageF:
		return "Optional. A driver artefact gets a fork ticket. A Forge engine bug gets an upstream Forge issue at low priority"
	case AllDiffer:
		return "1. Rule out a harness cause on each side: strict_miss, leftover, decision-shape divergence. 2. A hand oracle scenario written from the Oracle text and the CR, using the W-series two-role authoring (2026-09-27-oracle-text-card-audit.md), is the arbiter"
	case GorgeForgeNoX:
		return "It cannot count toward compliance (§3). An xmage_lacks card still needs its hand Oracle scenario (XMage design §8, lines 198-201)"
	case GorgeNoForgeNoX:
		return "Oracle text plus the CR. Most often a gorge bug, sometimes a Forge bug"
	case ForgeHarness:
		return "none"
	case Inconsistent:
		return "The three pairwise verdicts match no section 8.1 row: re-check the pair whose row is stale or harnessed before classifying."
	}
	return ""
}

// ForgeOracleResult converts a Forge row's snapshots into the OracleResult
// that CompareOpts takes as its left-hand side, so the unchanged comparator
// can diff Forge against XMage (DESIGN section 7.1). It carries no Fails and
// no Decisions: a Forge harness row has no F-vs-X leg (ThreeWay reports
// ForgeHarness instead), and a caller passes the result with a nil error.
func ForgeOracleResult(x XResult) rules.OracleResult {
	return rules.OracleResult{Snapshots: x.Snapshots}
}
