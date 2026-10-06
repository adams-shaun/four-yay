package gate

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance"
)

func TestLevelBXMageLacksDoesNotCountAgainstLevelA(t *testing.T) {
	row := compliance.VerdictRow{Template: "trigger#0.1", Status: compliance.StatusXMageLacks}
	if rowCountsAtLevel(row, "A") {
		t.Fatal("level-B xmage_lacks row counted against level A")
	}
	if !rowCountsAtLevel(row, "B") {
		t.Fatal("level-B xmage_lacks row did not count at level B")
	}
}
