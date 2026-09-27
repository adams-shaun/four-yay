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
}

// drawFor is DrawFor with an optional enclosing Draw cursor. A nonnegative
// cursor is recorded on a dredge decision so rules can continue that exact
// multi-card resolution after its replacement is answered.
func drawFor(h Host, p state.PlayerID, cursor int, resumeSA *cards.SA, upto drawUptoRider) {
	g := h.Game()
	lib := zoneOf(g, state.ZLibrary, p)
	if len(lib) == 0 {
		h.Emit(events.Event{Kind: events.PlayerLost, Player: p, Text: "drew from an empty library"})
		return
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
		return
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
		if h.Ask(d) {
			return
		}
	}
	h.Emit(events.Event{Kind: events.Draw, Player: p, Obj: lib[0],
		From: state.ZLibrary, To: state.ZHand, Secret: true})
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

func effDraw(h Host, c *Ctx, sa *cards.SA) {
	n := Num(h, c, sa, "NumCards", 1)
	if n <= 0 {
		return
	}
	// RememberDrawn$ records every card actually drawn into the resolution's
	// Remembered (Breathstealer's Crypt's reveal-and-maybe-discard chain acts
	// on exactly the drawn card; a library that ran out mid-draw records only
	// what moved). Unread before this — the whole sub-chain saw nothing. A
	// draw that parked on a dredge ask has not happened yet, so the record
	// waits until the draw is real (the suspend check below).
	// The corpus uses both True and AllReplaced; both record cards this
	// ability's draws actually moved into a hand (replaced draws are not here).
	remember := strings.TrimSpace(sa.Params["RememberDrawn"]) != ""
	if remember {
		// A triggered ability's resolution starts with its fire-time event
		// capture already in Ctx.Remembered (rules/resolution.go seeds both
		// Remembered and Captured from the ability object's own Remembered).
		// That object -- for Communal Brewing's self-ETB trigger, the
		// entering Brewing itself -- is not a card drawn this way, so it must
		// not inflate Remembered$Amount ("one ingredient counter ... for each
		// card drawn this way") nor defeat the did-I-draw-anything gate
		// (Mr. Foxglove's `ConditionDefined$ Remembered | ConditionCompare$
		// EQ0`). Drop it before recording what the draws actually moved; the
		// helper is the same capture-exclusion every TriggerRemembered$Amount
		// read uses, and it is a no-op for an activated ability (no capture).
		c.Remembered = rememberedExcludingCapture(h, c)
	}
	targets := actingPlayers(h, c, sa)
	total := int32(len(targets)) * n
	// OptionalDecider$ (Mystic Remora, Rhystic Study — Forge's DrawEffect
	// resolve: optional = hasParam("OptionalDecider")... the decider confirms
	// "do you want to draw N cards?" and a decline skips): the DRAW itself is
	// optional, decided by the NAMED decider, resolved through the same
	// player selector grammar every other resolver owns ("You" — the
	// corpus's dominant value, and its bare "True" spelling — is the
	// resolving controller, the enchantment's controller asking themselves
	// whether to draw off their own trigger; TargetedController is the
	// targeted spell's controller (Vex's "that spell's controller may draw
	// a card"), TriggeredCardController the entering creature's (Selvala),
	// Opponent the controller's opponents). The ask is the same mid-
	// resolution KChoose yes/no every other asking primitive poses, answered
	// through rules' "draw_optional" resume arm into Ctx.DrawOpt; a host that
	// cannot ask keeps the pre-ask mandatory draw (the R-9 degradation). A
	// spec the grammar cannot resolve fails closed below this read's own
	// convention: the pre-ask mandatory draw stays and one loud Note names
	// the unmodelled value. A target with an empty library makes the draw a
	// non-choice — Forge's canDrawAmount guard skips those silently, so the
	// ask only fires when SOME target could actually draw; with none, no
	// question is posed and nothing is drawn (an empty-library draw event is
	// a no-op either way).
	if decider := strings.TrimSpace(sa.Params["OptionalDecider"]); decider != "" && total > 0 {
		answered := c.DrawOpt
		c.DrawOpt = "" // fx42 scoping: consumed once; a nested optional draw poses its own ask
		if answered == "" {
			canDraw := false
			for _, t := range targets {
				if len(zoneOf(h.Game(), state.ZLibrary, t)) > 0 {
					canDraw = true
					break
				}
			}
			if canDraw {
				seat := c.Controller
				spec := decider
				if strings.EqualFold(spec, "True") {
					spec = "You"
				}
				askable := true
				if !strings.EqualFold(spec, "You") {
					resolved := Defined(h, c, &cards.SA{Params: map[string]string{"Defined": spec}})
					if len(resolved) == 0 || !resolved[0].IsPlayer {
						// The decider's identity is unresolvable — a spec the
						// selector grammar does not carry resolves to no player
						// (the self-default fallback returns an OBJECT, which
						// the IsPlayer gate rejects). Fail closed: the pre-ask
						// mandatory draw stays, one loud Note names the value,
						// and NO ask is posed (a yes/no ask to the controller
						// would be exactly the wrong-seat decision this read
						// exists to avoid).
						h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
							Text: "unmodelled Draw OptionalDecider$ " + decider})
						askable = false
					} else {
						seat = resolved[0].Player
					}
				}
				if askable {
					d := &decision.Decision{Player: seat, Kind: decision.KChoose, Min: 1, Max: 1,
						ResumeKind: "draw_optional", ResumeSA: sa, Source: c.Source,
						Prompt: "Draw " + strconv.Itoa(int(n)) + " card(s)?"}
					d.Options = []decision.Option{
						{Index: 0, Kind: "yes", Label: "Yes — draw", Player: seat},
						{Index: 1, Kind: "no", Label: "No", Player: seat},
					}
					if Ask(h, d) == AskAsked {
						return
					}
				}
			} else {
				answered = "no"
			}
		}
		if answered == "no" {
			// Declined (or no drawable pool): no draw; the walk continues to
			// any SubAbility$ chain.
			return
		}
	}
	// Upto$ True (task mordorparams1: Arcane Denial's "Its controller may
	// draw up to two cards at the beginning of the next turn's upkeep",
	// Truce's "Each player may draw up to two cards"): the draw is a real
	// per-target COUNT choice, one KChoose per Defined$ target before that
	// target's draws, Min 0, Max min(NumCards, the target's library size),
	// options the top cards of the TARGET's own library (the library-search
	// ask's private card options — a decision is visible only to
	// Decision.Player, so no leak). Answered through rules' "draw_upto"
	// resume arm into Ctx.DrawUptoIdx/Count/Answered (the DrawOpt pattern,
	// fx42-scoped per target); a batch that parks on a Dredge choice
	// re-enters through the dredge arm, which restores the in-flight
	// target's cursor from the ask's ResumeUpto rider. A host that cannot
	// ask keeps the pre-ask mandatory draw (the R-9 degradation every
	// effDraw arm takes). A library with fewer than n cards caps the ask at
	// what is there; an empty library is a no-op (never a decision whose
	// only answer is empty — OnlyEmptyAnswer refuses it — and never a
	// mandatory draw event that would mill a player the card only offered
	// to draw).
	if strings.EqualFold(strings.TrimSpace(sa.Params["Upto"]), "True") {
		for idx := int(c.DrawUptoIdx); idx < len(targets); idx++ {
			p := targets[idx]
			if !c.DrawUptoAnswered {
				lib := zoneOf(h.Game(), state.ZLibrary, p)
				m := n
				if int32(len(lib)) < m {
					m = int32(len(lib))
				}
				if m <= 0 {
					c.DrawUptoIdx = int32(idx + 1)
					continue
				}
				d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 0, Max: int(m),
					ResumeKind: "draw_upto", ResumeSA: sa, ResumeTarget: idx, Source: c.Source,
					// The walk's Remembered rides the ask (the hidden-library
					// search's ResumeRemembered precedent): the re-entered
					// effDraw recomputes `targets` from Defined$, and for the
					// Remembered-valued selectors -- Arcane Denial's
					// `Defined$ DelayTriggerRemembered` is the corpus shape --
					// a resume that rebuilt an empty set would resolve a
					// DIFFERENT target list than the one the cursor indexes,
					// so the answered count would be drawn for the wrong
					// player or for nobody. The ability-object resume path
					// restores the same set from o.Remembered; this covers
					// every other frame.
					ResumeRemembered: append([]state.Target(nil), c.Remembered...),
					Prompt:           "Draw up to " + strconv.Itoa(int(n)) + " card(s)?"}
				for i := int32(0); i < m; i++ {
					id := lib[i]
					label := "a card"
					if o := h.Game().Obj(id); o != nil && o.Face() != nil {
						label = o.Face().Name
					}
					d.Options = append(d.Options, decision.Option{Index: len(d.Options),
						Kind: "card", Label: label, Obj: id, Player: p})
				}
				if Ask(h, d) == AskAsked {
					return
				}
				// No-host (R-9): the pre-ask mandatory draw of what was offered.
				c.DrawUptoCount, c.DrawUptoAnswered = m, true
			}
			for c.DrawDone < c.DrawUptoCount {
				var lib []state.ObjID
				if remember {
					lib = zoneOf(h.Game(), state.ZLibrary, p)
				}
				drawFor(h, p, int(c.DrawDone), sa, drawUptoRider{idx: idx, count: c.DrawUptoCount})
				if h.Suspended() {
					// A Dredge choice is between individual draws. Its resume
					// point restores this target's cursor (idx, answered count
					// and DrawDone); do not run later targets yet.
					return
				}
				if remember && len(lib) > 0 {
					c.Remembered = append(c.Remembered, state.Target{Obj: lib[0]})
				}
				c.DrawDone++
			}
			c.DrawDone = 0
			c.DrawUptoIdx = int32(idx + 1)
			c.DrawUptoCount, c.DrawUptoAnswered = 0, false
		}
		return
	}
	for c.DrawDone < total {
		p := targets[c.DrawDone/n]
		var lib []state.ObjID
		if remember {
			lib = zoneOf(h.Game(), state.ZLibrary, p)
		}
		drawFor(h, p, int(c.DrawDone), sa, drawUptoRider{})
		if h.Suspended() {
			// A Dredge choice is between individual draws. Its resume point
			// carries this cursor; do not run later draws, Remembered or
			// SubAbility$ yet.
			return
		}
		if remember && len(lib) > 0 {
			c.Remembered = append(c.Remembered, state.Target{Obj: lib[0]})
		}
		c.DrawDone++
	}
	// DrawDone is scoped to this primitive like the other Ctx answer fields.
	c.DrawDone = 0
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
		rememberCards:   strings.EqualFold(sa.Params["RememberDiscarded"], "True"),
		rememberPlayers: strings.EqualFold(sa.Params["RememberDiscardingPlayers"], "True"),
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
	if strings.TrimSpace(sa.Params["Defined"]) != "" {
		// definedPlayers applies Forge's getDefinedPlayers rule: the plain
		// Remembered family contributes remembered PLAYERS only, never a
		// remembered card's controller (Summon: Valefor's per-opponent loop).
		return definedPlayers(h, c, sa)
	}
	if _, targeted := sa.Params["ValidTgts"]; targeted {
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
	if strings.EqualFold(sa.Params["AnyNumber"], "True") {
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
	if strings.EqualFold(sa.Params["Optional"], "True") {
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
	if desc := strings.TrimSpace(sa.Params["DiscardValidDesc"]); desc != "" {
		noun = desc
	} else if v := strings.TrimSpace(sa.Params["DiscardValid"]); v != "" && !strings.ContainsAny(v, ".,+") && v != "Card" {
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
	answers := c.Discard
	answerTarget := c.DiscardTarget
	vote := c.DiscardVote
	answered := answers != nil
	voted := vote != ""
	c.Discard = nil
	c.DiscardTarget = 0
	c.DiscardVote = ""
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
	firstPass := !answered && !voted && c.UnlessElected == ""
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
	mode := sa.Params["Mode"]
	valid := sa.Params["DiscardValid"]
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
				for _, id := range answers {
					if containsID(hand, id) {
						discardAndRemember(h, c, riders, id, p)
					}
				}
				continue
			}
			eligible := discardEligible(g, c, hand, valid)
			if len(eligible) == 0 {
				continue
			}
			askMin, askMax := discardBounds(h, c, sa, len(eligible))
			d := discardAsk(g, c, sa, eligible, chooser, askMin, askMax, targetIndex)
			if Ask(h, d) == AskAsked {
				suspended = true
				return // resolution suspended; the answer re-enters with Ctx.Discard set.
			}
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
				for _, id := range answers {
					if !containsID(hand, id) {
						continue
					}
					discardAndRemember(h, c, riders, id, p)
				}
				continue
			}
			// The may-discard election (below) answered for this target:
			// earlier targets were fully processed before it was posed, a
			// "no" discards nothing for this target, and a "yes" proceeds to
			// the card pick with the zero-card answer removed.
			if voted && targetIndex < answerTarget {
				continue
			}
			mayElected := voted && targetIndex == answerTarget
			if mayElected && vote != "yes" {
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
			elected := c.UnlessElected
			c.UnlessElected = ""
			unlessSpec := strings.TrimSpace(sa.Params["UnlessType"])
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
					if Ask(h, d) == AskAsked {
						suspended = true
						return // resolution suspended; the answer re-enters with Ctx.Discard set.
					}
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
				if Ask(h, d) == AskAsked {
					suspended = true
					return // resolution suspended; the answer re-enters with Ctx.UnlessElected set.
				}
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
			if askMin == 0 && !strings.EqualFold(sa.Params["AnyNumber"], "True") {
				if !mayElected {
					d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
						Source: c.Source, ResumeKind: "discard_may", ResumeSA: sa, ResumeTarget: targetIndex,
						Prompt: discardMayPrompt(sa, askMax),
						Options: []decision.Option{
							{Index: 0, Kind: "yes", Label: "Yes — discard", Player: p},
							{Index: 1, Kind: "no", Label: "No — don't discard", Player: p},
						}}
					if Ask(h, d) == AskAsked {
						suspended = true
						return // resolution suspended; the answer re-enters with Ctx.DiscardVote set.
					}
				} else {
					askMin = 1
				}
			}
			if strings.EqualFold(sa.Params["AnyNumber"], "True") {
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
				Prompt:  "Choose " + strconv.Itoa(askMin) + ".." + strconv.Itoa(askMax) + " card(s) to discard",
				Options: opts}
			if Ask(h, d) == AskAsked {
				suspended = true
				return // resolution suspended; the answer re-enters with Ctx.Discard set.
			}
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
			if strings.EqualFold(sa.Params["Optional"], "True") {
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
				if Ask(h, d) == AskAsked {
					suspended = true
					return // resolution suspended; the answer re-enters with Ctx.DiscardVote set.
				}
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
			if dc := strings.TrimSpace(sa.Params["DefinedCards"]); dc != "" {
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
	remember := strings.EqualFold(sa.Params["RememberMilled"], "True")
	show := strings.EqualFold(strings.TrimSpace(sa.Params["ShowMilledCards"]), "True")
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

// effDig implements Forge's Dig: look at the top DigNum cards of Defined$'s
// library, move up to ChangeNum of the ones matching ChangeValid$ (default
// "Card") to DestinationZone$ (default "Hand"), and put everything else on
// the BOTTOM of the library in an order the player picks (the default Forge
// leaves unwritten; measured at the current pin, 518 of the corpus's 737 Dig
// lines carry no DestinationZone2$ and no SkipReorder$ and take this
// default). The remainder's second destination DOES exist in the corpus as
// "DestinationZone2$" (with "LibraryPosition2$" placing it in a library) --
// an earlier note here wrongly claimed the parameter does not exist; it is
// read below. The primary LibraryPosition$ is applied after the primary
// pile settles, including across an ordered-bottom remainder ask.
//
// A real card can also write "ChangeNum$ All" (e.g. Goblin Guide's own Dig)
// to mean every matching card within the DigNum look, with no cap short of
// that. changeNum's own upper bound is already the size of the dug slice, so
// defaulting it to digNum and only overriding that default for a literal or
// SVar ChangeNum$ handles "All" for free: it is simply the case where
// nothing narrows the cap below the number of cards looked at.
//
// The look-and-take ask (dig1): where the top DigNum window holds STRICTLY
// more ChangeValid$-eligible cards than ChangeNum, the pick is a real
// decision and effDig poses it -- the same strict-supersets rule effDiscard's
// TgtChoose arm already uses, so a decision nobody could answer differently
// is never emitted. ChangeNum$ 0 (the corpus's three reveal-machinery digs:
// birthing_ritual, sanity_grinding, stomping_slabs) takes nothing, so the
// ask gate also requires changeNum > 0 -- otherwise a zero cap with any
// eligible card would pose a Min==Max==0 KChoose whose only legal answer is
// the empty one, a decision nobody could answer differently by definition.
// ChangeNum$ Any is the third cap: Forge's any-number take (Jace, the Mind
// Sculptor's "You may put that card on the bottom", Through the Forest
// Gate's "put any number of land cards"), so it caps at the window and
// lowers the ask's Min to 0 whenever an eligible card exists -- a real
// choice, unlike what an earlier note here claimed take-all (measured: the
// unresolvable-value Num read degraded Any to 0 and those digs took
// NOTHING). The look is recorded first as a Secret Note carrying the
// window's ids (only the library's owner may know what sat on top; the same
// channel effRearrangeTopOfLibrary uses, with the ids added so the owner's
// client can render what was seen -- view/redact.go rule (1) passes a Secret
// event's payload to its own Player and strips it from everyone else). The
// decision is a KChoose over the ELIGIBLE cards only, in library order: an
// ineligible card must not be pickable, so it is not offered (the window
// itself is on the look Note; the prompt names the card text). Min honours
// Optional$ -- 0 when the take is optional, ChangeNum when it is not -- and
// Max is ChangeNum. The answer re-enters through ResumeKind "dig" with
// Ctx.Dig/DigDone and the asking target's index set (rules/resolution.go),
// scoped to this primitive like every other Ctx answer field.
//
// A host that cannot answer (the fuzz/no-engine stand-in, R-9) and the
// no-choice path (eligible <= ChangeNum) keep the take deterministic: the
// first ChangeNum eligible cards in zone order move, and the remainder takes
// its second destination -- by default the bottom, ordered (asked; offered
// order without a host). A resumed multi-target Dig applies
// the answer only to the target that asked, skips earlier targets that already
// completed before suspension, and preserves that same deterministic behaviour
// for every later target; chained per-library asks remain separate work. The
// no-choice path asks NO take decision, but it is not event-free when the
// remainder moves: a default-remainder Dig still moves its untaken cards to
// the bottom (asking for that order when two or more remain), so only a game
// that never reaches a Dig whose remainder moves replays byte-identically to
// the pre-dig1 engine.
//
// The variant params (task inbox-paramcensus-dig-variants), each read
// below:
//
//   - Reveal$ True reveals the dug window to the whole table BEFORE any
//     take -- a non-Secret Note carrying the window's ids, the same shape
//     effReveal's public arm emits (Ad Nauseam "Reveal the top card", Chaos
//     Warp, Goblin Guide, Matter Reshaper). It replaces the ask path's
//     private look when a real choice follows the reveal.
//   - NoReveal$ True suppresses that window reveal. A guard, not a
//     behaviour change: the corpus's 84 NoReveal lines carry no Reveal$, and
//     the dig's moves were already Secret before this task (Impulse).
//   - ForceRevealToController$ True reveals each MOVED card publicly before
//     its Secret move -- Ancient Stirrings' "you may reveal a colorless card
//     from among them and put it into your hand" (the window itself stays
//     private). Suppressed when the window was already revealed.
//   - Tapped$ True taps a card the primary move sends to the battlefield,
//     the same MoveZone-then-Tap pair effChangeZone's Tapped$ movers emit
//     (Through the Forest Gate: "put any number of land cards ... onto the
//     battlefield tapped").
//   - DestinationZone2$ (with LibraryPosition2$) is the remainder's second
//     destination: every window card the primary move did not take goes
//     there (Chaos Warp and Goblin Guide's unmatched card back to the
//     library, Matter Reshaper's unmatched card to the hand). OMITTED -- the
//     corpus's default -- it is the library bottom (Ancient Stirrings' own
//     Oracle: "put the rest on the bottom of your library in any order").
//   - LibraryPosition2$ places a library DestinationZone2$: "0" = top,
//     which leaves the cards exactly where they are, so the placement
//     emits nothing; "-1" = bottom, the ordered-bottom KArrange ask (or,
//     for a one-card remainder, the deterministic move -- one card has
//     exactly one possible order); anything else is named in a loud Note
//     and the card stays (the corpus carries only "0" and "-1").
//   - SkipReorder$ True suppresses both the bottom default and a
//     DestinationZone2$ remainder placement: the untaken cards never move,
//     so they stay on top in their existing relative order (the corpus
//     never pairs the two; Through the Forest Gate carries it without one).
//   - WithMayLook$ True (Ixhel, Scion of Atraxa; Gonti; Thief of Sanity;
//     Scarlet Witch) grants the exiling effect's controller -- never the
//     exiled card's owner -- a lasting look at the face of every card the
//     Dig exiles face down. It is applied by the shared applyFaceDownMarker
//     (effects/zone.go) on the primary move's MoveZone, so the Dig calls the
//     one read every other face-down mover uses: the looker rides the
//     "exiled_with_face_down_maylook" marker's Amount and the projection
//     (view/cardViews) admits exactly that player. Absent the param the
//     marker is unchanged, so every non-maylook Dig emits byte-identically.
//
// Still unread here: RestRandomOrder$ (the bottom pile returns in the
// answered/offered order, never shuffled) and the exotic DestinationZone2
// values (PlanarDeck). The primary optional election, primary
// LibraryPosition$, Choser$ and DigNum$ X are handled by this walk.
func effDig(h Host, c *Ctx, sa *cards.SA) {
	digNum := Num(h, c, sa, "DigNum", 1)
	// Forge's DigNum$ X names the resolving X value when one was paid, but
	// trigger bodies also use the same spelling for their face SVar (Keldon
	// Flamesage's SVar:X:Count$CardPower). A zero paid-X slot is not enough to
	// distinguish those forms, so use the named SVar as the trigger fallback
	// when the resolution carries one and its count body resolves.
	if strings.TrimSpace(sa.Params["DigNum"]) == "X" && c.X == 0 && c.SVars != nil {
		if body, ok := c.SVars["X"]; ok {
			if n, resolved := EvalCountOK(h, c, body); resolved {
				digNum = n
			}
		}
	}
	if digNum < 0 {
		digNum = 0
	}
	changeNum := digNum
	anyNum := false
	if raw := sa.Params["ChangeNum"]; raw != "" && raw != "All" {
		if raw == "Any" {
			// Forge's any-number cap: the take is uncapped within the window
			// and the answer may be empty -- a real choice whenever any
			// eligible card exists, which the ask gate and Min below read.
			anyNum = true
		} else {
			changeNum = Num(h, c, sa, "ChangeNum", digNum)
		}
	}
	if changeNum < 0 {
		changeNum = 0
	}
	// WithTotalCMC$ is a cumulative mana-value budget over the picked cards
	// ("put any number of nonland permanent cards with total mana value 4 or
	// less"): a card whose own mana value exceeds it can never be picked, and
	// the running sum of the picks must not exceed it either. Absent the
	// param (the corpus default) the budget is 0 and every read below is a
	// no-op, so a non-budget Dig emits byte-identically. Present but
	// unresolvable degrades to budget 0 -- Num's documented convention, "the
	// card does nothing" -- and takes nothing.
	budget, hasBudget := NumResolved(h, c, sa, "WithTotalCMC", 0)
	if budget < 0 {
		budget = 0
	}
	spec := sa.Params["ChangeValid"]
	if spec == "" {
		spec = "Card"
	}
	spec = permanentCardSpec(spec)
	destName := sa.Params["DestinationZone"]
	if destName == "" {
		destName = "Hand"
	}
	dest := ParseZone(destName)
	optional := sa.Params["Optional"] == "True"
	promptToSkipOptional := strings.EqualFold(strings.TrimSpace(sa.Params["PromptToSkipOptionalAbility"]), "True") ||
		strings.TrimSpace(sa.Params["OptionalAbilityPrompt"]) != ""
	// The variant params (see the comment block above the function for what
	// each means and which corpus card carries it).
	revealWin := strings.EqualFold(strings.TrimSpace(sa.Params["Reveal"]), "True") &&
		!strings.EqualFold(strings.TrimSpace(sa.Params["NoReveal"]), "True")
	noLooking := strings.EqualFold(strings.TrimSpace(sa.Params["NoLooking"]), "True")
	forceReveal := strings.EqualFold(strings.TrimSpace(sa.Params["ForceRevealToController"]), "True")
	skipReorder := strings.EqualFold(strings.TrimSpace(sa.Params["SkipReorder"]), "True")
	tapped := strings.EqualFold(strings.TrimSpace(sa.Params["Tapped"]), "True")
	// FromBottom$ True (task scrybottom): the Dig window is the BOTTOM DigNum
	// cards of the library rather than the top. The Temporal Anchor's
	// "exile that many cards from the bottom of your library" is the corpus
	// carrier (`/usr/bin/grep -rlE 'FromBottom\$'` = 2 files); everything
	// after the window (the primary move, the remainder placement) is
	// unchanged, so a non-FromBottom Dig emits byte-identically.
	fromBottom := strings.EqualFold(strings.TrimSpace(sa.Params["FromBottom"]), "True")
	lookText := "looks at the top of the library"
	lookWhere := "top"
	if fromBottom {
		lookText = "looks at the bottom of the library"
		lookWhere = "bottom"
	}
	primaryPos := strings.TrimSpace(sa.Params["LibraryPosition"])
	dest2Name := strings.TrimSpace(sa.Params["DestinationZone2"])
	pos2 := strings.TrimSpace(sa.Params["LibraryPosition2"])
	// Forge's omitted second destination means bottom-of-library remainder.
	if dest2Name == "" {
		dest2Name, pos2 = "Library", "-1"
	}
	// bottomRest says the remainder moves to the library bottom (the default,
	// or an explicit Library + LibraryPosition2$ "-1"), so a no-choice tail
	// with two or more untaken cards must record the look before the ordered
	// bottom ask -- the same record the take-ask path makes before ITS ask.
	bottomRest := !skipReorder && strings.EqualFold(strings.TrimSpace(dest2Name), "Library") && pos2 == "-1"
	// fx42 scoping: capture and clear the answered pick BEFORE the target
	// loop. DigTarget identifies the exact target that asked: earlier targets
	// completed before suspension and must be skipped, that target consumes
	// the answer, and later targets retain M1's deterministic processing until
	// chained per-library asks exist. A nested Dig below this walk therefore
	// poses its own ask instead of inheriting any of these fields.
	digAns := c.Dig
	digDone := c.DigDone
	digTarget := c.DigTarget
	c.Dig, c.DigDone, c.DigTarget = nil, false, 0
	// The arrange done-marker, consumed and cleared here (fx42 scoping):
	// handleArrange has already applied the asking target's answered bottom
	// order (the LibraryOrder event). ArrangeTarget is the cursor -- the walk
	// skips through that target and keeps the deterministic processing for
	// the LATER ones (each may pose its own arrange, suspending again);
	// dropping them would strand a multi-target Dig mid-walk.
	arrangeThrough := -1
	if c.Arrange {
		c.Arrange = false
		arrangeThrough = c.ArrangeTarget
		c.ArrangeTarget = 0
	}
	g := h.Game()
	players := definedPlayers(h, c, sa)
	selection := *c // IsRemembered in ChangeValid reads the pre-clear set.
	initForgetOtherSnapshot(h, c, sa, players, 2)
	forgetOtherRemembered(h, c, sa)
	// One classification for the whole Dig call, before the target walk: a
	// degrading Attacking$ rider is one Note per dig, not one per taken card
	// (nor one per Defined$ library).
	rider := classifyAttackingEntry(c, sa, dest)
	for targetIndex, p := range players {
		lib := zoneOf(g, state.ZLibrary, p)
		n := digNum
		if int32(len(lib)) < n {
			n = int32(len(lib))
		}
		top := append([]state.ObjID(nil), lib[:n]...)
		if fromBottom {
			top = append([]state.ObjID(nil), lib[int32(len(lib))-n:]...)
		}
		// An empty answer to the optional-ability election declines the entire
		// Dig ability: leave the looked-at window in place and do not process
		// its remainder, reveal, or destination side effects.
		if digDone && targetIndex == digTarget && promptToSkipOptional && len(digAns) == 0 {
			continue
		}
		// primaryMoved is the temporary library pile for a primary
		// DestinationZone$ Library move. It is placed after the remainder has
		// settled, so the primary LibraryPosition$ cannot be lost to the
		// remainder's ordered-bottom ask.
		primaryMoved := make([]state.ObjID, 0, len(top))
		placePrimary := func() {
			if dest != state.ZLibrary || len(primaryMoved) == 0 {
				return
			}
			switch primaryPos {
			case "", "0":
				libraryOrderPlacement(h, p, primaryMoved, false)
			case "-1":
				// MoveZone already appends the primary pile at the bottom.
			default:
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: p,
					Text: "LibraryPosition$ " + primaryPos + " is not implemented; the cards sit at the BOTTOM of the library"})
			}
		}
		// take moves one window card to the primary destination, revealing
		// it first when ForceRevealToController$ asks (a public Note naming
		// the card, then the Secret move -- the same reveal-then-secret-move
		// shape effChangeZone's Reveal$ fetch emits; suppressed when Reveal$
		// already made the whole window public) and tapping it right after a
		// Tapped$ True battlefield entry.
		take := func(id state.ObjID) {
			if forceReveal && !revealWin {
				h.Emit(events.Event{Kind: events.Note, Player: p, IDs: []state.ObjID{id}})
			}
			ev := moveZoneEvent(c, id, state.ZLibrary, dest)
			ev.Player, ev.Secret = p, true
			applyFaceDownMarker(h, sa, c, &ev, dest)
			if dest == state.ZLibrary {
				primaryMoved = append(primaryMoved, id)
			}
			// ExileFaceDown$ True with an exile destination (Ugin, the
			// Ineffable's [+1]: "Exile the top card of your library face down
			// and look at it") carries the same face-down exile payload
			// Hideaway's move uses -- the exiling source rides in Amount --
			// so a replay derives FaceDown identically and a projection
			// withholds the card.
			// applyFaceDownMarker preserves ExileFaceDown$'s source-carrying
			// payload (Counter, Amount and nil IDs), while also stamping
			// battlefield FaceDown$ entries.
			h.Emit(ev)
			if strings.EqualFold(strings.TrimSpace(sa.Params["Imprint"]), "True") && c.Source != 0 {
				if moved := g.Obj(id); moved != nil && moved.Zone == dest && !moved.IsToken {
					h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: []state.ObjID{id}})
				}
			}
			digRemember(c, sa, id)
			if tapped && dest == state.ZBattlefield {
				h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: p, Text: "entered tapped"})
			}
			rider.apply(h, c, id, p, dest)
			if dest == state.ZBattlefield && strings.EqualFold(strings.TrimSpace(sa.Params["GainControl"]), "True") {
				h.Emit(events.Event{Kind: events.ControlChange, Obj: id, Player: c.Controller})
			}
			// StaticEffect$ on a battlefield take (Arbiter of the Ideal's
			// "put it onto the battlefield ... it's an enchantment"): the same
			// rider registration every ChangeZone mover applies.
			if dest == state.ZBattlefield {
				applyStaticEffect(h, c, sa, dest, []state.ObjID{id})
			}
		}
		// rest moves the window cards the primary move did not take to the
		// second destination (DestinationZone2$, placed by LibraryPosition2$).
		// With DestinationZone2$ Library / "-1" -- the omitted default -- the
		// cards go to the bottom, in the answered order when two or more remain
		// (the ask above) and in their existing order otherwise.
		rest := func(ids []state.ObjID) bool {
			if skipReorder {
				return false
			}
			dest2 := ParseZone(dest2Name)
			if dest2 == state.ZLibrary && pos2 == "-1" {
				if len(ids) == 0 {
					return false
				}
				// A one-card remainder has exactly one possible order, so no
				// decision anybody could answer differently is posed -- the same
				// rule the take ask's ChangeNum$ 0 gate applies. It moves to the
				// bottom directly (skipped when it already sits there).
				if len(ids) == 1 {
					moveRestToBottom(h, g, p, ids)
					return false
				}
				// The ordered-bottom ask: Min == Max == len(ids), so the answer
				// is a full permutation -- every remaining card is placed, and
				// the ANSWER order is the bottom order (the hideaway contract;
				// handleArrange's "dig_bottom" case applies it as
				// untouched-library + answered remainder). ResumeKind
				// "dig_arrange" is this ask's OWN continuation: reusing the
				// take ask's "dig" would feed the answered order back through
				// the "dig" arm as a TAKE answer and re-dig the next window.
				d := &decision.Decision{Player: p, Kind: decision.KArrange, Min: len(ids), Max: len(ids), Source: c.Source,
					ResumeKind: "dig_arrange", ResumeSA: sa, ResumeTarget: targetIndex,
					Prompt:           "Put the remaining cards on the bottom of your library in any order",
					ResumeDigPrimary: append([]state.ObjID(nil), primaryMoved...),
					// The ForgetOtherRemembered$ pre-clear snapshot rides every
					// ask that can suspend this walk: a later window's
					// eligibility and the answered window's own re-entry still
					// match the pre-clear candidates after the clear.
					ResumeForgetOtherSnapshot: copyTargets(c.ForgetOtherSnapshot),
					ResumeForgetOtherOwners:   append([]state.PlayerID(nil), c.ForgetOtherOwners...),
					ResumeForgetOtherReady:    c.ForgetOtherReady,
					ResumeForgetOtherCleared:  c.ForgetOtherCleared}
				for i, id := range ids {
					name := "a card"
					if !noLooking || revealWin {
						if o := g.Obj(id); o != nil && o.Face() != nil {
							name = o.Face().Name
						}
					}
					d.Options = append(d.Options, decision.Option{Index: i, Kind: "dig_bottom", Label: name, Obj: id, Player: p})
				}
				if Ask(h, d) == AskAsked {
					return true
				}
				// R-9 no-host stand-in: the OFFERED order is the bottom order --
				// the exact permutation botpolicy's clamp top-up answers, so the
				// two deterministic readers cannot drift.
				moveRestToBottom(h, g, p, ids)
				return false
			}
			for _, id := range ids {
				if dest2 == state.ZLibrary {
					if pos2 != "" && pos2 != "0" {
						h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: p,
							Text: "LibraryPosition2$ " + pos2 + " is not implemented; the card stays on top"})
					}
					continue
				}
				if forceReveal && !revealWin {
					h.Emit(events.Event{Kind: events.Note, Player: p, IDs: []state.ObjID{id}})
				}
				ev := moveZoneEvent(c, id, state.ZLibrary, dest2)
				ev.Player, ev.Secret = p, true
				h.Emit(ev)
				digRemember(c, sa, id)
			}
			return false
		}
		if arrangeThrough >= 0 {
			// The arrange re-entry: every target up to and including the one
			// whose arrange was answered is complete.
			if targetIndex <= arrangeThrough {
				continue
			}
		} else {
			if digDone && targetIndex < digTarget {
				// This target completed on the first pass before a later library
				// suspended the effect. Re-running it could move a second batch (or
				// newly create a choice after its first batch left), so skip it.
				continue
			}
			if digDone && targetIndex == digTarget {
				// Re-entry: move exactly the answered cards that still sit in the
				// ASKING target's window (a per-window filter keeps a stray answer
				// from moving an object that left the window meanwhile), in the
				// player's answer order; the rest of the window goes to the
				// second destination.
				picked := make(map[state.ObjID]bool, len(digAns))
				moved := make([]state.ObjID, 0, len(digAns))
				for _, id := range digAns {
					if !containsID(top, id) {
						continue
					}
					picked[id] = true
					take(id)
					moved = append(moved, id)
				}
				restIDs := make([]state.ObjID, 0, len(top))
				for _, id := range top {
					if !picked[id] {
						restIDs = append(restIDs, id)
					}
				}
				if rest(restIDs) {
					return
				}
				placePrimary()
				continue
			}
		}
		eligible := make([]state.ObjID, 0, len(top))
		for _, id := range top {
			if MatchesSpecCtx(g, spec, id, forgetOtherPreClearContext(&selection, c)) {
				eligible = append(eligible, id)
			}
		}
		// affordable says whether a card may be picked at all under
		// WithTotalCMC$: a card whose own mana value exceeds the budget can
		// never fit, however few are taken.
		affordable := func(id state.ObjID) bool { return !hasBudget || manaValueOf(g, id) <= int(budget) }
		// budgetEligible is the pickable set: spec-matching AND individually
		// affordable (no budget => identical to eligible).
		budgetEligible := eligible
		if hasBudget {
			budgetEligible = make([]state.ObjID, 0, len(eligible))
			for _, id := range eligible {
				if affordable(id) {
					budgetEligible = append(budgetEligible, id)
				}
			}
		}
		// greedy is the deterministic forced take under the cumulative budget:
		// walk budgetEligible in zone order and take each card only while the
		// running sum of mana values still fits. This is the exact take the
		// no-choice tail and the R-9 no-host fallback apply, and it is also
		// what decides whether a choice exists (below).
		greedy := make([]state.ObjID, 0, len(budgetEligible))
		running := 0
		for _, id := range budgetEligible {
			if int32(len(greedy)) >= changeNum {
				break
			}
			mv := manaValueOf(g, id)
			if hasBudget && running+mv > int(budget) {
				continue
			}
			running += mv
			greedy = append(greedy, id)
		}
		// forcedAll says the forced greedy take consumes every budget-eligible
		// card, so the answer cannot differ from it and no ask is warranted.
		forcedAll := len(greedy) == len(budgetEligible)
		askBudget := hasBudget && len(budgetEligible) > 0 && !forcedAll
		// The ask gate must allow a LATER target to pose its own take ask after
		// an EARLIER target's take answer resumed the walk (targetIndex >
		// digTarget), while an arrange re-entry keeps main's deliberate
		// deterministic processing for every target past arrangeThrough.
		optionalChoice := (optional || promptToSkipOptional) && len(budgetEligible) > 0 && changeNum > 0
		takeChoice := int32(len(budgetEligible)) > changeNum || anyNum && len(budgetEligible) > 0
		chooser := p
		if rawChooser := strings.TrimSpace(sa.Params["Choser"]); rawChooser != "" {
			if cp, ok := chooserPlayer(h, c, rawChooser); ok {
				chooser = cp
			}
		}
		if (!digDone || targetIndex > digTarget) && arrangeThrough < 0 && changeNum > 0 && (takeChoice || optionalChoice || askBudget) {
			// A real choice: record the look, then ask the library's owner.
			// Reveal$ True makes the record a PUBLIC reveal of the window (the
			// same non-Secret ids-Note shape effReveal's public arm emits);
			// otherwise the look stays private to the library's owner.
			if revealWin {
				h.Emit(events.Event{Kind: events.Note, Player: p, IDs: top})
			} else if !noLooking {
				emitLook(h, []state.PlayerID{p}, state.ZLibrary, top, lookText)
			}
			minv := int32(0)
			if !optional && !promptToSkipOptional && !anyNum {
				minv = changeNum
			}
			// A mandatory budget dig whose changeNum exceeds what the budget
			// affords must not demand more picks than it can pay for: lower the
			// Min to the forced affordable count so the ask can be satisfied.
			// (Corpus carriers are all Min 0; this is general-correctness code.)
			if hasBudget && minv > int32(len(greedy)) {
				minv = int32(len(greedy))
			}
			maxv := int(changeNum)
			if maxv > len(budgetEligible) {
				maxv = len(budgetEligible)
			}
			verb := "you may put up to "
			if !optional && !promptToSkipOptional && !anyNum {
				verb = "put "
			}
			look := "Look at the " + lookWhere + " " + strconv.Itoa(int(n)) + " card(s) of your library"
			if noLooking && !revealWin {
				look = "Choose from the " + lookWhere + " " + strconv.Itoa(int(n)) + " card(s) of your library"
			}
			// A self-contained library move (Jace, the Mind Sculptor's "you
			// may put it on the bottom" shape) is represented by an optional
			// KChoose.  "Choose" alone is ambiguous here: the selected card
			// moves to the bottom, while an unselected card does not move.  Say
			// both sides of that choice in the prompt and on each option.
			bottomChoice := dest == state.ZLibrary && primaryPos == "-1" && skipReorder
			prompt := look + ": " + verb + strconv.Itoa(int(changeNum)) + " matching card(s) into " + digDestPhrase(dest)
			if bottomChoice {
				selectVerb := "Select up to "
				if !optional && !promptToSkipOptional && !anyNum {
					selectVerb = "Select "
				}
				prompt = look + ": " + selectVerb + strconv.Itoa(int(changeNum)) + " matching card(s) to put on the bottom of your library. Leave unselected card(s) on top."
			}
			if hasBudget {
				prompt += " (total mana value " + strconv.Itoa(int(budget)) + " or less)"
			}
			d := &decision.Decision{Player: chooser, Kind: decision.KChoose,
				Min:          int(minv),
				Max:          maxv,
				MaxSum:       int(budget),
				Source:       c.Source,
				ResumeKind:   "dig",
				ResumeSA:     sa,
				ResumeTarget: targetIndex,
				Prompt:       prompt,
				// The ForgetOtherRemembered$ pre-clear snapshot rides the ask: the
				// resumed walk's later windows (and any re-entry revalidation)
				// still match the candidates the first pass offered under.
				ResumeForgetOtherSnapshot: copyTargets(c.ForgetOtherSnapshot),
				ResumeForgetOtherOwners:   append([]state.PlayerID(nil), c.ForgetOtherOwners...),
				ResumeForgetOtherReady:    c.ForgetOtherReady,
				ResumeForgetOtherCleared:  c.ForgetOtherCleared}
			for _, id := range budgetEligible {
				name := "a card"
				if !noLooking || revealWin {
					if o := g.Obj(id); o != nil && o.Face() != nil {
						name = o.Face().Name
					}
				}
				label := name
				if bottomChoice {
					label = "Put " + name + " on bottom"
				}
				opt := decision.Option{Index: len(d.Options),
					Kind: "dig", Label: label, Obj: id, Player: p}
				// Only a budget Dig carries a Value: Option.Value is
				// omitempty, and setting it on a budget-less Dig would put a
				// "value" field on the wire for every offered card although
				// MaxSum is 0 and nothing reads it. Keeping it budget-only
				// leaves every existing (non-budget) option list serialising
				// byte-identically.
				if hasBudget {
					opt.Value = manaValueOf(g, id)
				}
				d.Options = append(d.Options, opt)
			}
			if Ask(h, d) == AskAsked {
				return // resolution suspended; the answer re-enters with Ctx.Dig set.
			}
			// Fuzz/no-engine host: the deterministic stand-in (R-9) takes the
			// greedy affordable set -- the exact mirror of the budget-aware bot
			// arm -- with the Note that records why the richer path did not run.
			// AskEmpty is unreachable here by construction (the ask gate requires
			// options >= 1), but the shared helper owns the guard either way.
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: p,
				Text: "takes the first matching card(s) (no engine host to ask)", Secret: true})
			taken := make(map[state.ObjID]bool, len(greedy))
			for _, id := range greedy {
				take(id)
				taken[id] = true
			}
			restIDs := make([]state.ObjID, 0, len(top))
			for _, id := range top {
				if !taken[id] {
					restIDs = append(restIDs, id)
				}
			}
			if rest(restIDs) {
				return
			}
			placePrimary()
			continue
		}
		// No take decision to ask about (eligible <= ChangeNum): the M1 silent
		// TAKE runs, but the tail may still act -- a Reveal$ window reveal, a
		// Tapped$ Tap, a look Note ahead of an ordered-bottom ask, or a
		// remainder move can each add an event, and a default-remainder card
		// emits one. The forced greedy take moves in window order while the
		// cumulative budget (WithTotalCMC$) allows; everything else in the
		// window -- unmatched, over-budget and beyond the cap alike -- goes to
		// the second destination (by default the bottom, ordered) or stays
		// exactly where it is (SkipReorder$, or a top LibraryPosition2$).
		if revealWin && len(top) > 0 {
			h.Emit(events.Event{Kind: events.Note, Player: p, IDs: top})
		}
		if bottomRest && !revealWin && !noLooking && int32(len(top)-len(greedy)) >= 2 {
			// The ordered-bottom ask is coming: the look that authorises it is
			// recorded here, the same Secret owner's Note the take-ask path
			// emits before ITS ask (a Reveal$ window is already public).
			emitLook(h, []state.PlayerID{p}, state.ZLibrary, top, lookText)
		}
		taken := make(map[state.ObjID]bool, len(greedy))
		for _, id := range greedy {
			take(id)
			taken[id] = true
		}
		restIDs := make([]state.ObjID, 0, len(top))
		for _, id := range top {
			if !taken[id] {
				restIDs = append(restIDs, id)
			}
		}
		if rest(restIDs) {
			return
		}
		placePrimary()
	}
	// The walk completed: release the ride (the same boundary the search and
	// hidden walks end at), so a later ability in the chain cannot inherit it.
	endForgetOtherSnapshot(c)
}

