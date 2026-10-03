package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Explore is Forge's ExploreEffect (api:Explore, CR 701.35a — 42 raw corpus
// files carry a `DB$ Explore` body or an `AB$/SP$ Explore` action: the
// Ixalan/LCI explore mechanic — Merfolk Cave-Diver, Queen's Agent, Twists and
// Turns, the Map token's activated ability, ...). "A creature explores by
// revealing the top card of their library. If it's a land card, its owner
// puts it into their hand. Otherwise, its owner puts a +1/+1 counter on the
// creature, then puts the card back on top of their library or into their
// graveyard" (the LCI wording the corpus's SpellDescriptions spell out).
//
// Per explorer and per Num$ (default 1):
//
//   - The replacement tier is consulted FIRST (CR 614.4's window is before
//     the process): ExploreReplaced runs any R:Event$ Explore replacement's
//     ReplaceWith$ body in place and this explorer's own process is skipped
//     entirely — "instead it explores, then it explores again" replaces the
//     whole reveal/counter/move process with the body's own fresh explores.
//   - The top card is revealed publicly (one non-Secret ids-Note, the same
//     shape the Dig window reveal and effReveal's public arm emit).
//   - A land moves library → hand and the explore records.
//   - A nonland poses the destination KChoose ("put the card back or put it
//     into your graveyard"). Options are ordered state-changing-first —
//     option 0 "into your graveyard", option 1 "back on top" — so the no-host
//     stand-in and botpolicy's clamp both bin the card deterministically (the
//     R-9 / TapOrUntap discipline). The +1/+1 counter and the destination
//     move are applied TOGETHER after the answer arrives, in CR order
//     (counter before destination): one code path for the answered,
//     no-host and resumed shapes. The ask therefore precedes the counter in
//     the event log by one recorded election — a DecisionAsk/Made marker
//     pair, no state change, and the counter-before-destination order the
//     rule states is preserved in every state-changing event.
//   - The explore is recorded by one events.Explore marker (trig:Explores
//     matches it; Apply folds nothing).
//
// Defined$ selectors and the ValidTgts$ target ask resolve through the
// ordinary machinery (Defined falls back to the source for a body that names
// neither — the bare `DB$ Explore` self-explore trigger shape). Num$ resolves
// through the ordinary Num evaluator (a literal 2 is the common carrier; an
// SVar/X body resolves at resolution time).
func init() {
	Register("Explore", effExplore)
}

// effExplore resolves every Defined$ creature through its explore process.
// SubAbility$ chains are resolved by the ordinary Resolve walk, not here.
func effExplore(h Host, c *Ctx, sa *cards.SA) {
	n := Num(h, c, sa, "Num", 1)
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue
		}
		// The resume cursor: skip targets before the one whose explore
		// suspended (the Dig discipline — re-entry replays the loop from its
		// head, so earlier targets must not explore a second time). The
		// pending explore is applied on this target's first loop iteration
		// and clears the cursor, so later targets proceed fresh.
		if c.ExploreObj != 0 && t.Obj != c.ExploreObj {
			continue
		}
		// The resumed explorer continues its Num$ count at the explore whose
		// election was answered (Ctx.ExploreCount, carried on the ask): the
		// re-entry replays the loop from its head, and a count restarted at
		// zero posed a fresh election after every applied one, so an
		// "explores X times" never finished while its reveals were nonland.
		i := int32(0)
		if c.ExploreDone && c.ExploreObj == t.Obj {
			i = c.ExploreCount
		}
		for ; i < n; i++ {
			if c.ExploreDone && c.ExploreCard != 0 && c.ExploreObj == t.Obj {
				// The resumed destination election for THIS explorer. Consumed
				// and cleared at the point of application (fx42 scoping), so
				// this explorer's remaining explores and every later target
				// pose their own fresh path.
				card, toGrave := c.ExploreCard, c.ExploreChoice != "top"
				c.ExploreObj, c.ExploreCard, c.ExploreChoice, c.ExploreDone, c.ExploreCount = 0, 0, "", false, 0
				applyNonlandExplore(h, t.Obj, card, toGrave)
				continue
			}
			if exploreOnce(h, c, sa, t.Obj, i) {
				// The election was posted; the resolution is suspended. Nothing
				// after the ask may run on this pass — the answer re-enters
				// through rules' "explore" resume arm (the effDig discipline).
				return
			}
		}
	}
	// Leftover pending state that no target consumed (the pending explorer
	// left play, a malformed resume): consumed and cleared, never inherited.
	c.ExploreObj, c.ExploreCard, c.ExploreChoice, c.ExploreDone, c.ExploreCount = 0, 0, "", false, 0
}

