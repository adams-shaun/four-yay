package decision

import "testing"

// TestAllowNoneAdmitsTheEmptyAnswerOnly: AllowNone adds the empty answer to
// the Min..Max range and nothing else -- a size between 0 and Min stays
// illegal, and without the flag the empty answer is refused as before.
func TestAllowNoneAdmitsTheEmptyAnswerOnly(t *testing.T) {
	d := &Decision{Seq: 7, Player: 1, Kind: KChoose, Min: 2, Max: 2,
		Options: []Option{{Index: 0}, {Index: 1}, {Index: 2}}}
	in := func(c ...int) Intent { return Intent{Seq: 7, Player: 1, Choices: c} }
	if err := d.Validate(in()); err == nil {
		t.Fatal("Min 2 without AllowNone accepted the empty answer")
	}
	d.AllowNone = true
	for _, tc := range []struct {
		choices []int
		ok      bool
	}{{nil, true}, {[]int{0}, false}, {[]int{0, 1}, true}, {[]int{0, 1, 2}, false}} {
		if err := d.Validate(in(tc.choices...)); (err == nil) != tc.ok {
			t.Fatalf("AllowNone answer %v: err=%v, want ok=%v", tc.choices, err, tc.ok)
		}
	}
}
