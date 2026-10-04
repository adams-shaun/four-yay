package rules

import (
	"strconv"
	"testing"
)

// TestParseInt10MatchesParseInt pins parseInt10 to strconv.ParseInt(s, 10,
// 64)'s accept/refuse verdict and value.
func TestParseInt10MatchesParseInt(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "0", "7", "-3", "+12", "+", "-", "1_000", "0x10", " 1", "1 ", "X",
		"Count$xPaid", "9223372036854775807", "9223372036854775808", "-9223372036854775808", "00012", "1e3", "٣"} {
		v, err := strconv.ParseInt(s, 10, 64)
		got, ok := parseInt10(s)
		if ok != (err == nil) || (ok && got != v) {
			t.Errorf("parseInt10(%q) = %d, %v; ParseInt = %d, %v", s, got, ok, v, err)
		}
	}
}
