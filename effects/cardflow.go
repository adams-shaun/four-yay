package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("Draw", effDraw)
	Register("Discard", effDiscard)
	Register("Mill", effMill)
	Register("Dig", effDig)
	Register("Reveal", effReveal)
	Register("RevealHand", effReveal)
	Register("PeekAndReveal", effReveal)
	Register("RearrangeTopOfLibrary", effRearrangeTopOfLibrary)
	Register("Scry", effScry)
	Register("Surveil", effSurveil)
	Register("DigUntil", effDigUntil)
	Register("NameCard", effNameCard)
	Register("Hideaway", effHideaway)
}

// zoneOf is a bounds-checked g.Zone. PlayerOf returns a target's raw Player
// field (or, for effNameCard, Ctx.Controller is passed straight through)
// with no validation of its own, and Game.Zone computes
// int(p)*numZones+int(z) and indexes a fixed-size slice without checking
// that p names a real seat -- an out-of-range PlayerID reaches that
// arithmetic and panics with index out of range (Ruling T18-a). Every call
// site in this file that reads a zone to decide what to move goes through
// this rather than g.Zone directly, mirroring count.go's own PlayerID bounds
// check ahead of g.Players[c.Controller]. An invalid seat degrades to nil --
// the same shape as a real, empty zone -- so the primitive simply finds
// nothing there rather than panicking or erroring.
func zoneOf(g *state.Game, z state.Zone, p state.PlayerID) []state.ObjID {
	if int(p) >= len(g.Players) {
		return nil
	}
	return g.Zone(z, p)
}

// DrawFor is exported so the rules package can use the same code path for the
// draw step. Drawing from an empty library is a loss, checked by SBAs.
func DrawFor(h Host, p state.PlayerID) { drawFor(h, p, -1, nil, drawUptoRider{}) }

// DrawForTurn is DrawFor for the draw step's turn-based draw: a Dredge ask it
// poses is served from the resolution kernel's tape when a tape run (begun by
// the pass that ends the upkeep) serves it.
func DrawForTurn(h Host, p state.PlayerID) { drawFor(h, p, -1, nil, drawUptoRider{turn: true}) }

// drawUptoRider is the Upto$ Draw continuation an in-flight upto batch
// carries across a Dredge ask (Arcane Denial's "may draw up to two" whose
// draw parks on a Dredge replacement): idx is the Defined$ target index
// whose batch is in flight (-1 = no upto in flight, the zero value every
// non-upto caller passes) and count the answered count for it. It rides the
// ask as Decision.ResumeUptoIdx/ResumeUptoCount; rules' dredge arm restores
// the Ctx fields from them.
type drawUptoRider struct {
	idx   int
	count int32
	// turn marks the draw step's own draw (DrawForTurn).
	turn bool
}

// drawFor is DrawFor with an optional enclosing Draw cursor. A nonnegative
// cursor is recorded on a dredge decision so rules can continue that exact
// multi-card resolution after its replacement is answered.
//
// It reports whether the draw was the subject of a Dredge ask the resolution
// kernel answered from its tape: rules' dredge answer record (shared with
// the "dredge" resume arm) has then already applied the replacement or the
// ordinary draw, and the caller continues exactly as that arm's re-entry
// does -- past this draw's cursor, without the drawn-card bookkeeping the
// re-entry skips. Only a caller with a cursor and a resume SA (an effect
// walk) is served from the tape; the bare DrawFor keeps the legacy ask.
func drawFor(h Host, p state.PlayerID, cursor int, resumeSA *cards.SA, upto drawUptoRider) bool {
	g := h.Game()
	lib := zoneOf(g, state.ZLibrary, p)
	if len(lib) == 0 {
		h.EmitPlayerLost(p, "Milled", "drew from an empty library")
		return false
	}
	// A DrawFor reached while the resolution is already suspended: a caller
	// that does not check h.Suspended() between draws drove a second draw
	// after the first one parked on a dredge ask. Posing a second ask here
	// would overwrite the outstanding one (the orphaned-decision failure
	// findings-sol4 proved); the guarded callers (effDraw's cursor loop,
	// the turn draw, rules' lifeReplacementDraw park) never reach this
	// suspended, so this degrades the unguarded one deterministically: no
	// ask, an ordinary draw, one Note naming why.
	if h.Suspended() {
		h.Emit(events.Event{Kind: events.Note, Player: p,
			Text: "drew without a dredge choice: another decision is already pending"})
		h.Emit(events.Event{Kind: events.Draw, Player: p, Obj: lib[0],
			From: state.ZLibrary, To: state.ZHand, Secret: true})
		return false
	}
	// Dredge (CR 702.55): before a player draws a card, if they have a card
	// with Dredge in the graveyard they may instead mill N cards (N = the
	// dredge number) and return that card from the graveyard to their hand,
	// and the draw is replaced. This is a player choice at the point of the
	// draw, so it is posed as a mid-resolution KModes choice over every legal
	// dredger in graveyard scan order plus the ordinary draw. A fluff/no-host
	// host declines and draws normally.
	if candidates := dredgeCandidates(g, p); len(candidates) > 0 {
		// Pose every legal replacement plus the ordinary draw. A player with
		// several dredgers chooses which replacement applies (CR 616.1).
		// The upto rider travels so the dredge resume restores the answered
		// up-to batch instead of re-asking its decision.
		riderIdx, riderCount := -1, int32(0)
		if upto.idx >= 0 {
			riderIdx, riderCount = upto.idx, upto.count
		}
		d := &decision.Decision{Player: p, Kind: decision.KModes, Min: 1, Max: 1,
			ResumeKind: "dredge", ResumeTarget: cursor, ResumeSA: resumeSA,
			ResumeUptoIdx: riderIdx, ResumeUptoCount: riderCount,
			Prompt: "Replace draw with Dredge?"}
		for _, candidate := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "dredge",
				Label: "Dredge " + strconv.Itoa(int(candidate.n)) + " (mill, then return " + objName(g, candidate.id) + " to hand)", Obj: candidate.id, Player: p})
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "draw", Label: "Draw card", Player: p})
		// Every draw's Dredge ask is served from the resolution kernel's
		// tape when a run serves it -- a bare DrawFor too (a GainLife->Draw
		// replacement's draws, Nefarious Lich): the answer record
		// (rules' dredgeAnswerApply) performs the replacement or the
		// ordinary draw, so nothing is left for the caller.
		if _, ok := AskTape(h, d); ok {
			return true
		}
		if h.Ask(d) {
			return false
		}
	}
	h.Emit(events.Event{Kind: events.Draw, Player: p, Obj: lib[0],
		From: state.ZLibrary, To: state.ZHand, Secret: true})
	return false
}

type dredgeCandidate struct {
	id state.ObjID
	n  int32
}

// dredgeCandidates returns every graveyard Dredge replacement that is legal:
// CR 702.55 requires enough cards to mill the full number. Graveyard scan
// order is deterministic and becomes the decision's stable option order.
func dredgeCandidates(g *state.Game, p state.PlayerID) []dredgeCandidate {
	library := g.Zone(state.ZLibrary, p)
	var out []dredgeCandidate
	for _, id := range g.Zone(state.ZGraveyard, p) {
		o := g.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		if n, ok := o.Face().KeywordParam("Dredge"); ok {
			if v, err := strconv.Atoi(strings.TrimSpace(n)); err == nil && v > 0 && len(library) >= v {
				out = append(out, dredgeCandidate{id: id, n: int32(v)})
			}
		}
	}
	return out
}

func objName(g *state.Game, id state.ObjID) string {
	if o := g.Obj(id); o != nil && o.Face() != nil {
		return o.Face().Name
	}
	return "it"
}

// effDiscard moves cards from a player's hand to their graveyard. Which
// cards, and who chooses, is driven by the corpus params that the real
// discard spells use and that this primitive now reads:
//
//   - Mode$ RevealYouChoose (Thoughtseize, Duress): the CASTER — c.Controller,
//     not the discarding player — looks at the target's hand and chooses which
//     card is discarded. This is a real mid-resolution ask: the effect poses a
//     KModes decision over the DiscardValid$-filtered hand, suspends, and on
//     re-entry discards exactly the card Ctx.Discard names (the continuation
//     arm in rules/resolution.go set it from the recorded answer). A host
//     that cannot ask falls back to the deterministic front-card stand-in.
//   - Mode$ TgtChoose (Mind Rot, Faithless Looting, Thirst for Knowledge,
//     Riddlesmith): the ordinary "discard N cards" — the DISCARDING player,
//     p (the target, not the caster), chooses which of their own cards to
//     drop, without the hand being revealed first. Same mid-resolution ask
//     shape as RevealYouChoose, same ResumeKind "discard", but the decision's
//     Player is p and the no-ask fallback takes the front of the FILTERED
//     hand. A hand with fewer eligible cards than NumCards$ discards what it
//     owns and asks nothing (there is no choice to be made), and a hand
//     whose eligible count is at or below NumCards$ likewise resolves
//     deterministically with no question. Optional$ True (Mox Diamond's
//     "you may discard a land card", "discard up to two cards") first poses
//     a yes/no may-discard election (ResumeKind "discard_may", answered into
//     Ctx.DiscardVote): "no" discards nothing, "yes" poses the pick with
//     Min 1.
//   - Mode$ RevealDiscardAll (Cabal Therapy): a FILTER, not a choice. Every
//     card in the target's hand matching DiscardValid$ is discarded, no ask.
//   - Mode$ Hand (Reforge the Soul, Windfall, Magus of the Wheel, Dark
//     Deal): the whole-hand wheel — every card in the target's hand, in hand
//     order, one events.Discard per card, no ask and no Note (a mandatory
//     line has no choice to record). Forge's DiscardEffect HAND mode
//     discards the ENTIRE hand and never reads NumCards$ there. The
//     Optional$ True variant is a real may-discard election: a per-player
//     yes/no (Ctx.DiscardVote with the per-player cursor Ctx.DiscardTarget)
//     whose decline discards nothing.
//   - Mode$ Random: CR 701.8b's random discard — the engine's own seeded RNG
//     (h.Rand) picks NumCards$ cards out of the DiscardValid$-filtered hand
//     without replacement. No seat is asked and no Note is recorded: the
//     randomness IS the rule, not a stand-in for a missing ask.
//   - Mode$ Defined: DefinedCards$ names the cards (the Remembered set,
//     Breathstealer's Crypt); only cards still in the target's hand move.
//   - Mode$ LookYouChoose / YouChoose / RevealTgtChoose: the same asking
//     shape as RevealYouChoose — a CHOSER (the caster for Look/You; the
//     first player target for RevealTgtChoose) names the cards out of the
//     discarder's hand, a per-target cursor (Ctx.DiscardTarget) attaches
//     each answer to the target that gave it, and every later target poses
//     its own ask.
//
// DiscardValid$ is a Forge filter spec ("Card.nonLand", "Card.NamedCard"),
// evaluated with MatchesSpecFrom (the same resolver effDig uses for
// ChangeValid$). Its default is "Card". The chooser/target split is what keeps
// Thoughtseize from letting the opponent pick their own discard, and what
// keeps a Mind Rot target's own choice from being made by the caster.
// discardRiders carries the two Discard riders Forge applies per discarded
// card (DiscardEffect -> Player.discard): RememberDiscarded$ records the card
// in the resolution's Remembered set -- the ctx-level set a chained
// SubAbility reads for "the cards discarded this way" -- AND event-backed on
// the source (Forge adds to the host card's remembered list, which persists
// past the resolution and is what Card.IsRemembered matches later);
// RememberDiscardingPlayers$ records the discarding player (Forge's
// discardedMap.keySet(), one corpus line).
type discardRiders struct {
	rememberCards   bool
	rememberPlayers bool
}

func discardRidersOf(sa *cards.SA) discardRiders {
	return discardRiders{
		rememberCards:   strings.EqualFold(sa.ParamStr(cards.PKRememberDiscarded), "True"),
		rememberPlayers: strings.EqualFold(sa.ParamStr(cards.PKRememberDiscardingPlayers), "True"),
	}
}

// actingPlayers resolves a PLAYER-acting SA's targets: the explicit
// Defined$/targeted set when the script names one; otherwise Forge's
// player-side default (SpellAbilityEffect.getPlayers reads
// paramOrDefault("Defined", "You")) -- the resolving ability's OWN controller,
// not the source object's CURRENT controller. The two agreed everywhere until
// the mid-resolution control-change primitives (RememberControlled$) went
// live: they diverge the moment a resolution steals its own source (Kain,
// Traitorous Dragoon -- "that player gains control of Kain. If they do, you
// draw that many cards" must draw for the original controller, not the
// taker). Object-acting effects keep Defined()'s source-object default. The
// corpus's no-Defined population for these primitives (Draw, Discard, Mill,
// Scry/Surveil, RearrangeTopOfLibrary) is behaviour-identical under the
// change (c.Controller == the source's controller unless a mid-resolution
// control change moved it), so no golden game moves.
func actingPlayers(h Host, c *Ctx, sa *cards.SA) []state.PlayerID {
	if DefinedRefOf(sa).Set() {
		// definedPlayers applies Forge's getDefinedPlayers rule: the plain
		// Remembered family contributes remembered PLAYERS only, never a
		// remembered card's controller (Summon: Valefor's per-opponent loop).
		return definedPlayers(h, c, sa)
	}
	if TargetsOf(sa).Has(TgtValidPresent) {
		return playerIDsFromTargets(h, c, "", Defined(h, c, sa))
	}
	return []state.PlayerID{c.Controller}
}

