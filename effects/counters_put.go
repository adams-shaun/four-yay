package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func effPutCounter(h Host, c *Ctx, sa *cards.SA) {
	pc := PutCounterOf(sa)
	// fx42 scoping: consume and clear the answered Optional$ election at the
	// top, so a nested PutCounter in the same chain poses its own ask.
	optAns := c.PutOpt
	c.PutOpt = ""
	// Adapt$ (CR 702.35a; task param-adapt): an AB/DB$ PutCounter carrying
	// Adapt$ N reads N as the count -- Pteramander's `Adapt$ 4`, Jetfire's
	// chained `SVar:DBAdapt:DB$ PutCounter | Adapt$ 3`. The corpus writes only
	// literal values (measured: Adapt$ 1-4 over the 25 raw lines) and no
	// carrier pairs it with CounterNum$, so the read is a fallback, never a
	// competition. An unresolvable body degrades to 0, Num's convention.
	n := int32(1)
	if pc.CounterNumSet {
		n = numText(h, c, pc.CounterNum, 1)
	} else if pc.AdaptSet {
		n = numText(h, c, pc.Adapt, 1)
	} else if pc.MonstrositySet {
		// Monstrosity$ is a fallback count for its named counter placement.
		n = numText(h, c, pc.Monstrosity, 1)
	} else if pc.RenownSet {
		// Renown$ carries the CR 702.112 count and, like Monstrosity$, names
		// the counters placed by this keyword's resolving trigger.
		n = numText(h, c, pc.Renown, 1)
	}
	if n < 0 {
		n = 0
	}
	adapt := pc.AdaptSet
	mono := pc.MonstrositySet
	renown := pc.RenownSet
	// fx42 scoping: take every answered comma-list transport at entry and
	// clear it before this SA can resolve a sub-ability. Resolve shares one
	// Ctx across the chain, so leaving any of these live makes a nested
	// PutCounter reuse the outer kind instead of asking its own question.
	kindAns, kindDone := c.CounterKind, c.CounterKindDone
	kindsAns, kindsDone := append([]string(nil), c.CounterKinds...), c.CounterKindsDone
	kindAnswers := append([]string(nil), c.CounterKindAnswers...)
	kindAnswerIndex, kindAnswerSet := c.CounterKindAnswerIndex, c.CounterKindAnswerSet
	c.CounterKind, c.CounterKindDone = "", false
	c.CounterKinds, c.CounterKindsDone = nil, false
	c.CounterKindAnswers, c.CounterKindAnswerIndex, c.CounterKindAnswerSet = nil, 0, false
	kind := pc.Kind
	if placer := pc.Placer; placer != "" {
		if _, ok := putCounterPlacerFor(h, c, placer); !ok {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "PutCounter Placer$ unresolvable (" + placer + ")"})
			return
		}
	}
	if pc.OptionalTrue {
		switch {
		case optAns != "" && optAns != "yes":
			// Answered "no" (or any non-affirmative marker): the decline. No
			// counter is placed and no Note is emitted; the chained
			// SubAbility$ STILL RUNS -- the chain is owned by Resolve, not by
			// this body (the Attach.Optional precedent,
			// effects/attach.go:96-131; the chain-skip mechanism is the
			// DIFFERENT UnlessCost$ + UnlessResolveSubs$ pair, which none of
			// the corpus's Optional$ PutCounter lines carry). Black Widow's
			// "If you don't, ..." sub gates itself on its own Condition$ read
			// of the (empty) Remembered set, exactly as the oracle says.
			return
		case optAns == "":
			// Unanswered: pose the yes/no election -- but only when the put
			// would actually place something (at least one live recipient and
			// n > 0); with nothing legal to put on, decline and accept are the
			// same, so no ask (the Attach precedent's len(legal) == 0 gate).
			// The pickAnswered guard is the two-ask shape's own discipline:
			// an Optional$ bare-Choices$/DividedAsYouChoose$ SA asks TWICE
			// (election, then the recipient pick), and each resume builds a
			// FRESH Ctx -- the pick re-entry arrives with PutOpt already
			// consumed, so without this guard the election would re-pose over
			// the answered pick. The Done flag names the pick, never the
			// election: a pickDone pass is past the election by construction.
			pickAnswered := c.CounterPickDone || c.CounterDistDone
			if n > 0 && !pickAnswered && putCounterWouldPlace(h, c, sa) {
				d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
					Source: c.Source, ResumeKind: "put_optional", ResumeSA: sa,
					ResumeRemembered: copyTargets(c.Remembered),
					Prompt:           "Put a counter on it?",
					Options: []decision.Option{
						{Index: 0, Kind: "yes", Label: "Yes — put the counter", Player: c.Controller},
						{Index: 1, Kind: "no", Label: "No", Player: c.Controller},
					}}
				// AskAsked suspends; the answer re-enters with Ctx.PutOpt set.
				// AskNoHost is the deterministic decline stand-in (R-9) — the
				// same class the Attach election falls back to (the clamp-
				// answered bot path answers option 0 = "yes", so a bot game
				// stays byte-identical to the pre-ask silent always-put).
				_ = Ask(h, d)
				return
			}
			// optAns == "yes" (or nothing to place): fall through to the
			// ordinary placement paths.
		}
	}
	// Bolster$ (CR 701.36's bolster keyword action; 24 raw corpus lines, every
	// one of them a PutCounter SA -- SP/AB/DB heads alike -- so no separate
	// DB$ Bolster API is needed): "choose a creature with the least toughness
	// among creatures you control and put N +1/+1 counters on it." Bolster$
	// names N (a literal, or an SVar -- Sandsteppe War Riders' Bolster$ X);
	// the counter kind is the ordinary CounterType$ (default P1P1) computed
	// above. fx42 scoping: consume and clear the answered tie pick first, so
	// a nested PutCounter in the same chain cannot inherit it.
	if pc.Bolster.Present {
		pickAns := c.CounterPick
		pickDone := c.CounterPickDone
		c.CounterPick, c.CounterPickDone = nil, false
		putCounterBolster(h, c, sa, kind, pickAns, pickDone)
		return
	}
	// Support$ N (CR 701.41's support keyword action; 19 raw corpus lines,
	// every one a PutCounter SA): CR 701.41a -- "Support N" on a permanent
	// means "Put a +1/+1 counter on each of up to N OTHER target creatures";
	// on an instant or sorcery spell, "... on each of up to N target
	// creatures" (no "other" -- a spell resolving from the stack is not a
	// creature, so it could never be its own target anyway). ONE counter per
	// chosen creature; N is the TARGET COUNT (literal, X -- the announced X
	// of a spell with X in its cost -- or SVar, through the shared Num read),
	// never a per-creature count. The recipient pick is the counter_pick
	// decision shape (Min 0: "up to"), reusing the bare-Choices$ pick's
	// answer fields and resume arm; the default spec follows the CR 701.41a
	// split (putCounterSupport), or the SA's own Choices$ spec when it
	// carries one (no corpus support line does, measured). fx42 scoping:
	// consume and clear the answered pick first, so a nested PutCounter
	// below cannot inherit it.
	if pc.Support.Present {
		supAns := c.CounterPick
		supDone := c.CounterPickDone
		c.CounterPick, c.CounterPickDone = nil, false
		putCounterSupport(h, c, sa, kind, supAns, supDone)
		return
	}
	// DividedAsYouChoose$ (Vastwood Hydra's "you may distribute a number of
	// +1/+1 counters equal to the number of +1/+1 counters on CARDNAME among
	// any number of creatures you control", 54 raw corpus PutCounter lines):
	// the CounterNum$ TOTAL is divided among the recipients, not placed on
	// each. Two carrier shapes, split on where the recipients come from:
	// Choices$ names a mid-resolution battlefield pick bounded by
	// MinChoiceAmount$/ChoiceAmount$; without Choices$ the recipients are the
	// ordinary chosen targets (ValidTgts$, already asked by the targeting
	// machinery).
	divided := pc.Divided
	// fx45 scoping: capture and clear the answered Choices$ pick BEFORE the
	// branch, so a nested PutCounter below cannot inherit the outer answer
	// (the fx42 discipline every answered field follows).
	distAns := c.CounterDist
	distDone := c.CounterDistDone
	c.CounterDist, c.CounterDistDone = nil, false
	// The bare-Choices$ pick's answer rides its own pair of fields (the
	// divided family and the bare pick can never both ask for one SA, but
	// each consumes and clears only its own).
	pickAns := c.CounterPick
	pickDone := c.CounterPickDone
	c.CounterPick, c.CounterPickDone = nil, false
	if divided {
		if pc.Choices != "" {
			putCounterPickDistribute(h, c, sa, n, kind, distAns, distDone)
			return
		}
		placed := putCounterSplit(h, n, kind, Defined(h, c, sa))
		rememberPlaced(c, sa, placed)
		return
	}
	// ETB$ True (the K:etbCounter expansion's body, Wishclaw Talisman and
	// every "enters with N counters" card): the counters are placed on the
	// ENTERING object as it enters, so the target does not have to be a
	// settled battlefield permanent yet. The replacement machinery runs the
	// body after the entry move in the ordinary flow, but a body reached
	// while the object is still mid-entry must place the counters anyway,
	// not skip on the battlefield precondition.
	etb := pc.ETB
	// CounterType$ EachFromSource (task eachfromsource): the copy-each-kind
	// shape -- the target(s) take a counter of each kind the EachFromSource$
	// source carried. Not a real counter kind, so the ordinary loop below
	// would put nothing of a nonexistent kind; dispatch to the shape's own
	// walk. The referent is read HERE (not in the helper) so the parameter
	// census's static scan attributes the read to this primitive directly.
	if strings.EqualFold(strings.TrimSpace(kind), "EachFromSource") {
		putCounterEachFromSource(h, c, sa, etb, pc.EachFromSource)
		return
	}
	// CounterType$ comma lists are choices between counter kinds, never a
	// composite counter name. A bare Choices$ picks its recipient FIRST, then
	// asks for the individual kind while carrying that answered recipient on
	// the decision's continuation.
	counterKinds := pc.Kinds
	perKind := pc.CounterTypePerDefined
	if pc.Choices != "" {
		putCounterChoose(h, c, sa, n, kind, pickAns, pickDone, counterKinds, kindAns, kindDone)
		return
	}
	if len(counterKinds) > 1 {
		if pc.RandomType {
			eligible := make([]string, 0, len(counterKinds))
			for _, k := range counterKinds {
				seen := false
				for _, t := range Defined(h, c, sa) {
					if !t.IsPlayer {
						if o := h.Game().Obj(t.Obj); o != nil && o.Counter(k) > 0 {
							seen = true
						}
					}
				}
				if !seen {
					eligible = append(eligible, k)
				}
			}
			if len(eligible) == 0 {
				return
			}
			kind = eligible[h.Rand(len(eligible))]
			counterKinds = []string{kind}
		} else if pc.ChooseDifferent {
			if !kindsDone {
				d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 2, Max: 2, Source: c.Source, ResumeKind: "counter_kinds", ResumeSA: sa, ResumeRemembered: copyTargets(c.Remembered), Prompt: "Choose different counter kinds"}
				for i, k := range counterKinds {
					d.Options = append(d.Options, decision.Option{Index: i, Kind: "counter_kinds", Label: k, Player: c.Controller})
				}
				if Ask(h, d) == AskAsked {
					return
				}
				kindsAns = append([]string(nil), counterKinds[:2]...)
				kindsDone = true
			}
			counterKinds = append([]string(nil), kindsAns...)
		} else if perKind {
			// PerDefined asks independently below, once for each recipient.
		} else if !kindDone {
			d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1, Source: c.Source, ResumeKind: "counter_kind", ResumeSA: sa, ResumeRemembered: copyTargets(c.Remembered), Prompt: "Choose a counter kind"}
			for i, k := range counterKinds {
				d.Options = append(d.Options, decision.Option{Index: i, Kind: "counter", Label: k, Player: c.Controller})
			}
			if Ask(h, d) == AskAsked {
				return
			}
			kindAns = counterKinds[0]
			kindDone = true
		}
		if kindDone && !perKind {
			kind = kindAns
		}
	}
	// CounterNumPerDefined$ (task param-putcounter-counternumperdefined): the
	// count is evaluated PER AFFECTED OBJECT, not once for the resolving
	// source -- Canopy Gargantuan's upkeep trigger puts +1/+1 counters on
	// each other creature equal to THAT creature's toughness
	// (`SVar:X:Count$CardToughness` behind `CounterNumPerDefined$ X`). The
	// value is an SVar name resolved through the resolution's own table, or
	// an inline Count$ body; EvalCountOnObject re-anchors the source-anchored
	// heads on each recipient. A body the evaluator does not model degrades
	// to 0 for every recipient (Num's convention), so such a card places
	// nothing rather than something arbitrary. The corpus's three carriers
	// pair the param with none of the Optional$/Divided$/Choices$/Bolster
	// shapes above, so those keep the shared `n`.
	perDefExpr := ""
	if raw := pc.CounterNumPerDefined; raw != "" {
		perDefExpr = raw
		if body, ok := c.SVars[raw]; ok {
			perDefExpr = body
		}
	}
	var placed []state.Target
	for ti, t := range Defined(h, c, sa) {
		if perKind && len(counterKinds) > 1 {
			// A resumed later recipient must not replay CounterChange events
			// already emitted before its ask suspended the same SA.
			if kindAnswerSet && ti < kindAnswerIndex {
				continue
			}
			if ti >= len(kindAnswers) || kindAnswers[ti] == "" {
				d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1, Source: c.Source, ResumeKind: "counter_kind", ResumeSA: sa, ResumeTarget: ti, ResumeRemembered: copyTargets(c.Remembered), Prompt: "Choose a counter kind"}
				for i, k := range counterKinds {
					d.Options = append(d.Options, decision.Option{Index: i, Kind: "counter", Label: k, Player: c.Controller})
				}
				if Ask(h, d) == AskAsked {
					return
				}
				// The no-host fallback is the same first option botpolicy takes;
				// no persistent state is needed because it did not suspend.
				kindAnswers = append(kindAnswers, make([]string, ti-len(kindAnswers)+1)...)
				kindAnswers[ti] = counterKinds[0]
			}
			kind = kindAnswers[ti]
		}
		if t.IsPlayer {
			// A player target takes a PLAYER counter (energy's "you get {E}{E}{E}",
			// poison's "gets a poison counter"): the same instruction an object
			// target takes, but on the PlayerCounterChange event the engine's
			// player-counter state folds through. Skipping these (the pre-fix
			// behaviour) silently dropped the whole instruction -- the corpus
			// carries 156 player-targeted PutCounter lines.
			if p := PlayerOf(h, c, t); int(p) >= 0 && int(p) < len(h.Game().Players) {
				emitPutCounterChange(h, c, sa, events.Event{Kind: events.PlayerCounterChange, Player: p,
					Counter: kind, Amount: n})
				if n > 0 && pc.RememberPut {
					placed = append(placed, state.Target{Player: p, IsPlayer: true})
				}
			}
			continue
		}
		// CR 122.1: counters can exist on an object in ANY zone -- a
		// suspended card's time counters live on the exiled card (CR
		// 702.62a), and The Tenth Doctor's recalled permanent takes them in
		// exile. The gate was a battlefield-only precondition, which silently
		// dropped the whole instruction for a non-battlefield recipient (the
		// ETB$ True special case was the only tolerated exception); the
		// recipient is now whatever object the effect's own Defined$ referent
		// resolved, in whatever zone it currently sits. CounterChange folds
		// through events.Apply for any live object regardless of zone.
		o := h.Game().Obj(t.Obj)
		if o == nil {
			continue
		}
		// Adapt$'s put is itself conditional (CR 702.35a: "If this creature has
		// no +1/+1 counters on it, put N +1/+1 counters on it"). For an AB$
		// activation the offer-time gate (rules/legal.go's adaptGateOK) already
		// withheld the ability while counters were present, but counters can
		// arrive in response between activation and resolution -- the effect's
		// own if-condition, not the activation restriction, is what governs the
		// put. It is also the only gate a chained DB$ body (Jetfire's
		// "then adapt 3") ever gets, since an activation restriction does not
		// govern a resolution-time body.
		if adapt && o.Counter("P1P1") > 0 {
			continue
		}
		// CR 701.31b defense-in-depth: a monstrosity ability's activation is
		// gated once-only at offer time (rules/legal.go's monstrosityGateOK),
		// but the resolve-time read keeps an already-monstrous permanent from
		// taking a second batch through a path no offer gate covers (a
		// chained body, a future granted route). Corpus-unreachable today.
		if mono && o.Monstrous {
			continue
		}
		if renown && o.Renowned {
			continue
		}
		// CR 702.112a: the Renown trigger's "it" is the source PERMANENT --
		// "puts N +1/+1 counters on it and it becomes renowned". Once the
		// source has left the battlefield there is no "it": a combat-damage
		// trigger on the stack resolves even after instant-speed removal sent
		// its source to the graveyard (or hand/exile), and the ordinary loop
		// is deliberately zone-agnostic (CR 122.1), so without this gate the
		// departed card would take the counters and the designation in its
		// new zone. The gate is the mark's, not the trigger's: the ability
		// still resolves and its other riders (if any) are untouched; only
		// the counter batch and the Renowned designation fizzle with the
		// source. The corpus's only Renown$ carrier is the keyword expansion
		// body, which is never ETB$ True, so no mid-entry shape needs the
		// battlefield exception the ETB$ True special case tolerates.
		if renown && o.Zone != state.ZBattlefield {
			continue
		}
		amount := n
		if perDefExpr != "" {
			// The per-object amount: the affected object's own value. A player
			// target has no object to anchor on and keeps the shared `n` (no
			// corpus carrier pairs the param with a player target -- measured).
			amount = EvalCountOnObject(h, c, perDefExpr, o.ID)
			if amount < 0 {
				amount = 0
			}
		}
		if len(kindsAns) > 0 && kindsDone {
			for _, chosenKind := range kindsAns {
				emitPutCounterChange(h, c, sa, events.Event{Kind: events.CounterChange, Obj: o.ID, Counter: chosenKind, Amount: amount})
			}
		} else {
			emitPutCounterChange(h, c, sa, events.Event{Kind: events.CounterChange, Obj: o.ID, Counter: kind, Amount: amount})
		}
		// The mark (CR 701.31b: "...and it becomes monstrous"): one
		// AlterAttribute per placed object, emitted AFTER its counters so the
		// BecomeMonstrous triggers see the counters already landed. Amount is
		// the monstrosity COUNT -- Hydra Broodmaster's
		// `SVar:MonstrosityX:TriggerCount$Amount` reads the triggering event's
		// Amount -- and Player names the controller at mark time so the
		// trigger's referents bind it. Gated on the param's presence, so every
		// other PutCounter shape emits byte-identically; gated on n > 0, so a
		// body whose monstrosity amount resolves to 0 (Clay Golem's
		// `Monstrosity$ X` where X is a die result the unmodelled RollDice
		// cost token never publishes -- Num degrades it to 0) never emits a
		// mark and never fires its BecomeMonstrous trigger: the creature
		// never became monstrous, and a paid no-op must not Berserk.
		if mono && n > 0 {
			h.Emit(events.Event{Kind: events.AlterAttribute, Obj: o.ID,
				Player: o.Controller, Text: "Monstrous", Amount: n})
		}
		if renown && n > 0 {
			h.Emit(events.Event{Kind: events.AlterAttribute, Obj: o.ID,
				Player: o.Controller, Text: "Renowned", Amount: n})
		}
		// RememberPut$ (Synth Eradicator's DBEnergy) names the objects this
		// pass actually CounterChanged, never the attempt: a body whose count
		// resolves to zero (a `CounterNum$ X` the unmodelled cost token leaves
		// at 0, or a per-defined head degrading to 0) emits no positive
		// CounterChange, so it must not remember the recipient and must not let
		// a gated follow-up run as though a counter landed. The player branch
		// above carries the same n > 0 guard.
		if !t.IsPlayer && t.Obj != 0 && amount > 0 {
			placed = append(placed, t)
		}
	}
	rememberPlaced(c, sa, placed)
}

