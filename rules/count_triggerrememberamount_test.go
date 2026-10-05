// Count$TriggerRememberAmount — the trigger's remembered amount (ticket
// cli-20261005T092134Z-fed167ca, part 1). TDM New Way Forward's prevention
// rider is a reflexive "when damage is prevented this way" trigger:
// `DBImmediateTrigger:DB$ ImmediateTrigger | ... | RememberSVarAmount$ X` with
// `SVar:X:ReplaceCount$DamageAmount`, and `SVar:Y:Count$TriggerRememberAmount`
// sizes its damage and draw. Forge's ImmediateTriggerEffect addRemembered()s
// the calculated RememberSVarAmount$ Integer and Count$TriggerRememberAmount
// sums the trigger's remembered Integers. This build does not yet model that
// Integer channel separately; the amount a firing trigger carries is the
// engine-recorded Ctx.TriggerAmount, which rides the spawning context onto the
// minted reflexive trigger (QueueReflexiveTrigger copies c.TriggerContext).
// The pin drives New Way Forward's REAL DBImmediateTrigger through
// effects.Resolve and reads the queued reflexive trigger, so the amount's
// passage from spawning context to the head is exercised end to end rather
// than the head alone.
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
)

// TestTriggerRememberAmountHeadOnNewWayForward pins the head against New Way
// Forward's real DBImmediateTrigger and SVar Y. Two different carried amounts
// prove the read turns on the trigger's captured amount, not a constant.
func TestTriggerRememberAmountHeadOnNewWayForward(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoardCard(t, e, 0, corpusCard(t, "New Way Forward"))
	face := e.G.Obj(src).Face()

	sa := cards.ResolveSVar(face.SVars, "DBImmediateTrigger")
	if sa == nil {
		t.Fatal("test precondition: New Way Forward has no DBImmediateTrigger SVar")
	}
	// Precondition: SVar Y is the head under test and the reflexive body
	// references it. If the fixture changes, this test no longer covers the
	// head it names.
	if got := face.SVars["Y"]; got != "Count$TriggerRememberAmount" {
		t.Fatalf("test precondition: SVar Y = %q, want Count$TriggerRememberAmount", got)
	}
	if got := sa.Params["Execute"]; got != "TrigDamage" {
		t.Fatalf("test precondition: DBImmediateTrigger Execute$ = %q, want TrigDamage", got)
	}
	if got := face.SVars["TrigDamage"]; !strings.Contains(got, "NumDmg$ Y") {
		t.Fatalf("test precondition: TrigDamage = %q, want a NumDmg$ Y body", got)
	}

	for _, amount := range []int32{3, 7} {
		before := len(e.pendingTriggers)
		ctx := effects.NewCtxPtr(src, 0, effects.CtxInit{SVars: face.SVars})
		ctx.TriggerAmount = amount
		effects.Resolve(e, ctx, sa)
		if len(e.pendingTriggers) != before+1 {
			t.Fatalf("precondition: Resolve queued %d reflexive triggers, want 1 (queue path not taken?)",
				len(e.pendingTriggers)-before)
		}
		pt := &e.pendingTriggers[len(e.pendingTriggers)-1]
		if pt.Ctx.TriggerAmount != amount {
			t.Fatalf("reflexive trigger carried amount = %d, want %d", pt.Ctx.TriggerAmount, amount)
		}
		n, ok := effects.EvalCountOK(e, &pt.Ctx, face.SVars["Y"])
		if !ok {
			t.Fatalf("Count$TriggerRememberAmount did not resolve (ok false) with amount %d", amount)
		}
		if n != amount {
			t.Fatalf("Count$TriggerRememberAmount = %d, want %d", n, amount)
		}
		// Consume the queued trigger so the next iteration's precondition
		// count is deterministic.
		e.pendingTriggers = e.pendingTriggers[:len(e.pendingTriggers)-1]
	}
}