// moveRestToBottom moves the untaken Dig window cards to the BOTTOM of
// their owner's library in the given order, as one Secret events.LibraryOrder
// carrying the complete reordered library (Ruling J1) -- the same single-event
// record rules' handleArrange emits for an answered arrange, so a replay
// re-derives the same order either way. The emit is skipped when the move
// would change nothing (the cards already sit on the bottom).
func moveRestToBottom(h Host, g *state.Game, p state.PlayerID, ids []state.ObjID) {
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

// digRemember honours a Dig's RememberChanged$ True: each card the dig moved
// joins the resolution's Remembered, where a chained SubAbility$ reads it --
// Atsushi's DBEffect RememberObjects$ RememberedCard seeds the registered
// may-play grant's Remembered from exactly this list. Absent the parameter
// (the corpus default) the walk adds nothing, so every pre-existing game
// replays byte-identically.
func digRemember(c *Ctx, sa *cards.SA, id state.ObjID) {
	if strings.EqualFold(strings.TrimSpace(sa.Params["RememberChanged"]), "True") {
		c.Remembered = append(c.Remembered, state.Target{Obj: id})
	}
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

// effDigUntil implements Forge's reveal-until search (DigUntilEffect; task
// diguntil1) — a DIFFERENT primitive from Dig, with its own param family:
// reveal cards from the top of Defined$'s library (default: the resolving
// controller) in zone order until one matches Valid$, publicly reveal every
// card turned over INCLUDING the found one (the same non-Secret ids-Note
// shape effDig's Reveal$ arm emits), move the found card(s) to
// FoundDestination$ (default Hand) and the revealed rest to
// RevealedDestination$ (default Library; RevealedLibraryPosition$ "-1" =
// bottom via Move's zone append, "0"/absent = the stay-in-place default so
// no event). The cards AFTER the found card stay in the library untouched —
// the scan stops at the amount-th match, unlike Dig's fixed window.
//
// OptionalFoundMove$ True (4 corpus lines — Songbirds' Blessing) makes the
// found move a real yes/no ask to the library's owner (the attach_optional
// KChoose shape): "yes" moves to FoundDestination$, "no" — the decline —
// to OptionalNoDestination$ when the SA carries one, else the found card
// JOINS the revealed pile (the corpus oracles all say "then put all cards
// revealed this way that weren't put onto the battlefield on the bottom":
// Genesis Storm, Hei Bai, Aurora Awakener). A no-host (AskNoHost) declines
// deterministically (R-9); botpolicy's clamp fallback answers option 0 =
// "yes". RevealRandomOrder$ True (54 corpus lines) shuffles the pile's
// RETURN order to the bottom of the library through the engine's seeded
// generator (h.Rand), so it replays exactly; the public reveal Note and the
// Remembered capture stay in scan order because reveal order is a reveal-time
// fact. A stay-in-place placement (RevealedLibraryPosition$ "0"/absent, e.g.
// Indomitable Creativity) cannot express a random order at all, so it keeps
// the existing order behind one loud Note.
//
// Riders implemented: RememberFound$ / RememberRevealed$ (the ctx-level
// Remembered discipline digRemember uses), Tapped$ (the MoveZone-then-Tap
// pair effDig's battlefield take emits) and GainControl$ (the found
// permanent enters under the resolving controller via the ordinary
// ControlChange event). A found card with an AURA face put onto the
// battlefield gets the CR 303.4f non-cast-entry attach, degraded to the
// deterministic stand-in: the first permanent in the controller's
// battlefield zone order that satisfies the Enchant keyword's own spec. If
// more than one permanent qualifies, the controller chooses the bearer; a
// sole candidate is taken without an answer. With NO eligible bearer the Aura
// stays in the library (CR 303.4f's remain-in-current-zone) rather than
// entering unattached and dying to the CR 704.5m SBA. Riders withheld with
// one loud Note per parameter and the core move still running: a non-literal
// Amount$ token is resolved as an SVar name through the count evaluator
// (literals 1..5 ARE honoured as "keep revealing until N matches", literal 0
// means the scan reveals nothing); Shuffle$ True shuffles the dug library
// after the moves (ShuffleCondition$ NoneFound restricts it to a scan that
// found nothing); NoMoveFound$ True leaves the found card in the library;
// FoundLibraryPosition$ places a library-destination found card at the bottom
// ("-1") or leaves it on top ("0"/absent, no event); ImprintFound$ /
// ImprintRevealed$ record the found / all-revealed cards on the source's
// imprint association (the Seek "seek-found" list); NoneFoundDestination$ /
// NoneFoundLibraryPosition$ give the nothing-found branch its own
// destination. What remains withheld: Amount$ whose SVar is absent or
// unresolvable (amount 1 then) and DigZone$ (every corpus value is
// PlanarDeck, and this build has no planar tier). RevealRandomOrder$ True is
// implemented for the library-bottom return (h.Rand, seeded and replay-exact);
// a stay-in-place placement keeps the existing order behind one loud Note.
func effDigUntil(h Host, c *Ctx, sa *cards.SA) {
	spec := sa.Params["Valid"]
	if spec == "" {
		spec = "Card"
	}
	spec = permanentCardSpec(spec)
	foundDestName := sa.Params["FoundDestination"]
	if foundDestName == "" {
		foundDestName = "Hand"
	}
	foundDest := ParseZone(foundDestName)
	revDest := state.ZLibrary
	if raw := strings.TrimSpace(sa.Params["RevealedDestination"]); raw != "" {
		revDest = ParseZone(raw)
	}
	revPos := strings.TrimSpace(sa.Params["RevealedLibraryPosition"])
	optionalMove := strings.EqualFold(strings.TrimSpace(sa.Params["OptionalFoundMove"]), "True")
	noMoveRevealed := strings.EqualFold(strings.TrimSpace(sa.Params["NoMoveRevealed"]), "True")
	revealRandomOrder := strings.EqualFold(digUntilParamValue(sa, "RevealRandomOrder"), "True")
	tapped := strings.EqualFold(strings.TrimSpace(sa.Params["Tapped"]), "True")
	gainControl := strings.EqualFold(strings.TrimSpace(sa.Params["GainControl"]), "True")
	rememberFound := strings.EqualFold(strings.TrimSpace(sa.Params["RememberFound"]), "True")
	rememberRevealed := strings.EqualFold(strings.TrimSpace(sa.Params["RememberRevealed"]), "True")
	amount := int32(1)
	var withheld []string
	if raw := strings.TrimSpace(sa.Params["Amount"]); raw != "" && raw != "1" {
		if n, err := strconv.Atoi(raw); err == nil && n > 1 && n <= 5 {
			amount = int32(n)
		} else if n, resolved := digUntilAmountSVar(h, c, raw); resolved {
			// A non-literal token names an SVar (X/MassX/Y/VoteNum); its body
			// is read by the count evaluator, the same read effDig's DigNum$ X
			// arm uses. A zero tally is legitimate (Selvala's Stampede with no
			// wild vote reveals nothing).
			amount = n
		} else {
			// Absent or unresolvable SVar: fail-safe to amount 1, and keep the
			// loud-unimplemented Note contract.
			withheld = append(withheld, "Amount$ "+raw)
		}
	}
	// DigZone$ stays withheld: every corpus value is PlanarDeck, and this
	// build has no planar deck or planar zone (the planechase approximation).
	// The ordinary library scan remains the deterministic core move.
	if v := digUntilParamValue(sa, "DigZone"); v != "" {
		withheld = append(withheld, "DigZone$ "+v)
	}
	noMoveFound := digUntilTrueFlag(sa, "NoMoveFound", &withheld)
	shuffle := digUntilTrueFlag(sa, "Shuffle", &withheld)
	// ShuffleCondition$ models exactly NoneFound (Tunnel Vision's
	// FindThePrecious: "otherwise, that player shuffles"); any other value is
	// named loudly rather than guessed at.
	shuffleNoneFound := false
	if v := digUntilParamValue(sa, "ShuffleCondition"); v != "" {
		if strings.EqualFold(v, "NoneFound") {
			shuffleNoneFound = true
		} else {
			withheld = append(withheld, "ShuffleCondition$ "+v)
		}
	}
	imprintFound := digUntilTrueFlag(sa, "ImprintFound", &withheld)
	imprintRevealed := digUntilTrueFlag(sa, "ImprintRevealed", &withheld)
	// FoundLibraryPosition$ places a found card whose destination IS the
	// library: "-1" the bottom, "0"/absent the stay-in-place top default. Any
	// other value is named loudly and the card stays on top.
	foundPos := strings.TrimSpace(sa.Params["FoundLibraryPosition"])
	if foundPos != "" && foundPos != "0" && foundPos != "-1" {
		withheld = append(withheld, "FoundLibraryPosition$ "+foundPos)
		foundPos = ""
	}
	// NoneFound* is the nothing-found branch (Tunnel Vision carries both):
	// when the scan finds nothing its revealed cards go to
	// NoneFoundDestination$ at NoneFoundLibraryPosition$ instead of the
	// RevealedDestination$/RevealedLibraryPosition$ pair. Absent keys leave the
	// ordinary revealed destination in force.
	noneFoundSet := strings.TrimSpace(sa.Params["NoneFoundDestination"]) != "" ||
		strings.TrimSpace(sa.Params["NoneFoundLibraryPosition"]) != ""
	noneFoundDest := state.ZLibrary
	noneFoundPos := ""
	if raw := strings.TrimSpace(sa.Params["NoneFoundDestination"]); raw != "" {
		noneFoundDest = ParseZone(raw)
	}
	if raw := strings.TrimSpace(sa.Params["NoneFoundLibraryPosition"]); raw != "" {
		if raw != "0" && raw != "-1" {
			withheld = append(withheld, "NoneFoundLibraryPosition$ "+raw)
		} else {
			noneFoundPos = raw
		}
	}
	// fx42 scoping: capture and clear the answered found-move election BEFORE
	// the target loop, so a nested DigUntil in the same chain poses its own
	// ask instead of inheriting the answer. moveDone also suppresses the
	// re-emit of the reveal Note (recorded before the first-pass ask) and of
	// the withheld-params Note.
	moveAns := c.DigUntilMove
	moveDone := c.DigUntilMoveDone
	auraBearer := c.DigUntilAuraBearer
	auraDone := c.DigUntilAuraDone
	c.DigUntilMove, c.DigUntilMoveDone = "", false
	c.DigUntilAuraBearer, c.DigUntilAuraDone = 0, false
	if moveAns == "" {
		moveAns = "no"
	}
	g := h.Game()
	if !moveDone && !auraDone {
		for _, param := range withheld {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "DigUntil withholds " + param + "; the core move runs without it"})
		}
	}
	targets := Defined(h, c, sa)
	if sa.Params["Defined"] == "" && sa.Params["ValidTgts"] == "" {
		// Forge's default for a reveal-until with no Defined$ and no targets:
		// the resolving controller's own library (Songbirds' Blessing's
		// trigger). Defined's source-object fallback is wrong here — the
		// source is the resolving permanent, not a player.
		targets = []state.Target{{Player: c.Controller, IsPlayer: true}}
	}
	// DigZone$ is withheld above. The ordinary library scan remains the
	// deterministic core move even for the PlanarDeck carriers; their full
	// planar-zone semantics are outside this primitive.
	// declineDest is where the found card goes when the optional move is
	// declined: OptionalNoDestination$ when the SA carries one, else the
	// revealed pile (the corpus oracles' "put all cards revealed this way
	// that weren't put onto the battlefield ...").
	declineDest := revDest
	if raw := strings.TrimSpace(sa.Params["OptionalNoDestination"]); raw != "" {
		declineDest = ParseZone(raw)
	}
	// One classification for the whole DigUntil call, before the player walk.
	// The rider is only ever delivered on a battlefield entry (apply gates on
	// the destination it is handed), so it is classified against that zone
	// rather than against either of the two destinations the walk picks
	// between.
	rider := classifyAttackingEntry(c, sa, state.ZBattlefield)
	players := playerIDsFromTargets(h, c, sa.Params["Defined"], targets)
	selection := *c // Valid$ Card.IsRemembered uses the pre-clear set.
	// A DigUntil re-runs its scan filter on the answered re-entry too (the
	// found-move election answers mid-walk), so the snapshot arms for any
	// player count, a single library's re-scan included.
	initForgetOtherSnapshot(h, c, sa, players, 1)
	forgetOtherRemembered(h, c, sa)
	// RememberFound$ replaces the resolution's Remembered set with found
	// cards, or with all revealed cards when RememberRevealed$ is also set.
	// Trigger referents remain in Ctx.Captured. Accumulate across the
	// player walk so a later player's reveal does not erase earlier ones.
	var digRemembered []state.Target
	var imprintObjs []state.ObjID
	for _, p := range players {
		lib := zoneOf(g, state.ZLibrary, p)
		if len(lib) == 0 {
			continue
		}
		var revealed, found []state.ObjID
		// A zero Amount$ (an SVar tally of 0) reveals nothing: the loop's own
		// `len(found) >= amount` would otherwise stop on the first card.
		if amount > 0 {
			for _, id := range lib {
				revealed = append(revealed, id)
				if MatchesSpecCtx(g, spec, id, forgetOtherPreClearContext(&selection, c)) {
					found = append(found, id)
					if int32(len(found)) >= amount {
						break
					}
				}
			}
		}
		// The reveal is PUBLIC (the same non-Secret ids-Note effDig's Reveal$
		// arm emits), recorded before the ask, once per resolution -- a
		// re-entry after the optional-move answer must not reveal again.
		if !moveDone && !auraDone && len(revealed) > 0 {
			h.Emit(events.Event{Kind: events.Note, Player: p, IDs: revealed})
		}
		if optionalMove && !moveDone {
			verb := digDestPhrase(foundDest)
			d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
				Source:     c.Source,
				ResumeKind: "diguntil_move", ResumeSA: sa,
				// The ForgetOtherRemembered$ pre-clear snapshot rides the ask:
				// the answered re-entry re-runs the scan filter against the
				// pre-clear candidates after the clear.
				ResumeForgetOtherSnapshot: copyTargets(c.ForgetOtherSnapshot),
				ResumeForgetOtherOwners:   append([]state.PlayerID(nil), c.ForgetOtherOwners...),
				ResumeForgetOtherReady:    c.ForgetOtherReady,
				ResumeForgetOtherCleared:  c.ForgetOtherCleared,
				Prompt:                    "Put the revealed matching card(s) onto " + verb + "?",
				Options: []decision.Option{
					{Index: 0, Kind: "yes", Label: "Yes — put into " + verb, Player: p},
					{Index: 1, Kind: "no", Label: "No", Player: p},
				}}
			if Ask(h, d) == AskAsked {
				return // resolution suspended; the answer re-enters with Ctx.DigUntilMove set.
			}
			// Fuzz/no-engine host: the deterministic decline (R-9) — the found
			// card(s) join the decline destination.
			moveDone = true
			moveAns = "no"
		}
		switch {
		case rememberRevealed:
			// The revealed set already includes every found card. Alone,
			// RememberRevealed$ retains its append semantics; paired with
			// RememberFound$ it replaces the trigger capture at the end.
			for _, id := range revealed {
				if rememberFound {
					digRemembered = append(digRemembered, state.Target{Obj: id})
				} else {
					c.Remembered = append(c.Remembered, state.Target{Obj: id})
				}
			}
		case rememberFound:
			for _, id := range found {
				digRemembered = append(digRemembered, state.Target{Obj: id})
			}
		}
		foundJoinedRevealed := false
		if len(found) > 0 {
			dest := foundDest
			if noMoveFound {
				// NoMoveFound$ True: the found card is not moved to its
				// destination. Its effective destination is the library, so a
				// FoundLibraryPosition$ still places it within the library.
				dest = state.ZLibrary
			}
			if optionalMove && !noMoveFound && moveAns != "yes" {
				dest = declineDest
				foundJoinedRevealed = dest == revDest
			}
			for _, id := range found {
				if dest == state.ZBattlefield {
					// An AURA face put onto the battlefield by a non-cast effect
					// never got a cast-time target. With NO eligible bearer the
					// card stays in the library (CR 303.4f's
					// remain-in-current-zone) instead of entering unattached and
					// dying to the CR 704.5m SBA.
					bearer, isAuraFace := auraEntryBearer(g, id, p)
					if isAuraFace {
						bearers, _ := auraEntryBearers(g, id, p)
						switch {
						case auraDone:
							// The answered bearer is revalidated against the
							// current battlefield before it is used.
							bearer = 0
							for _, candidate := range bearers {
								if candidate == auraBearer {
									bearer = candidate
									break
								}
							}
							auraDone = false
						case len(bearers) == 0:
							bearer = 0
						case len(bearers) == 1:
							bearer = bearers[0]
						default:
							d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
								Source: c.Source, ResumeKind: "diguntil_aura", ResumeSA: sa,
								ResumeDigUntilMove: moveAns, ResumeDigUntilMoveDone: moveDone,
								ResumeForgetOtherSnapshot: copyTargets(c.ForgetOtherSnapshot),
								ResumeForgetOtherOwners:   append([]state.PlayerID(nil), c.ForgetOtherOwners...),
								ResumeForgetOtherReady:    c.ForgetOtherReady,
								ResumeForgetOtherCleared:  c.ForgetOtherCleared,
								Prompt:                    "Choose a permanent for the revealed Aura to enchant"}
							for i, candidate := range bearers {
								d.Options = append(d.Options, decision.Option{Index: i, Kind: "card", Obj: candidate, Player: p})
							}
							if Ask(h, d) == AskAsked {
								return
							}
							// R-9: a host without an answer takes the
							// deterministic first candidate.
							bearer = bearers[0]
						}
					}
					if isAuraFace && bearer == 0 {
						h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: p,
							Text: "no permanent the revealed Aura can enchant; it stays in the library (CR 303.4f)"})
						continue
					}
					ev := moveZoneEvent(c, id, state.ZLibrary, dest)
					ev.Player, ev.Secret = p, true
					h.Emit(ev)
					if tapped {
						h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: p, Text: "entered tapped"})
					}
					rider.apply(h, c, id, p, dest)
					if gainControl {
						h.Emit(events.Event{Kind: events.ControlChange, Obj: id, Player: c.Controller})
					}
					if bearer != 0 {
						// CR 303.4f: the selected permanent is the Aura's
						// chosen bearer on this non-cast battlefield entry.
						emitAttach(h, id, bearer)
					}
					// StaticEffect$ on a DigUntil battlefield take: the same
					// rider registration every ChangeZone mover applies (no
					// corpus carrier rides a DigUntil today; hooked so the
					// class cannot miss one).
					applyStaticEffect(h, c, sa, dest, []state.ObjID{id})
					continue
				}
				if dest == state.ZLibrary {
					// The found card's destination IS the library (an explicit
					// FoundDestination$ Library, or NoMoveFound$ True).
					// FoundLibraryPosition$ "-1" puts it on the bottom via a real
					// library-to-library move (Move's zone append — the same
					// contract the revealed-rest "-1" arm below uses); "0"/absent
					// is the stay-in-place top default, so no event.
					if foundPos == "-1" {
						h.Emit(events.Event{Kind: events.MoveZone, Obj: id,
							From: state.ZLibrary, To: state.ZLibrary, Player: p, Secret: true})
					}
					continue
				}
				ev := moveZoneEvent(c, id, state.ZLibrary, dest)
				ev.Player, ev.Secret = p, true
				h.Emit(ev)
			}
		}
		// The revealed rest (plus a found card whose decline joined the pile)
		// move to RevealedDestination$; when the scan found NOTHING and the SA
		// carries a NoneFound* key they move to NoneFoundDestination$ at
		// NoneFoundLibraryPosition$ instead. NoMoveRevealed$ True (8 corpus
		// lines) leaves them where they are.
		restDest, restPos := revDest, revPos
		if len(found) == 0 && noneFoundSet {
			restDest, restPos = noneFoundDest, noneFoundPos
		}
		if !noMoveRevealed {
			// The rest is the revealed pile minus any found card that really
			// left the pile; a library-bottom random return shuffles exactly
			// THIS list (the order the per-card Secret MoveZone events are
			// emitted in IS the returned bottom order — zone append lands each
			// card at the bottom in emit order). Reveal order is NOT shuffled:
			// the public Note and the Remembered capture above stay in scan
			// order.
			toReturn := make([]state.ObjID, 0, len(revealed))
			for _, id := range revealed {
				isFound := false
				for _, fid := range found {
					if fid == id {
						isFound = true
						break
					}
				}
				if isFound && !foundJoinedRevealed {
					continue
				}
				toReturn = append(toReturn, id)
			}
			if restDest == state.ZLibrary && revealRandomOrder {
				switch {
				case restPos == "-1":
					// A full Fisher-Yates over the return list (the h.Rand idiom
					// the random pick/discard arms use) draws once per position,
					// so the seeded generator replays byte-identically. This is
					// the engine's seeded randomness, not a library shuffle:
					// T:Mode$ Shuffled triggers must not fire for a bottom return
					// that merely happens to be random.
					for i := 0; i < len(toReturn); i++ {
						j := i + h.Rand(len(toReturn)-i)
						toReturn[i], toReturn[j] = toReturn[j], toReturn[i]
					}
				case restPos == "" || restPos == "0":
					// Stay-in-place placement keeps the existing order (no
					// library randomisation is expressible there); name the
					// limitation once.
					h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: p,
						Text: "RevealRandomOrder$ with stay-in-place placement keeps existing order"})
				}
			}
			for _, id := range toReturn {
				if restDest == state.ZLibrary {
					// Library placement: "-1" (bottom) is a real
					// library-to-library move (Move's zone append lands it at the
					// bottom — the exact contract effDig's LibraryPosition2$ "-1"
					// arm documents); "0"/absent is the engine's stay-in-place
					// default (the cards already sit on top in their existing
					// relative order) so no event; anything else is named loudly
					// and the card stays.
					if restPos == "-1" {
						h.Emit(events.Event{Kind: events.MoveZone, Obj: id,
							From: state.ZLibrary, To: state.ZLibrary, Player: p, Secret: true})
					} else if restPos != "" && restPos != "0" {
						h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: p,
							Text: "RevealedLibraryPosition$ " + restPos + " is not implemented; the card stays on top"})
					}
					continue
				}
				ev := moveZoneEvent(c, id, state.ZLibrary, restDest)
				ev.Player, ev.Secret = p, true
				h.Emit(ev)
			}
		}
		// Shuffle$ True: after the found move and the revealed-rest moves,
		// shuffle the dug player's library (the same Fisher-Yates +
		// Secret events.Shuffle contract effShuffle emits). ShuffleCondition$
		// NoneFound restricts it to a scan that found nothing.
		if shuffle && (!shuffleNoneFound || len(found) == 0) {
			order := h.ShuffleLibrary(p, g.Zone(state.ZLibrary, p))
			h.Emit(events.Event{Kind: events.Shuffle, Player: p, IDs: order, Secret: true})
		}
		// ImprintFound$/ImprintRevealed$ (Forge's addImprintedLists) are
		// accumulated across the player walk and emitted once after it, so a
		// suspension re-entry cannot double-record them.
		if imprintFound && len(found) > 0 {
			imprintObjs = append(imprintObjs, found...)
		}
		if imprintRevealed {
			imprintObjs = append(imprintObjs, revealed...)
		}
	}
	// The walk completed: release the ride (the same boundary the search and
	// hidden walks end at), so a later ability in the chain cannot inherit it.
	endForgetOtherSnapshot(c)
	if len(imprintObjs) > 0 && c.Source != 0 {
		// Forge's addImprintedLists links the found (or all revealed) cards to
		// the resolving source. It rides the Seek "seek-found" list, not the
		// ordinary Imprinted one: DigUntil's continuation readers (Defined$
		// Imprinted, Card.IsImprinted) read the association wherever the card
		// currently sits — library, battlefield — where the ordinary list's CR
		// 607.2a exile-only filter would hide it.
		h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source,
			IDs: append([]state.ObjID(nil), imprintObjs...), Text: "seek-found"})
	}
	if rememberFound {
		c.Remembered = digRemembered
	}
}