// putCounterEachFromSource runs the CounterType$ EachFromSource shape (task
// eachfromsource): the target(s) take a counter of each kind the
// EachFromSource$ source carried -- Forge's PutCounterEffect eachFromSource
// arm, the "put those counters on target permanent" copy. The corpus's 23
// carriers split on ONE axis: the referent (TriggeredCardLKICopy 19, Self 3,
// Remembered 1), so the source is resolved through the ordinary Defined
// referent machinery and the read never guesses at a fallback -- an unknown
// or missing referent fails closed to a loud Note and no counter.
//
// The source's counters are read through a three-rung LKI ladder (CR 603.10
// "look back in time"), because the usual case has the source's live
// counters already gone:
//  1. the source object's LIVE counters, when it holds a positive count
//     (a battlefield source -- Denry's Self, Blue Loyal Raptor's Self, a
//     clone origin -- or a token Remembered while it is still in play);
//  2. the trigger's pre-move LKI snapshot (Ctx.LKI) when it names this
//     object -- a permanent that LEFT the battlefield has had its live
//     counters cleared by Move's fold, so Resourceful Defense's
//     TriggeredCardLKICopy reads the snapshot;
//  3. the cost-sacrifice LKI snapshot (Ctx.Sacrificed) when it names this
//     object -- Zack Fair's Self was sacrificed as its own activation cost
//     (commitCast captured it at the instant of the sacrifice).
//
// Each kind's amount is its count times CounterNum$ (the multiplier, default
// 1; the corpus's one non-default is Blue Loyal Raptor's CounterNum$ 1),
// emitted as one CounterChange per kind per target -- the same one-event-per-
// kind discipline effMultiplyCounter uses, so the event stream records the
// real folds. Non-nil object targets are eligible in any zone;
// RememberCards$ still remembers what was actually countered.
func putCounterEachFromSource(h Host, c *Ctx, sa *cards.SA, etb bool, ref string) {
	g := h.Game()
	if ref == "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented PutCounter shape: CounterType$ EachFromSource with no EachFromSource$ referent"})
		return
	}
	srcs, ok := knownDefinedTargets(h, c, ref)
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented PutCounter shape: EachFromSource$ " + ref})
		return
	}
	// The CounterNum$ multiplier (default 1): present-but-unresolvable
	// degrades to 0 -- nothing is placed, the Num convention.
	mult := int32(1)
	if pc := PutCounterOf(sa); pc.CounterNumSet {
		mult = numText(h, c, pc.CounterNum, 1)
	}
	var placed []state.Target
	// SNAPSHOT FIRST, place second: a target that is also a source (the
	// Remembered destination of a death trigger whose remembered list is the
	// same one the TriggeredCardLKICopy referent reads -- Ambitious
	// Augmenter's Fractal token) must be read as it WAS, not re-read after an
	// earlier target's emission already countered it; the copy is of the
	// counters the source HAD (CR 603.10's look-back), never of the running
	// result.
	srcCounters := make([][]state.Counter, 0, len(srcs))
	for _, src := range srcs {
		if src.IsPlayer {
			srcCounters = append(srcCounters, nil)
			continue
		}
		srcCounters = append(srcCounters, eachFromSourceCounters(h, c, src.Obj))
	}
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue // no corpus carrier targets a player here
		}
		o := g.Obj(t.Obj)
		if o == nil {
			continue
		}
		anyPlaced := false
		for i, src := range srcs {
			if src.IsPlayer {
				continue
			}
			for _, k := range srcCounters[i] {
				if amt := k.N * mult; amt > 0 {
					h.Emit(events.Event{Kind: events.CounterChange, Obj: o.ID,
						Counter: k.Kind, Amount: amt})
					anyPlaced = true
				}
			}
		}
		// The same RememberPut$/RememberCards$ positivity contract the
		// ordinary target loop keeps: a source whose counters are all gone
		// (or a multiplier resolving to 0) places nothing, so the recipient
		// must not enter the remembered set.
		if anyPlaced {
			placed = append(placed, t)
		}
	}
	rememberPlaced(c, sa, placed)
}

