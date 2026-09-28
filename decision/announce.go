package decision

import (
	"fmt"

	"github.com/adams-shaun/gorge/state"
)

// AnnounceSelection is the announce-then-pay selector (spec
// docs/superpowers/specs/2026-09-27-announce-then-pay.md §3): it names an
// offered PaymentAction by ID and asks the engine to begin that ordinary cast
// WITHOUT a payment witness, so the caster pays in the CR 601.2g mana window
// ("select mana" prompt) instead. It is exclusive with Choices, Rest and
// Payment, valid only on a priority answer, and only for an action that
// carries at least one plan -- the planner's proof that the floating pool plus
// untapped sources can pay the cast.
type AnnounceSelection struct {
	ActionID string `json:"action_id"`
}

// ManaPaymentWindow is the announced CR 601.2g window's readout (spec §4.1):
// the spell being paid for, its total mana cost, what the floating pool does
// not yet cover, the pool itself, and the sources Auto-fill would activate.
// It is present only on that window, so every other decision serialises
// byte-identically.
type ManaPaymentWindow struct {
	Card     state.ObjID   `json:"card"`
	Cost     PaymentCost   `json:"cost"`
	Owed     PaymentCost   `json:"owed"`
	Pool     ManaAmount    `json:"pool"`
	AutoFill []state.ObjID `json:"autofill,omitempty"`
}

// Option kinds the announced window offers beside its per-ability "mana"
// options (spec §4.2). The window is a chooseCast decision, so the chosen
// option's kind routes the answer.
const (
	OptAutoFill   = "autofill"
	OptUndoTap    = "undo_tap"
	OptCancelCast = "cancel_cast"
)

// CloneAnnounceSelection copies a submitted announce selector.
func CloneAnnounceSelection(s *AnnounceSelection) *AnnounceSelection {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}

// CloneManaPaymentWindow deep-copies a window readout.
func CloneManaPaymentWindow(w *ManaPaymentWindow) *ManaPaymentWindow {
	if w == nil {
		return nil
	}
	c := *w
	c.AutoFill = append([]state.ObjID(nil), w.AutoFill...)
	return &c
}

// validateAnnounce is Validate's announce branch: priority only, exclusive
// with every other selector, and naming an offered action that has a plan.
// Rules re-plans the cast independently at Submit (ValidateCastAnnounce); this
// check needs no engine.
func (d *Decision) validateAnnounce(in Intent) error {
	if d.Kind != KPriority {
		return fmt.Errorf("announce selector is only accepted on a priority answer, not %s", d.Kind)
	}
	if len(in.Choices) != 0 || len(in.Rest) != 0 || in.Payment != nil {
		return fmt.Errorf("announce selector requires empty choices, rest and payment")
	}
	if in.Announce.ActionID == "" {
		return fmt.Errorf("announce selector has no action id")
	}
	for _, a := range d.PaymentActions {
		if a.ID != in.Announce.ActionID {
			continue
		}
		if len(a.Plans) == 0 {
			return fmt.Errorf("payment action %q has no plan; it cannot be announced", a.ID)
		}
		return nil
	}
	return fmt.Errorf("payment action %q is not offered", in.Announce.ActionID)
}
