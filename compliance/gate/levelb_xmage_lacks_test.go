package gate

import (
	"fmt"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestLevelBXMageLacksDoesNotCountAgainstLevelA(t *testing.T) {
	row := compliance.VerdictRow{Template: "trigger#0.1", Status: compliance.StatusXMageLacks}
	if rowCountsAtLevel(row, "A") {
		t.Fatal("level-B xmage_lacks row counted against level A")
	}
	if !rowCountsAtLevel(compliance.VerdictRow{Template: "play-land"}, "A") {
		t.Fatal("level-A play-land row was not recognized as an A row")
	}
}

func TestLevelBXMageLacksIsReportedForItsScenario(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Dáin Ironfoot"
	card, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%q missing from corpus", name)
	}
	var itemTemplate string
	for _, req := range levelb.Requirements(card) {
		it, skip := templates.GenerateB(reg, name, req)
		if skip == nil {
			itemTemplate = it.Template
			break
		}
	}
	if itemTemplate == "" {
		t.Fatalf("%q has no generable level-B requirement", name)
	}
	var problems []string
	scan := &setScan{verdicts: map[string]map[string]compliance.VerdictRow{
		name: {itemTemplate: {Template: itemTemplate, Status: compliance.StatusXMageLacks}},
	}}
	bProblems(reg, name, card, scan, func(_ string, format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	})
	if len(problems) != 1 || !strings.Contains(problems[0], "verdict xmage_lacks") {
		t.Fatalf("level-B xmage_lacks row problems = %v, want its scenario reported", problems)
	}

	// The gate must build the same XMage-stamped subject item as the pass,
	// or a valid level-B row's ScenarioSHA will be rejected as stale.
	var expected oraclegen.Item
	for _, candidate := range levelb.Requirements(card) {
		it, skip := templates.GenerateB(reg, name, candidate)
		if skip == nil {
			expected = it
			break
		}
	}
	if expected.Template == "" {
		t.Fatal("no generated scenario available for XMage spelling check")
	}
	expected.XMageName = "Dain Ironfoot"
	spellingScan := &setScan{
		verdicts: map[string]map[string]compliance.VerdictRow{
			name: {expected.Template: {Template: expected.Template, Status: compliance.StatusAgree, ScenarioSHA: ItemSHA(expected)}},
		},
		xmageSpelling: map[string]string{compliance.FoldName(name): "Dain Ironfoot"},
	}
	problems = nil
	bProblems(reg, name, card, spellingScan, func(_ string, format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	})
	for _, problem := range problems {
		if strings.Contains(problem, "older scenario") {
			t.Fatalf("gate did not stamp XMage subject spelling: %v", problems)
		}
	}
}