// discardAndRemember emits one discard with the riders bound above.
func discardAndRemember(h Host, c *Ctx, r discardRiders, id state.ObjID, p state.PlayerID) {
	discardAndRememberEvent(h, c, r, events.Discard(id, p))
}

// The random choice rides the canonical discard move itself: Amount is the
// one-based index into the remaining eligible hand (0 means an ordinary
// discard). Apply moves Obj, while the event records both the RNG outcome and
// its consequence for log-only replay without exposing an unlogged choice.
func discardAndRememberEvent(h Host, c *Ctx, r discardRiders, ev events.Event) {
	h.Emit(ev)
	id, p := ev.Obj, ev.Player
	if r.rememberCards {
		c.Remembered = append(c.Remembered, state.Target{Obj: id})
		eventRemember(h, c, id)
	}
	if r.rememberPlayers {
		// RememberDiscardingPlayers$ is a two-half rider like
		// RememberDiscarded$: the resolution-local Ctx.Remembered entry dies
		// with the resolution, but Professor Onyx's ultimate and Snort's
		// follow-up read the SOURCE CARD's persistent remembered list
		// (Player.IsRemembered) from a later ability, so the discarding player
		// must also land there through the same event-backed write the
		// RememberDiscarded$ branch and rememberInvestigatingPlayers use.
		// rememberPlayerBothHalves keeps the two dedup checks independent: the
		// persistent write must not be conditional on the transient entry (a
		// player an earlier subeffect already put in Ctx.Remembered but not on
		// the source is still a discarder this rider must persist).
		rememberPlayerBothHalves(h, c, p)
	}
}

// rememberPlayerBothHalves records p on BOTH halves of the remembered
// state a player-remember rider owns: the resolution-local Ctx.Remembered
// set (which dies with the resolution) and the source object's event-backed
// persistent remembered list (which Player.IsRemembered and the filter
// spelling read from a LATER ability).
//
// The two dedup checks are deliberately INDEPENDENT. The transient check
// keeps one Ctx.Remembered entry per player; the persistent check keeps one
// event-backed entry per player. Tying the persistent write to the transient
// guard is wrong: an earlier subeffect in the same resolution can place the
// player in Ctx.Remembered WITHOUT persisting it (ChoosePlayer's
// RememberChosen$, another remember rider), and then the persistent write is
// skipped and a later Player.IsRemembered read still cannot see the player.
// Conversely a persistent entry must not be re-emitted just because the
// transient set no longer holds it.
//
// eventRemember self-gates on c.Source == 0; when the source object is
// absent there is nothing to dedup against and one write is correct.
func rememberPlayerBothHalves(h Host, c *Ctx, p state.PlayerID) {
	want := state.Target{Player: p, IsPlayer: true}
	if !targetIn(c.Remembered, want) {
		c.Remembered = append(c.Remembered, want)
	}
	if o := h.Game().Obj(c.Source); o != nil && targetIn(o.Remembered, want) {
		return
	}
	eventRemember(h, c, state.PlayerRef(p))
}

// discardBounds is the min/max Forge's DiscardEffect computes for its
// chooseCardsToDiscardFrom call, shared by the asking arms: AnyNumber$ is
// min 0 with no cap short of the eligible hand, Optional$ lowers min to 0,
// and otherwise min == max == NumCards (capped at the eligible count).
// AnyNumber$ is implemented for the two asking modes (RevealYouChoose and
// TgtChoose); the measured corpus population is TgtChoose-only (22 lines,
// every one also Optional$ True), but the bounds themselves are one Forge
// code path for all modes.
func discardBounds(h Host, c *Ctx, sa *cards.SA, eligible int) (int, int) {
	if strings.EqualFold(sa.ParamStr(cards.PKAnyNumber), "True") {
		return 0, eligible
	}
	n := int(Num(h, c, sa, "NumCards", 1))
	if n < 1 {
		n = 1
	}
	if n > eligible {
		n = eligible
	}
	min := n
	if strings.EqualFold(sa.ParamStr(cards.PKOptional), "True") {
		min = 0
	}
	return min, n
}

// unlessTypeEligible returns the hand cards matching any comma-separated
// UnlessType$ spec, in hand order -- the unless alternative's candidate set
// (Thirst for Knowledge's "discard an artifact card").
func unlessTypeEligible(g *state.Game, c *Ctx, hand []state.ObjID, unless string) []state.ObjID {
	var out []state.ObjID
	for _, id := range hand {
		for spec := range strings.SplitSeq(unless, ",") {
			spec = strings.TrimSpace(spec)
			if spec != "" && MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
				out = append(out, id)
				break
			}
		}
	}
	return out
}

// discardMayPrompt is the may-discard election's question: the card text's
// own DiscardValidDesc$ noun when the script names one ("a land card"),
// otherwise a plain count.
func discardMayPrompt(sa *cards.SA, max int) string {
	noun := "card"
	if desc := strings.TrimSpace(sa.ParamStr(cards.PKDiscardValidDesc)); desc != "" {
		noun = desc
	} else if v := strings.TrimSpace(sa.ParamStr(cards.PKDiscardValid)); v != "" && !strings.ContainsAny(v, ".,+") && v != "Card" {
		noun = strings.ToLower(v) + " card"
	}
	if max <= 1 {
		article := "a "
		if strings.ContainsRune("aeiouAEIOU", rune(noun[0])) {
			article = "an "
		}
		return "Discard " + article + noun + "?"
	}
	return "Discard up to " + strconv.Itoa(max) + " " + noun + "(s)?"
}

func discardEligible(g *state.Game, c *Ctx, hand []state.ObjID, valid string) []state.ObjID {
	out := make([]state.ObjID, 0, len(hand))
	for _, id := range hand {
		if MatchesSpecCtx(g, valid, id, c.SpecContext(c.Controller)) {
			out = append(out, id)
		}
	}
	return out
}

func discardChooser(c *Ctx, mode string) state.PlayerID {
	switch mode {
	case "RevealTgtChoose":
		for _, t := range c.Targets {
			if t.IsPlayer {
				return t.Player
			}
		}
	}
	return c.Controller
}

func discardAsk(g *state.Game, c *Ctx, sa *cards.SA, eligible []state.ObjID, chooser state.PlayerID, min, max, target int) *decision.Decision {
	opts := make([]decision.Option, 0, len(eligible))
	for _, id := range eligible {
		name := "a card"
		if o := g.Obj(id); o != nil && o.Face() != nil {
			name = o.Face().Name
		}
		opts = append(opts, decision.Option{Index: len(opts), Kind: "discard", Label: "Discard " + name, Obj: id, Player: chooser})
	}
	return &decision.Decision{Player: chooser, Kind: decision.KModes, Min: min, Max: max, Source: c.Source,
		ResumeKind: "discard", ResumeSA: sa, ResumeTarget: target,
		Prompt: "Choose " + strconv.Itoa(min) + ".." + strconv.Itoa(max) + " card(s) to discard", Options: opts}
}