// eachFromSourceCounters reads one source object's copied counters through
// the three-rung LKI ladder documented on putCounterEachFromSource, filtered
// to POSITIVE counts (state keeps a drained slot in the slice at N == 0, and
// a zero-count kind is not a kind the source "had").
func eachFromSourceCounters(h Host, c *Ctx, id state.ObjID) []state.Counter {
	g := h.Game()
	if o := g.Obj(id); o != nil {
		if ks := positiveCounters(o.Counters); len(ks) > 0 {
			return ks
		}
	}
	if c.LKI != nil && c.LKI.ID == id {
		if ks := positiveCounters(c.LKI.Counters); len(ks) > 0 {
			return ks
		}
	}
	for _, s := range c.Sacrificed {
		if s.Obj == id && len(s.Counters) > 0 {
			return s.Counters
		}
	}
	return nil
}

// positiveCounters copies the kinds a counter slice holds at a POSITIVE
// count, in slice order (deterministic; never a map walk).
func positiveCounters(cs []state.Counter) []state.Counter {
	var out []state.Counter
	for i := range cs {
		if cs[i].N > 0 {
			out = append(out, cs[i])
		}
	}
	return out
}

// targetCountersLKI answers the counters an OBJECT TARGET should be read as
// having when it has left the battlefield since targeting: CR 608.2b/h's
// last-known-information look-back. o is the live object (nil if it no longer
// exists). While the object is still a battlefield permanent its live
// counters are authoritative and ok is false; once it has left -- destroyed,
// sacrificed, exiled, bounced -- the departure-boundary snapshot supplies the
// counters (rules' Engine.emit captures them on the MoveZone that moves the
// target off the battlefield, immediately before the Move fold clears them;
// Resolve's entry capture is the fallback for a departure the host did not
// see), and ok is true. A missing entry fails closed to the live read, so a
// target this chain never captured is unchanged.
func targetCountersLKI(c *Ctx, id state.ObjID, o *state.Object) ([]state.Counter, bool) {
	if o != nil && o.Zone == state.ZBattlefield {
		return nil, false
	}
	if c == nil || c.TargetCountersLKI == nil {
		return nil, false
	}
	cs, ok := c.TargetCountersLKI[id]
	if !ok {
		return nil, false
	}
	return cs, true
}

