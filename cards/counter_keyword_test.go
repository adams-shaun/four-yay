package cards

import (
	"strings"
	"testing"
)

// TestCounterKeywordFirstLetterRejectIsExact pins the first-letter fast
// reject against the full fold scan: every byte as a kind's first letter
// answers exactly what the list scan answers.
func TestCounterKeywordFirstLetterRejectIsExact(t *testing.T) {
	slow := func(kind string) bool {
		head := strings.TrimSpace(kind)
		if i := strings.IndexByte(head, ':'); i >= 0 {
			head = strings.TrimSpace(head[:i])
		}
		for _, name := range counterKeywordCounters {
			if strings.EqualFold(name, head) {
				return true
			}
		}
		return false
	}
	var kinds []string
	for _, name := range counterKeywordCounters {
		kinds = append(kinds, name, strings.ToUpper(name), strings.ToLower(name), name+":Black")
	}
	for c := 1; c < 256; c++ {
		kinds = append(kinds, string(rune(c))+"lying", string([]byte{byte(c)})+"1P1")
	}
	kinds = append(kinds, "P1P1", "CHARGE", "LOYALTY", "ſhadow", "KEY")
	for _, k := range kinds {
		if _, got := CounterKeyword(k); got != slow(k) {
			t.Fatalf("CounterKeyword(%q) = %v, the list scan says %v", k, got, slow(k))
		}
	}
}
