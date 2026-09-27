package decision

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// The announce selector and the window readout are additive: an intent or a
// decision that does not use them serialises exactly as before.
func TestAnnounceFieldsAreAbsentWhenUnused(t *testing.T) {
	in, _ := json.Marshal(Intent{Seq: 3, Player: 1, Choices: []int{2}})
	if string(in) != `{"seq":3,"player":1,"choices":[2]}` {
		t.Fatalf("legacy intent wire = %s", in)
	}
	d, _ := json.Marshal(Decision{Seq: 3, Kind: KChoose, Min: 1, Max: 1, Options: []Option{{Index: 0, Kind: "done", Label: "Done"}}})
	if strings.Contains(string(d), "mana_payment") || strings.Contains(string(d), "announce") {
		t.Fatalf("legacy decision wire carries announce fields: %s", d)
	}
}

func TestAnnounceValidation(t *testing.T) {
	cast := PlannedCast{Object: 7, Face: 0, Origin: "hand"}
	aid, err := PaymentActionID(PaymentPlanV1, 5, 0, cast)
	if err != nil {
		t.Fatal(err)
	}
	d := &Decision{Seq: 5, Player: 0, Kind: KPriority, Min: 1, Max: 1,
		Options:        []Option{{Index: 0, Kind: "pass", Label: "Pass"}},
		PaymentActions: []PaymentAction{{ID: aid, Cast: cast, Plans: []PaymentPlan{{Version: PaymentPlanV1}}}}}
	ok := Intent{Seq: 5, Player: 0, Announce: &AnnounceSelection{ActionID: aid}}
	if err := d.Validate(ok); err != nil {
		t.Fatalf("valid announce rejected: %v", err)
	}
	bad := map[string]Intent{
		"choices":  {Seq: 5, Player: 0, Choices: []int{0}, Announce: &AnnounceSelection{ActionID: aid}},
		"rest":     {Seq: 5, Player: 0, Rest: []int{0}, Announce: &AnnounceSelection{ActionID: aid}},
		"payment":  {Seq: 5, Player: 0, Announce: &AnnounceSelection{ActionID: aid}, Payment: &PaymentSelection{ActionID: aid}},
		"unknown":  {Seq: 5, Player: 0, Announce: &AnnounceSelection{ActionID: "x"}},
		"empty":    {Seq: 5, Player: 0, Announce: &AnnounceSelection{}},
		"stale":    {Seq: 4, Player: 0, Announce: &AnnounceSelection{ActionID: aid}},
		"wrongsea": {Seq: 5, Player: 1, Announce: &AnnounceSelection{ActionID: aid}},
	}
	for name, in := range bad {
		if err := d.Validate(in); err == nil {
			t.Fatalf("%s: malformed announce accepted", name)
		}
	}
	planless := d.Clone()
	planless.PaymentActions[0].Plans = nil
	if err := planless.Validate(ok); err == nil {
		t.Fatal("announce of a planless action accepted")
	}
	choose := d.Clone()
	choose.Kind = KChoose
	if err := choose.Validate(ok); err == nil {
		t.Fatal("announce accepted on a non-priority decision")
	}
	// Clone and CloneIntent own their copies.
	c := CloneIntent(ok)
	c.Announce.ActionID = "mutated"
	if ok.Announce.ActionID != aid {
		t.Fatal("CloneIntent aliases the announce selector")
	}
	w := &Decision{ManaPayment: &ManaPaymentWindow{Card: 7, AutoFill: []state.ObjID{1, 2}}}
	wc := w.Clone()
	wc.ManaPayment.AutoFill[0] = 9
	if w.ManaPayment.AutoFill[0] != 1 {
		t.Fatal("Decision.Clone aliases the mana payment readout")
	}
}
