package manabrewhttp

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// headSeq is the live head's event seq for match k of table t — the same
// "no explicit ?seq=" derivation host/httpapi's view handler makes from
// Matches, reused here because this package has no lower-level access to a
// match's internals (by design: the adapter drives the engine only through
// the exported Registry surface, D2's fence).
func headSeq(r *host.Registry, t host.TableID, k int) (uint64, error) {
	ms, err := r.Matches(t)
	if err != nil {
		return 0, err
	}
	for _, m := range ms {
		if m.Match == k {
			if m.Events == 0 {
				return 0, host.ErrBeyondHead{Head: 0}
			}
			return uint64(m.Events - 1), nil
		}
	}
	return 0, host.ErrNotFound
}

// pollView is the one read this package makes of the live engine state for a
// seat: its own redacted view at the head, and the decision currently
// pending for it (nil, not an error, when nothing is — a human seat between
// decisions is the ordinary steady state, not a fault). Any other Pending
// failure (a non-human seat, a finished match) is folded into "nothing
// pending" the same permissive way: a caller that cannot legally ask this
// seat anything simply sees no prompt, which is the same observable shape a
// spectator-only stream would have to fall back to anyway.
func (h *handler) pollView(t host.TableID, k int, seat state.PlayerID) (view.View, *decision.Decision, error) {
	seq, err := headSeq(h.reg, t, k)
	if err != nil {
		return view.View{}, nil, err
	}
	v, err := h.reg.ViewAtSeat(t, k, seq, seat)
	if err != nil {
		return view.View{}, nil, err
	}
	d, _ := h.reg.Pending(t, k, seat)
	return v, d, nil
}
