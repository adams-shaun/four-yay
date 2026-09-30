package rules

import "testing"

// TestOracleActivateKindCoversSpecialActions pins the option Kind set the
// `activate` scenario op may select. Before the station/unlock/turn_face_up
// kinds were added, a scenario could assert one of those special actions was
// OFFERED (the `offered` observable matches an arbitrary kind exactly) but
// could never SELECT it, so it could never observe the action's effect. This
// is the focused unit proof for that membership; TestOracleAudit's scenarios
// (Exploration Broodship, Meat Locker, Belltoll Dragon) prove the end-to-end
// selection.
func TestOracleActivateKindCoversSpecialActions(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"ability", "activate", "station", "unlock", "turn_face_up"} {
		if !oracleActivateKind(kind) {
			t.Errorf("oracleActivateKind(%q) = false, want true (the activate op must select it)", kind)
		}
	}
	// Kinds that must stay outside the activate filter: a scenario's `cast`
	// branch handles "cast", and pass/concede/play_land are never activated.
	for _, kind := range []string{"cast", "pass", "concede", "play_land", "granted", "specialize", ""} {
		if oracleActivateKind(kind) {
			t.Errorf("oracleActivateKind(%q) = true, want false (the activate op must not select it)", kind)
		}
	}
}

// TestOracleActivateLabelsMatchSpecialActions pins the labels the three
// special-action offers carry to the ability strings a scenario writes, since
// selection is kind-gated AND label-matched. The engine builds these labels
// verbatim in rules/legal.go ("Station "+name, "Unlock "+name,
// "Turn face up ("+cost+")").
func TestOracleActivateLabelsMatchSpecialActions(t *testing.T) {
	t.Parallel()
	cases := []struct{ label, want string }{
		{"Station Exploration Broodship", "Station "},
		{"Unlock Drowned Diner", "Unlock Drowned Diner"},
		{"Turn face up (pay 5 U U)", "Turn face up"},
	}
	for _, c := range cases {
		if !oracleLabelMatches(c.label, c.want) {
			t.Errorf("oracleLabelMatches(%q, %q) = false, want true", c.label, c.want)
		}
	}
}
