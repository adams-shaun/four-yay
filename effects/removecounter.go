package effects

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// effRemoveCounter is Forge's RemoveCounterEffect: for each object or player
// the Defined$ spec names (an ability with no Defined$ acts on its chosen
// targets through Ctx.Targets; one that names neither acts on its source --
// the ordinary Defined contract), remove CounterNum$ counters of CounterType$
// from it. The corpus shape is overwhelmingly the chained DB$ sub-ability
// (178 of 202 raw RemoveCounter lines), Self-dominated: Prize Pig's
// "remove those counters and untap it" payoff is the pin.
//
// CounterNum$ resolves through the shared Num evaluator (a literal, an SVar
// name such as the corpus's X/Y/SekkiX/Result/NumDmg, or an inline Count$);
// "All" means every counter of that kind the object actually has -- the
// AllCounters$ discipline effRemoveCounterAll uses, reading the count first
// so the event's Amount never overstates. A literal n over an object holding
// fewer also clamps to what is there (state.Object.AddCounter clamps at zero
// either way; emitting -n would overstate). One events.CounterChange with a
// signed negative Amount per (object, kind); an object with none of the kind
// emits nothing (the zero-batch no-op discipline both siblings follow).
//
// CounterType$ All means EVERY kind the object holds, in its own
// deterministic slice order (the counterKinds helper this file already
// shares with MultiplyCounter), one CounterChange per kind present.
//
// RememberRemoved$ True records one remembered entry per removed counter on
// the source's persistent (event-backed) remembered list -- one Choose
// "remembered" event per (object, kind) batch, the object's id repeated once
// per removed counter, so Count$RememberedSize (the HOST CARD's list,
// effects/count.go) reads the truthful size. Prize Pig's untap gate is
// ConditionCheckSVar$ X with SVar:X:Count$RememberedSize, so this rider is
// load-bearing for it. Duplicates are deliberate: the only consumer measured
// for this rider is a size count (and Cleanup's ClearRemembered$ clears the
// list again), so one entry per counter is the honest encoding.
//
// Exotic shapes stay LOUD (the effPutCounterAll exotic pattern -- one Note
// naming the shape, nothing moves): CounterType$ Any (a choose-which-kind
// ask), ChoiceOptional$ without Choices$ (a malformed mid-resolution pick),
// UpTo$ (a bounded election), CounterNum$ Any, CounterNumShared$, and a
// TgtZone$ naming anything but the battlefield (the suspended-TIME-counter
// family), RememberAmount$
// (a removed NUMBER the remembered list has no honest channel for) and
// Optional$ (a may-remove election). Registering the API removed the generic
// "unimplemented API" fallback, so without these notes the shapes would
// silently remove nothing.
// One Note per Defined$-named OBJECT that is not on the battlefield (or no
// longer exists) instead of the silent skip r1 shipped: the corpus's
// RememberedLKI/Imprinted/ChosenCard defined sets can name a graveyard or
// exile card, the TgtZone$ gate above is the only sanctioned off-battlefield
// family, and swallowing a named target silently is an unledgered narrowing.
// The note records the skip loudly; nothing moves (removal off-battlefield
// stays a TgtZone$ task).
func effRemoveCounter(h Host, c *Ctx, sa *cards.SA) {
	// fx42 scoping: the answered bare-Choices$ pick rides the SHARED
	// "counter_pick" arm's fields (Ctx.CounterPick — the same transport
	// PutCounter's bare pick uses, since the option lists decode identically),
	// so capture and clear them at the very top, before any exotic check or
	// nested RemoveCounter in the same chain can see them.
	pickAns := c.CounterPick
	pickDone := c.CounterPickDone
	c.CounterPick, c.CounterPickDone = nil, false
	rp := RemoveCounterOf(sa)
	if !pickDone {
		// Once per call: the answered re-entry already noted on its first
		// pass.
		noteUnreadParams(h, c, "RemoveCounter", rp.Unread)
	}
	if rp.Choices != "" {
		// The card-election arm (counterchoice1): the 10 raw corpus
		// RemoveCounter lines carrying Choices$. Shapes this arm cannot
		// express stay loud inside removeCounterChoose.
		removeCounterChoose(h, c, sa, rp, pickAns, pickDone)
		return
	}
	if rp.ExoticNote != "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: rp.ExoticNote})
		return
	}
	kind := rp.Kind
	allKinds := rp.AllKinds
	numAll := rp.NumAll
	n := int32(1)
	if rp.NumSet && !numAll {
		n = numText(h, c, rp.CounterNum, 1)
		if n < 0 {
			n = 0
		}
	}
	kindArg := kind
	if allKinds {
		kindArg = "" // counterKinds: every kind the carrier holds
	}
	g := h.Game()
	for _, t := range DefinedRef(h, c, rp.Defined, sa) {
		if t.IsPlayer {
			p := PlayerOf(h, c, t)
			if int(p) < 0 || int(p) >= len(g.Players) {
				continue
			}
			pl := &g.Players[p]
			for _, k := range dedupeKinds(counterKinds(kindArg, len(pl.Counters), func(i int) string { return pl.Counters[i].Kind }, func(i int) int32 { return pl.Counters[i].N })) {
				count := pl.Counter(k)
				amt := n
				if numAll {
					amt = count
				}
				removed := amt
				if removed > count {
					removed = count
				}
				if removed <= 0 {
					continue
				}
				h.Emit(events.Event{Kind: events.PlayerCounterChange, Player: p, Counter: k, Amount: -removed})
				rememberRemoved(h, c, rp, state.PlayerRef(p), removed)
			}
			continue
		}
		o := g.Obj(t.Obj)
		if o == nil {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: fmt.Sprintf("RemoveCounter target %d no longer exists; skipped", t.Obj)})
			continue
		}
		// An exiled card keeps its counters (CR 122.2 -- a suspended card's
		// time counters, CR 702.62a), so removing one from it is real:
		// Greater Gargadon's own ActivationZone$ Exile ability, Jhoira's
		// Timebug-style "suspended card you own". Every other off-battlefield
		// zone holds no counters worth removing and stays a loud skip.
		if o.Zone != state.ZBattlefield && o.Zone != state.ZExile {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: fmt.Sprintf("RemoveCounter target %d is not on the battlefield (zone %s); skipped", o.ID, o.Zone)})
			continue
		}
		for _, k := range dedupeKinds(counterKinds(kindArg, len(o.Counters), func(i int) string { return o.Counters[i].Kind }, func(i int) int32 { return o.Counters[i].N })) {
			count := o.Counter(k)
			amt := n
			if numAll {
				amt = count
			}
			removed := amt
			if removed > count {
				removed = count
			}
			if removed <= 0 {
				continue
			}
			h.Emit(events.Event{Kind: events.CounterChange, Obj: o.ID, Counter: k, Amount: -removed})
			rememberRemoved(h, c, rp, o.ID, removed)
		}
	}
}

