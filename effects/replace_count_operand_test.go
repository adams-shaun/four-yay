package effects

import "testing"

func TestReplaceCountOperandResolvesSVar(t *testing.T) {
	h := newHost(t, 2)
	c := &Ctx{Repl: ReplacementInputs{Amount: 5}, SVars: map[string]string{"Y": "Count$xPaid"}, X: 3}
	if got, ok := EvalCountOK(h, c, "ReplaceCount$DamageAmount/Plus.Y"); !ok || got != 8 {
		t.Fatalf("ReplaceCount$DamageAmount/Plus.Y = (%d, %v), want (8, true)", got, ok)
	}
}
