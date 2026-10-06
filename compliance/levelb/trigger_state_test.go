package levelb

import "testing"

func TestSelfCounterGate(t *testing.T) {
	for _, tc := range []struct {
		spec string
		kind string
		n    int
		ok   bool
	}{
		{"Card.Self+counters_GE4_PAGE", "PAGE", 4, true},
		{"Card.Self+counters_GE9_INCARNATION", "INCARNATION", 9, true},
		{"Card.Self+counters_GEX_CHARGE+YouCtrl", "", 0, false},
		{"Card.Self+counters_LE2_LOYALTY", "", 0, false},
		{"Creature.counters_GE4_FUSE", "", 0, false},
		{"Card.Self+withFlying", "", 0, false},
		{"Island.YouCtrl", "", 0, false},
	} {
		kind, n, ok := SelfCounterGate(tc.spec)
		if kind != tc.kind || n != tc.n || ok != tc.ok {
			t.Errorf("SelfCounterGate(%q) = %q, %d, %v; want %q, %d, %v", tc.spec, kind, n, ok, tc.kind, tc.n, tc.ok)
		}
	}
}