// digUntilParamValue is the withhold-list keys' trimmed value read (the
// paramcensus's dynamic-key rule: the key is this helper's own parameter,
// and every call site passes a string literal). Empty means absent-or-False.
func digUntilParamValue(sa *cards.SA, key string) string {
	v := strings.TrimSpace(sa.Params[key])
	if strings.EqualFold(v, "False") {
		return ""
	}
	return v
}

// digUntilAmountSVar resolves a non-literal Amount$ token as an SVar name
// (empty_the_laboratory's Y, kindred_summons' X, mass_polymorph's MassX,
// selvalas_stampede's runtime VoteNum). The body is evaluated with the count
// evaluator -- the same read effDig's DigNum$ X arm uses; an absent SVar table
// or name, or an unmodelled count head, reports not-resolved so the caller
// keeps its fail-safe amount 1.
func digUntilAmountSVar(h Host, c *Ctx, token string) (int32, bool) {
	if c == nil || c.SVars == nil {
		return 0, false
	}
	body, ok := c.SVars[token]
	if !ok {
		return 0, false
	}
	return EvalCountOK(h, c, body)
}

// digUntilTrueFlag reads a boolean DigUntil rider. Absent or False means the
// rider is off. Any other value is not part of the modelled grammar: it is
// named loudly (appended to withheld) and treated as off rather than guessed
// at.
func digUntilTrueFlag(sa *cards.SA, key string, withheld *[]string) bool {
	v := digUntilParamValue(sa, key)
	if v == "" {
		return false
	}
	if strings.EqualFold(v, "True") {
		return true
	}
	*withheld = append(*withheld, key+"$ "+v)
	return false
}