// removeCounterChoose runs the bare-Choices$ RemoveCounter card-election arm
// (counterchoice1; 10 raw corpus RemoveCounter lines carry Choices$). The
// CHOOSER (the resolving controller; every carrier) picks objects out of the
// Choices$ pool in ChoiceZone$ (default the battlefield; Amy Pond's and Mari
// the Killing Quill's triggers name Exile), and EACH chosen object loses
// CounterNum$ counters of CounterType$ (the shared Num grammar — Amy Pond's
// CounterNum$ X is the triggering damage amount). The answer re-enters
// through the shared "counter_pick" resume arm (Ctx.CounterPick), consumed
// and cleared by effRemoveCounter at its top (fx42).
//
// How many objects: ChoiceNum$-exact (default 1; Amy Pond, Mari the Killing
// Quill) unless ChoiceOptional$ True is present, which is Forge's "each of
// ANY number" — both reachable carriers (Garnet, Princess of Alexandria's
// "remove a lore counter from each of any number of Sagas you control" and
// Chandra, Legacy of Fire's [0] over "any number of permanents you control")
// pair it with NO ChoiceNum$, so the bound is 0..len(eligible) and a two-Saga
// board really can take BOTH. ChoiceOptional$ True WITH an explicit
// ChoiceNum$ reads as up-to-N (0..ChoiceNum$); no corpus line carries that
// pair today, so that branch is corpus-unreachable.
//
// RememberAmount$ True is the removed-COUNT transport (Garnet's
// SVar:X:Count$RememberedNumber payoff, Chandra's Z, Dyadrine, Synthesis
// Amalgam's DBDraw/DBToken ConditionCheckSVar$ Z): the chosen object's id is
// appended to Ctx.Remembered ONCE PER COUNTER REMOVED, exactly the encoding
// effMoveCounter's own RememberAmount$ rider uses, so Count$RememberedNumber
// reads the truthful total across every chosen object. It is orthogonal to
// RememberRemoved$, which writes the event-backed host list instead; a line
// carrying both gets both.
//
// The ask gate is the strict-supersets rule (a decision nobody could answer
// differently is never emitted): zero eligible objects acts on nothing, an
// exact count at or above the eligible count takes the deterministic first
// Max, and everything else — an Optional$ 0..1 over even ONE eligible object
// — is a real election. The no-host fallback (R-9) and botpolicy's
// "counter_pick" arm both take the first Max eligible in zone order, so a
// bot-answered ask emits the same events a silent build would have.
//
// Loud-and-unmodelled (one Note naming the shapes, nothing moves — the same
// exotic discipline the defined path keeps): CounterType$ Any/All (a
// which-kind pick stacked on the card election), CounterNum$ Any (a
// how-many-per-card ask), UpTo$/CounterNumShared$ (a divided total), and a
// ChoiceZone$ naming neither the battlefield nor exile.
func removeCounterChoose(h Host, c *Ctx, sa *cards.SA, rp *RemoveCounterParams, ans []state.ObjID, done bool) {
	kind := rp.Kind
	zone := rp.ChoiceZone
	if rp.ChoiceNote != "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: rp.ChoiceNote})
		return
	}
	if done {
		removeCounterPickApply(h, c, rp, zone, kind, numText(h, c, rp.CounterNum, 1), ans)
		return
	}
	g := h.Game()
	spec := rp.Choices
	var eligible []state.ObjID
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(zone, p) {
			if MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
				eligible = append(eligible, id)
			}
		}
	}
	num := numText(h, c, rp.CounterNum, 1)
	if num < 0 {
		num = 0
	}
	exact := numText(h, c, rp.ChoiceNum, 1)
	if exact < 0 {
		exact = 0
	}
	minv, maxv := exact, exact
	if rp.ChoiceOptional {
		// Forge's "each of any number": Min 0, and — with no explicit
		// ChoiceNum$ bounding it (both reachable carriers) — Max the whole
		// eligible pool, so Garnet's two Sagas can BOTH be chosen. An
		// explicit ChoiceNum$ alongside it keeps its own Max (up-to-N).
		minv = 0
		if !rp.ChoiceNumSet {
			maxv = int32(len(eligible))
		}
	}
	if maxv > int32(len(eligible)) {
		maxv = int32(len(eligible))
	}
	if minv > maxv {
		minv = maxv
	}
	fallback := func() {
		picks := eligible
		if int32(len(picks)) > maxv {
			picks = picks[:maxv]
		}
		removeCounterPickApply(h, c, rp, zone, kind, num, picks)
	}
	if len(eligible) == 0 || (minv == maxv && maxv >= int32(len(eligible))) {
		// Nothing eligible, or the forced exact set (the strict-supersets
		// rule): the deterministic act with no ask.
		fallback()
		return
	}
	d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose,
		Min:        int(minv),
		Max:        int(maxv),
		Source:     c.Source,
		ResumeKind: "counter_pick",
		ResumeSA:   sa,
		Prompt:     "Choose card(s) to remove " + kind + " counters from"}
	for _, id := range eligible {
		name := "a card"
		if o := g.Obj(id); o != nil && o.Face() != nil {
			name = o.Face().Name
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options),
			Kind: "counter_pick", Label: name, Obj: id, Player: c.Controller})
	}
	if ans, ok := AskTape(h, d); ok {
		// The "counter_pick" answer in hand: the re-entry's removal, with
		// the count read exactly as the re-entry reads it.
		removeCounterPickApply(h, c, rp, zone, kind, numText(h, c, rp.CounterNum, 1), counterAnswerObjs(ans))
		return
	}
	if Ask(h, d) == AskAsked {
		return // resolution suspended; the answer re-enters with Ctx.CounterPick set.
	}
	fallback()
}

