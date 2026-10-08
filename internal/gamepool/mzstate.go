//go:build gamepool

package gamepool

import (
	"sort"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/mzenc"
	"github.com/adams-shaun/gorge/state"
)

// mzActionType maps a gorge decision kind onto mzenc's six ActionEncoder
// decision types (PRIORITY, CHOOSE_NUM, BLANK, CHOOSE_TARGET, MAKE_CHOICE,
// CHOOSE_USE).
func mzActionType(k decision.Kind) int {
	switch k {
	case decision.KPriority:
		return 0
	case decision.KChoose:
		return 1
	case decision.KTarget:
		return 3
	case decision.KTriggerOptional, decision.KMulligan:
		return 5
	default:
		return 4
	}
}

// encodeStateMZ writes the decision's MageZero feature-id set into out as a
// multi-hot vector: each mzenc id (a hash in [0, 2^31-1)) folds into a slot by
// modulus. It is a pure function of the request's view, so a decision's state
// vector is content-derived and deterministic. Ids are walked in sorted order
// so the result never depends on map iteration.
func encodeStateMZ(r *Request, out []float32) {
	for i := range out {
		out[i] = 0
	}
	ids := mzenc.ProcessState(r.View, nil, state.PlayerID(r.Decision.Player),
		mzActionType(r.Decision.Kind), string(r.Decision.Kind))
	sorted := make([]int32, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	for _, id := range sorted {
		out[int(id)%len(out)] = 1
	}
}
