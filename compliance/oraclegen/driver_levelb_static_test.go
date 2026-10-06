package oraclegen_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// snapshotBody returns the text of ScenarioReplay.snapshot, so the assertions
// below cannot be satisfied by a keywords mention elsewhere in the file.
func snapshotBody(t *testing.T, java string) string {
	t.Helper()
	i := strings.Index(java, "private JsonObject snapshot(String checkpoint, Game g) {")
	if i < 0 {
		t.Fatal("ScenarioReplay.java has no snapshot method")
	}
	rest := java[i:]
	j := strings.Index(rest, "\n    }\n")
	if j < 0 {
		t.Fatal("snapshot method has no closing brace")
	}
	return rest[:j]
}

// TestDriverSnapshotEmitsPermanentKeywords pins L12: the XMage snapshot adds a
// sorted, de-duplicated `keywords` array to every permanent, built from the
// permanent's ability names (spec 2.3, H5), so L10's opt-in field is not
// empty on the XMage side.
func TestDriverSnapshotEmitsPermanentKeywords(t *testing.T) {
	b, err := os.ReadFile(scenarioReplayPath)
	if err != nil {
		t.Fatal(err)
	}
	java := string(b)
	snap := snapshotBody(t, java)

	// The property is added inside the per-permanent loop, not on the
	// snapshot or a player.
	loop := strings.Index(snap, "for (Permanent perm : g.getBattlefield().getAllPermanents())")
	kw := strings.Index(snap, `o.add("keywords"`)
	end := strings.Index(snap, `s.add("permanents", perms)`)
	if loop < 0 || end < 0 || kw < 0 || kw < loop || kw > end {
		t.Fatalf("snapshot does not add a keywords property per permanent (loop %d, keywords %d, end %d)", loop, kw, end)
	}

	helper := regexp.MustCompile(`(?s)List<String> keywordNames\(Permanent perm, Game g\) \{.*?\n    \}\n`).FindString(java)
	if helper == "" {
		t.Fatal("ScenarioReplay.java has no keywordNames helper")
	}
	for what, needle := range map[string]string{
		"reads the game-state abilities (so granted keywords show)": "perm.getAbilities(g)",
		"takes the ability's rule name":                             "a.getRule()",
		"restricts to keyword ability classes":                      `"mage.abilities.keyword."`,
		"sorts and de-duplicates":                                   "java.util.TreeSet<String>",
		"strips HTML tags (menace's <i>(reminder)</i> hint)":        `replaceAll("<[^>]*>", "")`,
		"cuts reminder text at the first paren":                     `rule.indexOf("(")`,
	} {
		if !strings.Contains(helper, needle) {
			t.Errorf("keywordNames does not do this: %s (want %q)", what, needle)
		}
	}
	if !strings.Contains(snap, "keywordNames(perm, g)") {
		t.Error("snapshot does not call keywordNames for each permanent")
	}
}
