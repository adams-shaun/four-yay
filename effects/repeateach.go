package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// api:RepeatEach's resolution (effRepeatEach and its subject, ordering and
// election helpers), moved out of choose_control.go so the leak ratchet can
// hold it to zero parameter reads: compileRepeatEach (repeateach_params.go)
// is the one reader of a RepeatEach ability's parameters. The loop helpers
// other primitives share (iterationBase, rememberIteration, repeatPlayers)
// stay in choose_control.go.

// repeatedCards is a card loop's subject list: the DefinedCards$ selection
// when the ability names one, else every RepeatCards$ match in the compiled
// Zone$ set, scanned zone by zone in a fixed order and seat order from the
// controller. ok is false when neither names the subjects.
func repeatedCards(h Host, c *Ctx, rp *RepeatEachParams) ([]state.Target, bool) {
	if spec := rp.DefinedCards; spec != "" {
		switch repeatedCards4841Codes.Code(string(strings.Split(spec, ".")[0])) {
		case repeatedCards4841Targeted:
			return objectsOf(c.Targets), true
		case repeatedCards4841Remembered:
			return objectsOf(c.Remembered), true
		case repeatedCards4841ChosenCard:
			return objectsOf(c.Chosen), true
		}
		return objectsOf(DefinedSpec(h, c, spec)), true
	}
	spec := rp.Cards
	if spec == "" {
		return nil, false
	}
	var out []state.Target
	for _, z := range [...]state.Zone{state.ZBattlefield, state.ZHand, state.ZLibrary, state.ZGraveyard, state.ZExile, state.ZStack} {
		if !rp.Zones.Has(z) {
			continue
		}
		players := h.Game().AliveFrom(c.Controller)
		if z == state.ZStack { // the stack is one shared zone, not one per seat
			players = []state.PlayerID{0}
		}
		for _, p := range players {
			for _, id := range h.Game().Zone(z, p) {
				if o := h.Game().Obj(id); o != nil && choiceMatches(h, h.Game(), c, spec, o) {
					out = append(out, state.Target{Obj: id})
				}
			}
		}
	}
	return out, true
}

