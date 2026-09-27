package rules

import (
	"strings"
	"testing"
)

func TestContainsTargetFoldMatchesToLower(t *testing.T) {
	for _, k := range []string{"", "Target", "ValidTgts", "TgtPrompt", "TargetType", "xTARGETx", "targe", "TargetMin", "AITgts", "İtarget", "Tärget", "ChangeTargets", "Defined"} {
		if got, want := containsTargetFold(k), strings.Contains(strings.ToLower(k), "target"); got != want {
			t.Errorf("containsTargetFold(%q) = %v, want %v", k, got, want)
		}
	}
}