// putCounterWouldPlace reports whether the put this SA describes would
// place at least one counter on a live recipient, mirroring the live-
// recipient conditions each placement path applies:
//   - the two Choices$ shapes (bare pick and DividedAsYouChoose$
//     distribute): the Choices$ pool must hold an eligible battlefield
//     object AND the ask's Max (ChoiceAmount$, default 1 for the bare pick,
//     the CounterNum$ total for the divided distribute) must be at least
//     one -- the same bounds putCounterChoose/putCounterPickDistribute
//     read, so the election is never posed over a pool the placement
//     would refuse;
//   - the plain target loop: a player target whose PlayerOf resolves, or
//     any existing object target (in any zone -- CR 122.1, the same
//     recipients the ordinary loop now places on; an ETB$ True mid-entry
//     object included, since it is a live object either way).
//
// It is a pure read: no event, no state change, replay-safe.
func putCounterWouldPlace(h Host, c *Ctx, sa *cards.SA) bool {
	g := h.Game()
	pc := PutCounterOf(sa)
	spec := pc.Choices
	if spec != "" {
		found := false
		for _, p := range g.AliveFrom(0) {
			for _, id := range g.Zone(state.ZBattlefield, p) {
				if MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
		defMax := int32(1)
		if pc.Divided {
			defMax = numText(h, c, pc.CounterNum, 1)
		}
		return numText(h, c, pc.ChoiceAmount, defMax) >= 1
	}
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			if p := PlayerOf(h, c, t); int(p) >= 0 && int(p) < len(h.Game().Players) {
				return true
			}
			continue
		}
		if o := g.Obj(t.Obj); o != nil {
			return true
		}
	}
	return false
}

