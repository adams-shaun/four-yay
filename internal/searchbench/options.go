package searchbench

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/decision"
)

// NamedOption translates a recorded human card action into a legal offered
// option that names it. The source records a printing, not a physical card
// object, so equal-name copies are indistinguishable evidence; the first
// offered copy is the deterministic representative.
func NamedOption(d *decision.Decision, verb, card string) (decision.Intent, error) {
	if d == nil {
		return decision.Intent{}, fmt.Errorf("searchbench: no pending decision")
	}
	want := verb + card
	match := -1
	for i := range d.Options {
		if strings.HasPrefix(d.Options[i].Label, want) {
			if match < 0 {
				match = d.Options[i].Index
			}
		}
	}
	if match < 0 {
		return decision.Intent{}, fmt.Errorf("searchbench: %q is not offered at %s", want, d.Kind)
	}
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{match}}, nil
}
