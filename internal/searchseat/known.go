package searchseat

import (
	"errors"

	"github.com/adams-shaun/gorge/internal/searchprobe"
)

// KnownTracker is the incremental known-card projection a seat keeps across
// one game: Update folds the frames a History gained since the last call (in
// ProjectKnownCards' order: frame i, then the actor's answer to frame i,
// then frame i+1), so a decision costs its new frames, not the whole game.
// The answer to the newest frame is folded at the next Update, once the
// driver has recorded it.
//
// It lives here, not in a seat package, because the projection has one owner:
// the Feed (Feed.Known) folds the same frames every seat reads back through
// HistoryRef, so azmcts and sbsearch no longer keep private copies that could
// drift. azmcts.KnownTracker is a type alias for this type.
type KnownTracker struct {
	t    *searchprobe.KnownCardTracker
	next int
	err  error
}

// Update folds h's new frames and returns the projection as of h's last
// frame. An error is sticky: the tracker is dead for the rest of the game.
func (k *KnownTracker) Update(h searchprobe.History) (searchprobe.KnownCards, error) {
	if k.err != nil {
		return searchprobe.KnownCards{}, k.err
	}
	if k.t == nil {
		k.t = searchprobe.NewKnownCardTracker(h.Actor)
	}
	if k.next > len(h.Frames) {
		k.err = errors.New("searchseat: known-card history shrank")
		return searchprobe.KnownCards{}, k.err
	}
	for k.next < len(h.Frames) {
		if k.next > 0 {
			if a, ok := h.Answers[k.next-1]; ok {
				k.t.Answer(a)
			}
		}
		if err := k.t.Observe(h.Frames[k.next]); err != nil {
			k.err = err
			return searchprobe.KnownCards{}, err
		}
		k.next++
	}
	return k.t.Known(), nil
}