// rememberPlaced folds the recipients a PutCounter pass just countered into
// the resolution's Remembered set, when the SA carries RememberCards$ True or
// RememberPut$ True. The
// flag names the cards that WERE countered, never the attempt: a pass that
// placed no counter remembers nothing. A RepeatEach loop's rememberIteration
// propagates what the iteration remembered into the loop's own set, so
// Promise of Loyalty's chained SacAllOthers (the SAME iteration) and its
// loop-tail DBEffect (RememberObjects$ Remembered) both see the vowed
// creatures without any event-backed persistence.
func rememberPlaced(c *Ctx, sa *cards.SA, placed []state.Target) {
	if len(placed) == 0 {
		return
	}
	if pc := PutCounterOf(sa); !pc.RememberCards && !pc.RememberPut {
		return
	}
	c.Remembered = append(c.Remembered, placed...)
}

// putCounterPickDistribute runs the Choices$ + DividedAsYouChoose$ shape: the
// recipients are a battlefield pick over the Choices$ filter, bounded by
// MinChoiceAmount$ (the ask's Min) and ChoiceAmount$ (the ask's Max, default
// the CounterNum$ total), and the CounterNum$ total is then divided among the
// chosen recipients.
//
// The recipient SET is a real KChoose ask whenever more than one eligible
// creature exists and a smaller-than-all set is legal (MinChoiceAmount$ below
// the eligible count): the strict-supersets gate every asking primitive here
// follows -- a set the rules force (Min == Max == the eligible count) or a
// single-eligible board leaves nothing to choose, so no decision is posed and
// the split runs over the only legal recipient list.
//
// The DIVISION among the chosen recipients is the deterministic stand-in the
// damage primitive's DividedAsYouChoose$ already ships (effects/damage.go):
// one counter at a time, round-robin in the player's answer order, so the
// earlier-chosen recipients take the extras. A per-counter division ask (the
// repeated one-pick ask Forge's UI models by clicking) is not posed.
func putCounterPickDistribute(h Host, c *Ctx, sa *cards.SA, total int32, kind string, ans []state.ObjID, ansDone bool) {
	if total <= 0 {
		return
	}
	g := h.Game()
	pc := PutCounterOf(sa)
	spec := pc.Choices
	if ansDone {
		// Re-entry: the answered pick, in answer order. A recipient that left
		// the battlefield while the decision was outstanding takes nothing
		// (its share is lost, not redistributed -- the same totality stance
		// the target-based split takes).
		placed := putCounterSplit(h, total, kind, objTargets(ans))
		rememberPlaced(c, sa, placed)
		return
	}
	var eligible []state.ObjID
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			if MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
				eligible = append(eligible, id)
			}
		}
	}
	minCh := numText(h, c, pc.MinChoiceAmount, 0)
	if minCh < 0 {
		minCh = 0
	}
	maxCh := numText(h, c, pc.ChoiceAmount, total)
	if maxCh < 0 {
		maxCh = 0
	}
	if maxCh > int32(len(eligible)) {
		maxCh = int32(len(eligible))
	}
	if minCh > maxCh {
		minCh = maxCh
	}
	// The no-choice fallbacks share one deterministic recipient list: the
	// first maxCh eligible creatures in zone order -- for a forced set
	// (minCh >= the eligible count) that IS the only legal answer, and for
	// the fuzz/no-host run (R-9) it is the exact mirror of botpolicy's
	// "counter_dist" arm, so a bot-answered ask emits the same events the
	// silent build did.
	fallback := func() {
		picks := eligible
		if int32(len(picks)) > maxCh {
			picks = picks[:maxCh]
		}
		placed := putCounterSplit(h, total, kind, objTargets(picks))
		rememberPlaced(c, sa, placed)
	}
	// Ask gate: a real recipient choice needs two or more eligible creatures
	// room to differ (maxCh >= 1 leaves at least one recipient; minCh below
	// the eligible count leaves a smaller set legal).
	if len(eligible) < 2 || maxCh < 1 || minCh >= int32(len(eligible)) {
		fallback()
		return
	}
	d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose,
		Min:        int(minCh),
		Max:        int(maxCh),
		Source:     c.Source,
		ResumeKind: "counter_dist",
		ResumeSA:   sa,
		Prompt:     "Distribute " + kind + " counter(s): choose where to place " + strconv.Itoa(int(total))}
	for _, id := range eligible {
		name := "a creature"
		if o := g.Obj(id); o != nil && o.Face() != nil {
			name = o.Face().Name
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options),
			Kind: "counter_dist", Label: name, Obj: id, Player: c.Controller})
	}
	if Ask(h, d) == AskAsked {
		return // resolution suspended; the answer re-enters with Ctx.CounterDist set.
	}
	fallback()
}

