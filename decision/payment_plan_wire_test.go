package decision

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPaymentPlanAuditOverlongQuantityVectorRejected is the wire-boundary
// contract for a mana vector: encoding/json discards surplus elements when it
// decodes a JSON array into a Go array, so without ManaAmount.UnmarshalJSON a
// 7-element vector decodes clean and silently drops a quantity.
func TestPaymentPlanAuditOverlongQuantityVectorRejected(t *testing.T) {
	raw := `{"seq":1,"player":0,"choices":[],"payment":{"action_id":"a","plan":{"version":1,"id":"x","cost":{"generic":0,"mana":[0,1,0,0,0,0,7]},"activations":[],"pool_spend":[0,0,0,0,0,0],"pool_after":[0,0,0,0,0,0]}}}`
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var in Intent
	if err := dec.Decode(&in); err == nil {
		t.Fatalf("7-element mana vector decoded without error (cost=%v)", in.Payment.Plan.Cost.Mana)
	}
}

// TestManaAmountRequiresExactlySixNonNegativeIntegers pins the whole decode
// rule so a sibling field cannot regress it: exactly six is accepted, and the
// 5- and 7-element and off-contract token shapes (quoted number, negative,
// fraction, object) are all rejected.
func TestManaAmountRequiresExactlySixNonNegativeIntegers(t *testing.T) {
	// Precondition: the pristine zero value, so a silent partial write is
	// distinguishable from the accepted vector below.
	var m ManaAmount
	if m != (ManaAmount{}) {
		t.Fatalf("precondition: zero value = %v, want the zero vector", m)
	}

	if err := json.Unmarshal([]byte(`[1,2,3,4,5,6]`), &m); err != nil {
		t.Fatalf("exactly six quantities rejected: %v", err)
	}
	if got, want := m, (ManaAmount{1, 2, 3, 4, 5, 6}); got != want {
		t.Fatalf("decoded %v, want %v", got, want)
	}

	// A sorted slice, not a map: nothing here reaches an event, but a
	// deterministic report order costs nothing and keeps the repo's rule.
	cases := []struct {
		name string
		in   string
	}{
		// only "five" and "seven" are new behavior -- main's stdlib array
		// decode accepted five (padded with zero) and seven (truncated); the
		// rest are pinned so a rewrite cannot widen the token contract.
		{"five", `[1,2,3,4,5]`},
		{"seven", `[1,2,3,4,5,6,7]`},
		// encoding/json would decode a JSON string into json.Number; a quoted
		// quantity is not an integer token and must be rejected.
		{"quoted", `["1",2,3,4,5,6]`},
		{"negative", `[1,2,3,4,5,-1]`},
		{"fraction", `[1,2,3,4,5,1.5]`},
		{"overflow", `[1,2,3,4,5,2147483648]`},
		{"object", `{"0":1,"1":2,"2":3,"3":4,"4":5,"5":6}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Precondition: the value under test starts at the zero vector, so
			// an accept-and-write cannot hide behind a stale non-zero result.
			var got ManaAmount
			if err := json.Unmarshal([]byte(tc.in), &got); err == nil {
				t.Fatalf("%s vector decoded without error: %v", tc.name, got)
			}
		})
	}
}

// TestManaAmountNullIsANoOp pins the one shape the decode rule deliberately
// leaves alone: JSON null (whole vector or one element) leaves the destination
// unchanged, which is exactly main's standard-library array-decode semantics.
func TestManaAmountNullIsANoOp(t *testing.T) {
	pre := ManaAmount{9, 9, 9, 9, 9, 9}
	for _, tc := range []struct {
		name string
		in   string
		want ManaAmount
	}{
		{"null", `null`, pre},
		{"element null", `[null,8,null,null,null,null]`, ManaAmount{9, 8, 9, 9, 9, 9}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := pre
			if err := json.Unmarshal([]byte(tc.in), &m); err != nil {
				t.Fatalf("%s rejected: %v", tc.name, err)
			}
			if m != tc.want {
				t.Fatalf("%s = %v, want %v", tc.name, m, tc.want)
			}
		})
	}
}

// TestPaymentPlanManaVectorRoundTrips guards the wire encoding the engine
// emits: a marshalled vector is the same six-element JSON array it always was
// and re-decodes to the identical value.
func TestPaymentPlanManaVectorRoundTrips(t *testing.T) {
	want := ManaAmount{0, 1, 0, 0, 0, 0}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if got, w := string(b), "[0,1,0,0,0,0]"; got != w {
		t.Fatalf("marshalled %s, want %s", got, w)
	}
	var got ManaAmount
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("round-trip %v, want %v", got, want)
	}
}
