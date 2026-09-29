package cards

import "testing"

func TestCanonicalKeywordLineCantAttackOrBlock(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"CARDNAME can't attack or block.", "CantAttackOrBlock"},
		{"cardname CAN'T attack or block.", "CantAttackOrBlock"},
		{"CantAttackOrBlock", "CantAttackOrBlock"},
	} {
		if got := CanonicalKeywordLine(tc.in); got != tc.want {
			t.Errorf("CanonicalKeywordLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