func effRepeatEach(h Host, c *Ctx, sa *cards.SA) {
	if c.SVars == nil {
		return
	}
	rp := RepeatEachOf(sa)
	sub := cards.ResolveSVar(c.SVars, rp.SubAbility)
	if sub == nil {
		return
	}
	var subjects []state.Target
	start := 0
	// DamageMap$ True (Price of Progress, Wing Storm, Baki's Curse -- 87
	// corpus files): the loop's damage is ONE damage batch. Forge accumulates
	// every iteration's dealDamage into a per-SA damage table and deals it
	// once after the loop (RepeatEachEffect.resolve's DamageMap halves); in
	// this build that is the existing damage-batch bracket -- opened around
	// the whole loop, closed after the last iteration -- so the loop's
	// DamageDealtOnce triggers latch once per batch instead of once per
	// iteration's own batch-of-one. The deal sites stay inside the body (each
	// DealDamage's own bracket nests inside this one; the batch is depth-
	// counted), and the events themselves are unchanged -- same order, same
	// amounts -- so a game without a batch-latched trigger replays exactly as
	// before. Opened only on the first pass: a mid-loop suspension leaves the
	// engine's open batch intact across the resume, and the re-entry pass
	// closes it when the loop completes, so the bracket is balanced however
	// many resumes interleave.
	batched := rp.DamageMap
	// ChangeZoneTable$ True (Forge's RepeatEachEffect CardZoneTable -- 47
	// corpus carrier files): the zone changes every iteration's body causes
	// are accumulated and reach Mode$ ChangesZoneAll ONCE, after the loop
	// completes, as one "one or more" batch; Mode$ ChangesZone keeps firing
	// per move. The seam is the zone twin of the damage bracket above:
	// opened around the whole loop on the first pass, closed after the last
	// iteration, events unchanged -- same order, same objects -- so a game
	// with no ChangesZoneAll observer in the window replays exactly as
	// before. Opened only on the first pass: a mid-loop suspension leaves
	// the engine's open batch intact across the resume, and the re-entry
	// pass closes it when the loop completes, so the bracket is balanced
	// however many resumes interleave.
	zoneTable := rp.ChangeZoneTable
	// AmountFromVotes$ True (task votepb1: Mob Verdict, Círdan the Shipwright,
	// Trap the Trespassers): before each body runs, bind the reserved name
	// "Votes" to the CURRENT loop subject's tally from the most recent
	// api:Vote (Ctx.VoteCounts). It is Forge's RepeatEachEffect.setVoteAmount
	// -- `sa.setSVar("Votes", saVote.getSVar("VoteNum" + o))` -- which is why
	// the loop's own count is untouched: the body's NumCards$ Votes /
	// CounterNum$ Votes / NumDmg$ SVar$Votes/Times.2 reads size themselves,
	// and a subject with no tally binds 0 rather than a stale SVar.
	fromVotes := rp.AmountFromVotes
	var batcher interface {
		BeginDamageBatch()
		EndDamageBatch()
	}
	if b, ok := h.(interface {
		BeginDamageBatch()
		EndDamageBatch()
	}); ok {
		batcher = b
	}
	var zoneBatcher interface {
		BeginZoneBatch()
		EndZoneBatch()
	}
	if z, ok := h.(interface {
		BeginZoneBatch()
		EndZoneBatch()
	}); ok {
		zoneBatcher = z
	}
	// Once per call, on the first pass: a resumed loop already noted.
	noteUnreadParams(h, c, "RepeatEach", rp.Unread)
	var ok bool
	// cardsSubjects is true only when the subjects came from Forge's
	// repeatCards list (RepeatCards$/DefinedCards$): ChooseOrder$ orders
	// that list and only that list. The RepeatPlayers$,
	// RepeatSpellAbilities$ and RepeatTargeted$ loops are never ordered in
	// Forge, so the ask is gated on this flag.
	var cardsSubjects bool
	switch {
	case rp.Players != "":
		var ps []state.PlayerID
		ps, ok = repeatPlayers(h, c, rp.Players)
		for _, p := range ps {
			subjects = append(subjects, state.Target{Player: p, IsPlayer: true})
		}
	case rp.SpellAbilities != "":
		subjects, ok = validStackTargets(h.Game(), rp.SpellAbilities, c), true
	case rp.Targeted:
		subjects, ok = copyTargets(c.Targets), true
	default:
		subjects, ok = repeatedCards(h, c, rp)
		cardsSubjects = true
	}
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "RepeatEach selector unimplemented"})
		return
	}
	// The loop's first-pass setup runs HERE, before the ChooseOrder$ ask
	// below: the answered ask re-enters the SA with a Repeat cursor, so
	// this else is never taken again and any first-pass-only step that
	// stayed after the ask would never run for a hosted ordering loop.
	// Measured otherwise-broken carrier: Ezuri's Predation carries BOTH
	// ChooseOrder$ and ChangeZoneTable$ -- with the zone bracket opened
	// after the ask, its ChangesZoneAll batching silently became
	// per-move. The clear is likewise ordered before the ask so the ask's
	// suspension (and thus the re-entered loop) binds the post-clear
	// Remembered.
	// ClearRememberedBeforeLoop$ True (Forge's RepeatEachEffect: "clear the
	// host's remembered list before the loop"): drop the resolving spell or
	// ability's accumulated Remembered before the FIRST iteration body runs,
	// so a chain's earlier remembered players/cards do not leak into the
	// loop's iterations. Corpus carriers: Seize the Spotlight (clear the
	// GenericChoice's remembered choosers before walking the notated players),
	// Master of Ceremonies, Enter the Dungeon, Shahrazad. It is applied ONCE,
	// on the first pass only: a resume after a mid-loop suspension must keep
	// what the completed iterations remembered. It is applied AFTER the
	// subject selector resolves, so `RepeatPlayers$ Remembered` (a real
	// selector in the corpus) still sees the remembered set it names -- the
	// clear is a loop-hygiene bound on the iteration bodies, not on the
	// loop's own subject derivation.
	if rp.ClearRemembered {
		c.Remembered = nil
	}
	// The damage/zone brackets open around the WHOLE loop (see the
	// DamageMap$/ChangeZoneTable$ comments above for the Forge semantics).
	// Opened only here, on the first pass -- a mid-loop suspension (the
	// ordering ask included) leaves the engine's open batch intact across
	// the resume, and the re-entry pass closes it when the loop completes,
	// so the bracket is balanced however many resumes interleave.
	if batched && batcher != nil {
		batcher.BeginDamageBatch()
	}
	if zoneTable && zoneBatcher != nil {
		zoneBatcher.BeginZoneBatch()
	}
	// ChooseOrder$ (Forge RepeatEachEffect.resolve): when the repeatCards
	// list has more than one entry, the chooser orders it BEFORE the loop
	// runs, and the loop then processes that order. `True` means the
	// resolving controller chooses; any other value names a defined player
	// (Aetherspouts/Chaotic Transformation `ChooseOrder$ RememberedPlayer`).
	// The ask is posed once, on the first pass, before any body: the
	// answer permutes the loop cursor's subject slice, so every later
	// iteration -- and every mid-loop suspension -- carries the chosen
	// order and the subjects are never re-derived or re-sorted. Subjects
	// are NOT silently sorted: the offered list is the selector/scan order
	// and the answer names a permutation of it. A no-host host (R-9) keeps
	// that scan order as its deterministic stand-in.
	if cardsSubjects && len(subjects) > 1 && rp.ChooseOrder != "" {
		subjects = repeatEachChooseOrder(h, c, sa, rp, subjects)
	}
	// RepeatOptionalForEachPlayer$ True (Tempting Contract, the Tempt cycle,
	// Zagorka): each subject of the loop is offered its own yes/no election
	// before its body runs, with RepeatOptionalMessage$ as the prompt. The
	// answer is not a body suspension -- the body may not run at all -- so it
	// rides Ctx.RepeatEachOptional on re-entry and skips that subject's body
	// on a decline. A nil field is the first pass; the cursor's own Election
	// flag marks which frame is the offer.
	optionalForEach := rp.OptionalForEach
	optionalMsg := rp.OptionalMessage
	electedIdx, electedAccept := -1, false
	for i := start; i < len(subjects); i++ {
		t := subjects[i]
		if optionalForEach {
			if i == electedIdx && !electedAccept {
				// This subject declined its own offer: skip its body and
				// continue with the next subject.
				continue
			}
			if i != electedIdx {
				// This subject has not been offered yet: pose its election.
				// A yes re-enters at i and runs the body below; a no is the
				// skip above. R-9: a host with no decision channel declines.
				if ans, ok := AskTape(h, repeatEachElectionDecision(h, c, sa, t, i, optionalMsg)); ok {
					// The resolution kernel's answer in hand (the
					// "repeat_each_optional" arm's Accept): a decline skips
					// this subject, a yes runs its body below.
					if len(ans) == 0 || ans[0].Kind != "yes" {
						continue
					}
				} else {
					// No answer served: the subject is declined.
					continue
				}
			}
		}
		cc := *c
		// Forge binds the current loop subject as Remembered; the resolving
		// source/controller remain those of the outer spell or ability.
		base := iterationBase(c, t)
		cc.Remembered = append(copyTargets(base), t)
		// UseImprinted$ names the same subject "Imprinted" for the body's
		// selectors (UnlessPayer$ ImprintedController, Defined$
		// ImprintedController). The suspension carries it so a resumed ask
		// inside the body still binds it.
		cc.RepeatSubject = t
		if fromVotes {
			// The per-iteration binding lives on this iteration's Ctx copy
			// (scalar fields, so the copy is safe), read back through
			// runtimePublished's "Votes" arm. An unvoted subject binds 0: Forge
			// leaves VoteNum<subject> unset for it and the body reads 0, never a
			// fallback to the source's own SVar table.
			cc.VotePublished = 0
			if n, ok := voteCountFor(c, t); ok {
				cc.VotePublished = int32(n)
			}
			cc.VotePublishedSet = true
		}
		Resolve(h, &cc, sub)
		// A loop body runs on a Ctx copy. Its first FlipCoin may allocate
		// the shared memory lazily, so retain that pointer on the outer Ctx
		// before copying the next iteration or returning through a suspension.
		// Mana Clash's post-loop FlippedTails reader must see every player's
		// FlipClash result, not a fresh per-iteration list.
		if c.FlipMemory == nil && cc.FlipMemory != nil {
			c.FlipMemory = cc.FlipMemory
			// Resolve published cc.FlipMemory (nil on entry) for the
			// iteration and its defer restored that nil on the way out, so
			// retaining the pointer on the outer Ctx is not enough: the
			// engine's published slot must be re-pointed too. Without this,
			// an ask posed AFTER the loop -- the enclosing RepeatEach's own
			// SubAbility$ -- captures nil onto its resume point, and the
			// fresh Ctx the answer rebuilds loses every flip the loop
			// recorded before a later Defined$ FlippedHeads/FlippedTails
			// reader runs.
			if fh, ok := h.(flipMemoryHost); ok {
				fh.SetResolutionFlipMemory(c.FlipMemory)
			}
		}
		if h.Suspended() {
			h.SuspendRepeat(RepeatSuspension{
				RepeatCursor: RepeatCursor{SA: sa, Subjects: copyTargets(subjects), Next: i + 1},
				Body:         copyTargets(cc.Remembered),
				Subject:      t,
				Outer:        copyTargets(c.Remembered),
				Chosen:       copyTargets(c.Chosen),
				ChosenValid:  c.ChosenValid,
				// The tally rides the suspension so the re-entered body and the
				// loop's remaining iterations re-derive "Votes" after the fresh
				// Ctx a resume rebuilds (the vote is a PRIOR chain link, so the
				// table cannot be re-derived from the resumed SA).
				VoteCounts: append([]VoteCount(nil), c.VoteCounts...),
			})
			return
		}
		c.Remembered = rememberIteration(c.Remembered, cc.Remembered, base, t)
	}
	if batched && batcher != nil {
		// The loop completed: close the batch opened for it. A re-entry pass
		// closes the batch the FIRST pass opened (same SA, same host, so the
		// open/close conditions agree); every pass that suspends mid-loop
		// returns before this line and leaves the bracket to a later pass.
		batcher.EndDamageBatch()
	}
	if zoneTable && zoneBatcher != nil {
		// The loop completed: close the zone batch the FIRST pass opened
		// (same reasoning as the damage batch above).
		zoneBatcher.EndZoneBatch()
	}
}