func effDiscard(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	riders := discardRidersOf(sa)
	// fx42: capture the answered discard choice into a local and clear
	// c.Discard before the target loop. The answer must stay scoped to the
	// discard primitive that asked: a DISCARD reached below this one in the
	// same walk (this effect's SubAbility$ chain) must pose its own ask
	// instead of inheriting this one's answered cards. Capturing first keeps
	// the load-bearing multi-target behaviour intact — every target of a
	// multi-target discard sees the SAME answered list, which is exactly what
	// the old per-target c.Discard read produced. Ctx.Discard's only reader is
	// this primitive, so clearing here is safe.
	answers := ([]state.ObjID)(nil)

	answerTarget := int(0)

	vote := string("")

	answered := answers != nil
	voted := vote != ""
	// The UnlessType$ election's cursor: an answered "discard_unless"
	// re-entry carries the asking target's index in Ctx.DiscardTarget, and
	// the targets before it were fully processed on the pass that asked.
	electedTarget := -1

	// Discard-batch bracket (Mode$ DiscardedAll): one api:Discard resolution
	// is ONE discard action, so the batch trigger fires once for the whole
	// resolution rather than once per discarded card. The open must span a
	// mid-resolution suspension (an answered election re-enters this same
	// call with the answer, so the resumed pass is the SAME action), and it
	// must close exactly once, on the pass that completes without asking.
	// firstPass is the resumed-pass test the answered/voted/UnlessElected
	// locals already express: only a first pass has none of them set. The
	// deferred close is skipped while suspended, so the bracket stays open
	// across the resume and the completing pass closes it. A host double
	// without the bracket interface simply fires DiscardedAll per card
	// rather than failing to compile (the mill bracket's shape), and a
	// non-api:Discard producer (a cost or cleanup discard) never opens the
	// bracket, so each is its own batch-of-one exactly as before this gate.
	firstPass := !answered && !voted
	suspended := false
	if b, ok := h.(interface {
		BeginDiscardBatch()
		EndDiscardBatch()
	}); ok {
		// Open only on the FIRST pass (the resumed pass is the same action);
		// the close-defer is registered on EVERY pass, because the pass that
		// completes the action may be a resumed one. Closing a batch that was
		// already closed (a stray non-first entry with no open bracket) is a
		// no-op in closeDiscardBatch's depth guard.
		if firstPass {
			b.BeginDiscardBatch()
		}
		defer func() {
			if !suspended {
				b.EndDiscardBatch()
			}
		}()
	}
	mode := sa.ParamStr(cards.PKMode)
	valid := sa.ParamStr(cards.PKDiscardValid)
	if valid == "" {
		valid = "Card"
	}
	// The four choosing modes share one ask shape: a CHOSER looks at the
	// discarder's hand and names the cards, then the discarder discards them.
	// RevealYouChoose/LookYouChoose disclose the hand to the caster;
	// YouChoose is the caster; RevealTgtChoose is the first target (Rakdos
	// Augermage: the target opponent chooses out of the caster's revealed
	// hand). A per-target cursor (answerTarget/targetIndex) keeps each
	// acting player's answer attached to the target that gave it, so a
	// multi-target discard asks every target instead of applying target 0's
	// answer to the rest.
	chooseMode := mode == "RevealYouChoose" || mode == "LookYouChoose" ||
		mode == "YouChoose" || mode == "RevealTgtChoose"
	if chooseMode {
		chooser := discardChooser(c, mode)
		for targetIndex, t := range actingPlayers(h, c, sa) {
			p := t
			hand := zoneOf(g, state.ZHand, p)
			if answered && targetIndex < answerTarget {
				continue // fully processed before a later target's ask
			}
			if answered && targetIndex == answerTarget {
				// Re-entry: the chooser's answer was recorded, so discard
				// exactly those cards that still sit in this target's hand (a
				// stray answer must not move an object that left meanwhile).
				discardAnswered(h, c, riders, hand, answers, p)
				continue
			}
			eligible := discardEligible(g, c, hand, valid)
			if len(eligible) == 0 {
				continue
			}
			askMin, askMax := discardBounds(h, c, sa, len(eligible))
			d := discardAsk(g, c, sa, eligible, chooser, askMin, askMax, targetIndex)
			if ans, ok := AskTape(h, d); ok {
				// The resolution kernel's answer in hand: discard exactly
				// what the re-entry above discards for this target.
				discardAnswered(h, c, riders, hand, answerObjs(ans), p)
				continue
			}
			_ = Ask(h, d)

			// Fuzz/no-engine host: the deterministic front-of-ELIGIBLE-hand
			// stand-in (R-9) for the chooser, with the Note that records why
			// the richer path did not run.
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "discards its first card (no engine host to ask)"})
			for i := 0; i < askMax; i++ {
				discardAndRemember(h, c, riders, eligible[i], p)
			}
		}
		return
	}
	for targetIndex, t := range actingPlayers(h, c, sa) {
		p := t
		hand := zoneOf(g, state.ZHand, p)

		switch mode {
		case "TgtChoose":
			// The resolution kernel's tape-served elections for this target:
			// the locals that stand for what the "discard_may" and
			// "discard_unless" re-entries read off Ctx.DiscardVote and
			// Ctx.UnlessElected. A served election re-walks this target from
			// tgtChoose, exactly as its re-entry does.
			tapeVote, tapeElected := "", ""
		tgtChoose:
			// Re-entry: the discarding player's choice was answered and the
			// continuation set Ctx.Discard to the chosen object(s). Discard
			// exactly those that sit in this target's hand (a per-hand filter
			// keeps a stray answer from moving an object that left the hand
			// meanwhile). The cursor keeps the answer attached to the target
			// that gave it: earlier targets were fully processed before a later
			// target's ask and must not be re-run, and only the cursor target
			// consumes the answer.
			if answered && targetIndex < answerTarget {
				continue
			}
			if answered && targetIndex == answerTarget {
				discardAnswered(h, c, riders, hand, answers, p)
				continue
			}
			// The may-discard election (below) answered for this target:
			// earlier targets were fully processed before it was posed, a
			// "no" discards nothing for this target, and a "yes" proceeds to
			// the card pick with the zero-card answer removed.
			if voted && targetIndex < answerTarget {
				continue
			}
			if targetIndex < electedTarget {
				continue // fully processed before a later target's election
			}
			elVote := ""
			if voted && targetIndex == answerTarget {
				elVote = vote
			}
			if tapeVote != "" {
				elVote = tapeVote
			}
			mayElected := elVote != ""
			if mayElected && elVote != "yes" {
				continue
			}
			// First pass: narrow the target's hand to the cards DiscardValid$
			// allows. This is the discarding player's own hand, so the choice
			// is presented to p.
			eligible := make([]state.ObjID, 0, len(hand))
			for _, id := range hand {
				if MatchesSpecCtx(g, valid, id, c.SpecContext(c.Controller)) {
					eligible = append(eligible, id)
				}
			}
			if len(eligible) == 0 {
				continue
			}
			// UnlessType$ (Thirst for Knowledge's "discard two cards unless
			// you discard an artifact card"): the discarding player may instead
			// discard ONE card of the named type. Forge's DiscardEffect reads
			// the key only in its TgtChoose branch, swapping the ordinary
			// numCards pick for chooseCardsToDiscardUnlessType, so that is the
			// only mode this walk widens. The election is a real choice the
			// moment one unless-eligible card is in hand -- discarding the
			// artifact and discarding the full count are answers nobody else
			// can make, and the strict-supersets no-ask rule below would
			// otherwise silently drop the alternative. The election's answer
			// re-enters through the "discard_unless" resume arm; the unless
			// arm's own multi-candidate pick re-uses the ordinary "discard"
			// arm (Min == Max == 1). fx42 scoping: the election is consumed and
			// cleared before any further ask this walk poses.
			elected := string("")

			if tapeElected != "" {
				elected = tapeElected
			}
			unlessSpec := strings.TrimSpace(sa.ParamStr(cards.PKUnlessType))
			if elected == "unless" && unlessSpec != "" {
				picks := unlessTypeEligible(g, c, hand, unlessSpec)
				if len(picks) == 1 {
					discardAndRemember(h, c, riders, picks[0], p)
					continue
				}
				if len(picks) > 1 {
					opts := make([]decision.Option, 0, len(picks))
					for _, id := range picks {
						name := "a card"
						if o := g.Obj(id); o != nil && o.Face() != nil {
							name = o.Face().Name
						}
						opts = append(opts, decision.Option{Index: len(opts), Kind: "discard",
							Label: "Discard " + name + " instead", Obj: id, Player: p})
					}
					d := &decision.Decision{Player: p, Kind: decision.KModes,
						Min: 1, Max: 1, Source: c.Source,
						ResumeKind: "discard", ResumeSA: sa, ResumeTarget: targetIndex,
						Prompt:  "Discard one " + unlessSpec + " card instead",
						Options: opts}
					if ans, ok := AskTape(h, d); ok {
						// The resolution kernel's answer in hand: the
						// "discard" re-entry's discard for this target.
						discardAnswered(h, c, riders, hand, answerObjs(ans), p)
						continue
					}
					_ = Ask(h, d)

					h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
						Text: "discards its first " + unlessSpec + " card (no engine host to ask)"})
					discardAndRemember(h, c, riders, picks[0], p)
					continue
				}
				// The unless-eligible card left the hand meanwhile; fall through
				// to the ordinary discard below.
			} else if elected == "" && unlessSpec != "" && len(unlessTypeEligible(g, c, hand, unlessSpec)) > 0 {
				nOrd := Num(h, c, sa, "NumCards", 1)
				d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
					Source: c.Source, ResumeKind: "discard_unless", ResumeSA: sa, ResumeTarget: targetIndex,
					Prompt: "Discard one " + unlessSpec + " card instead of " + strconv.FormatInt(int64(nOrd), 10) + "?",
					Options: []decision.Option{
						{Index: 0, Kind: "unless", Label: "Yes — discard one " + unlessSpec, Player: p},
						{Index: 1, Kind: "ordinary", Label: "No — discard normally", Player: p},
					}}
				if ans, ok := AskTape(h, d); ok {
					// The resolution kernel's answer in hand (the
					// "discard_unless" arm's UnlessElected): re-walk this
					// target with the election, as its re-entry does.
					tapeVote, tapeElected = "", "ordinary"
					if len(ans) > 0 && ans[0].Kind == "unless" {
						tapeElected = "unless"
					}
					goto tgtChoose
				}
				_ = Ask(h, d)

				// Fuzz/no-engine host: the deterministic stand-in takes the
				// unless alternative's first card -- Forge's AI does the same
				// thing (PlayerControllerAi.chooseCardsToDiscardUnlessType
				// discards the min-CMC card of the type whenever one exists),
				// with the Note that records why the richer path did not run.
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "discards its first " + unlessSpec + " card instead (no engine host to ask)"})
				picks := unlessTypeEligible(g, c, hand, unlessSpec)
				discardAndRemember(h, c, riders, picks[0], p)
				continue
			}
			askMin, askMax := discardBounds(h, c, sa, len(eligible))
			// Optional$ True ("you MAY discard a land card", Mox Diamond's
			// replacement; "discard up to two cards"): the zero-card answer
			// is a real choice, and a bare Min 0 card pick expressed it only
			// as an empty submission -- no option said "don't discard", so a
			// player shown nothing but land faces had no visible way to
			// decline. The decline is posed the way every other may-election
			// here is (the Mode$ Hand Optional$ variant, the UnlessType$
			// election): a yes/no first, whose "no" discards nothing and whose
			// "yes" poses the pick with Min 1, so "up to N" stays 1..N.
			// AnyNumber$ is not an election -- zero is one count among many
			// in its own pick. A host that cannot ask keeps the prior R-9
			// stand-in unchanged: straight on to the front-of-eligible
			// discard below, with no extra event.
			if askMin == 0 && !strings.EqualFold(sa.ParamStr(cards.PKAnyNumber), "True") {
				if !mayElected {
					d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
						Source: c.Source, ResumeKind: "discard_may", ResumeSA: sa, ResumeTarget: targetIndex,
						Prompt: discardMayPrompt(sa, askMax),
						Options: []decision.Option{
							{Index: 0, Kind: "yes", Label: "Yes — discard", Player: p},
							{Index: 1, Kind: "no", Label: "No — don't discard", Player: p},
						}}
					if ans, ok := AskTape(h, d); ok {
						// The resolution kernel's answer in hand (the
						// "discard_may" arm's DiscardVote): re-walk this
						// target with the election, as its re-entry does.
						tapeVote, tapeElected = "no", ""
						if answerYes(ans) {
							tapeVote = "yes"
						}
						goto tgtChoose
					}
					_ = Ask(h, d)

				} else {
					askMin = 1
				}
			}
			if strings.EqualFold(sa.ParamStr(cards.PKAnyNumber), "True") {
				// "discard any number of cards": any eligible count from zero
				// up is a real choice the moment one eligible card exists, so
				// the strict-supersets gate does not apply to it -- a hand with
				// exactly one eligible card can still legitimately answer
				// "discard it" or "discard nothing".
			} else if askMin == askMax && askMin == len(eligible) {
				// Only a real choice when there are STRICTLY more eligible cards
				// than must be discarded. A hand with NumCards$ eligible cards (or
				// fewer) must drop all of them with no question: the player could
				// not answer differently, so emitting a decision nobody can
				// meaningfully resolve would just be noise (R-9 contract).
				for _, id := range eligible {
					discardAndRemember(h, c, riders, id, p)
				}
				continue
			}
			opts := make([]decision.Option, 0, len(eligible))
			for _, id := range eligible {
				name := "a card"
				if o := g.Obj(id); o != nil && o.Face() != nil {
					name = o.Face().Name
				}
				opts = append(opts, decision.Option{Index: len(opts), Kind: "discard",
					Label: "Discard " + name, Obj: id, Player: p})
			}
			d := &decision.Decision{Player: p, Kind: decision.KModes,
				Min: askMin, Max: askMax, Source: c.Source,
				ResumeKind: "discard", ResumeSA: sa, ResumeTarget: targetIndex,
				ResumeRemembered: copyTargets(c.Remembered),
				Prompt:           "Choose " + strconv.Itoa(askMin) + ".." + strconv.Itoa(askMax) + " card(s) to discard",
				Options:          opts}
			if ans, ok := AskTape(h, d); ok {
				// The resolution kernel's answer in hand: discard exactly
				// what the re-entry above discards for this target.
				discardAnswered(h, c, riders, zoneOf(g, state.ZHand, p), answerObjs(ans), p)
				continue
			}
			_ = Ask(h, d)

			// Fuzz/no-engine host: the deterministic front-of-ELIGIBLE-hand
			// stand-in (R-9) for the discarding player, with the Note that
			// records why the richer path did not run. AskEmpty is
			// unreachable here by construction (eligible nonempty and strictly
			// greater than n above, Min == Max == n >= 1), but the shared
			// helper owns the guard either way.
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "discards its first card (no engine host to ask)"})
			for i := 0; i < askMax; i++ {
				discardAndRemember(h, c, riders, eligible[i], p)
			}

		case "RevealDiscardAll":
			// A FILTER, not a choice (Cabal Therapy): discard every card in
			// the target's hand that DiscardValid$ allows, no matter what
			// NumCards$ says. No ask.
			for _, id := range hand {
				if MatchesSpecCtx(g, valid, id, c.SpecContext(c.Controller)) {
					discardAndRemember(h, c, riders, id, p)
				}
			}

		case "Hand":
			// Mode$ Hand is the whole-hand wheel (Reforge the Soul, Windfall,
			// Magus of the Wheel, Dark Deal): "each player discards their hand".
			// Forge's DiscardEffect HAND mode discards the ENTIRE hand and
			// never reads NumCards$ there; the pre-fix engine fell through to
			// the default arm and discarded only the front card. A mandatory
			// Hand line has no choice to record, so there is no ask and no
			// Note: every card in hand order, one events.Discard per card via
			// discardAndRemember (which applies the RememberDiscarded$ /
			// RememberDiscardingPlayers$ riders per card, exactly what Windfall's
			// "greatest number discarded" draw reads). The measured corpus
			// population (108 raw lines) carries no NumCards$, DiscardValid$ or
			// AnyNumber$, so none is read here.
			if strings.EqualFold(sa.ParamStr(cards.PKOptional), "True") {
				// "each player MAY discard their hand and draw N" (5 corpus
				// lines): a real may-discard election. The answer is a yes/no per
				// acting player, carried on Ctx.DiscardVote with the per-player
				// cursor Ctx.DiscardTarget; a declined election discards nothing.
				if voted && targetIndex < answerTarget {
					continue // fully processed before a later player's ask
				}
				if voted && targetIndex == answerTarget {
					if vote == "yes" {
						for _, id := range hand {
							discardAndRemember(h, c, riders, id, p)
						}
					}
					continue
				}
				d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
					Source: c.Source, ResumeKind: "discard_hand", ResumeSA: sa, ResumeTarget: targetIndex,
					Prompt: "Discard your hand?",
					Options: []decision.Option{
						{Index: 0, Kind: "yes", Label: "Yes — discard your hand", Player: p},
						{Index: 1, Kind: "no", Label: "No — keep it", Player: p},
					}}
				if ans, ok := AskTape(h, d); ok {
					// The resolution kernel's answer in hand: the
					// "discard_hand" re-entry's whole-hand discard (or decline).
					if answerYes(ans) {
						for _, id := range hand {
							discardAndRemember(h, c, riders, id, p)
						}
					}
					continue
				}
				_ = Ask(h, d)

				// Fuzz/no-engine host: the deterministic stand-in takes the
				// discard (R-9), with the Note that records why the richer path
				// did not run.
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "may discard resolved as discard (no engine host to ask)"})
			}
			for _, id := range hand {
				discardAndRemember(h, c, riders, id, p)
			}

		case "Random":
			// CR 701.8b: a random discard. Forge's DiscardEffect Random mode
			// picks Aggregates.random(list, numCards) from the DiscardValid$-
			// filtered hand, so the engine's own seeded RNG chooses the cards
			// (h.Rand, never an ambient source) without replacement. The
			// one-based pick index rides the applied discard event's Amount;
			// Obj names the picked card. No seat is asked and no Note recorded.
			eligible := discardEligible(g, c, hand, valid)
			n := int(Num(h, c, sa, "NumCards", 1))
			if n > len(eligible) {
				n = len(eligible)
			}
			for i := 0; i < n; i++ {
				j := h.Rand(len(eligible))
				ev := events.Discard(eligible[j], p)
				ev.Amount = int32(j + 1)
				discardAndRememberEvent(h, c, riders, ev)
				eligible = append(eligible[:j], eligible[j+1:]...)
			}

		case "Defined":
			// DefinedCards$ names the cards to discard (Breathstealer's
			// Crypt: "that player discards it" — the Remembered drawn card).
			// Only cards still in this target's hand move; everything else
			// (already gone, or never theirs) is skipped. The old default arm
			// ignored DefinedCards$ entirely and discarded the front of hand.
			if dc := DefinedOf(sa).Cards.Text; dc != "" {
				for _, t := range discardDefinedCards(h, c, dc) {
					if t.IsPlayer {
						continue
					}
					// Re-read the hand per named card: events.remove
					// rebuilds the zone slice, so the captured one goes
					// stale the moment this arm discards anything, and a
					// DefinedCards$ list naming two cards would test the
					// second against a hand that still shows the first.
					// The move goes through discardAndRemember so a
					// Defined discard applies RememberDiscarded$ /
					// RememberDiscardingPlayers$ exactly like every other
					// mode -- emitting events.Discard directly here would
					// silently drop both riders.
					if containsID(zoneOf(g, state.ZHand, p), t.Obj) {
						discardAndRemember(h, c, riders, t.Obj, p)
					}
				}
				break
			}
			fallthrough

		default:
			// Deterministic discard from the top of hand order. Real discard is
			// a choice, but the clean-up step and Delve-style costs have no
			// player to ask and must stay exactly as they were; "first in hand"
			// is deterministic and adequate there.
			n := Num(h, c, sa, "NumCards", 1)
			for i := int32(0); i < n; i++ {
				// Re-read the hand each iteration (B2): events.remove rebuilds
				// the zone slice rather than mutating it in place, so a hand
				// captured once — as this primitive used to — never sees the
				// card it just moved, and a NumCards$ >= 2 discard emits the
				// SAME front card N times instead of N distinct cards. Reading
				// the zone per iteration is what the original pre-hoist code
				// did, and is what makes the N-card discard honest.
				cur := zoneOf(g, state.ZHand, p)
				if len(cur) == 0 {
					break
				}
				discardAndRemember(h, c, riders, cur[0], p)
			}
		}
	}
}

