package searchbench

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules"
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
