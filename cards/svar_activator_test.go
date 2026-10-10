package cards

import "testing"

func TestSVarGrantHasActivator(t *testing.T) {
	svars := map[string]string{
		"Plain": "AB$ Draw | Cost$ T | NumCards$ 1 | SpellDescription$ Draw a card.",
		"Opp":   "AB$ Draw | Cost$ T | Activator$ Opponent | NumCards$ 1 | SpellDescription$ Draw a card.",
		"Trig":  "DB$ Draw | Activator$ Player | NumCards$ 1",
	}
	for name, want := range map[string]bool{"Plain": false, "Opp": true, "Trig": false, "Missing": false, "": false} {
		if got := SVarGrantHasActivator(svars, name); got != want {
			t.Errorf("SVarGrantHasActivator(%q) = %v, want %v", name, got, want)
		}
	}
}
