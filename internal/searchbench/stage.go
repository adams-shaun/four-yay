package searchbench

import (
	"context"
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// SubmitRecordedPlay advances a hypothetical root to its next decision and
// submits one recorded land or card play. It is deliberately narrow: any
// trigger, target, payment or timing ambiguity returns an error to the item
// builder, which records a rejection instead of inventing history.
func SubmitRecordedPlay(e *rules.Engine, verb, card string) error {
	if e == nil {
		return fmt.Errorf("searchbench: nil engine")
	}
	if err := e.AdvanceHypothetical(); err != nil {
		return err
	}
	d := e.Pending()
	if d == nil {
		return fmt.Errorf("searchbench: game ended before %s%s", verb, card)
	}
	if d.Kind != decision.KPriority {
		return fmt.Errorf("searchbench: %s%s reached %s", verb, card, d.Kind)
	}
	in, err := NamedOption(d, verb, card)
	if err != nil {
		return err
	}
	return e.SubmitHypothetical(in)
}

func PlayLand(e *rules.Engine, card string) error { return SubmitRecordedPlay(e, "Play ", card) }
func CastCard(e *rules.Engine, card string) error { return SubmitRecordedPlay(e, "Cast ", card) }

// NamedPayment selects the first exact engine-provided automatic payment
// witness for a recorded cast. A source action names the spell but has no
// mana-tapping transcript, so this avoids inventing manual mana actions.
func NamedPayment(e *rules.Engine, d *decision.Decision, card string) (decision.Intent, error) {
	if e == nil || d == nil || d.Kind != decision.KPriority {
		return decision.Intent{}, fmt.Errorf("searchbench: no priority for payment")
	}
	want := "Cast " + card
	for _, action := range e.EnsurePaymentActions() {
		if strings.HasPrefix(action.Label, want) && len(action.Plans) != 0 {
			return decision.Intent{Seq: d.Seq, Player: d.Player, Payment: &decision.PaymentSelection{ActionID: action.ID, Plan: action.Plans[0]}}, nil
		}
	}
	return decision.Intent{}, fmt.Errorf("searchbench: %q has no automatic payment witness", want)
}

// SubmitFallback advances through one non-recorded decision with the ordinary
// redacted bot. It bridges source omissions and is therefore diagnostic-only;
// callers must count every use in their fidelity ledger.
func SubmitFallback(e *rules.Engine, b *seat.Bot, player state.PlayerID) error {
	if e == nil || b == nil {
		return fmt.Errorf("searchbench: nil fallback")
	}
	if err := e.AdvanceHypothetical(); err != nil {
		return err
	}
	d := e.Pending()
	if d == nil {
		return fmt.Errorf("searchbench: game ended at fallback")
	}
	v := view.Project(e.G, e, player, d)
	in, err := b.Decide(context.Background(), v, *d)
	if err != nil {
		return err
	}
	return e.SubmitHypothetical(in)
}

// SubmitBridge advances a source omission without inventing an additional
// spell or land drop. Priority omissions are always a pass; non-priority
// questions still need the redacted deterministic bot to supply an answer.
func SubmitBridge(e *rules.Engine, b *seat.Bot, player state.PlayerID) error {
	if e == nil || b == nil {
		return fmt.Errorf("searchbench: nil bridge")
	}
	if err := e.AdvanceHypothetical(); err != nil {
		return err
	}
	d := e.Pending()
	if d == nil {
		return fmt.Errorf("searchbench: game ended at bridge")
	}
	if d.Kind != decision.KPriority {
		v := view.Project(e.G, e, player, d)
		in, err := b.Decide(context.Background(), v, *d)
		if err != nil {
			return err
		}
		return e.SubmitHypothetical(in)
	}
	for _, option := range d.Options {
		if option.Label == "Pass priority" {
			return e.SubmitHypothetical(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{option.Index}})
		}
	}
	return fmt.Errorf("searchbench: priority has no pass option")
}
