package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
)

// trigChainMark remembers where a triggered ability's chain-link target
// announcement (CR 603.3d, trigSubAsk) stood when one of its target asks was
// answered, so the oracle record can say how many "up to N" links the engine
// then settled without posing them. XMage still asks each of those slots.
type trigChainMark struct {
	ts   *trigSubAsk
	from int // index of the first link after the answered decision's own
}

// markTrigChain notes the in-flight chain announcement d belongs to. The zero
// mark (no chain) answers 0 from unposed.
func markTrigChain(ts *trigSubAsk, d *decision.Decision) trigChainMark {
	if ts == nil || d.Kind != decision.KTarget || ts.obj != d.Source || ts.stage > len(ts.subs) {
		return trigChainMark{}
	}
	from := ts.stage
	if d.ResumeKind == "trig_sub" {
		from++
	}
	return trigChainMark{ts: ts, from: from}
}

// unposed counts the Min-0 links between the answered decision and the next
// posed one (or the chain's end) that were settled with an empty answer. now
// is the engine's chain announcement after the answer: the same ask still
// pending its next link, or nil once every link is settled.
func (m trigChainMark) unposed(now *trigSubAsk) int {
	if m.ts == nil {
		return 0
	}
	end := len(m.ts.subs)
	if now != nil && now.obj == m.ts.obj {
		end = now.stage
	}
	n := 0
	for i := m.from; i < end && i < len(m.ts.subs); i++ {
		if len(m.ts.ans[i]) == 0 && upToOneLink(m.ts.subs[i]) {
			n++
		}
	}
	return n
}

// upToOneLink reports a chain link with a literal Min 0 and at least one
// target allowed: the "up to N target" slot XMage asks even with no candidate.
func upToOneLink(sa *cards.SA) bool {
	tp := effects.TargetsOf(sa)
	return tp.BoundMin == 0 && tp.BoundMax >= 1
}