// discardAnswered discards the answered cards that still sit in hand, in
// answer order (a stray answer must not move an object that left the hand
// meanwhile): the one home of a "discard" answer, shared by the re-entry and
// the resolution kernel's tape answer.
func discardAnswered(h Host, c *Ctx, r discardRiders, hand, answers []state.ObjID, p state.PlayerID) {
	for _, id := range answers {
		if containsID(hand, id) {
			discardAndRemember(h, c, r, id, p)
		}
	}
}

// containsID reports whether id is present in ids.
func containsID(ids []state.ObjID, id state.ObjID) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// effMill moves cards from the top of a player's library straight to their
// graveyard -- Discard's sibling, minus the hand. With RememberMilled$ True
// every card it actually moves joins the resolution's remembered set, the
// same both-halves recording discardAndRemember does, so a chained pickup
// ("put a card from among them into your hand") filtering on IsRemembered
// finds them instead of silently failing to find. With ShowMilledCards$ True
// the mill REVEALS what it milled: one public ids-Note per acting player
// after that player's moves (the same payload shape effDig's Reveal$ arm
// emits -- Demonic Covenant's "mill two cards" transcript line), so the
// table reads what was milled even though a graveyard move's own MoveZone
// event carries no reveal line.
func effMill(h Host, c *Ctx, sa *cards.SA) {
	n := Num(h, c, sa, "NumCards", 1)
	if n < 0 {
		n = 0
	}
	remember := strings.EqualFold(sa.ParamStr(cards.PKRememberMilled), "True")
	show := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKShowMilledCards)), "True")
	// One api:Mill resolution is ONE mill action (Forge's one Mill call),
	// so the Mode$ MilledAll batch ("whenever one or more cards are
	// milled") must fire once for the whole call, not once per milled card.
	// The bracket is opened here and closed after every acting player's
	// moves; the per-card Mode$ Milled trigger needs no batch and fires on
	// each MoveZone exactly as before. The bracket is a type assertion, the
	// zoneBatch bracket's shape (effects/choose_control.go's zoneBatcher),
	// so a host double without it simply fires MilledAll per card rather
	// than failing to compile.
	if b, ok := h.(interface {
		BeginMillBatch()
		EndMillBatch()
	}); ok {
		b.BeginMillBatch()
		defer b.EndMillBatch()
	}
	g := h.Game()
	for _, t := range actingPlayers(h, c, sa) {
		p := t
		var milledIDs []state.ObjID
		for i := int32(0); i < n; i++ {
			lib := zoneOf(g, state.ZLibrary, p)
			if len(lib) == 0 {
				break
			}
			id := lib[0]
			h.Emit(events.Mill(id, p))
			if remember {
				rememberMilled(h, c, id)
			}
			milledIDs = append(milledIDs, id)
		}
		if show && len(milledIDs) > 0 {
			h.Emit(events.Event{Kind: events.Note, Player: p, IDs: milledIDs})
		}
	}
}

// rememberMilled records one milled card in both halves of the remembered
// state, exactly as discardAndRemember does for a discarded one: the
// resolution's Ctx.Remembered set (what a chained sub-ability and an in-
// flight hidden pick filter read this walk) and the source object's event-
// backed Remembered list (what survives the resolution for a later
// Card.IsRemembered / Count$RememberedSize read).
func rememberMilled(h Host, c *Ctx, id state.ObjID) {
	c.Remembered = append(c.Remembered, state.Target{Obj: id})
	eventRemember(h, c, id)
}

// moveRestToBottom moves the untaken Dig window cards to the BOTTOM of
// their owner's library, as one Secret events.LibraryOrder carrying the
// complete reordered library (Ruling J1) -- the same single-event record
// rules' handleArrange emits for an answered arrange, so a replay re-derives
// the same order either way. The emit is skipped when the move would change
// nothing (the cards already sit on the bottom).
//
// random selects the order: false keeps the caller's offered order, true
// draws a FULL Fisher-Yates permutation from the engine's seeded generator
// (h.Rand) -- the same idiom the RevealRandomOrder$ arm of effDigUntil uses,
// so the order is replay-exact and no seat can influence it. RestRandomOrder$
// True is the caller (task fdn-dig-rest-random-order); a one-card list has a
// single possible order, so the shuffle leaves it untouched.
func moveRestToBottom(h Host, g *state.Game, p state.PlayerID, ids []state.ObjID, random bool) {
	if random && len(ids) > 1 {
		// The shuffle mutates the caller's slice in place. Every caller passes
		// a freshly built remainder, and the LibraryOrder below copies before
		// Apply stores it, so no shared state aliases this ordering.
		for i := len(ids) - 1; i > 0; i-- {
			j := h.Rand(i + 1)
			ids[i], ids[j] = ids[j], ids[i]
		}
	}
	lib := zoneOf(g, state.ZLibrary, p)
	if len(ids) > len(lib) {
		return
	}
	newLib := make([]state.ObjID, 0, len(lib))
	newLib = append(newLib, lib[len(ids):]...)
	newLib = append(newLib, ids...)
	same := len(newLib) == len(lib)
	for i := 0; same && i < len(newLib); i++ {
		same = newLib[i] == lib[i]
	}
	if same {
		return
	}
	h.Emit(events.Event{Kind: events.LibraryOrder, Player: p, IDs: newLib, Secret: true})
}

// manaValueOf is the offered card's own mana value -- the same Face().Cmc()
// read state.SacrificedInfoOf uses. It is the per-card price under a
// WithTotalCMC$ cumulative budget (Decision.MaxSum): every budgeted picker
// (Dig, the hidden pick, the library search, the Play grant) shares this one
// read so the four cannot drift on what a card's mana value is.
func manaValueOf(g *state.Game, id state.ObjID) int {
	if o := g.Obj(id); o != nil && o.Face() != nil {
		return int(o.Face().Cmc())
	}
	return 0
}

// digDestPhrase names the take's destination in the human-readable prompt;
// it is Dig's own phrasing (the picked card GOES to the destination, unlike
// KArrange's Kind which names pile B's), kept separate from
// destinationPhrase so the two vocabularies cannot drift into each other.
// The library arm says the BOTTOM because that is where the take lands:
// events.Move appends to the destination zone, so a library take is a
// move to the bottom -- which is exactly the shape the corpus's
// library-destination digs describe (Jace, the Mind Sculptor's "you may
// put that card on the bottom", mesmeric_sliver's LibraryPosition$ -1).
// A take at a DIFFERENT library position (the primary LibraryPosition$, e.g.
// munda_ambush_leader's "0") is placed by the Dig walk after its primary
// pile and any remainder have settled.

// permanentCardSpec rewrites a leading `Permanent` base token to
// `PermanentCard` -- the shared matcher's battlefield-object base -- so a
// spec evaluated against cards AWAY from the battlefield reads the base as
// Forge's "permanent card": a Dig's ChangeValid$ window is always the
// library, where a bare Permanent must mean Chaos Warp's "If it's a
// permanent card" and Matter Reshaper's "if it's a permanent card with
// mana value 3 or less", never the on-the-battlefield reading
// matchesBase gives the base. rules/stack.go's targetSpecForZone carries
// the same rewrite for target specs; the two cannot share code (effects
// must not import rules), only the rule, and only the leading token is
// rewritten -- every qualifier rides along.
func permanentCardSpec(spec string) string {
	if spec == "Permanent" {
		return "PermanentCard"
	}
	if len(spec) > len("Permanent") && spec[:len("Permanent")] == "Permanent" {
		switch spec[len("Permanent")] {
		case '.', '+', ',':
			return "PermanentCard" + spec[len("Permanent"):]
		}
	}
	// Forge's OTHER spelling of the same base, `Card.Permanent[.rest|+pred]` (Ao,
	// the Dawn Sky's `ChangeValid$ Card.Permanent+nonLand`): the shared
	// matcher reads a `Permanent` PREDICATE as deliberately fail-closed
	// (see matchesObjectText's contextualSameName comment), because a bare
	// `Permanent` base already means an on-the-battlefield object -- a Dig
	// window is always the library, so `Card.Permanent` there must mean
	// Forge's permanent CARD. Rewrite the leading `Card.Permanent` to the
	// internal `PermanentCard` base, moving the predicate separator to the
	// dot the grammar requires (`Card.Permanent+nonLand` ->
	// `PermanentCard.nonLand`; `Card.Permanent` -> `PermanentCard`).
	if rest, ok := strings.CutPrefix(spec, "Card.Permanent"); ok {
		switch {
		case rest == "":
			return "PermanentCard"
		case rest[0] == '.':
			return "PermanentCard" + rest
		case rest[0] == '+', rest[0] == ',':
			// The leading `Permanent` predicate (or OR alternative) is
			// absorbed into the base: drop the consumed separator and
			// re-join the tail with the dot the predicate grammar needs.
			tail := rest[1:]
			if rest[0] == ',' {
				return "PermanentCard," + tail
			}
			return "PermanentCard." + tail
		}
	}
	return spec
}
func digDestPhrase(dest state.Zone) string {
	switch dest {
	case state.ZHand:
		return "your hand"
	case state.ZGraveyard:
		return "your graveyard"
	case state.ZExile:
		return "exile"
	case state.ZBattlefield:
		return "the battlefield"
	case state.ZLibrary:
		return "the bottom of your library"
	default:
		return "its destination"
	}
}