// repeatEachChooseOrder poses a RepeatEach ChooseOrder$ ordering ask over
// subjects (see effRepeatEach) and returns the loop order: the answered
// permutation when the resolution kernel serves it, the offered order for a
// no-host stand-in, or suspended after a legacy ask (the loop cursor parked
// on SuspendRepeat).
func repeatEachChooseOrder(h Host, c *Ctx, sa *cards.SA, rp *RepeatEachParams, subjects []state.Target) []state.Target {
	chooser := c.Controller
	if !strings.EqualFold(rp.ChooseOrder, "True") {
		if ps := definedPlayerIDs(h, c, rp.ChooseOrder); len(ps) > 0 {
			chooser = ps[0]
		}
	}
	d := &decision.Decision{Player: chooser, Kind: decision.KChoose,
		Min: len(subjects), Max: len(subjects), Source: c.Source,
		ResumeKind: "repeat_choose_order", ResumeSA: sa,
		Prompt: "Choose the order the repeated ability processes these in"}
	for i, t := range subjects {
		o := decision.Option{Index: i, Kind: "order", Player: PlayerOf(h, c, t)}
		if t.IsPlayer {
			o.Label = "player " + strconv.Itoa(int(t.Player))
		} else if obj := h.Game().Obj(t.Obj); obj != nil && obj.Face() != nil {
			o.Obj, o.Label = t.Obj, obj.Face().Name
		}
		d.Options = append(d.Options, o)
	}
	if ans, ok := AskTape(h, d); ok {
		// The resolution kernel's answer in hand: permute the
		// subjects exactly as the "repeat_choose_order" arm does (a
		// malformed answer, unreachable past validation, keeps the
		// offered order), and run the loop in that order.
		return repeatChooseOrderApply(subjects, ans)
	}
	return subjects
}

