package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
)

// legalWalkScratch is the priority offer walk's per-engine scratch: reusable
// buffers and incremental log-scan watermarks the walk reads instead of
// re-deriving the same answer from the whole log on every walk. Every field
// is a pure function of the event log (append-only history) or plain
// capacity, so it carries no state replay could disagree with; Clone leaves
// it zero and a clone simply rescans its (copied) log once.
type legalWalkScratch struct {
	// airbendScanned is how many leading log events airbendLogged has
	// inspected, and airbendSeen whether any of them was an airbend exile
	// (a MoveZone carrying effects.AirbendExileCounter). Once seen it stays
	// seen: the log only grows.
	airbendScanned int
	airbendSeen    bool
}

// airbendLogged reports whether the log holds ANY airbend exile move. It is
// airbendCastAvailable's exact precheck: without such a move no card's most
// recent move can carry the marker, so the per-card backward log scan --
// which otherwise runs for every card in the walking seat's exile on every
// walk -- is skipped. The scan is incremental over the append-only log; a
// log shorter than the watermark (a different log installed on this engine)
// restarts it, so the answer is always the full log's.
func (e *Engine) airbendLogged() bool {
	s := &e.legalScratch
	log := e.L.Events
	if s.airbendScanned > len(log) {
		s.airbendScanned, s.airbendSeen = 0, false
	}
	if !s.airbendSeen {
		for i := s.airbendScanned; i < len(log); i++ {
			if log[i].Kind == events.MoveZone && log[i].Counter == effects.AirbendExileCounter {
				s.airbendSeen = true
				break
			}
		}
		s.airbendScanned = len(log)
	}
	return s.airbendSeen
}