// effReveal backs Reveal, RevealHand and PeekAndReveal, which the brief
// specifies as one row sharing a single Amount param (NumCards, default 1)
// and one behaviour: reveal cards without disturbing them, recorded via a
// non-Secret Note carrying their identities so view projection stops
// redacting them. PeekAndReveal looks at the library; Reveal and RevealHand
// look at hand.
//
// Look$ True (28 corpus RevealHand lines — Gitaxian Probe, Glasses of
// Urza, Slayer's Bounty) turns the reveal into a PRIVATE look (CR 701.20e:
// a card looked at is shown only to the player the effect specifies — here
// the activator, not every seat): the note becomes a Secret Note scoped to
// the looker through emitLook, the one private-look channel every
// looker-scoped effect shares. The public reveal (no Look$) is unchanged.
//
// RevealHand's Forge semantics act on the WHOLE hand ("look at target
// player's hand", "target opponent reveals their hand" — both shapes are
// whole-hand), so when its SA carries NO NumCards$ parameter the amount is
// the pool's entire size, not the shared default 1 (task revealhand1:
// Gitaxian Probe revealed exactly one card of a seven-card hand). The key's
// PRESENCE, not its value, is the switch — a future script writing NumCards$
// wins — and the switch keys on API so Reveal (a card-selector family:
// RevealValid$/Defined$ picking specific cards; 6 of its 85 raw corpus
// lines carry NumCards$) and PeekAndReveal keep their count behaviour. The
// corpus carries ZERO NumCards$ on RevealHand (81 raw lines), so every
// compiled RevealHand SA today takes the whole hand. The pool is only known
// inside the walk, so the whole-hand amount is applied per target.
func effReveal(h Host, c *Ctx, sa *cards.SA) {
	_, hasNum := sa.Param(cards.PKNumCards)
	wholeHand := sa.API == "RevealHand" && !hasNum
	amt := int32(1)
	if hasNum {
		amt = Num(h, c, sa, "NumCards", 1)
		if amt < 0 {
			amt = 0
		}
	}
	zone := state.ZHand
	if sa.API == "PeekAndReveal" {
		zone = state.ZLibrary
		// PeekAmount$ (Herald's Horn, the Kinship family): how many library
		// cards the peek LOOKS at. The reveal below then covers the subset of
		// that window RevealValid$ admits, so the amount and the valid-filter
		// compose ("look at the top card; if it's a creature card of the
		// chosen type, you may reveal it"). An unresolvable value degrades to
		// zero through Num's present-but-unresolvable convention, which for
		// a peek means an empty window and no reveal ask -- the correct
		// reading of a count this build cannot compute.
		amt = Num(h, c, sa, "PeekAmount", 1)
		if amt < 0 {
			amt = 0
		}
	}
	// The may-reveal ask (task fb-3f1cc033, Delver of Secrets' peek; widened
	// to Optional$ by the round-2 review's Look$ task): the deciding player
	// is asked whether to reveal before the Note goes out. The ask is the
	// same mid-resolution vocabulary every other asking primitive uses —
	// KChoose yes/no with a ResumeKind, the answer re-entering effReveal
	// through rules' resumeResolution with Ctx.RevealOpt set. A host that
	// cannot ask (an effects-package double, fuzz) keeps the pre-ask
	// behaviour: the mandatory reveal, as the deterministic fallback (the
	// same R-9 degradation Scry/Surveil carry). Still unread here,
	// deliberately: NoReveal$/NoPeek$ and RememberRevealedPlayer$ — see the
	// report's Issues section. PeekAmount$ and RevealValid$ ARE read (the
	// PeekAndReveal arm above takes the peek window from PeekAmount$; the
	// RevealValid$ filter below narrows the may-reveal to the matching
	// subset for every API in this row).
	answer := string("")

	// optTarget is the Defined$ target index whose yes/no answer this is (the
	// decision's ResumeTarget); it travels with the answer exactly as
	// pickTarget travels with RevealPick. An answer applies to its cursor
	// target alone and every later target poses its own ask.
	optTarget := int(0)

	// The answered hand-reveal pick (task infernaltutor1), consumed once per
	// walk exactly as RevealOpt is: a nested Reveal-family effect below this
	// one must pose its own ask instead of inheriting this walk's answer.
	// Non-nil means answered (the resume arm always builds the slice, so an
	// empty "reveal none" answer is non-nil), mirroring Ctx.Discard.
	picks := ([]state.ObjID)(nil)

	pickTarget := int(0)

	// The bare-look ack (lookack): consumed once per WALK, together with its
	// per-target cursor — the answer attaches to the exact Defined$ target
	// that asked (the decision's ResumeTarget). Targets before the cursor
	// were fully processed on the pass that suspended and are skipped, the
	// cursor target emits without re-asking, and every LATER bare look in
	// the walk poses its own ack. Consuming at walk entry (fx42) also keeps
	// a nested bare look below this walk posing its own instead of
	// inheriting the answer.
	lookAck := false
	lookAckTarget := int(0)

	look := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKLook)), "True")
	revealType := strings.TrimSpace(sa.ParamStr(cards.PKRevealType))
	// The may-reveal ask: PeekAndReveal poses it through RevealOptional$
	// (Delver of Secrets); the Reveal/RevealHand shapes pose it through
	// Optional$ ("you may reveal" — Liar's Pendulum's two RevealHand lines
	// and the corpus's five Reveal lines), which used to be ignored and the
	// reveal forced. No corpus line combines Look$ with either flag, but the
	// ask is shaped to work for one anyway: a look is asked of the LOOKER
	// (the activator gains the information), a reveal of the player whose
	// cards would be shown.
	optional := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRevealOptional)), "True") ||
		strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKOptional)), "True")
	remember := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberRevealed)), "True")
	revealDefined := sa.ParamStr(cards.PKRevealDefined)
	// RememberTargets$ remembers the CHOSEN targets (Forge's sa.getTargets()),
	// so it applies only where the walk's subjects are those targets: a
	// targeting SA with no Defined$/RevealDefined$ override.
	rememberTargets := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberTargets)), "True") &&
		TargetsOf(sa).Targeted() && DefinedRefOf(sa).Raw == "" && revealDefined == ""
	random := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRandom)), "True")
	g := h.Game()
	// Forge's RevealDefined$ is the reveal family's equivalent of Defined$:
	// it replaces only the target selector, resolved over the same ability
	// (its ValidTgts$ fallback included), so the common resolver owns every
	// Self/Targeted/Remembered spelling without mutating the shared compiled
	// corpus. This matters for opening-hand reveals: Chancellor of the Tangle
	// must reveal the chosen Chancellor, not an unrelated first card in its
	// controller's hand.
	revealRef := DefinedRefOf(sa)
	if spec := revealDefined; spec != "" {
		revealRef = RefOf(spec)
	}
	for targetIndex, t := range DefinedRef(h, c, revealRef, sa) {
		if lookAck && targetIndex < lookAckTarget {
			// The cursor skip: this target was fully processed (note emitted,
			// RememberRevealed$ captured) on an earlier pass of this same
			// resume chain, before the walk suspended on a later target's
			// ack — re-running it would duplicate its events.
			continue
		}
		if picks != nil && targetIndex < pickTarget {
			// The pick cursor's skip: the targets before it were fully
			// processed (their pick answered, their reveal emitted) on the pass
			// that suspended on the cursor target's own pick; re-running them
			// would duplicate their events and re-pose their asks.
			continue
		}
		if answer != "" && targetIndex < optTarget {
			// The optional-ask cursor's skip, the same discipline: targets
			// before the cursor were fully processed (their yes/no answered,
			// their reveal/decline applied) on the pass that suspended on the
			// cursor target's own optional ask; re-running them would
			// duplicate their events and re-pose their asks.
			continue
		}
		// answerForTarget scopes the walk's single consumed RevealOpt answer to
		// the target it was asked of. Every other target reads "" and so poses
		// its own yes/no; without this the answer answered target 0 and then
		// silently applied to every later target too.
		answerForTarget := answer
		if answer != "" && targetIndex != optTarget {
			answerForTarget = ""
		}
		// A pick answers the optional gate only for the target that posed it.
		// Later Defined$ targets still need their own may-reveal choice.
		pickForTarget := picks != nil && targetIndex == pickTarget
		// revealTarget is where a tape-served reveal_optional answer re-walks
		// its own target, exactly as the "reveal_optional" re-entry does for
		// the cursor target (the answered yes may still pose the hand pick).
	revealTarget:
		if revealRef.Has(RefPlainRemembered) && !t.IsPlayer {
			// Forge's getDefinedPlayers("Remembered") adds remembered PLAYERS
			// only; a remembered card must not widen the reveal's library/hand
			// scope to its controller (Summon: Valefor's per-opponent loop).
			continue
		}
		if rememberTargets {
			// RememberTargets$ True (Struggle for Sanity, Hint of Insanity,
			// Dreams of Steel and Oil -- every corpus Reveal-family carrier is
			// a targeted RevealHand): the chosen target player joins both
			// remembered halves, so a later Defined$ Player.IsRemembered
			// chooser (Struggle's "that player exiles a card") and a
			// RememberedPlayerCtrl/Own filter name them. It was unread here,
			// so Struggle's opponent was never asked. Placed after the cursor
			// skips, so a resumed walk never remembers a target twice.
			rememberTarget(h, c, t)
		}
		p := PlayerOf(h, c, t)
		pool := zoneOf(g, zone, p)
		if revealDefined != "" && !t.IsPlayer {
			// A RevealDefined object is itself the card to reveal, not a
			// selector for the first card in that player's zone.
			pool = []state.ObjID{t.Obj}
		}
		if revealType != "" {
			// RevealType$ (Slayer's Bounty: "look at the creature cards in
			// target opponent's hand") narrows the pool to the cards of that
			// type before any count is taken — Forge's RevealHandEffect
			// filters the hand by RevealType the same way. An unresolvable
			// spec matches nothing (the filter's fail-closed convention), so
			// a look/reveal over an unknown type shows nothing rather than
			// everything.
			filtered := make([]state.ObjID, 0, len(pool))
			for _, id := range pool {
				if MatchesSpecCtx(g, revealType, id, c.SpecContext(c.Controller)) {
					filtered = append(filtered, id)
				}
			}
			pool = filtered
		}
		if rv := strings.TrimSpace(sa.ParamStr(cards.PKRevealValid)); rv != "" {
			// RevealValid$ (Herald's Horn's Creature.ChosenType, the Kinship
			// family's Card.sharesCreatureTypeWith): the may-reveal covers
			// only the pool's matching subset — "if it's a creature card of
			// the chosen type, you may reveal it". With nothing matching there
			// is no reveal and no RememberRevealed$ capture, so a chained
			// ConditionDefined$ Remembered gate correctly skips; the same
			// fail-closed convention RevealType$ applies above.
			filtered := make([]state.ObjID, 0, len(pool))
			for _, id := range pool {
				if MatchesSpecCtx(g, rv, id, c.SpecContext(c.Controller)) {
					filtered = append(filtered, id)
				}
			}
			pool = filtered
		}
		n := amt
		if wholeHand || int32(len(pool)) < n {
			n = int32(len(pool))
		}
		if rav := strings.TrimSpace(sa.ParamStr(cards.PKRevealAllValid)); rav != "" {
			// RevealAllValid$ (Break Expectations' Card.cmcGE2+
			// TargetedPlayerCtrl, Mind Spike's Card.nonLand+nonCreature+
			// TargetedPlayerCtrl): the reveal covers EVERY card in the pool
			// matching the spec — "Target player reveals all cards with mana
			// value 2 or greater in their hand" — so the spec narrows the
			// pool and the count becomes the whole matching set. Pre-fix the
			// spec was never fed to the filter, so the reveal took pool[:1],
			// the FIRST card of the whole hand whether or not it matched. An
			// unresolvable spec matches nothing (the fail-closed convention
			// RevealType$ and RevealValid$ already apply above), so the
			// n == 0 skip below cleanly emits no Note and captures no
			// RememberRevealed$; this is also the arm that keeps the
			// revealer's own pick off the walk (the `pickable` gate reads the
			// same parameter).
			filtered := make([]state.ObjID, 0, len(pool))
			for _, id := range pool {
				if MatchesSpecCtx(g, rav, id, c.SpecContext(c.Controller)) {
					filtered = append(filtered, id)
				}
			}
			pool = filtered
			n = int32(len(pool))
		}
		// A hand reveal is a CHOICE when the eligible pool holds strictly more
		// cards than the answer must show. Forge asks the pool's owner which
		// cards to reveal -- Infernal Tutor's "Reveal a card from your hand"
		// (NumCards default 1 over a seven-card hand), an AnyNumber$ miss
		// ("Reveal any number of green cards in your hand": zero through all
		// of them) and an Optional$ miss ("You may reveal a Dinosaur card from
		// your hand": none or the one). Pre-fix the walk silently took
		// pool[:n], the FRONT cards of the hand, so the chained sub read the
		// wrong card entirely. The pick is posed as a KChoose to the pool's
		// owner and carried back on Ctx.RevealPick, the same answer-shape the
		// discard ask uses.
		//
		// Not pickable, deliberately: RevealHand (the whole hand is public, no
		// choice), Random$ (the engine picks, deterministically), a Look$
		// (the looker sees the whole filtered set; Slayer's Bounty), and the
		// RevealAllValid$ family (a filter that reveals EVERY match -- the
		// revealer chooses nothing; the caster's later pick is a separate
		// sub-ability). RevealValid$/RevealType$ narrow the pool BEFORE the
		// pick, exactly as Forge's own filter does, so the options are the
		// matching cards alone.
		revealAllValid := strings.TrimSpace(sa.ParamStr(cards.PKRevealAllValid))
		pickable := zone == state.ZHand && !wholeHand && !random && !look && revealAllValid == ""
		if pickable {
			minPick, maxPick := n, n
			if anyNumber := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKAnyNumber)), "True"); anyNumber {
				minPick, maxPick = 0, int32(len(pool))
			}
			if maxPick > int32(len(pool)) {
				maxPick = int32(len(pool))
			}
			if minPick > maxPick {
				minPick = maxPick
			}
			// A real choice exists only when strictly more eligible cards than
			// the answer's minimum. A hand of exactly the mandatory count (or
			// fewer) must show all of them with no question, the same
			// strict-supersets discipline effDiscard applies. An Optional$
			// reveal answers its own yes/no ask FIRST (the block below); the pick
			// then poses on that accepted resume. The answered pick suppresses
			// that optional question only for its own target, so every later
			// Defined$ target still receives its own may-reveal ask (fx42).
			// A DECLINED optional (fx45) must never reach the pick: the
			// reveal_optional resume sets answer == "no", which makes
			// deferToOptionalAsk false, and the block below then posed a
			// MANDATORY reveal_pick over the declined cards (measured: a
			// two-card hand and `SP$ Reveal | Defined$ You | Optional$ True`
			// resumed into a Min/Max 1/1 pick instead of finishing). The
			// decline `continue` below runs after this block, so gate here.
			declined := optional && answerForTarget == "no"
			deferToOptionalAsk := optional && answerForTarget == "" && !pickForTarget
			if int32(len(pool)) > minPick && !deferToOptionalAsk && !declined {
				// The answer applies to exactly the cursor target: a pickable
				// reveal over several Defined$ players poses one ask per target,
				// and the re-entered walk must not apply target 0's answer to
				// target 1's distinct hand (its ids cannot occur there, so the
				// pool would empty and every later player would be silently
				// skipped). Every non-cursor target poses its own ask below.
				hasAnswer := pickForTarget
				if !hasAnswer {
					opts := make([]decision.Option, 0, len(pool))
					for _, id := range pool {
						name := "a card"
						if o := g.Obj(id); o != nil && o.Face() != nil {
							name = o.Face().Name
						}
						opts = append(opts, decision.Option{Index: len(opts), Kind: "reveal",
							Label: "Reveal " + name, Obj: id, Player: p})
					}
					prompt := "Choose " + strconv.Itoa(int(minPick)) + ".." + strconv.Itoa(int(maxPick)) + " card(s) to reveal"
					if minPick == 0 {
						prompt = "You may reveal 0.." + strconv.Itoa(int(maxPick)) + " card(s)"
					}
					d := &decision.Decision{Player: p, Kind: decision.KChoose,
						Min: int(minPick), Max: int(maxPick), Source: c.Source,
						ResumeKind: "reveal_pick", ResumeSA: sa,
						ResumeTarget: targetIndex,
						Prompt:       prompt,
						Options:      opts}
					if ans, ok := AskTape(h, d); ok {
						// The resolution kernel's answer in hand: the same
						// narrowing the "reveal_pick" re-entry applies below.
						pool = revealPickSelected(pool, answerObjs(ans))
						n = int32(len(pool))
						pickForTarget = true
					} else {
						_ = Ask(h, d)
					}

					// No host to ask (R-9): fall through with n unchanged, so
					// the reveal takes the same first maxPick cards the pre-pick
					// build did -- the reveal family's existing no-host
					// convention (the Optional$ ask falls through the same way),
					// deterministic run to run and byte-identical for fuzz.
				} else {
					// The answer: reveal exactly the chosen cards, in answer order,
					// filtered against the pool the re-entry rebuilt (a card that left
					// the hand meanwhile cannot be revealed). Replacing the pool --
					// rather than the emit below -- keeps the Note/RememberRevealed$
					// payload in one place. minPick/maxPick are deliberately not
					// re-enforced here: the resume rebuilt the pool from live state,
					// and a client's validated answer is trusted.
					pool = revealPickSelected(pool, picks)
					n = int32(len(pool))
				}
			}
		}
		if random && len(pool) > 0 {
			// Random$ True (Urza's Bauble: "Look at a card at random in target
			// player's hand"): the pool narrows to n DISTINCT random cards,
			// drawn from the engine's seeded generator through Host.Rand — the
			// same deterministic source choose_control's AtRandom/Random
			// discards use — so the pick replays identically. The narrowing
			// runs AFTER the count is resolved (Rise // Fall's NumCards$ 2
			// reveals two, not one); a partial Fisher-Yates over a copy picks
			// the n cards without repeating one. For n==1 the shuffle's first
			// swap is exactly the old single h.Rand(len(pool)) pick, so every
			// seeded chain that ran the one-card shape replays byte-identically.
			// The pick is a LOOK, not a reveal: the NoReveal$ arm below is the
			// corpus's carrier (Urza's Bauble reveals nothing of what it saw —
			// the activator alone learns the card), and the public Note below
			// is skipped for it.
			rp := append([]state.ObjID(nil), pool...)
			for i := 0; i < int(n); i++ {
				j := i + h.Rand(len(rp)-i)
				rp[i], rp[j] = rp[j], rp[i]
			}
			pool = rp[:n]
		}
		if n == 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKNoReveal)), "True") {
			// NoReveal$ True (Mishra's Bauble: "Look at the top card of target
			// player's library" — a look, never a reveal): the identity goes
			// to the ACTIVATOR alone through emitLook, the one private-look
			// channel (CR 701.20e's shown-only-to-the-looker rule), and the
			// public Note the reveal path emits does not happen. RememberRevealed$
			// finds nothing — a chained gate correctly does not fire; the
			// corpus's NoReveal$ carriers chain zone-less riders (Mishra's
			// slowtrip DelayedTrigger) that do not read the walked Remembered.
			//
			// The look is also the one information transfer with no decision
			// attached, which a client's auto-passing priority streams past
			// unread — the pacing defect the reporter hit. The bare look now
			// gates on the look_ack ack FIRST (ask-first: ask → suspend → the
			// resume arm sets Ctx.LookAck → the re-entered walk lands the note
			// below the modal). A Random$ narrowing re-derives from the seeded
			// generator on every pass, so a random bare look would show the
			// resume a DIFFERENT card than the prompt named — it keeps the
			// ungated shape (measured at the corpus pin: ZERO Reveal-family
			// lines combine Random$ with NoReveal$/Look$; the one Random$
			// carrier, Urza's Bauble, is the public-reveal path). The ack is
			// addressed by the per-target cursor (LookAckTarget, the decision's
			// ResumeTarget): the cursor target emits without re-asking, and a
			// later bare look in the same walk poses its own ack — consuming
			// the flag at the FIRST bare-look target instead would leave the
			// later target's ack unanswered and loop forever.
			if lookAck && targetIndex == lookAckTarget {
				// The answered target: its ack's resume pass re-entered here, so
				// fall through to the emit without re-asking.
			} else if !random {
				if poseLookAck(h, c, sa, c.Controller, p, zone, pool[:n], targetIndex) {
					return
				}
				// No host to ask (R-9), or the ask was skipped: fall through
				// and emit the look immediately — information is never lost to
				// a host that cannot ask, the same deterministic degradation
				// Scry/Surveil carry.
			}
			emitLook(h, []state.PlayerID{c.Controller}, zone, pool[:n], "")
			if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberPeeked)), "True") {
				next := make([]state.Target, 0, len(c.Remembered)+int(n))
				next = append(next, c.Remembered...)
				for _, id := range pool[:n] {
					next = append(next, state.Target{Obj: id})
					eventRemember(h, c, id)
				}
				c.Remembered = next
			}
			continue
		}
		asker := p
		if look {
			asker = c.Controller
		}
		if optional && answerForTarget == "" && !pickForTarget {
			// The peek ask's wording and payload are byte-stable: a golden
			// game (Delver of Secrets) poses exactly this ask.
			var prompt, yesLabel string
			options := []decision.Option{
				{Index: 0, Kind: "yes", Label: "", Player: asker},
				{Index: 1, Kind: "no", Label: "No", Player: asker},
			}
			if sa.API == "PeekAndReveal" && !look {
				// The ask must carry WHAT is being revealed: the peeking player is
				// deciding whether to reveal a card only they can see, and the
				// library is not projected to that seat (view exposes only
				// LibrarySize), so a count-only prompt asks a blind question.
				// The card names go into the prompt and the yes option's label,
				// and the top card rides the option's Obj — the same private
				// channel the hidden-library "search" options use (view.project
				// attaches a decision only to its own Decision.Player, so this
				// payload reaches the peeking seat alone; even an Omniscient
				// spectator gets no decision).
				names := make([]string, 0, n)
				for _, id := range pool[:n] {
					if o := g.Obj(id); o != nil && o.Face() != nil {
						names = append(names, o.Face().Name)
					}
				}
				prompt = "Reveal the top " + strconv.Itoa(int(n)) + " card(s) of your library?"
				if len(names) > 0 {
					prompt = "Reveal the top " + strconv.Itoa(int(n)) + " card(s) of your library — " + strings.Join(names, ", ") + "?"
				}
				yesLabel = "Yes — reveal"
				if len(names) == 1 {
					yesLabel = "Yes — reveal " + names[0]
				}
				options[0].Obj = pool[0]
			} else {
				// The decider already owns what is being decided over (a hand
				// reveal asks its owner; a look asks its looker), so the ask
				// carries no payload of its own — what "yes" later emits
				// reaches exactly the seats the shape allows.
				if look {
					prompt, yesLabel = "Look at the target player's hand?", "Yes — look"
				} else {
					prompt, yesLabel = "Reveal your hand?", "Yes — reveal"
				}
			}
			options[0].Label = yesLabel
			d := &decision.Decision{Player: asker, Kind: decision.KChoose, Min: 1, Max: 1,
				ResumeKind: "reveal_optional", ResumeSA: sa, Source: c.Source,
				ResumeTarget: targetIndex,
				Prompt:       prompt,
				Options:      options}
			if ans, ok := AskTape(h, d); ok {
				// The resolution kernel's answer in hand (the
				// "reveal_optional" arm's RevealOpt): re-walk this target
				// with it, as the cursor target's re-entry does.
				answerForTarget = "no"
				if answerYes(ans) {
					answerForTarget = "yes"
				}
				goto revealTarget
			}
			_ = Ask(h, d)

		}
		if optional && answerForTarget == "no" {
			// Declined: no Note, and RememberRevealed$ finds nothing —
			// a chained gate (Delver's ConditionDefined$ Remembered)
			// correctly does not fire. The walk continues.
			continue
		}
		revealed := append([]state.ObjID(nil), pool[:n]...)
		if look {
			// CR 701.20e: a card looked at this way is shown only to the
			// player the effect specifies — the activator — so the record is
			// a Secret Note scoped to the looker (emitLook), NOT the public
			// Note the pre-fix build emitted here (the round-2 review's
			// Gitaxian Probe leak: every seat and spectator read the target's
			// whole hand off it). RememberRevealed$ below still sees the
			// looked-at cards: the chained subs that read Remembered are part
			// of the same walk the looker's own card drives.
			//
			// The mandatory look is gated on the look_ack ack (lookack) like
			// the NoReveal$ arm above — ask-first, the note lands on the
			// resume pass, addressed by the same per-target cursor. The
			// Look$+Optional$ combination keeps the reveal_optional ask ALONE:
			// the player who just answered "yes — look" has consented to the
			// follow-through, so no second gate (the brief's scope boundary;
			// measured at the corpus pin: zero corpus lines combine Look$ with
			// Optional$/RevealOptional$).
			if lookAck && targetIndex == lookAckTarget {
				// The answered target: its ack's resume pass re-entered here, so
				// fall through to the emit without re-asking.
			} else if !optional && !random {
				// The same Random$ re-derivation guard the NoReveal$ arm
				// carries (measured: zero corpus lines combine Random$ with
				// Look$, so the guard is dormant groundwork).
				if poseLookAck(h, c, sa, asker, p, zone, revealed, targetIndex) {
					return
				}
				// R-9: no host to ask — emit immediately, deterministically.
			}
			emitLook(h, []state.PlayerID{asker}, zone, revealed, "")
		} else {
			// No Text: the Note's payload is the ids, and view.Describe renders
			// them ("player 0 reveals Mountain #82") — defect 1's second half,
			// the client's only data path for hidden-zone ids in a reveal. A
			// Text-carrying Note would need the names baked in at emit time,
			// duplicating Describe's obj() naming; an empty Text with ids keeps
			// the naming in one place. Ruling T23-w still passes the Note
			// through RedactEvents unchanged (it is non-Secret).
			h.Emit(events.Event{Kind: events.Note, Player: p, IDs: revealed})
			// The opening-hand RevealCard ability of Impatient Iguana carries
			// this flag. The public reveal happened, so its "If you do" clause
			// takes effect as a replayed state transition; a declined optional
			// reveal reaches the continue above and cannot change the starter.
			if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKBecomeStartingPlayer)), "True") {
				h.Emit(events.Event{Kind: events.StartingPlayerChange, Player: c.Controller})
			}
		}
		if remember {
			// RememberRevealed$ (task fb-3f1cc033): the revealed cards join
			// the walk's Remembered set, where a chained ConditionDefined$
			// Remembered gate (Delver's transform) reads them. Fresh backing
			// array: on an ability resume Ctx.Remembered aliases the stack
			// object's own Remembered slice, and appending in place would
			// write shared state without an event. Measured at the corpus
			// pin: of the 67 PeekAndReveal+RememberRevealed SVar lines, 43
			// have downstream subs that read Remembered — all of them
			// condition gates or Defined$ Remembered bodies that Forge
			// itself intends to see the reveal (the Kinship family), so
			// inheriting the reveal here is the semantics, not a leak.
			// The revealed cards ALSO join the source object's event-backed
			// Remembered list (eventRemember, the rememberMilled two-halves
			// discipline): Forge's host.addRemembered is the PERSISTENT host
			// card list, and Count$RememberedSize reads only the source half
			// — Temple of the Dragon Queen's DragonPresence gate counts the
			// remembered reveal through it (a ctx-only capture is invisible
			// there, and a ctx-first RememberedSize read would over-count
			// every trigger resolution's capture seed — the Mind Maggots
			// defect the ctx-preference attempt caused).
			next := make([]state.Target, 0, len(c.Remembered)+len(revealed))
			next = append(next, c.Remembered...)
			for _, id := range revealed {
				next = append(next, state.Target{Obj: id})
				eventRemember(h, c, id)
			}
			c.Remembered = next
		}
	}
}

