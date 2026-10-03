package effects

import (
	"github.com/adams-shaun/gorge/decision"
)

func optionIndexes(options []decision.Option) []int {
	out := make([]int, len(options))
	for i, option := range options {
		out[i] = option.Index
	}
	return out
}
