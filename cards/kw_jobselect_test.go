// kw:Job select (CR 702.182) expands to exactly the For Mirrodin!/Living
// Weapon shape -- an enters-the-battlefield trigger that mints a token,
// remembers it and chains Attach -- with the Hero token script. This unit
// test pins the expansion's shape from a minimal script, so the keyword
// cannot silently stop expanding even when the compiled corpus is absent.
package cards

import (
	"strings"
	"testing"
)

func TestJobSelectExpandsToHeroEtbAttach(t *testing.T) {
	f := expanded(t, "Name:C\nManaCost:1\nTypes:Creature\nPT:1/1\nK:Job select\nOracle:x\n")
	if len(f.Triggers) != 1 {
		t.Fatalf("K:Job select produced %d triggers, want 1 (the ETB token+attach trigger)", len(f.Triggers))
	}
	tr := f.Triggers[0]
	if tr.Mode != "ChangesZone" || tr.Params["Destination"] != "Battlefield" ||
		tr.Params["ValidCard"] != "Card.Self" {
		t.Fatalf("trigger = %q / dest %q / validcard %q, want a self ChangesZone ETB",
			tr.Mode, tr.Params["Destination"], tr.Params["ValidCard"])
	}
	if tr.Params["Keyword"] != "Job select" || tr.Params["KeywordLine"] != "Job select" {
		t.Fatalf("trigger keyword tags = %q/%q, want Job select", tr.Params["Keyword"], tr.Params["KeywordLine"])
	}
	// The body mints the Hero token and chains the attach SVar.
	body := f.SVars[tr.Params["Execute"]]
	if !strings.Contains(body, "TokenScript$ c_1_1_hero") {
		t.Fatalf("trigger body = %q, want the c_1_1_hero token script", body)
	}
	if !strings.Contains(body, "RememberTokens$ True") || !strings.Contains(body, "SubAbility$ __kwJSAttach") {
		t.Fatalf("trigger body = %q, want RememberTokens$ True and the chained __kwJSAttach", body)
	}
	if got := f.SVars["__kwJSAttach"]; got != "DB$ Attach | Defined$ Remembered | Object$ Self" {
		t.Fatalf("__kwJSAttach = %q, want the remembered-token attach", got)
	}

	// Idempotence: a second expansion never re-adds the line.
	n := len(f.Triggers)
	f.expandKeywords()
	if len(f.Triggers) != n {
		t.Fatal("a second expansion re-expanded the Job select line")
	}
}