// putCounterSplit divides the CounterNum$ total among the recipients
// round-robin in recipient order (the deterministic division stand-in) and
// emits one CounterChange per recipient with its share. Recipients that are
// no longer on the battlefield take nothing; the total is exact (every
// counter lands somewhere or is lost with a departed recipient, never
// invented).
func putCounterSplit(h Host, total int32, kind string, ts []state.Target) []state.Target {
	if total <= 0 {
		return nil
	}
	g := h.Game()
	var live []state.ObjID
	for _, t := range ts {
		if t.IsPlayer {
			continue
		}
		if o := g.Obj(t.Obj); o != nil && o.Zone == state.ZBattlefield {
			live = append(live, t.Obj)
		}
	}
	if len(live) == 0 {
		return nil
	}
	shares := make(map[state.ObjID]int32, len(live))
	for i := int32(0); i < total; i++ {
		shares[live[i%int32(len(live))]]++
	}
	var placed []state.Target
	for _, id := range live {
		if amt := shares[id]; amt > 0 {
			h.Emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: kind, Amount: amt})
			placed = append(placed, state.Target{Obj: id})
		}
	}
	return placed
}

func objTargets(ids []state.ObjID) []state.Target {
	out := make([]state.Target, 0, len(ids))
	for _, id := range ids {
		if id != 0 {
			out = append(out, state.Target{Obj: id})
		}
	}
	return out
}

// putCounterChoose runs the bare-Choices$ PutCounter pick shape (no
// DividedAsYouChoose$ — task vow1; Promise of Loyalty's vow, mikey_mona's
// "target player chooses a creature they control and puts two +1/+1 counters
// on it", Haphazard Bombardment's four aim counters): the CHOOSER
// (Chooser$, default the resolving controller) picks
// MinChoiceAmount$..ChoiceAmount$ (default 1..1) battlefield objects out of
// the Choices$ pool, and EACH chosen object takes the full CounterNum$
// counters. The answer re-enters through ResumeKind "counter_pick" with
// Ctx.CounterPick; RememberCards$ True remembers the countered objects (the
// vow chain's SacAllOthers and DBEffect read them in the same walk).
//
// The ask gate is the strict-supersets rule every asking primitive here
// follows (a decision nobody could answer differently is never emitted): a
// pool with fewer than two eligible objects, a Max below one, or a Min at or
// above the eligible count leaves only the deterministic first-Max answer,
// so the placement runs without an ask. Promise of Loyalty's "each player
// chooses ONE creature" asks exactly when that player controls two or more
// eligible creatures.
func putCounterChoose(h Host, c *Ctx, sa *cards.SA, n int32, kind string, ans []state.ObjID, done bool, kinds []string, kindAns string, kindDone bool) {
	if n <= 0 {
		return
	}
	g := h.Game()
	pc := PutCounterOf(sa)
	spec := pc.Choices
	if done {
		putCounterChooseApply(h, c, sa, n, kind, ans, kinds, kindAns, kindDone)
		return
	}
	chooser, ok := putCounterChooserFor(h, c, strings.TrimSpace(pc.Chooser))
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "PutCounter Chooser$ unresolvable (" + pc.Chooser + ")"})
		return
	}
	var eligible []state.ObjID
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			if MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
				eligible = append(eligible, id)
			}
		}
	}
	minCh := numText(h, c, pc.MinChoiceAmount, 1)
	if minCh < 0 {
		minCh = 0
	}
	maxCh := numText(h, c, pc.ChoiceAmount, 1)
	if maxCh < 0 {
		maxCh = 0
	}
	if maxCh > int32(len(eligible)) {
		maxCh = int32(len(eligible))
	}
	if minCh > maxCh {
		minCh = maxCh
	}
	// The no-choice fallback shares one deterministic pick list with the
	// divided sibling: the first maxCh eligible objects in zone order -- for
	// a forced set (minCh >= the eligible count) that IS the only legal
	// answer, and for the fuzz/no-host run (R-9) it is the exact mirror of
	// botpolicy's "counter_pick" arm, so a bot-answered ask emits the same
	// events the silent build did.
	fallback := func() {
		picks := eligible
		if int32(len(picks)) > maxCh {
			picks = picks[:maxCh]
		}
		// A deterministic recipient is not a deterministic counter kind.
		// Route it through the same continuation as an answered recipient
		// selection so a comma list still gets its own real election.
		putCounterChooseApply(h, c, sa, n, kind, picks, kinds, kindAns, kindDone)
	}
	if len(eligible) < 2 || maxCh < 1 || minCh >= int32(len(eligible)) {
		fallback()
		return
	}
	d := &decision.Decision{Player: chooser, Kind: decision.KChoose,
		Min:        int(minCh),
		Max:        int(maxCh),
		Source:     c.Source,
		ResumeKind: "counter_pick",
		ResumeSA:   sa,
		Prompt:     pc.ChoiceTitle}
	for _, id := range eligible {
		name := "a creature"
		if o := g.Obj(id); o != nil && o.Face() != nil {
			name = o.Face().Name
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options),
			Kind: "counter_pick", Label: name, Obj: id, Player: chooser})
	}
	if Ask(h, d) == AskAsked {
		return // resolution suspended; the answer re-enters with Ctx.CounterPick set.
	}
	fallback()
}

