package effects

import "testing"

func TestCountOperatorSVarCycleIsBounded(t *testing.T) {
	h, c := fixtureHost(t)
	c.SVars = map[string]string{
		"X": "PlayerCountOpponents$Amount/Minus.Y",
		"Y": "PlayerCountOpponents$Amount/Minus.X",
	}

	if n, ok := EvalCountOK(h, c, "PlayerCountOpponents$Amount/Minus.X"); !ok || n != 1 {
		t.Fatalf("cyclic count-operator SVars = (%d, %v), want bounded (1, true)", n, ok)
	}
}
