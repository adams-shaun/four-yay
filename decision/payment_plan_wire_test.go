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
// 5- and 7-element and negative shapes are all rejected.
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

	for name, in := range map[string]string{
		"five":     `[1,2,3,4,5]`,
		"seven":    `[1,2,3,4,5,6,7]`,
		"negative": `[1,2,3,4,5,-1]`,
		"fraction": `[1,2,3,4,5,1.5]`,
		"object":   `{"0":1,"1":2,"2":3,"3":4,"4":5,"5":6}`,
	} {
		t.Run(name, func(t *testing.T) {
			var got ManaAmount
			if err := json.Unmarshal([]byte(in), &got); err == nil {
				t.Fatalf("%s vector decoded without error: %v", name, got)
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