// exploreOnce runs one explore process for explorer and reports whether the
// destination election was POSTED (the resolution suspended — the caller must
// stop its walk immediately, the effDig discipline). An explorer that has
// left the battlefield, an already-replaced explore or an empty library
// explores nothing (an empty library cannot reveal a card, so the process has
// nothing to record — no events.Explore marker, so no trigger fires).
//
// done is how many of this explorer's Num$ explores are already complete;
// the election carries it (Decision.ResumeExploreDone) so the resume
// continues the count.
func exploreOnce(h Host, c *Ctx, sa *cards.SA, explorer state.ObjID, done int32) bool {
	g := h.Game()
	o := g.Obj(explorer)
	if o == nil || o.Zone != state.ZBattlefield {
		return false
	}
	// CR 614.4: the replacement window is before the process. A matching
	// R:Event$ Explore replacement's body has now run (inside the host call)
	// and this explorer's own process is replaced whole.
	if h.ExploreReplaced(explorer) {
		return false
	}
	ctrl := o.Controller
	lib := g.Zone(state.ZLibrary, ctrl)
	if len(lib) == 0 {
		return false
	}
	top := lib[0]
	// The public reveal: the same non-Secret ids-Note the Dig window reveal
	// and effReveal's public arm emit ("X reveals Forest #12").
	h.Emit(events.Event{Kind: events.Note, Player: ctrl, IDs: []state.ObjID{top}})
	if revealed := g.Obj(top); revealed != nil && revealed.Face() != nil && revealed.Face().IsLand() {
		ev := moveZoneEvent(c, top, state.ZLibrary, state.ZHand)
		ev.Player = ctrl
		h.Emit(ev)
		h.Emit(events.Event{Kind: events.Explore, Obj: explorer, Player: ctrl,
			IDs: []state.ObjID{top}, Amount: 1})
		return false
	}
	// The LCI nonland shape: the +1/+1 counter, then "put the card back or
	// put it into your graveyard" — a real election. Options are ordered
	// state-changing-first (option 0 the graveyard, option 1 back on top) so
	// a host that cannot ask and botpolicy's clamp both take option 0 and the
	// game always moves. Both options are always legal, so the
	// strict-supersets never-ask rule does not apply. The counter and the
	// destination move are applied together after the answer (see the doc:
	// the CR counter-before-destination order is preserved in the event
	// stream; the recorded election precedes it).
	d := &decision.Decision{Player: ctrl, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: c.Source, ResumeKind: "explore", ResumeSA: sa,
		ResumeTarget: int(explorer), ResumeExploreDone: done,
		Prompt: "Put the revealed card back on top of your library or into your graveyard?"}
	d.Options = []decision.Option{
		{Index: 0, Kind: "graveyard", Label: "Put it into your graveyard", Obj: top, Player: ctrl},
		{Index: 1, Kind: "top", Label: "Put it back on top of your library", Obj: top, Player: ctrl},
	}
	if ans, ok := AskTape(h, d); ok {
		// The "explore" answer in hand: the re-entry's application (any
		// answer but "top" sends the card to the graveyard).
		applyNonlandExplore(h, explorer, top, len(ans) == 0 || ans[0].Kind != "top")
		return false
	}
	if Ask(h, d) == AskAsked {
		// Park the mid-explore state (documenting; the resume arm is what
		// actually re-seeds it — the suspended Ctx is discarded) and report
		// the suspension.
		c.ExploreObj = explorer
		c.ExploreCard = top
		return true
	}
	// No host (an effects-package test double, a fuzz run): option 0, the
	// state-changing choice — the exact mirror of botpolicy's clamp answer.
	applyNonlandExplore(h, explorer, top, true)
	return false
}

// applyNonlandExplore applies a nonland explore's outcome: the +1/+1 counter
// on the explorer (CR 701.35a's counter comes before the card's destination),
// then the destination move (into the graveyard, or back on top — a stay, no
// event), then the one events.Explore record both shapes share.
func applyNonlandExplore(h Host, explorer, card state.ObjID, toGraveyard bool) {
	g := h.Game()
	o := g.Obj(explorer)
	if o == nil {
		return
	}
	ctrl := o.Controller
	h.Emit(events.Event{Kind: events.CounterChange, Obj: explorer, Counter: "P1P1", Amount: 1})
	if toGraveyard {
		if co := g.Obj(card); co != nil && co.Zone == state.ZLibrary {
			ev := events.Event{Kind: events.MoveZone, Obj: card,
				From: state.ZLibrary, To: state.ZGraveyard}
			ev.Player = co.Owner
			h.Emit(ev)
		}
	}
	h.Emit(events.Event{Kind: events.Explore, Obj: explorer, Player: ctrl,
		IDs: []state.ObjID{card}, Amount: 0})
}