// revealPickSelected narrows a hand-reveal pool to the answered picks, in
// answer order: a card that left the pool meanwhile cannot be revealed. The
// one home of the "reveal_pick" answer, shared by the re-entry and the
// resolution kernel's tape answer.
func revealPickSelected(pool, picks []state.ObjID) []state.ObjID {
	selected := make([]state.ObjID, 0, len(picks))
	for _, id := range picks {
		for _, cand := range pool {
			if cand == id {
				selected = append(selected, id)
				break
			}
		}
	}
	return selected
}

// effRearrangeTopOfLibrary looks at the top NumCards of Defined$'s library
// and poses a KArrange decision over them: the player picks the order, and
// rules' handleArrange applies it as an events.LibraryOrder (Ruling J0/J1
// - the one general decision shape Scry, Surveil and Dig later share).
//
// Unlike effReveal's Note (a deliberate reveal, public to every seat), the
// private-look record is a Secret Note: only p, the library's own owner, may
// know what sat on top. Ruling T23-w makes a Note public by default
// (view.RedactEvents' rule 3 exempts Note entirely, on the theory that a
// Note IS the engine's "tell everyone" channel), so the one Note that must
// stay private has to opt OUT by being Secret -- the same shape
// rules/engine.go's Shuffle and this file's own effDraw already use for
// their own hidden-zone payloads. The Note records the look, not the order;
// the order that follows is the player's to choose.
//
// Min == Max == k (pile B is empty for a full reorder), one option per top
// card in top-down order, each Option.Kind "bottom" (nothing goes there for
// a reorder, but the vocabulary stays uniform so Scry/Surveil reuse it
// unchanged). Decision.Player is p, the library's owner — the player who is
// looking at and reordering their own top cards.
func effRearrangeTopOfLibrary(h Host, c *Ctx, sa *cards.SA) {
	// MayShuffle$ True (Ponder's "You may shuffle."): after the arrange is
	// applied, an optional shuffle. The ask is posed on the arrange RE-ENTRY
	// pass only -- the arrangement has been applied by then, so the shuffle
	// question asks about a settled library. The answer flows back through
	// the "arrange_mayshuffle" resume arm, which emits the Shuffle event
	// itself and re-enters this effect with both Arrange and MayShuffle set;
	// the MayShuffle done-marker (consumed and cleared here, fx42 scoping)
	// keeps that third pass from posing the ask again.
	mayShuffle := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKMayShuffle)), "True")
	// Re-entry after rules' handleArrange applied the answered KArrange starts
	// at the next target. If MayShuffle is present, that answer belongs to the
	// target at LibraryTarget; otherwise the current target still needs its
	// post-arrange shuffle election. In both cases the walk then continues to
	// later libraries.
	start := 0

	n := Num(h, c, sa, "NumCards", 1)
	if n < 0 {
		n = 0
	}
	g := h.Game()
	for targetIndex, t := range actingPlayers(h, c, sa) {
		if targetIndex < start {
			continue
		}
		c.LibraryTarget = targetIndex
		p := t
		lib := zoneOf(g, state.ZLibrary, p)
		k := n
		if int32(len(lib)) < k {
			k = int32(len(lib))
		}
		emitLook(h, []state.PlayerID{p}, state.ZLibrary, nil, "looks at the top of the library")
		d := &decision.Decision{Player: p, Kind: decision.KArrange,
			Min:          int(k),
			Max:          int(k),
			Source:       c.Source,
			ResumeKind:   "arrange",
			ResumeSA:     sa,
			ResumeTarget: targetIndex,
			Prompt:       "Rearrange the top " + strconv.Itoa(int(k)) + " card(s); the first card you pick goes on top"}
		for i := int32(0); i < k; i++ {
			name := "a card"
			if o := g.Obj(lib[i]); o != nil && o.Face() != nil {
				name = o.Face().Name
			}
			d.Options = append(d.Options, decision.Option{Index: int(i),
				Kind: "bottom", Label: name, Obj: lib[i], Player: p})
		}
		// The shared ask boundary (effects.Ask) refuses to post a KArrange
		// whose only legal answer is the empty one: with an empty library (or
		// NumCards$ 0) k is 0, Min == Max == 0 and there are no options -- the
		// exact wedge shape. AskEmpty (and AskNoHost alike) resolves through
		// the stand-in below: the order is (re)set unchanged and the
		// resolution completes.
		if _, ok := AskTapeIntent(h, d); ok {
			// The resolution kernel served the answer and its record applied
			// the arrangement (the KArrange answer record handleArrange
			// shares); the re-entry's MayShuffle$ election follows here.
			if mayShuffle && rearrangeMayShuffleTape(h, c, sa, p, targetIndex) {
				return
			}
			continue
		}
		_ = Ask(h, d)

		// Fuzz/no-engine host: the deterministic stand-in keeps the existing
		// order -- pile A = the offered options in offered order (J3) -- with
		// the LibraryOrder recording that the order was (re)set unchanged.
		h.Emit(events.Event{Kind: events.LibraryOrder, Player: p,
			IDs:    append([]state.ObjID(nil), lib...),
			Secret: true})
	}
}