// putCounterChooseApply resolves the counter-kind half of a bare Choices$
// PutCounter. It is shared by an answered recipient election and a forced
// recipient set: only the recipient can be deterministic; a comma list is
// always a real counter-kind choice.
func putCounterChooseApply(h Host, c *Ctx, sa *cards.SA, n int32, kind string, picks []state.ObjID, kinds []string, kindAns string, kindDone bool) {
	if len(kinds) > 1 && !kindDone {
		d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose,
			Min: 1, Max: 1, Source: c.Source, ResumeKind: "counter_kind",
			ResumeSA: sa, ResumeChoices: objTargets(picks),
			ResumeRemembered: copyTargets(c.Remembered), Prompt: "Choose a counter kind"}
		for i, k := range kinds {
			d.Options = append(d.Options, decision.Option{Index: i, Kind: "counter", Label: k, Player: c.Controller})
		}
		if Ask(h, d) == AskAsked {
			return
		}
		kindAns = kinds[0]
		kindDone = true
	}
	if kindDone {
		kind = kindAns
	}
	// A chosen creature that left while the decision was outstanding takes
	// nothing (the same totality stance the divided sibling takes).
	putCounterPickApply(h, c, sa, n, kind, picks)
}

// putCounterPickApply places CounterNum$ counters on each live chosen object
// and, when the SA carries RememberCards$ True, remembers exactly the ones
// that took a counter.
func putCounterPickApply(h Host, c *Ctx, sa *cards.SA, n int32, kind string, picks []state.ObjID) {
	g := h.Game()
	var placed []state.Target
	for _, id := range picks {
		o := g.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		emitPutCounterChange(h, c, sa, events.Event{Kind: events.CounterChange, Obj: id, Counter: kind, Amount: n})
		// RememberCards$/RememberPut$ name the recipients that actually took
		// a counter: a CounterNum$ resolving to 0 places nothing (the event
		// is a zero CounterChange), so it must not be remembered.
		if n > 0 {
			placed = append(placed, state.Target{Obj: id})
		}
	}
	rememberPlaced(c, sa, placed)
}

// putCounterBolster implements PutCounter's Bolster$ parameter (CR 701.36's
// bolster keyword action): the bolstering player -- the resolving controller;
// every corpus carrier bolsters "creatures you control" -- chooses a creature
// with the LEAST toughness among their creatures and puts N +1/+1 counters
// on it. The toughness read is the engine's effects-side P/T convention
// (face toughness plus P1P1 counters, the Count$CardToughness read --
// layer-derived toughness is not visible below rules). A tie at the minimum
// is a real election (CR 701.36's "choose"): a KChoose over the tied
// creatures answered through the shared "counter_pick" resume arm
// (Ctx.CounterPick/CounterPickDone, consumed and cleared by the caller --
// fx42); botpolicy's "counter_pick" arm takes the first option, so a
// bot-answered game emits the same events the R-9 no-host fallback (the
// first tied creature in zone order) does. RememberCards$ True rides
// putCounterPickApply, exactly like the bare Choices$ pick.
func putCounterBolster(h Host, c *Ctx, sa *cards.SA, kind string, ans []state.ObjID, done bool) {
	n := numText(h, c, PutCounterOf(sa).Bolster, 1)
	if n < 0 {
		n = 0
	}
	g := h.Game()
	var cands []state.ObjID
	best := int32(0)
	for _, id := range g.Zone(state.ZBattlefield, c.Controller) {
		o := g.Obj(id)
		if o == nil || o.Face() == nil || !hasType(o, "Creature") {
			continue
		}
		// The election reads the object's net toughness from its counters of
		// EVERY P/T kind (CR 122.1a/613.7d): a -0/-1 or -1/-1 counter shrinks
		// the candidate just as a +1/+1 one grows it.
		_, dt := o.CounterPTTotals()
		t := int32(o.Face().Toughness()) + dt
		if len(cands) == 0 || t < best {
			best, cands = t, []state.ObjID{id}
			continue
		}
		if t == best {
			cands = append(cands, id)
		}
	}
	if n == 0 || len(cands) == 0 {
		// Nothing to place or nobody to place it on: bolstering zero is a
		// no-op (the election and the decline would place the same thing).
		return
	}
	if len(cands) == 1 {
		putCounterPickApply(h, c, sa, n, kind, cands)
		return
	}
	if done {
		// Re-entry: the answered tie pick, applied as-is (a chosen creature
		// that left the battlefield while the decision was outstanding takes
		// nothing -- putCounterPickApply's zone guard).
		putCounterPickApply(h, c, sa, n, kind, ans)
		return
	}
	d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose,
		Min: 1, Max: 1, Source: c.Source,
		ResumeKind: "counter_pick", ResumeSA: sa,
		Prompt: "Bolster " + strconv.Itoa(int(n)) + " — choose a creature with the least toughness"}
	for _, id := range cands {
		name := "a creature"
		if o := g.Obj(id); o != nil && o.Face() != nil {
			name = o.Face().Name
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options),
			Kind: "counter_pick", Label: name, Obj: id, Player: c.Controller})
	}
	if Ask(h, d) == AskAsked {
		return // resolution suspended; the answer re-enters with Ctx.CounterPick set.
	}
	// The R-9 no-host fallback: the first tied creature in zone order -- the
	// exact mirror of botpolicy's "counter_pick" first-option answer.
	putCounterPickApply(h, c, sa, n, kind, cands[:1])
}