// repeatEachElectionDecision is subject subj's (loop index idx)
// RepeatOptionalForEachPlayer$ offer.
func repeatEachElectionDecision(h Host, c *Ctx, sa *cards.SA, subj state.Target, idx int, msg string) *decision.Decision {
	if msg == "" {
		msg = "Accept this offer?"
	}
	player := PlayerOf(h, c, subj)
	return &decision.Decision{Player: player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: msg, Source: c.Source,
		ResumeKind: "repeat_each_optional", ResumeSA: sa, ResumeRepeatNext: int32(idx),
		Options: []decision.Option{
			{Index: 0, Kind: "yes", Label: "Yes", Player: player},
			{Index: 1, Kind: "no", Label: "No", Player: player},
		}}
}

// repeatChooseOrderApply is a RepeatEach ChooseOrder$ answer applied to the
// offered subjects: option Index names the subject's offered position, the
// answer's order is the loop order (rules' "repeat_choose_order" arm reads
// it the same way). A malformed answer -- not a permutation -- keeps the
// offered order rather than dropping or duplicating a subject.
func repeatChooseOrderApply(subjects []state.Target, chosen []decision.Option) []state.Target {
	ordered := copyTargets(subjects)
	if len(chosen) != len(ordered) {
		return ordered
	}
	seen := make([]bool, len(ordered))
	for pos, o := range chosen {
		if o.Index < 0 || o.Index >= len(ordered) || seen[o.Index] {
			return copyTargets(subjects)
		}
		seen[o.Index] = true
		ordered[pos] = subjects[o.Index]
	}
	return ordered
}

const (
	repeatedCards4841Targeted   uint16 = 1 // "Targeted"
	repeatedCards4841Remembered uint16 = 2 // "Remembered", "RememberedLKI", "RememberedCard", "DirectRemembered", "ImprintedLKI"
	repeatedCards4841ChosenCard uint16 = 3 // "ChosenCard"
)

var repeatedCards4841Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "Targeted", Val: repeatedCards4841Targeted},
	state.StrEntry[uint16]{Key: "Remembered", Val: repeatedCards4841Remembered},
	state.StrEntry[uint16]{Key: "RememberedLKI", Val: repeatedCards4841Remembered},
	state.StrEntry[uint16]{Key: "RememberedCard", Val: repeatedCards4841Remembered},
	state.StrEntry[uint16]{Key: "DirectRemembered", Val: repeatedCards4841Remembered},
	state.StrEntry[uint16]{Key: "ImprintedLKI", Val: repeatedCards4841Remembered},
	state.StrEntry[uint16]{Key: "ChosenCard", Val: repeatedCards4841ChosenCard},
)