// rearrangeMayShuffleTape is effRearrangeTopOfLibrary's MayShuffle$ election
// on the resolution kernel's path, after the arrange answer for target
// index i was served: the same ask the arrange re-entry poses, answered from
// the tape (or, with the tape exhausted, posed and unwound). It reports a
// legacy suspension (the kernel declined the ask, so the legacy ask took it
// and the run falls back), on which the caller returns.
func rearrangeMayShuffleTape(h Host, c *Ctx, sa *cards.SA, p state.PlayerID, i int) bool {
	d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: c.Source, ResumeKind: "arrange_mayshuffle", ResumeSA: sa,
		ResumeTarget: i, Prompt: "Shuffle your library?",
		Options: []decision.Option{
			{Index: 0, Kind: "yes", Label: "Yes — shuffle", Player: p},
			{Index: 1, Kind: "no", Label: "No — keep the order", Player: p},
		}}
	if ans, ok := AskTape(h, d); ok {
		if len(ans) > 0 && ans[0].Kind == "yes" {
			shuffleLibraryOrder(h, p)
		}
		return false
	}
	return Ask(h, d) == AskAsked
}

// effScry implements the Scry prompt API (CR 701.18): look at the top
// ScryNum$ cards of Defined$'s library, put any number of them -- the ones
// the player does NOT pick -- on the BOTTOM of the library in the order they
// were offered, and the rest back on top in the order the player picks.
// It is the KArrange ask with Min 0, Max N and Option.Kind "bottom".
//
// The look is recorded as a Secret Note (the player alone may know what sat
// on top), then the answer is applied by rules' handleArrange, which routes
// the unchosen pile B to the destination named by the shared Kind
// ("bottom"). Re-entry after handleArrange set Ctx.Arrange must only let
// the chained SubAbility$ run, never re-ask -- the same done-marker
// discipline effRearrangeTopOfLibrary uses. The no-host stand-in (R-9)
// keeps every card on top in its existing order (pile B empty), which is
// narrower than the card text but deterministic.
func effScry(h Host, c *Ctx, sa *cards.SA) {
	// ScryNum$ is read HERE, at the api:Scry implementation, not inside the
	// shared KArrange body: the shared body takes the resolved count, so the
	// parameter read is a literal-key read on this API's own SA (Kozilek's
	// Command's `ScryNum$ X` Charm mode resolves the announced X through the
	// same Num grammar a literal would take).
	n := Num(h, c, sa, "ScryNum", 1)
	effLookAndArrange(h, c, sa, n, "bottom", "Scry", nil, false)
}

// effSurveil implements the Surveil prompt API (CR 701.42): look at the top
// Amount$ cards of Defined$'s library, put any number of them -- the ones
// the player does NOT pick -- into the GRAVEYARD (surveil has no bottom
// pile; the fsv1 survey's bottom-pile reading is wrong and this code follows
// CR 701.42), and the rest back on top in the order the player picks. It is
// the KArrange ask with Min 0, Max N and Option.Kind "graveyard".
//
// Re-entry and the no-host stand-in are exactly effScry's (the same shared
// helper): the stand-in puts nothing in the graveyard, which is narrower
// than the card text but deterministic.
//
// stat:SurveilNum raises the count ("You may look at an additional two
// cards each time you surveil"): the battlefield statics are read per
// surveilling player by rules (Host.SurveilLookExtra, the canonical
// activeStatics collector), and each Optional$ static's "may" is an
// independent election (surveilnum-r2): the ask offers ONE option per
// optional static and the player accepts any subset, never one
// all-or-nothing yes/no over the summed entries. The answered accepted
// ordinals ride Ctx.SurveilLookOpt (a CSV done-marker) and are consumed and
// cleared here (fx42 scoping), so a nested Surveil poses its own ask; the
// answer applies to the ASKING player only -- the first acting player
// carrying optionals, even when the Surveil resolves for several players
// later libraries still arrange, but keep their own base count -- and anything
// else (the no-host R-9 decline included) keeps the base count.
func effSurveil(h Host, c *Ctx, sa *cards.SA) {
	n := Num(h, c, sa, "Amount", 1)
	// The arrange re-entry pass (ctx.Arrange set by rules' handleArrange) must
	// go straight to effLookAndArrange's done-marker return: each resume
	// builds a fresh Ctx (fx42), so on that pass SurveilLookOpt is empty again
	// and re-posing the election here would ping-pong election -> arrange ->
	// election forever (the may-look answer belongs to the pass that posed
	// the KArrange, which already priced the extra cards into its window).
	arranging := false
	ans := string("")

	if ans == "" && !arranging {
		// First pass: pose the election once, for the FIRST acting player
		// carrying optionals. The answer belongs to that player and is carried
		// through extraOf below; later libraries keep their own base count.
		if players := actingPlayers(h, c, sa); len(players) > 0 {
			p := players[0]
			if _, opts := h.SurveilLookExtra(p); len(opts) > 0 {
				d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 0, Max: len(opts),
					Source: c.Source, ResumeKind: "surveil_look_optional", ResumeSA: sa,
					Prompt: "You may look at additional card(s) each time you surveil"}
				for i, v := range opts {
					d.Options = append(d.Options, decision.Option{Index: i, Kind: "static",
						Label:  "Look at " + strconv.Itoa(int(v)) + " additional card(s) each time you surveil",
						Player: p})
				}
				// AskAsked suspends; the answer re-enters with Ctx.SurveilLookOpt
				// carrying the accepted ordinals. A no-host (fuzz/effects-test
				// double) falls through to the mandatory-only surveil below:
				// declining the ELECTION must not drop the base Surveil, whose
				// own no-host path inside effLookAndArrange applies the standing
				// LibraryOrder stand-in (R-9). A real host's decline re-enters
				// with the "no" marker and reaches the same fall-through through
				// the ans != "" gate. The resolution kernel's tape answer is
				// that same marker, read here and carried on.
				if picks, ok := AskTape(h, d); ok {
					ans = SurveilLookOptAnswer(picks)
				} else {
					_ = Ask(h, d)
				}

			}
		}
	}
	// accepted holds the election's answered ordinals, indexed into the
	// deterministic optionals list the asking player's read returns. It is
	// derived on every pass (the answer re-enters with a fresh Ctx carrying
	// only the CSV marker); the read is deterministic, so the ordinals land
	// on the same statics that were offered.
	accepted := map[int]bool{}
	if ans != "" && ans != "no" {
		for tok := range strings.SplitSeq(ans, ",") {
			if i, err := strconv.Atoi(strings.TrimSpace(tok)); err == nil && i >= 0 {
				accepted[i] = true
			}
		}
	}
	// surveilAsker is the player the election belongs to: the first acting
	// player carrying optionals, re-derived the same way on every pass. The
	// accepted extras are applied to that player ONLY -- a multi-player
	// Surveil's other libraries keep their base count.
	surveilAsker := func() (state.PlayerID, bool) {
		players := actingPlayers(h, c, sa)
		if len(players) == 0 {
			return 0, false
		}
		p := players[0]
		if _, opts := h.SurveilLookExtra(p); len(opts) > 0 {
			return p, true
		}
		return 0, false
	}
	extraOf := func(p state.PlayerID) int32 {
		mand, opts := h.SurveilLookExtra(p)
		total := mand
		if asker, ok := surveilAsker(); ok && p == asker {
			for i := range opts {
				if accepted[i] {
					total += opts[i]
				}
			}
		}
		return total
	}
	effLookAndArrange(h, c, sa, n, "graveyard", "Surveil", extraOf, true)
}

// SurveilLookOptAnswer is the "surveil_look_optional" answer marker
// effSurveil reads: the accepted static ordinals as a CSV, or "no" for the
// empty answer (the real decline of every static). One home for rules'
// resume arm and the resolution kernel's tape answer.
func SurveilLookOptAnswer(chosen []decision.Option) string {
	if len(chosen) == 0 {
		return "no"
	}
	parts := make([]string, 0, len(chosen))
	for _, o := range chosen {
		parts = append(parts, strconv.Itoa(o.Index))
	}
	return strings.Join(parts, ",")
}

