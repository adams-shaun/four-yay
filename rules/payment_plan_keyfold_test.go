package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/rules/pay"
)

func TestContainsTargetFoldMatchesToLower(t *testing.T) {
	t.Parallel()
	for _, k := range []string{"", "Target", "ValidTgts", "TgtPrompt", "TargetType", "xTARGETx", "targe", "TargetMin", "AITgts", "İtarget", "Tärget", "ChangeTargets", "Defined"} {
		if got, want := pay.ContainsTargetFold(k), strings.Contains(strings.ToLower(k), "target"); got != want {
			t.Errorf("containsTargetFold(%q) = %v, want %v", k, got, want)
		}
	}
}
