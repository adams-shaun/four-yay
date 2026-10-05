package decision

import "testing"

// An optional power floor offers either the empty decline or a paying subset.
// Repair must never turn a partial paying answer into a mixed synthetic decline.
func TestTeamworkOptionalFloorSharedAnswer(t *testing.T) {
	d := &Decision{Kind: KChoose, Min: 1, Max: 3, AllowNone: true, MinSum: 3,
		Options: []Option{{Index: 0, Kind: "teamwork", Value: 2}, {Index: 1, Kind: "teamwork", Value: 2}, {Index: 2, Kind: "teamwork", Value: 2}}}
	if d.Options[0].Value >= d.MinSum || d.Options[0].Value+d.Options[1].Value < d.MinSum {
		t.Fatal("precondition: one option must fail and two must reach the floor")
	}
	if err := d.Validate(Intent{}); err != nil {
		t.Fatalf("decline rejected: %v", err)
	}
	if got := d.FitRequired(nil); len(got) != 0 {
		t.Fatalf("repair changed decline to %v", got)
	}
	if err := d.Validate(Intent{Choices: []int{0}}); err == nil {
		t.Fatal("accepted below-threshold subset")
	}
	repaired := d.FitRequired([]int{0})
	if err := d.Validate(Intent{Choices: repaired}); err != nil {
		t.Fatalf("repaired subset %v rejected: %v", repaired, err)
	}
	if len(repaired) != 2 {
		t.Fatalf("repair did not top up below-threshold selection: %v", repaired)
	}
}