// effLookAndArrange is the shared KArrange body behind effScry and
// effSurveil: the base count (ScryNum$ / Amount$, default 1) is resolved by
// the calling api implementation through Num; this body resolves Defined$
// (default = the ability's source, hence its controller), adds extraOf's
// per-player addition (the stat:SurveilNum static; nil for a Scry), and
// poses one KArrange decision per target library over the top min(N,
// len(lib)) cards. The unchosen pile B's destination is the shared
// Option.Kind passed in; only that differs between the two primitives.
//
// markSurveil selects the one verb-specific record emitted HERE: Surveil
// emits ONE events.Surveil marker per acting player -- the canonical record
// trig:Surveil matches ("whenever you surveil" -- Mirko, Obsessive
// Theorist; Dimir Spybug; Thoughtbound Phantasm; Whispering Snitch) -- while
// Scry's events.Scry record is emitted by rules (handleArrange for an
// answered ask; the stand-in's EmitScryRecord below for one that never was),
// so effects emits no Scry record of its own here. The marker is emitted
// INSIDE the per-player loop,
// at the point that player's arrangement is actually performed, NOT for
// every defined target up front. A suspended player's re-entry
// (Ctx.Arrange set) skips the completed target, so its marker is not
// re-emitted; the no-host stand-in and continuation passes each record only
// the library whose arrangement they reach.
func effLookAndArrange(h Host, c *Ctx, sa *cards.SA, n int32, kind, verb string, extraOf func(state.PlayerID) int32, markSurveil bool) {
	// Re-entry after rules' handleArrange resumes with the next library. The
	// cursor is shared with RearrangeTopOfLibrary, so every Defined$/targeted
	// Scry or Surveil library gets its own ask.
	start := 0

	if n < 0 {
		n = 0
	}
	g := h.Game()
	for targetIndex, t := range actingPlayers(h, c, sa) {
		if targetIndex < start {
			continue
		}
		c.LibraryTarget = targetIndex
		p := t
		if !markSurveil && strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKOptional)), "True") {
			opt := string("")

			if opt == "" {
				d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
					Source: c.Source, ResumeKind: "scry_optional", ResumeSA: sa, ResumeTarget: targetIndex,
					Prompt: "Scry?", Options: []decision.Option{
						{Index: 0, Kind: "yes", Label: "Yes", Player: p},
						{Index: 1, Kind: "no", Label: "No", Player: p},
					}}
				if ans, ok := AskTape(h, d); ok {
					// The resolution kernel's answer in hand (the
					// "scry_optional" arm's ScryOpt): a decline skips this
					// library, a yes scries it now.
					if len(ans) == 0 || ans[0].Kind != "yes" {
						continue
					}
				} else if Ask(h, d) != AskNoHost {
					return
				} else {
					// R-9: an unavailable host deterministically declines an
					// optional election; never treat an unanswered ask as consent.
					continue
				}
			} else if opt != "yes" {
				continue
			}
		}
		if markSurveil {
			h.Emit(events.Event{Kind: events.Surveil, Player: p, Obj: c.Source})
		}
		lib := zoneOf(g, state.ZLibrary, p)
		k := n
		if extraOf != nil {
			k += extraOf(p)
		}
		if verb == "Scry" {
			// The order choice parks the proposal before inspecting the library.
			// On re-entry consume its result once rather than replacing it again.
			proceed := true

			var pending bool
			k, proceed, pending = h.Scry(p, c.Source, k, sa, targetIndex)
			if pending {
				return
			}

			if !proceed {
				continue
			}
		}
		if k < 0 {
			k = 0
		}
		if int32(len(lib)) < k {
			k = int32(len(lib))
		}
		emitLook(h, []state.PlayerID{p}, state.ZLibrary, nil, "looks at the top of the library")
		d := &decision.Decision{Player: p, Kind: decision.KArrange,
			Min:          0,
			Max:          int(k),
			Restable:     true,
			Source:       c.Source,
			ResumeKind:   "arrange",
			ResumeSA:     sa,
			ResumeTarget: targetIndex,
			Prompt:       verb + " " + strconv.Itoa(int(k)) + ": pick the cards to keep on top, in order; the rest go to " + destinationPhrase(kind) + " in any order you give"}
		for i := int32(0); i < k; i++ {
			name := "a card"
			if o := g.Obj(lib[i]); o != nil && o.Face() != nil {
				name = o.Face().Name
			}
			d.Options = append(d.Options, decision.Option{Index: int(i),
				Kind: kind, Label: name, Obj: lib[i], Player: p})
		}
		// The shared ask boundary (effects.Ask) refuses to post a KArrange
		// whose only legal answer is the empty one: with an empty library (or
		// ScryNum$/SurveilNum$ 0) k is 0, Min 0 / Max 0 and there are no
		// options -- the exact wedge shape. AskEmpty (and AskNoHost alike)
		// resolves through the stand-in below: every zero cards keep their
		// place and the resolution completes.
		if _, ok := AskTapeIntent(h, d); ok {
			// The resolution kernel served the answer and its record applied
			// it (the KArrange answer record handleArrange shares): on to
			// the next library.
			continue
		}
		_ = Ask(h, d)

		// Fuzz/no-engine host: the deterministic stand-in keeps every card
		// on top in its existing order (pile B empty for a Scry, nothing to
		// the graveyard for a Surveil), with the LibraryOrder recording that
		// the order was (re)set unchanged.
		h.Emit(events.Event{Kind: events.LibraryOrder, Player: p,
			IDs:    append([]state.ObjID(nil), lib...),
			Secret: true})
		// A Scry that completes HERE -- no-host, or the never-posted empty
		// KArrange an empty library or ScryNum$ 0 produces -- still completed:
		// record its zero-card bottom pile (task scrybottom) through
		// EmitScryRecord, the same outside-the-replacement-pass route
		// handleArrange uses, so trig:Scry's plain "whenever you scry" fires
		// once per instruction and the ToBottom$ True gate stays closed. The
		// Surveil marker above already covers both routes for Surveil.
		if verb == "Scry" {
			h.EmitScryRecord(events.Event{Kind: events.Scry, Player: p,
				Obj: c.Source, Amount: 0})
		}
	}
}

// destinationPhrase names pile B's destination in the human-readable prompt;
// it mirrors the Option.Kind vocabulary so the prompt and the wire never
// disagree.
func destinationPhrase(kind string) string {
	switch kind {
	case "bottom":
		return "the bottom of your library"
	case "graveyard":
		return "your graveyard"
	case "exile":
		return "exile"
	case "hand":
		return "your hand"
	default:
		return "their destination"
	}
}

// effNameCard records a card-name choice. The real name is asked at cast
// time and recorded with a Choose event before this ever resolves (plan
// ruling R-6), so a source already carrying ChosenName is a no-op. Without
// one -- a script that uses NameCard outside an ETB replacement -- it names
// the first card in the controller's library, which is at least a
// deterministic, legal name for whatever downstream sub-ability expects
// one, recorded now as the Choose event so the choice survives replay.
// effHideaway implements CR 702.75: when a permanent with Hideaway enters,
// its controller looks at the top N cards, exiles one face down, then puts
// the rest on the bottom in the order they chose. Exile provenance is carried
// by MoveZone, so a later Play resolves Defined$ ExiledWith by identity.
func effHideaway(h Host, c *Ctx, sa *cards.SA) {

	g := h.Game()

	n := int(Num(h, c, sa, "Amount", 4))
	if n < 0 {
		n = 0
	}
	lib := zoneOf(g, state.ZLibrary, c.Controller)
	if n > len(lib) {
		n = len(lib)
	}
	if n == 0 {
		return
	}
	d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: c.Source, ResumeKind: "hideaway_pick", ResumeSA: sa,
		Prompt: "Choose a card to exile with Hideaway"}
	for i, id := range lib[:n] {
		d.Options = append(d.Options, decision.Option{Index: i, Kind: "hideaway", Label: objName(g, id), Obj: id, Player: c.Controller})
	}
	if ans, ok := AskTape(h, d); ok {
		// The resolution kernel's answer in hand: the "hideaway_pick"
		// re-entry's exile and bottom arrange.
		id := state.ObjID(0)
		if len(ans) == 1 {
			id = ans[0].Obj
		}
		hideawayPicked(h, c, sa, id)
		return
	}
	if h.Ask(d) {
		return
	}
	// The no-host degradation chooses the first card, then retains the offered
	// order for the rest on the bottom.
	h.Emit(events.Event{Kind: events.MoveZone, Obj: lib[0], From: state.ZLibrary, To: state.ZExile,
		Counter: "exiled_with_face_down", Amount: int32(c.Source), Secret: true})
	hideawayBottom(h, c, sa)
}

// hideawayPicked exiles the answered Hideaway card face down (validated
// against the library: a card that left meanwhile is not moved, and nothing
// follows) and then arranges the rest on the bottom. The one home of the
// "hideaway_pick" answer, shared by the re-entry and the resolution
// kernel's tape answer. Moving the card first leaves precisely the remaining
// cards at the top of the library for the KArrange handler.
func hideawayPicked(h Host, c *Ctx, sa *cards.SA, id state.ObjID) {
	lib := zoneOf(h.Game(), state.ZLibrary, c.Controller)
	if id == 0 || !containsObj(lib, id) {
		return
	}
	h.Emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZExile,
		Counter: "exiled_with_face_down", Amount: int32(c.Source), Secret: true})
	hideawayBottom(h, c, sa)
}

func hideawayBottom(h Host, c *Ctx, sa *cards.SA) {
	lib := zoneOf(h.Game(), state.ZLibrary, c.Controller)
	n := int(Num(h, c, sa, "Amount", 4)) - 1
	if n < 0 {
		n = 0
	}
	if n > len(lib) {
		n = len(lib)
	}
	if n == 0 {
		return
	}
	d := &decision.Decision{Player: c.Controller, Kind: decision.KArrange, Min: n, Max: n,
		Source: c.Source, ResumeKind: "hideaway_arrange", ResumeSA: sa,
		Prompt: "Put the remaining Hideaway cards on the bottom in any order"}
	for i, id := range lib[:n] {
		d.Options = append(d.Options, decision.Option{Index: i, Kind: "hideaway_bottom", Label: objName(h.Game(), id), Obj: id, Player: c.Controller})
	}
	if _, ok := AskTapeIntent(h, d); ok {
		// The resolution kernel served the order and its record (the
		// KArrange answer record's hideaway_bottom) applied it: done, as the
		// "hideaway_arrange" re-entry is.
		return
	}
	if h.Ask(d) {
		return
	}
	newLib := append(append([]state.ObjID(nil), lib[n:]...), lib[:n]...)
	h.Emit(events.Event{Kind: events.LibraryOrder, Player: c.Controller, IDs: newLib, Secret: true})
}

func containsObj(ids []state.ObjID, want state.ObjID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// legacyName is the pre-feature NameCard stand-in: the name of the top card
// of player p's library, or "a card" when that library is empty. It is the
// deterministic no-ask path for a host with no corpus universe, and it is
// kept byte-identical to the behaviour an older binary logged so a persisted
// match replays (host/persist.go sidecar.NameUniverse).
func legacyName(g *state.Game, p state.PlayerID) string {
	return LegacyNameFallback(g, p)
}

// LegacyNameFallback is legacyName exported for rules' as-enters NameCard ask
// (entryETBChoice), so the entry-boundary and mid-resolution NameCard paths
// fall back to the SAME stand-in name when their filtered name list is empty.
func LegacyNameFallback(g *state.Game, p state.PlayerID) string {
	if g == nil {
		return "a card"
	}
	if lib := zoneOf(g, state.ZLibrary, p); len(lib) > 0 {
		if o := g.Obj(lib[0]); o != nil && o.Face() != nil {
			return o.Face().Name
		}
	}
	return "a card"
}

func effNameCard(h Host, c *Ctx, sa *cards.SA) {
	if o := h.Game().Obj(c.Source); o != nil && o.ChosenName != "" {
		return
	}
	valid := sa.ParamStr(cards.PKValidCards)
	chooseFromList := sa.ParamStr(cards.PKChooseFromList)
	chooseFromDefined := sa.ParamStr(cards.PKChooseFromDefinedCards)
	universeBacked := len(h.Game().NameUniverse) > 0
	random := strings.EqualFold(sa.ParamStr(cards.PKAtRandom), "True")
	// The resolving context's numeric-RHS resolver (paid X, a published
	// StoreSVar) is threaded into the eligible-name filter so a dynamic
	// ValidCards$ such as `Creature.cmcEQX` restricts against the resolution
	// value instead of failing every universe card closed.
	sc := c.SpecContext(c.Controller)
	// ChooseFromDefinedCards asks the STRICT filter: the ordinary ValidCards
	// totality fallback (a matched-nothing spec returning the WHOLE universe)
	// would feed unrevealed names into the intersection below - a remembered
	// Forest with ValidCards$ Card.nonLand over a land-only universe offered
	// Forest. AtRandom already passes strict for the same reason.
	names := NameChoicesFromListCtx(h.Game(), valid, sa.ParamStr(cards.PKValidDescription), chooseFromList, &sc, random || chooseFromDefined != "")
	if chooseFromDefined != "" {
		// This selector narrows the normal ValidCards name universe to the
		// printed names of the Defined referents. Resolve through
		// knownDefinedTargets, NOT Defined: an unrecognised selector must fail
		// closed, while Defined's historical fallback acts on the SA source,
		// whose printed name may never have been revealed. The recognised
		// Remembered spelling keeps the same context-memory semantics
		// (resolvedRemembered) Defined's arm uses. With no corpus universe (or
		// no eligible referents), fail closed: a legacy fallback name could be
		// neither validated nor guaranteed revealed.
		defined, known := knownDefinedTargets(h, c, chooseFromDefined)
		eligible := make(map[string]bool)
		if known {
			for _, target := range defined {
				if target.IsPlayer || target.Obj == 0 {
					continue
				}
				if obj := h.Game().Obj(target.Obj); obj != nil && obj.Face() != nil {
					eligible[obj.Face().Name] = true
				}
			}
		}
		restricted := make([]string, 0, len(names))
		for _, name := range names {
			if eligible[name] {
				restricted = append(restricted, name)
			}
		}
		names = restricted
	}
	if chooseFromDefined == "" && len(names) == 0 && (!universeBacked || chooseFromList == "") {
		// R-9: a host without a supplied corpus still completes
		// deterministically, and reproduces the exact pre-feature NameCard
		// behaviour (name the top of the caster's own library) so a log an
		// older binary wrote replays byte for byte (host/persist.go's
		// sidecar.NameUniverse mode).
		names = []string{legacyName(h.Game(), c.Controller)}
	}
	if c.NameChoice == "" && universeBacked && random && len(names) > 0 {
		c.NameChoice = names[h.Rand(len(names))]
	}
	if c.NameChoice == "" {
		if len(names) == 0 {
			return
		}
		d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
			Source: c.Source, ResumeKind: "name", ResumeSA: sa, Prompt: "Choose a card name"}
		d.Options = NameOptions(names, c.Controller)
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand: the name rules'
			// resumeResolution binds from the "name" answer (the chosen
			// option's Label; Min == Max == 1), which the rest of the chain
			// reads off Ctx.NameChoice exactly as on the re-entry.
			if len(ans) == 1 {
				c.NameChoice = ans[0].Label
			}
		} else {
			_ = Ask(h, d)

			c.NameChoice = names[0]
		}

	}
	h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "name", Text: c.NameChoice})
}

// discardDefinedCards resolves a Discard SA's DefinedCards$ parameter to the
// concrete objects it names. Only the group spellings the corpus's Mode$
// Defined discards actually use are wired (Remembered and its aliases, the
// targets); any other spelling falls through the generic Defined resolver.
func discardDefinedCards(h Host, c *Ctx, spec string) []state.Target {
	switch strings.Split(spec, ".")[0] {
	case "Remembered", "RememberedLKI", "RememberedCard", "DirectRemembered":
		return objectsOf(c.Remembered)
	case "Targeted":
		return objectsOf(c.Targets)
	}
	return DefinedSpec(h, c, spec)
}