// putCounterChooserFor resolves one Chooser$/Placer$ value of the bare-
// Choices$ PutCounter pick to the player who answers. The allowlist covers
// every spelling the corpus's six Chooser$ carriers use plus the obvious
// defaults; anything else fails closed (nil, false) — a chooser is never
// guessed, because asking the WRONG player would record a choice nobody
// made. The Remembered-backed spellings read the RESOLUTION's Remembered
// player entries: inside a RepeatEach iteration the loop subject is exactly
// that entry (Promise of Loyalty's Player.IsRemembered, Eye of Doom's bare
// Remembered), falling back to the source's event-backed list the same way
// the Player.IsRemembered Defined selector does.
// putCounterSupport implements the Support$ N branch of effPutCounter: the
// "up to N target creatures, one counter each" pick. The choice reuses the
// bare-Choices$ pick's decision shape (ResumeKind "counter_pick", the
// CounterPick/CounterPickDone answer fields, the same resume arm and
// botpolicy arm), but its Max is the SUPPORT value and its Min is always 0
// -- "up to" -- so a decline is a real answer. The default spec encodes CR
// 701.41a verbatim: on a PERMANENT source (the ETB/ability carriers) it is
// "Creature.+Other" -- every battlefield creature other than the resolving
// source, the "other" the rule itself states and every creature-ETB reminder
// text repeats (Generous Patron's reminder: "up to two other target
// creatures") -- while on an instant/sorcery SPELL source (Lead by Example,
// Unity of Purpose) it is bare "Creature" (every creature). The spell half's
// difference is vacuous on this battlefield-only scan -- a spell resolving
// from the stack is never a battlefield creature -- but the split is encoded
// so the rule lives where the spec is built, not in a reminder text. The
// SA's own Choices$ spec (no corpus carrier has one, measured) overrides
// both. One counter of the SA's kind per chosen creature.
func putCounterSupport(h Host, c *Ctx, sa *cards.SA, kind string, ans []state.ObjID, done bool) {
	if done {
		// Re-entry: the answered pick, in answer order. A chosen creature
		// that left the battlefield while the decision was outstanding takes
		// nothing (putCounterPickApply's zone guard).
		putCounterPickApply(h, c, sa, 1, kind, ans)
		return
	}
	pc := PutCounterOf(sa)
	maxT := numText(h, c, pc.Support, 1)
	if maxT < 0 {
		maxT = 0
	}
	if maxT == 0 {
		return
	}
	g := h.Game()
	spec := pc.Choices
	if spec == "" {
		// CR 701.41a's permanent/spell split (see the doc comment above).
		spec = "Creature.+Other"
		if o := g.Obj(c.Source); o != nil && o.Face() != nil &&
			(o.Face().IsInstant() || o.Face().IsSorcery()) {
			spec = "Creature"
		}
	}
	var eligible []state.ObjID
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			if MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
				eligible = append(eligible, id)
			}
		}
	}
	if len(eligible) == 0 {
		return
	}
	max := maxT
	if max > int32(len(eligible)) {
		max = int32(len(eligible))
	}
	d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose,
		Min:        0,
		Max:        int(max),
		Source:     c.Source,
		ResumeKind: "counter_pick",
		ResumeSA:   sa,
		Prompt:     pc.ChoiceTitle}
	for _, id := range eligible {
		name := "a creature"
		if o := g.Obj(id); o != nil && o.Face() != nil {
			name = o.Face().Name
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options),
			Kind: "counter_pick", Label: name, Obj: id, Player: c.Controller})
	}
	if Ask(h, d) == AskAsked {
		return // resolution suspended; the answer re-enters with Ctx.CounterPick set.
	}
	// The no-host stand-in (R-9): the first max eligible creatures in zone
	// order -- the exact mirror of botpolicy's "counter_pick" arm, so a
	// bot-answered ask emits the same placement events the silent fallback
	// would.
	picks := eligible
	if int32(len(picks)) > max {
		picks = picks[:max]
	}
	putCounterPickApply(h, c, sa, 1, kind, picks)
}

func putCounterPlacerFor(h Host, c *Ctx, v string) (state.PlayerID, bool) {
	g := h.Game()
	switch v {
	case "Controller":
		return c.Controller, true
	case "Owner":
		if o := g.Obj(c.Source); o != nil {
			return o.Owner, true
		}
		return 0, false
	case "TriggeredSource":
		if o := g.Obj(c.TriggerSource); o != nil {
			return o.Controller, true
		}
		return 0, false
	case "TriggeredSourceController":
		if o := g.Obj(c.TriggerSource); o != nil {
			return o.Controller, true
		}
		return 0, false
	}
	return putCounterChooserFor(h, c, v)
}

// emitPutCounterChange publishes the Placer$ role only while this event is
// folded. The event format remains unchanged; the engine's existing in-flight
// counter-adder channel is consumed synchronously by replacement and trigger
// matching, and replay re-executes this setter around the same emission.
func emitPutCounterChange(h Host, c *Ctx, sa *cards.SA, ev events.Event) {
	if placer := PutCounterOf(sa).Placer; placer != "" {
		if p, ok := putCounterPlacerFor(h, c, placer); ok {
			previous := h.SetCounterAdder(p)
			h.Emit(ev)
			h.SetCounterAdder(previous)
			return
		}
	}
	h.Emit(ev)
}

func putCounterChooserFor(h Host, c *Ctx, v string) (state.PlayerID, bool) {
	g := h.Game()
	firstRememberedPlayer := func() (state.PlayerID, bool) {
		for _, t := range c.Remembered {
			if t.IsPlayer {
				return t.Player, true
			}
		}
		if o := g.Obj(c.Source); o != nil {
			for _, t := range o.Remembered {
				if t.IsPlayer {
					return t.Player, true
				}
			}
		}
		return 0, false
	}
	firstChosenPlayer := func() (state.PlayerID, bool) {
		for _, t := range c.Chosen {
			if t.IsPlayer {
				return t.Player, true
			}
		}
		if o := g.Obj(c.Source); o != nil {
			for _, t := range o.Chosen {
				if t.IsPlayer {
					return t.Player, true
				}
			}
		}
		return 0, false
	}
	switch v {
	case "", "You", "True":
		return c.Controller, true
	case "Player.IsRemembered", "Remembered", "RememberedController":
		return firstRememberedPlayer()
	case "ChosenPlayer", "Player.Chosen":
		return firstChosenPlayer()
	case "TriggeredPlayer":
		if c.TriggerPlayer.IsPlayer {
			return c.TriggerPlayer.Player, true
		}
		return 0, false
	case "ThisTargetedPlayer", "TargetedPlayer", "Targeted":
		for _, t := range c.Targets {
			if t.IsPlayer {
				return t.Player, true
			}
			if o := g.Obj(t.Obj); o != nil {
				return o.Controller, true
			}
		}
		return 0, false
	}
	return 0, false
}
