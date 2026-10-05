package rules

import "testing"

func TestOraclePickKindSeparatesPlayerTargetFromOpponentChoice(t *testing.T) {
	for _, tc := range []struct {
		name, resume, option, want string
	}{
		{name: "target player", resume: "target", option: "player", want: "player"},
		{name: "controller opponent selection", resume: "opp_pick", option: "player", want: "opponent_choice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := oraclePickKind(tc.resume, tc.option); got != tc.want {
				t.Fatalf("oraclePickKind(%q, %q) = %q, want %q", tc.resume, tc.option, got, tc.want)
			}
		})
	}
}
