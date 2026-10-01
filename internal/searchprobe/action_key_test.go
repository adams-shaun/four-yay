package searchprobe

import (
	"bytes"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// TestIntentKeyIsActionsKey: IntentKey and AppendIntentActions answer what
// Actions does -- the same key bytes (AppendActionsKey of Actions' list),
// the same list, and an error exactly when Actions errs -- over every
// single-option answer, the empty answer, and refused answers.
func TestIntentKeyIsActionsKey(t *testing.T) {
	f := benchRoot(t)
	d := f.engine.Pending()
	c := f.collector.Clone()
	if _, err := c.ObserveDecision(f.engine, d); err != nil {
		t.Fatal(err)
	}
	ins := []decision.Intent{{}, {Choices: []int{len(d.Options)}}, {Choices: []int{0, 0}}}
	for i := range d.Options {
		ins = append(ins, decision.Intent{Choices: []int{i}})
	}
	other := NewCollector(1)
	for _, in := range ins {
		for _, col := range []*Collector{c, other} {
			acts, err := col.Actions(d, in)
			key, kerr := col.IntentKey(d, in)
			got, aerr := col.AppendIntentActions(nil, d, in)
			if (err == nil) != (kerr == nil) || (err == nil) != (aerr == nil) {
				t.Fatalf("intent %+v: Actions err %v, IntentKey err %v, AppendIntentActions err %v", in, err, kerr, aerr)
			}
			if err != nil {
				continue
			}
			if !bytes.Equal(key, AppendActionsKey(nil, acts)) || !slices.Equal(got, acts) {
				t.Fatalf("intent %+v: key or list differs from Actions", in)
			}
		}
	}
}
