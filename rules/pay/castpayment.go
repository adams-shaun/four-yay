package pay

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// CastPayment is a pending cast's payment state beside PaidCost (lasagna spec
// §9.2, E7 flow slice 4): the planned payment it executes, the announced
// CR 601.2g window and its taps, the cost-part ask cursors, the Convoke
// announcement and the flexible-pip announcement. Package rules embeds it in
// its pendingCast the way it embeds PaidCost, so the fields read as pending
// cast fields and the payment layer records and reads them without the rest
// of the cast. The clone tags are read by rules' Clone generator.
type CastPayment struct {
	// Payment is a privately-owned V1 witness selected at priority. It stays
	// on the ordinary cast continuation through target choices, then drives
	// only CR 601.2g mana activations. All resulting state changes remain in
	// the established mana activation and payment paths.
	Payment         *PlannedCastPayment       `clone:"deep"`
	PaymentNext     int                       `clone:"deep"`
	PaymentFallback *decision.PaymentFallback `clone:"deep"`
	// Announced marks a cast begun by Intent.Announce (announce-then-pay
	// spec, rules/announce_pay.go): its CR 601.2g window is the announced
	// one. WindowTaps records the activations made from that window, for
	// Undo last tap and Cancel cast. Both are zero for every other cast.
	Announced  bool        `clone:"deep"`
	WindowTaps []WindowTap `clone:"deep"`
	// WindowDone is set when the 601.2g mana window was answered "done", so
	// the payment proceeds straight to settling instead of re-offering it.
	WindowDone bool `clone:"deep"`
	// ManaConvertDone records the Optional$ ManaConvert election. Before the
	// election, feasibility uses the union so the cast remains offerable;
	// after it, the payment's conversion set uses only the selected optional
	// contribution.
	ManaConvertDone bool `clone:"deep"`
	ManaConvertUse  bool `clone:"deep"`

	// The cost-part ask cursors: each walks its part list in cost order
	// (SacPaid counts the units of the current Sac part already chosen).
	SacPart, SacPaid, DiscardPart, ExilePart    int `clone:"deep"`
	RevealPart, BeholdPart, TapPart, BlightPart int `clone:"deep"`
	RevealOrChoosePart                          int `clone:"deep"`

	// SubCounterPays records the counter-removal picks of every SubCounter
	// part, each entry tagged with the part index it belongs to. A
	// fixed-kind filtered part (SubCounter<N/Kind/Target>) records ONE entry
	// carrying the object it removes from, with an empty Kind; a wildcard
	// "Any" part records ONE entry per counter unit removed, each carrying
	// the object AND the chosen counter kind. Grouping by explicit part index
	// (rather than a positional append) is what keeps a cost mixing a
	// fixed-kind part before a wildcard part from mis-indexing the selected
	// kind. SubCounterPart walks the parts in cost order like SacPart.
	SubCounterPays []SubCounterPay `clone:"deep"`
	SubCounterPart int             `clone:"deep"`

	// Convoke is the announced set of creatures paying Convoke or Harmonize.
	// It is chosen after the complete mana cost exists and before the mana
	// ability window; a committed creature is therefore unavailable to make
	// mana as well as being tapped when payment is settled.
	Convoke     []ConvokePayment `clone:"deep"`
	ConvokeDone bool             `clone:"deep"`

	// PayIdx / PayColor / PayLife / PayGeneric carry the flexible-pip payment
	// announcement (CR 601.2b/107.4e-f). ManaAsk walks the cost's combined
	// announcement-pip list one decision at a time; PayIdx is the next
	// unsettled pip, PayColor accumulates the coloured spend the announced
	// pips chose, PayLife the life a Phyrexian face paid with two life costs,
	// and PayGeneric the generic a monocolour hybrid pip paid with its
	// generic face.
	PayIdx     int        `clone:"deep"`
	PayColor   state.Mana `clone:"deep"`
	PayLife    int32      `clone:"deep"`
	PayGeneric int32      `clone:"deep"`
}

// PlannedCastPayment is deliberately private continuation state rather than
// an alternate payment engine. Its plan is deep-copied at Submit and Clone.
type PlannedCastPayment struct {
	ActionID string
	Plan     decision.PaymentPlan
}

// ConvokePayment is one announced non-mana payment. Color is zero for a
// generic Convoke contribution or Harmonize; Power is zero for Convoke and
// is the amount a Harmonize creature reduces the generic total by.
type ConvokePayment struct {
	ID         state.ObjID
	Color      byte
	Power      int32
	CountsMana bool
	// Waterbend marks a tap that pays one generic of a RaiseCost
	// Waterbend<N>/<X> additional cost (ConvokeAsk's waterbend_generic
	// option); the count of such taps is capped at the Waterbend amount.
	Waterbend bool
}

// Committed reports whether id is already committed to this payment: an
// announced Convoke/Harmonize/Improvise/waterbend contribution or an elected
// tapXType permanent (paid's Taps). A committed permanent can be neither
// offered again nor tapped for mana.
func (cp *CastPayment) Committed(paid *PaidCost, id state.ObjID) bool {
	for _, c := range cp.Convoke {
		if c.ID == id {
			return true
		}
	}
	for _, tid := range paid.Taps {
		if tid == id {
			return true
		}
	}
	return false
}

// WindowTap records one mana activation made from an announced window (a
// "mana" answer or an Auto-fill step), for Undo last tap and Cancel cast
// (announce-then-pay spec §5). Mark and Trig are the event-log and
// trigger-queue lengths before the activation; Normal is the planner's tier
// verdict for the activated ability at activation time. The record is closed
// (its span judged) the next time the window is posed or the next activation
// begins, before any other event is logged.
type WindowTap struct {
	Source     state.ObjID
	Mark       int
	Trig       int
	Normal     bool
	Closed     bool
	Reversible bool
	Adds       []WindowTapAdd
}

// WindowTapAdd is one ManaAdd a reversible activation made: the exact counter
// and the positive amount a ManaUndo removes again.
type WindowTapAdd struct {
	Counter string
	Amount  int32
}

// CloneWindowTaps deep-copies a window-tap record list (rules' Clone).
func CloneWindowTaps(in []WindowTap) []WindowTap {
	if in == nil {
		return nil
	}
	out := make([]WindowTap, len(in))
	for i, t := range in {
		t.Adds = append([]WindowTapAdd(nil), t.Adds...)
		out[i] = t
	}
	return out
}
