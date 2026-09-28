package kshadow

import (
	"context"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/spellbench/sbsearch"
	"github.com/adams-shaun/gorge/internal/spellbench/v1agent"
)

// SBSearchStats counts sb-search's work on the shadow (sbsearch.Watch).
type SBSearchStats struct {
	Searched, Overrides, Refused, Rollouts, FailedWorlds int
	Reasons                                              map[string]int
}

// sbStats is the process's sb-search counter (one policy per agent
// process; sbsearch.Watch is a package hook).
var sbStats = SBSearchStats{Reasons: map[string]int{}}

func init() {
	sbsearch.Watch = func(dg sbsearch.Diag) {
		sbStats.Searched++
		if dg.Override {
			sbStats.Overrides++
		}
		sbStats.Rollouts += dg.Rollouts
		sbStats.FailedWorlds += dg.Failed
		if dg.Refused != "" {
			sbStats.Refused++
			r := dg.Refused
			if len(r) > 60 {
				r = r[:60]
			}
			sbStats.Reasons[r]++
		}
	}
}

// sbsearchAnswer is sb-search (internal/spellbench/sbsearch, the
// configured variant) at the shadow's pending decision. Its honest redeal
// reads the seat's observation feed: here a one-frame feed observing the
// staged shadow (everything this decision's observation shows; the dealt
// hidden zones are hidden moves, never introduced to the seat's view).
func (p *Policy) sbsearchAnswer(sh *Shadow, d *v1agent.Decision) (decision.Intent, error) {
	e := sh.E
	if p.gs.WantsPaymentActions() && e.Pending().Kind == decision.KPriority {
		e.EnsurePaymentActions()
	}
	pd := e.Pending()
	p.gs.SetPlanner(e)
	feed := searchseat.NewFeed(sh.Me)
	feed.Observe(e)
	brd := botpolicy.NewBoard(2)
	b := botpolicy.BoardFromGameInto(e.G, e, sh.Me, &brd)
	setup := searchprobe.PublicGame{Names: []string{"p0", "p1"},
		Decks: [][]*cards.Card{p.setup.Decks[0], p.setup.Decks[1]}, Tokens: p.cfg.Reg.Tokens}
	ss := sbsearch.New(p.gs, p.decisionSeed(d)^0x5b5e, p.cfg.SBSearch)
	return ss.DecideSearch(context.Background(), searchseat.Env{Setup: setup, Engine: e, Board: b, Feed: feed}, *pd)
}