// auraEntryBearer resolves a non-cast battlefield entry's Aura bearer: the
// first permanent in seat order, then battlefield order, that satisfies the
// face's Enchant keyword spec (or any permanent when the face
// carries no Enchant keyword — nothing in the corpus prints one, the same
// convention rules/attach.go's auraStillMatchesEnchant uses). aura is false
// when the face is not an Aura (no attach needed); aura && bearer == 0
// means the face IS an Aura but no eligible bearer exists. The filter read
// is the shared MatchesSpecFrom, so a compound spec (Enchant:
// Creature.YouCtrl) evaluates exactly like an attach-time legality check.
func auraEntryBearer(g *state.Game, id state.ObjID, p state.PlayerID) (state.ObjID, bool) {
	bearers, isAura := auraEntryBearers(g, id, p)
	if len(bearers) > 0 {
		return bearers[0], true
	}
	return 0, isAura
}

func auraEntryBearers(g *state.Game, id state.ObjID, p state.PlayerID) ([]state.ObjID, bool) {
	o := g.Obj(id)
	if o == nil || o.Face() == nil {
		return nil, false
	}
	isAura := false
	for _, t := range o.Face().Types {
		if strings.EqualFold(t, "Aura") {
			isAura = true
			break
		}
	}
	if !isAura {
		return nil, false
	}
	spec := "Permanent"
	if param, ok := o.Face().KeywordParam("Enchant"); ok && strings.TrimSpace(param) != "" {
		spec, _, _ = strings.Cut(param, ":")
	}
	spec = strings.TrimSpace(spec)
	var bearers []state.ObjID
	for seat := range g.Players {
		for _, bid := range g.Zone(state.ZBattlefield, state.PlayerID(seat)) {
			if bid == id {
				continue
			}
			if MatchesSpecFrom(g, spec, bid, p, id) {
				bearers = append(bearers, bid)
			}
		}
	}
	return bearers, true
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
	_, hasNum := sa.Params["NumCards"]
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
	answer := c.RevealOpt
	// optTarget is the Defined$ target index whose yes/no answer this is (the
	// decision's ResumeTarget); it travels with the answer exactly as
	// pickTarget travels with RevealPick. An answer applies to its cursor
	// target alone and every later target poses its own ask.
	optTarget := c.RevealOptTarget
	c.RevealOpt, c.RevealOptTarget = "", 0
	// The answered hand-reveal pick (task infernaltutor1), consumed once per
	// walk exactly as RevealOpt is: a nested Reveal-family effect below this
	// one must pose its own ask instead of inheriting this walk's answer.
	// Non-nil means answered (the resume arm always builds the slice, so an
	// empty "reveal none" answer is non-nil), mirroring Ctx.Discard.
	picks := c.RevealPick
	pickTarget := c.RevealPickTarget
	c.RevealPick, c.RevealPickTarget = nil, 0
	// The bare-look ack (lookack): consumed once per WALK, together with its
	// per-target cursor — the answer attaches to the exact Defined$ target
	// that asked (the decision's ResumeTarget). Targets before the cursor
	// were fully processed on the pass that suspended and are skipped, the
	// cursor target emits without re-asking, and every LATER bare look in
	// the walk poses its own ack. Consuming at walk entry (fx42) also keeps
	// a nested bare look below this walk posing its own instead of
	// inheriting the answer.
	lookAck := c.LookAck
	lookAckTarget := c.LookAckTarget
	c.LookAck, c.LookAckTarget = false, 0
	look := strings.EqualFold(strings.TrimSpace(sa.Params["Look"]), "True")
	revealType := strings.TrimSpace(sa.Params["RevealType"])
	// The may-reveal ask: PeekAndReveal poses it through RevealOptional$
	// (Delver of Secrets); the Reveal/RevealHand shapes pose it through
	// Optional$ ("you may reveal" — Liar's Pendulum's two RevealHand lines
	// and the corpus's five Reveal lines), which used to be ignored and the
	// reveal forced. No corpus line combines Look$ with either flag, but the
	// ask is shaped to work for one anyway: a look is asked of the LOOKER
	// (the activator gains the information), a reveal of the player whose
	// cards would be shown.
	optional := strings.EqualFold(strings.TrimSpace(sa.Params["RevealOptional"]), "True") ||
		strings.EqualFold(strings.TrimSpace(sa.Params["Optional"]), "True")
	remember := strings.EqualFold(strings.TrimSpace(sa.Params["RememberRevealed"]), "True")
	random := strings.EqualFold(strings.TrimSpace(sa.Params["Random"]), "True")
	g := h.Game()
	// Forge's RevealDefined$ is the reveal family's equivalent of Defined$.
	// Copy the SA and translate only the target selector, so the common
	// resolver owns every Self/Targeted/Remembered spelling without mutating
	// the shared compiled corpus. This matters for opening-hand reveals:
	// Chancellor of the Tangle must reveal the chosen Chancellor, not an
	// unrelated first card in its controller's hand.
	revealSA := *sa
	if spec := sa.Params["RevealDefined"]; spec != "" {
		revealSA.Params = make(map[string]string, len(sa.Params)+1)
		for key, value := range sa.Params {
			revealSA.Params[key] = value
		}
		revealSA.Params["Defined"] = spec
	}
	for targetIndex, t := range Defined(h, c, &revealSA) {
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
		if plainRememberedSelector(revealSA.Params["Defined"]) && !t.IsPlayer {
			// Forge's getDefinedPlayers("Remembered") adds remembered PLAYERS
			// only; a remembered card must not widen the reveal's library/hand
			// scope to its controller (Summon: Valefor's per-opponent loop).
			continue
		}
		p := PlayerOf(h, c, t)
		pool := zoneOf(g, zone, p)
		if sa.Params["RevealDefined"] != "" && !t.IsPlayer {
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
		if rv := strings.TrimSpace(sa.Params["RevealValid"]); rv != "" {
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
		if rav := strings.TrimSpace(sa.Params["RevealAllValid"]); rav != "" {
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
		revealAllValid := strings.TrimSpace(sa.Params["RevealAllValid"])
		pickable := zone == state.ZHand && !wholeHand && !random && !look && revealAllValid == ""
		if pickable {
			minPick, maxPick := n, n
			if anyNumber := strings.EqualFold(strings.TrimSpace(sa.Params["AnyNumber"]), "True"); anyNumber {
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
					if Ask(h, d) == AskAsked {
						return // resolution suspended; the answer re-enters with Ctx.RevealPick set.
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
					selected := make([]state.ObjID, 0, len(picks))
					for _, id := range picks {
						for _, cand := range pool {
							if cand == id {
								selected = append(selected, id)
								break
							}
						}
					}
					pool = selected
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
		if strings.EqualFold(strings.TrimSpace(sa.Params["NoReveal"]), "True") {
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
			if strings.EqualFold(strings.TrimSpace(sa.Params["RememberPeeked"]), "True") {
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
			if Ask(h, d) == AskAsked {
				return
			}
			// No host to ask (R-9), or the ask was skipped: fall through to the
			// mandatory reveal below, deterministic run to run. (A
			// reveal_optional decision is Min == Max == 1 over two options, so
			// AskEmpty is unreachable by construction; the shared helper owns
			// the guard either way.)
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
			if strings.EqualFold(strings.TrimSpace(sa.Params["BecomeStartingPlayer"]), "True") {
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
	mayShuffle := strings.EqualFold(strings.TrimSpace(sa.Params["MayShuffle"]), "True")
	// Re-entry after rules' handleArrange applied the answered KArrange starts
	// at the next target. If MayShuffle is present, that answer belongs to the
	// target at LibraryTarget; otherwise the current target still needs its
	// post-arrange shuffle election. In both cases the walk then continues to
	// later libraries.
	start := 0
	if c.Arrange {
		start = c.LibraryTarget
		c.Arrange = false
		if mayShuffle && c.MayShuffle == "" {
			players := actingPlayers(h, c, sa)
			if start >= 0 && start < len(players) {
				p := players[start]
				d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
					Source: c.Source, ResumeKind: "arrange_mayshuffle", ResumeSA: sa,
					ResumeTarget: start, Prompt: "Shuffle your library?",
					Options: []decision.Option{
						{Index: 0, Kind: "yes", Label: "Yes — shuffle", Player: p},
						{Index: 1, Kind: "no", Label: "No — keep the order", Player: p},
					}}
				if Ask(h, d) == AskAsked {
					return // resolution suspended; the answer re-enters with Ctx.MayShuffle set.
				}
			}
		}
		if mayShuffle {
			c.MayShuffle = ""
		}
		start++
	}
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
		if Ask(h, d) == AskAsked {
			return // resolution suspended; the answer re-enters with Ctx.Arrange set.
		}
		// Fuzz/no-engine host: the deterministic stand-in keeps the existing
		// order -- pile A = the offered options in offered order (J3) -- with
		// the LibraryOrder recording that the order was (re)set unchanged.
		h.Emit(events.Event{Kind: events.LibraryOrder, Player: p,
			IDs:    append([]state.ObjID(nil), lib...),
			Secret: true})
	}
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
	arranging := c.Arrange
	ans := c.SurveilLookOpt
	c.SurveilLookOpt = ""
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
				// the ans != "" gate.
				if Ask(h, d) == AskAsked {
					return
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
	if c.Arrange {
		start = c.LibraryTarget + 1
		c.Arrange = false
	} else if c.ScryReplacement {
		start = c.LibraryTarget
	}
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
		if !markSurveil && strings.EqualFold(strings.TrimSpace(sa.Params["Optional"]), "True") {
			opt := c.ScryOpt
			c.ScryOpt = ""
			if opt == "" {
				d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
					Source: c.Source, ResumeKind: "scry_optional", ResumeSA: sa, ResumeTarget: targetIndex,
					Prompt: "Scry?", Options: []decision.Option{
						{Index: 0, Kind: "yes", Label: "Yes", Player: p},
						{Index: 1, Kind: "no", Label: "No", Player: p},
					}}
				if Ask(h, d) != AskNoHost {
					return
				}
				// R-9: an unavailable host deterministically declines an
				// optional election; never treat an unanswered ask as consent.
				continue
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
			if c.ScryReplacement && c.LibraryTarget == targetIndex {
				k, proceed = c.ScryCount, c.ScryProceed
				c.ScryReplacement = false
			} else {
				var pending bool
				k, proceed, pending = h.Scry(p, c.Source, k, sa, targetIndex)
				if pending {
					return
				}
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
		if Ask(h, d) == AskAsked {
			return // resolution suspended; the answer re-enters with Ctx.Arrange set.
		}
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
	if c.HideawayArranged {
		c.HideawayArranged = false
		return
	}
	g := h.Game()
	if c.HideawayPicked {
		// The choice was recorded by rules' hideaway_pick resume arm. Move the
		// selected card before arranging: that leaves precisely the remaining
		// cards at the top of the library for the KArrange handler.
		id := c.Hideaway
		c.Hideaway = 0
		c.HideawayPicked = false
		lib := zoneOf(g, state.ZLibrary, c.Controller)
		if id == 0 || !containsObj(lib, id) {
			return
		}
		h.Emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZExile,
			Counter: "exiled_with_face_down", Amount: int32(c.Source), Secret: true})
		hideawayBottom(h, c, sa)
		return
	}
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
	if h.Ask(d) {
		return
	}
	// The no-host degradation chooses the first card, then retains the offered
	// order for the rest on the bottom.
	h.Emit(events.Event{Kind: events.MoveZone, Obj: lib[0], From: state.ZLibrary, To: state.ZExile,
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
	valid := sa.Params["ValidCards"]
	chooseFromList := sa.Params["ChooseFromList"]
	chooseFromDefined := sa.Params["ChooseFromDefinedCards"]
	universeBacked := len(h.Game().NameUniverse) > 0
	random := strings.EqualFold(sa.Params["AtRandom"], "True")
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
	names := NameChoicesFromListCtx(h.Game(), valid, sa.Params["ValidDescription"], chooseFromList, &sc, random || chooseFromDefined != "")
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
		if Ask(h, d) == AskAsked {
			return
		}
		c.NameChoice = names[0]
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
	return Defined(h, c, &cards.SA{Params: map[string]string{"Defined": spec}})
}
