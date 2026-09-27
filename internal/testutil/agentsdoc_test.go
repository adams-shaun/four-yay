package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AGENTS.md's "## Known approximations" table is a CLOSING REGISTER, frozen on
// 2026-09-22. It began as a 13-row audit of where "supported" was not the whole
// truth, and then became the place every landed ticket wrote a paragraph: by
// 2026-09-22 it was ~100 rows and 264 KB, loaded into every agent's context on
// every turn. Every remaining row now has a P1 ticket whose only job is to
// delete it.
//
// This test is the ratchet. It fails on GROWTH and never on shrinkage, so a
// ticket that closes a row only has to delete the row and lower the constant.
const (
	// knownApproximationRows is the number of data rows in the table. Lower it
	// by exactly the number of rows your change deletes. NEVER raise it.
	// After deleting the CR 103.1 toss-choice row, AGENTS.md measures 14
	// data rows (main had 15; its constant had not yet been lowered from 16).
	// Earlier merges kept four
	// disjoint closures alongside the prior deletions: this branch's rv1
	// RevealAllValid$ closure (effects/cardflow.go effReveal,
	// agent-20260922T191943Z-4ffa25b7), main's task scrybottom
	// `T:Mode$ Scry`/`R:Event$ Scry` row (whose "whenever you scry" trigger
	// half is closed there; the still-unclosed `R:Event$ Scry` replacement
	// half is recorded in the commit message and the task report rather than
	// a new row), main's four-mode trigger row
	// (cli-20260923T060000Z-trig-attackerblocked), and main's rv2b
	// damage-source / valid-players / count-heads row
	// (cli-20260923T060000Z-rv2b-countheads).
	// agent-20260918T232250Z-29aed5d6 deleted the CR 704.5j legend-rule row:
	// the controller choice landed in 74870371 and the IgnoreLegendRule
	// exemption (incl. its IsPresent$/PresentCompare$ condition gate) here.
	// agent-20260919T142536Z-0a361fe7 deleted the devthr1 `Count$`-bodies row:
	// its `CardNumAttacksThisTurn` member is now modelled (effects/count.go
	// evalCountBody reads state.Object.AttacksThisTurn); the other five bodies
	// it bundled (MaxOppDamageThisTurn, YourStartingLife, ResolvedThisTurn,
	// NonCombatDamageThisTurn, ChosenNumber) remain tracked bidirectionally by
	// rules/count_head_ratchet_test.go's knownUnmodelledCountHeads and are
	// recorded in that ticket's commit message and report, not a new row.
	// The PayLife<X> replacement closure (cli-20260922T225142Z-226d3d19)
	// deletes one further row (main measured 15 against a stale constant of 16).
	// The CR 103.1 toss-choice closure (cli-20260922T225141Z-e771720d)
	// deletes one more (toss-choice row).
	// The abcopy closure (cli-20260922T225141Z-4f6f20cb) deletes one more.
	// cli-20260922T225142Z-2f0df8e8 deletes one more row.
	// mayplay-mfa deletes the ValidLKI may-play provenance row.
	// cli-20260922T225140Z-c010b497 deletes the addcounter1/2 row.
	// planar-verbs deletes the three-planechase-verbs row: the planar deck
	// and zone now exist, api:Planeswalk rotates the deck (the PlanarWalk
	// fold), api:ChaosEnsues erupts the current plane, and T:Mode$ ChaosEnsues
	// fires on a chaos roll.
	knownApproximationRows = 8

	// standInCellLimit is the size cap, in bytes, on a row's Stand-in cell: what
	// still deviates today, plus any decision a future implementer must honour.
	// Measurements, corpus counts, "Pinned by Test..." lists, review-round
	// history and "what works" recaps belong in the commit message and the
	// ticket report.
	standInCellLimit = 600

	// knownOversizeRows is how many rows were already over standInCellLimit when
	// the table was frozen. Lower it when you delete one of them; never raise
	// it. A row grown past the cap pushes this over the constant and fails.
	// rv1 deleted the oversize RevealAllValid$ row, so this drops by one.
	knownOversizeRows = 7
)

func approximationRows(t *testing.T) []string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	var rows []string
	inside := false
	for _, line := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(line, "## Known approximations"):
			inside = true
		case inside && strings.HasPrefix(line, "## "):
			inside = false
		case inside && strings.HasPrefix(line, "| "):
			rows = append(rows, line)
		}
	}
	if len(rows) == 0 {
		t.Fatal("AGENTS.md: no `## Known approximations` table found -- did the heading move?")
	}
	return rows[1:] // drop the header row; the |---|---|---| separator never matches "| "
}

// standInCell is everything but the last two cells (Where, Removed by). Forge
// script fragments in a description carry their own "|", so splitting naively
// would put a script fragment in Where.
func standInCell(row string) string {
	cells := strings.Split(strings.Trim(strings.TrimSpace(row), "|"), "|")
	if len(cells) < 3 {
		return strings.TrimSpace(row)
	}
	return strings.TrimSpace(strings.Join(cells[:len(cells)-2], "|"))
}

func TestKnownApproximationsOnlyShrinks(t *testing.T) {
	rows := approximationRows(t)
	if len(rows) > knownApproximationRows {
		t.Errorf("AGENTS.md's Known approximations table has %d rows, above the frozen "+
			"count of %d.\n"+
			"The table is a CLOSING REGISTER: no row may be added. A deviation your "+
			"ticket cannot close goes in the COMMIT MESSAGE and the ticket report, not "+
			"here; if it needs tracking, say so in your report and the operator files a "+
			"ticket.\n"+
			"If you DELETED rows, lower knownApproximationRows in %s to %d.",
			len(rows), knownApproximationRows, "internal/testutil/agentsdoc_test.go", len(rows))
	}
	if len(rows) < knownApproximationRows {
		t.Logf("table is down to %d rows (constant says %d) -- lower knownApproximationRows "+
			"to %d in the same commit that deleted them.",
			len(rows), knownApproximationRows, len(rows))
	}
}

func TestKnownApproximationRowsAreShort(t *testing.T) {
	rows := approximationRows(t)
	var oversize []string
	for _, row := range rows {
		cell := standInCell(row)
		if len(cell) > standInCellLimit {
			head := cell
			if len(head) > 80 {
				head = head[:80]
			}
			oversize = append(oversize, head)
		}
	}
	if len(oversize) > knownOversizeRows {
		t.Errorf("%d Known-approximations rows exceed the %d-byte Stand-in cap, above the "+
			"frozen count of %d. A row may never be grown.\nOversize rows:\n  %s",
			len(oversize), standInCellLimit, knownOversizeRows, strings.Join(oversize, "\n  "))
	}
}