// removeCounterPickApply removes num counters of kind from each live chosen
// object (a chosen object that left the choice zone while the decision was
// outstanding takes nothing — the same staleness stance putCounterPickApply
// takes), honouring the RememberRemoved$ rider through the shared helper and
// the RememberAmount$ rider through Ctx.Remembered (the effMoveCounter
// encoding: the object's id once per counter removed, so the chained
// Count$RememberedNumber payoff — Garnet's X, Chandra's Z, Dyadrine's
// ConditionCheckSVar$ Z — reads the real total).
func removeCounterPickApply(h Host, c *Ctx, rp *RemoveCounterParams, zone state.Zone, kind string, num int32, picks []state.ObjID) {
	g := h.Game()
	rememberAmount := rp.RememberAmount
	var amountIDs []state.ObjID
	for _, id := range picks {
		o := g.Obj(id)
		if o == nil || o.Zone != zone {
			continue
		}
		count := o.Counter(kind)
		removed := num
		if removed > count {
			removed = count
		}
		if removed <= 0 {
			continue
		}
		h.Emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: kind, Amount: -removed})
		rememberRemoved(h, c, rp, id, removed)
		for i := int32(0); i < removed; i++ {
			amountIDs = append(amountIDs, id)
		}
	}
	if rememberAmount && len(amountIDs) > 0 {
		c.Remembered = append(c.Remembered, objTargets(amountIDs)...)
	}
}

// rememberRemoved records RememberRemoved$'s persistent half: one Choose
// "remembered" event on the source object with the removed-from object's id
// repeated once per removed counter (events.Apply appends each id, so the
// source's event-backed Remembered -- Count$RememberedSize's read -- grows by
// exactly the removed count). A player entry rides the PlayerRef encoding
// rememberedFrom decodes. The ctx-level list is deliberately NOT touched:
// the only consumer measured for this rider reads the persistent list, and a
// ctx append would widen a chained Defined$ Remembered reader for free.
func rememberRemoved(h Host, c *Ctx, rp *RemoveCounterParams, id state.ObjID, removed int32) {
	if !rp.RememberRemoved || c.Source == 0 {
		return
	}
	ids := make([]state.ObjID, 0, removed)
	for i := int32(0); i < removed; i++ {
		ids = append(ids, id)
	}
	h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "remembered", IDs: ids})
}
