package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// replChoice is one CR 616.1 order-selection suspension: the MoveZone event
// more than one replacement would modify, parked (never emitted, never
// applied -- the moving object stays put) along with the competing
// replacements, until the affected controller picks the order. The answer
// applies the chosen replacement for real. Plain value data (an events.Event
// plus a []replMatch whose *cards.Repl pointers are shared immutable corpus
// data), so Clone copies the queue with one slice copy, the same class as
// cmdZone.
type replChoiceKind uint8

const (
	replChoiceMove replChoiceKind = iota
	replChoiceMana
	replChoiceManaColor
	replChoicePhaseOrder
	replChoicePhaseOptional
	// replChoiceUntap parks competing effects that would replace one Untap.
	// It is appended so existing in-memory enum values remain unchanged.
	replChoiceUntap
	// replChoiceDamage parks a CR 616.1 order competition for an object
	// Damage event (repl:DamageDone): two or more applicable damage
	// replacements (a Prevent$ True shield, a DB$ ReplaceDamage subtraction,
	// a DB$ ReplaceEffect amount rewrite) and the affected player orders
	// them. Recomputed every cycle (CR 616.1e) rather than driven by
	// applied/applicable like the mana/phase kinds, because a redirect or
	// amount rewrite can change which of the ORIGINAL candidates still apply.
	replChoiceDamage
	// replChoiceCounter parks CR 616.1's order competition when two or more
	// repl:Counter replacements would stop the same spell/ability.
	replChoiceCounter
	// replChoiceAddCounter parks CR 616.1's order competition when two or
	// more AddCounter (CounterChange/PlayerCounterChange) replacements whose
	// bodies do not all commute would rewrite the same placement (Hardened
	// Scales' Plus.1 and Branching Evolution's Twice: 1 -> 2 -> 4 one way,
	// 1 -> 2 -> 3 the other). It is appended so existing in-memory enum
	// values remain unchanged.
	replChoiceAddCounter
	// replChoiceToken parks CR 616.1's order competition when two or more
	// CreateToken (repl:CreateToken) replacements whose bodies do not all
	// commute would rewrite the same creation (a multiplier and a script
	// rewriter: the rewriter composed after the multiplier rewrites every
	// duplicated mint, composed before only the original).
	replChoiceToken
	// replChoiceUpdated parks CR 616.1's order competition for an all-Updated
	// MoveZone composition whose bodies do not commute (a tap and an untap
	// fighting over the same tapped bit; the last body applied wins).
	replChoiceUpdated
	replChoiceScry
	// replChoiceEntryOrder parks the CR 616.1 order competition over an
	// ENTRY-CHARACTERISTIC counter grant (rules/entry_counters.go): the
	// staged move has not folded, and the answer resumes the competition on
	// an isolated post-entry preview before the staged entry re-emits. It is
	// appended so existing in-memory enum values remain unchanged.
	replChoiceEntryOrder
	replChoiceFaceUp
	// replChoiceDraw asks whether to apply a bodyless optional draw replacement.
	// It is appended so existing in-memory enum values remain unchanged.
	replChoiceDraw
	replChoiceLoseMana
)

type replChoice struct {
	kind         replChoiceKind
	ev           events.Event
	cands        []replMatch
	applied      []bool      // mana/phase: candidates that already had their opportunity
	applicable   []int       // order decision option -> candidate index
	selected     int         // mana colour / phase optional: candidate awaiting its answer
	changed      bool        // mana: at least one rewrite already happened
	manaTapped   bool        // mana: tap provenance survives the decision boundary
	manaProducer state.ObjID // mana: producer survives the decision boundary
	boundary     bool        // phase: this choice owns setStep's boundary cleanup
	leaving      state.Step
	before       *triggerSnapshot // immutable SBA look-back, safe to share in Clone
	untap        *untapStep       // remaining turn-based untaps after an order answer
	// life marks a life gain/loss competition (applyLifeReplacements): ev is
	// then the event as already modified by appliedRepls, the affected player
	// is ev.Player, and the answer continues the CR 616.1 loop rather than
	// finishing after one application. damaging, combatDamaging and
	// dmgSrcOverride are the synchronous damage context the parked event was
	// proposed under, restored while the answer emits it so provenance-reading
	// triggers and protection see the same source.
	life           bool
	exchange       *lifeExchangeTransaction
	appliedRepls   []replMatch
	damaging       state.ObjID
	combatDamaging bool
	dmgSrcOverride state.ObjID
	// used is kind == replChoiceDamage's own applied-set: the matches already
	// settled this CR 616.1e recomputation cycle for an object Damage event
	// (repl:DamageDone), tracked by value rather than applied's per-candidate
	// bool index because remainingDamageReplacements recomputes the candidate
	// set itself after every rewrite instead of indexing a fixed cands slice.
	// player is the resolved asking player (replacementAskPlayer's result,
	// which may differ from the CR 616.1e affected player recomputed fresh
	// into damageAffectedPlayer at every cycle) for kind == replChoiceDamage
	// or replChoiceCounter.
	used   []replMatch
	player state.PlayerID
	// combat marks a damage competition parked from the combat-damage step's
	// own assignment loop (rules/combat.go): the chosen replacement's
	// lifelink/deathtouch riders and commander-damage tally pay the way
	// ordinary combat damage does, and finishChosenDamage resumes
	// runCombatAssignments once this batch's parked choices all settle.
	// lifelink/deadly cache the damage source's keywords at park time (Task
	// 15's provenance-freezing discipline), toxic caches the source's total
	// CR 702.164 toxic N the same way (runCombatAssignments' synchronous emit
	// site reads it at the moment of the hit; a parked hit must poison the
	// same amount when it lands in finishChosenDamage), and cause is the
	// Counter replacement's cause object (counterReplacementMatches' third
	// argument), set only when kind == replChoiceCounter.
	combat   bool
	lifelink bool
	deadly   bool
	toxic    int
	cause    state.ObjID
	// counterAdderPlusOne is kind == replChoiceAddCounter's adder provenance
	// captured when the competition was POSED (PLUS ONE; 0 = unknown). The
	// answer can arrive after the proposing window has closed -- a body's
	// CounterChange poses from inside the replacement body, and the
	// applyingReplacement/replacingSource that named the body is gone by resume
	// time -- so the final emit in emitAddCounterReplacement republishes this
	// captured value instead of re-deriving it (rules/replacement.go).
	counterAdderPlusOne state.PlayerID
	// tokenPlan/tokenNext are kind == replChoiceToken's parked plan state: the
	// mints the already-applied matches produced and the cursor this choice
	// was posed at (candidates are cands[cursor:]). Plain value data, the
	// same Clone class as the rest of the queue.
	tokenPlan []tokenPlanMint
	tokenNext int
	// emitted marks kind == replChoiceUpdated's original event as already
	// emitted (the composition's preamble ran); a re-parked continuation
	// skips the emit and resolves only the remaining bodies.
	emitted bool
	// absorbed is kind == replChoiceUpdated's set of Updated PutCounter|
	// ETB$ True bodies (replIdentity) whose placement the entry move's Pairs
	// payload already carries (rules/entry_counters.go). It is captured from
	// foldEntryMove on the preamble pass and consulted again on every
	// re-parked continuation, so a later-answered absorbed body is never run
	// a second time.
	absorbed []string
	// stage is kind == replChoiceEntryOrder's parked entry (rules/
	// entry_counters.go): the move that has not folded and the competition
	// state its resume continues. Pointer data, the same Clone class as
	// before (*triggerSnapshot) -- the stage is reached only through its
	// own answer.
	stage *entryCounterStage
	// inResolution marks a competition posed while a stack resolution was in
	// flight (e.resolvingObj != 0): the pose's Engine.Ask then parked that
	// resolution on e.resume with the interrupted object still on the stack,
	// and the LAST answer round of the queue must resume it through its
	// recorded chain -- the same discipline the damage branch applies -- or
	// resolveTop re-resolves the interrupted object from the top on the next
	// priority pass, unbounded. A pose from turn structure or a cast window
	// records no suspension (Engine.Ask's frame there is the flow's own
	// bookkeeping, consumed by its own handler) and must not be resumed.
	inResolution bool
	// resumeAtPose is the Engine.Ask record the pose itself created, captured
	// when askReplacementChoice posed an ask with e.resume nil. For an
	// inResolution competition it IS the suspended resolution (and the answer
	// resumes it); otherwise it is the pose's own bookkeeping from a cast
	// window or turn structure -- nothing was suspended -- and the tail drops
	// it once the competition's work completed synchronously, because
	// resolveTop reads e.resume to decide whether its resolution suspended
	// and a stale frame makes it abandon a resolution that actually finished,
	// re-resolving it on every pass.
	resumeAtPose *resumePoint
}

// replacementChoicePlayer is the affected player a parked competition asks:
// a life event's player (the player whose life total changes), else mana/
// phase candidates by role, else the moving object's controller.
func (e *Engine) replacementChoicePlayer(rc replChoice) (state.PlayerID, bool) {
	if rc.life {
		return rc.ev.Player, int(rc.ev.Player) < len(e.G.Players)
	}
	switch rc.kind {
	case replChoiceMana, replChoiceManaColor, replChoiceScry, replChoiceLoseMana:
		return rc.ev.Player, int(rc.ev.Player) < len(e.G.Players)
	case replChoicePhaseOrder, replChoicePhaseOptional:
		return e.G.Active, int(e.G.Active) < len(e.G.Players)
	case replChoiceDamage, replChoiceCounter, replChoiceDraw:
		// Already resolved by poseDamageReplacementChoice/the recomputation
		// cycle (replacementAskPlayer over the freshly recomputed CR 616.1e
		// affected player), never re-derived from rc.ev.Obj's controller here
		// -- a redirect or an OptionalDecider$ can make the asking player
		// differ from that.
		return rc.player, int(rc.player) < len(e.G.Players)
	case replChoiceAddCounter, replChoiceEntryOrder:
		// Resolved at pose time (the counter's recipient or the affected
		// object's controller; the entry order's posed player; the token's
		// creator) and recomputed at every
		// re-pose (continueAddCounterReplacements / the drive).
		return rc.player, int(rc.player) < len(e.G.Players)
	default:
		// A replaced DRAW event has no object whose controller could be
		// consulted -- the affected player is the draw-er itself (the same
		// binding the replacement's ReplacedPlayer context carries).
		if rc.ev.Kind == events.Draw {
			return rc.ev.Player, int(rc.ev.Player) < len(e.G.Players)
		}
		o := e.G.Obj(rc.ev.Obj)
		if o == nil || int(o.Controller) >= len(e.G.Players) {
			return 0, false
		}
		return o.Controller, true
	}
}

// poseUntapReplacementChoice starts the CR 616.1 choice between effects
// replacing one Untap. The affected player is the untapped object's
// controller, not either replacement source's controller.
func (e *Engine) poseUntapReplacementChoice(ev events.Event, matches []replMatch) {
	o := e.G.Obj(ev.Obj)
	if o == nil || int(o.Controller) >= len(e.G.Players) {
		return
	}
	rc := replChoice{kind: replChoiceUntap, ev: ev, cands: matches, before: e.retainTriggerBefore()}
	if e.untapResume != nil {
		resume := *e.untapResume
		rc.untap = &resume
	}
	e.replChoices = append(e.replChoices, rc)
	if e.pending == nil {
		e.askReplacementChoice(o.Controller)
	}
}

// poseReplacementChoice starts a CR 616.1 order-selection suspension: the
// competing event is parked and the affected controller -- the controller of
// the moving object, CR 616.1's "affected player" -- is asked which
// replacement applies first. Mirrors commander-zone parking: the ask is posed
// only when no other decision is already pending (the caller has already
// ruled out a departed controller, which makes no choices under CR 800.4a).
func (e *Engine) poseReplacementChoice(ev events.Event, matches []replMatch) {
	p := ev.Player
	kind := replChoiceMove
	if ev.Kind == events.ManaClear {
		kind = replChoiceLoseMana
	}
	if ev.Kind == events.Draw && len(matches) == 1 && matches[0].repl.With == nil &&
		matches[0].repl.OptionalValue() {
		kind = replChoiceDraw
		p = e.replacementAskPlayer(matches, ev.Player)
	}
	if ev.Kind != events.Draw && ev.Kind != events.ManaClear {
		o := e.G.Obj(ev.Obj)
		if o == nil {
			return
		}
		p = o.Controller
	}
	if int(p) >= len(e.G.Players) {
		return
	}
	e.replChoices = append(e.replChoices, replChoice{kind: kind,
		ev: ev, cands: matches, player: p, before: e.retainTriggerBefore(), inResolution: e.resolvingObj != 0 || e.answerInResolution})
	if e.pending == nil {
		e.askReplacementChoice(p)
	}
}

func (e *Engine) askReplacementChoice(p state.PlayerID) {
	rc := e.replChoices[0]
	d := &decision.Decision{Player: p, Kind: decision.KReplacement, Min: 1, Max: 1,
		Source: rc.ev.Obj, ResumeKind: "replacement"}
	indices := make([]int, len(rc.cands))
	for i := range indices {
		indices[i] = i
	}
	switch rc.kind {
	case replChoiceLoseMana:
		d.Prompt = "Several replacement effects would convert unspent mana: choose which applies."
	case replChoiceDamage:
		d.Prompt = "Several replacement effects would modify damage: choose which applies next."
	case replChoiceCounter:
		d.Prompt = "Several replacement effects would modify this counter event: choose which applies."
	case replChoiceAddCounter:
		d.Prompt = "Several replacement effects would modify how many counters are put: choose which applies first."
	case replChoiceEntryOrder:
		d.Prompt = "Several replacement effects would modify how many counters this permanent enters with: choose which applies first."
	case replChoiceToken:
		d.Prompt = "Several replacement effects would modify this token creation: choose which applies first."
	case replChoiceScry:
		d.Prompt = "Several replacement effects would modify this scry: choose which applies next."
		indices = rc.applicable
	case replChoiceMana:
		d.Prompt = "Several replacement effects would change mana production: choose which applies next."
		indices = rc.applicable
	case replChoiceManaColor:
		d.Prompt = "Choose the colour of the replacement mana."
		for i, color := range []string{"W", "U", "B", "R", "G"} {
			d.Options = append(d.Options, decision.Option{Index: i, Kind: "mana", Obj: rc.cands[rc.selected].id,
				Label: "Add " + color, ManaSymbol: color})
		}
		e.ask(d)
		return
	case replChoicePhaseOrder:
		d.Prompt = "Several replacement effects would change this step: choose which applies first."
		indices = rc.applicable
	case replChoicePhaseOptional:
		m := rc.cands[rc.selected]
		name := "this replacement effect"
		if so := e.G.Obj(m.id); so != nil && so.Face() != nil && so.Face().Name != "" {
			name = so.Face().Name
		}
		d.Prompt = "Apply " + name + "'s optional replacement and skip this step?"
		d.Options = []decision.Option{
			{Index: 0, Kind: "apply", Obj: m.id, Label: "Yes — skip this step"},
			{Index: 1, Kind: "decline", Obj: m.id, Label: "No — do not apply this replacement"},
		}
		e.ask(d)
		return
	case replChoiceDraw:
		m := rc.cands[rc.selected]
		name := "this replacement effect"
		if o := e.G.Obj(m.id); o != nil && o.Face() != nil && o.Face().Name != "" {
			name = o.Face().Name
		}
		d.Prompt = "Apply " + name + "'s optional draw replacement?"
		d.Options = []decision.Option{{Index: 0, Kind: "apply", Obj: m.id, Label: "Yes — skip that draw"},
			{Index: 1, Kind: "decline", Obj: m.id, Label: "No — draw the card"}}
		if in, ok := parkTapeAnswer(e, d); ok {
			e.handle(d, in)
			return
		}
		e.ask(d)
		return
	case replChoiceFaceUp:
		m := rc.cands[rc.selected]
		name := "this replacement effect"
		if o := e.G.Obj(m.id); o != nil && o.Face() != nil && o.Face().Name != "" {
			name = o.Face().Name
		}
		d.Prompt = "Apply " + name + "'s optional turn-face-up replacement?"
		d.Options = []decision.Option{{Index: 0, Kind: "apply", Obj: m.id, Label: "Yes — apply this replacement"},
			{Index: 1, Kind: "decline", Obj: m.id, Label: "No — do not apply this replacement"}}
		// A turn-up is a tape-run boundary (turnup_tape.go: the special
		// action's Submit, or the resolution whose effect turns it up), so
		// the election is answered in place and the accepted body's own asks
		// (Vesuvan Shapeshifter's copy choice) are served from the same run
		// instead of taking their no-run defaults after a legacy park.
		if in, ok := parkTapeAnswer(e, d); ok {
			e.handle(d, in)
			return
		}
		e.ask(d)
		return
	case replChoiceUntap:
		name := "this object"
		if o := e.G.Obj(rc.ev.Obj); o != nil && o.Face() != nil && o.Face().Name != "" {
			name = o.Face().Name
		}
		d.Prompt = "Several replacement effects would change how " + name + " untaps: choose which applies first."
	default:
		name := "this object"
		if o := e.G.Obj(rc.ev.Obj); o != nil && o.Face() != nil && o.Face().Name != "" {
			name = o.Face().Name
		}
		prompt := "Several replacement effects would change how " + name + " moves: choose which applies first."
		if rc.life {
			what := "life gain"
			if _, _, loss := lifeLoss(rc.ev); loss {
				what = "life loss"
			}
			prompt = "Several replacement effects would change this " + what + ": choose the one that applies first."
		}
		d.Prompt = prompt
	}
	for _, candidate := range indices {
		c := rc.cands[candidate]
		label := "Apply a replacement"
		if so := e.G.Obj(c.id); so != nil && so.Face() != nil && so.Face().Name != "" {
			label = "Apply " + so.Face().Name + "'s replacement"
		} else if dsc := c.repl.ParamStr(cards.PKDescription); dsc != "" {
			label = "Apply: " + dsc
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "replacement", Obj: c.id, Label: label})
	}
	if rc.ev.Kind == events.Damage && hasOptionalReplacement(rc.cands) {
		d.Options = append(d.Options, decision.Option{Index: len(rc.cands), Kind: "skip_replacement",
			Label: "Do not apply an optional replacement"})
		// A single-optional competition asked of its OptionalDecider$ is the
		// replacement's own "may", not an order among several: pose it as
		// one (Battletide Alchemist round-2 finding).
		if len(rc.cands) == 1 && strings.EqualFold(rc.cands[0].repl.ParamStr(cards.PKOptional), "True") {
			label := "Apply the replacement"
			if o := e.G.Obj(rc.cands[0].id); o != nil && o.Face() != nil && o.Face().Name != "" {
				label = "Apply " + o.Face().Name + "'s replacement"
			}
			d.Prompt = label + " to this damage?"
		}
	}
	// A replacement-order choice can arise in the middle of an effect's Emit.
	// Enter through Host.Ask so effects.Resolve sees Suspended and records the
	// remaining SA chain — but only when something is actually resolving (a
	// stack resolution in flight, or a replacement body whose own chain the
	// ask would interrupt): Host.Ask's record is that suspension's
	// continuation, remembered on the parked choice so the tail can tell it
	// from the pose's own bookkeeping. A pose from a cast window or turn
	// structure has nothing to suspend — Engine.Ask's record there would
	// stale-resume (resolveTop reads e.resume to decide whether its
	// resolution suspended and abandons a resolution that finished) — so the
	// pose takes the plain ask and creates no record at all. A recomputation
	// ask already owns a resume point; pose it directly without overwriting
	// the original continuation.
	if e.resolvingObj != 0 || e.applyingReplacement {
		// Under the resolution kernel the order is answered in place (W3
		// step 5, lasagna spec §7.2): the chosen replacement applies at the
		// point of the competing event, not after the rest of the chain.
		if in, ok := parkTapeAnswer(e, d); ok {
			e.handle(d, in)
			return
		}
	}
	e.ask(d)
}

// emitDeclinedDrawReplacement lets a declined optional bodyless replacement
// continue the proposed draw without matching the same effect again.
func emitDeclinedDrawReplacement(emit func(events.Event) events.Event, applying *bool, ev events.Event) {
	prior := *applying
	*applying = true
	emit(ev)
	*applying = prior
}

// handleReplacement applies an answered CR 616.1 order choice: the front
// parked competition's chosen replacement is applied for real -- the SAME
// applyReplacement a lone matching replacement would run -- and, if more
// competitions are parked, the next one's controller is asked. Because the
// chosen replacement is the one the player decided applies FIRST, and a
// "Replaced" replacement discards the modified event (so the remaining
// replacements then see a destination the original no longer matches), one
// application completes the relocation for the competing shape. The choice
// is in the log as the Intents entry plus the DecisionAsk/DecisionMade
// events every decision emits; the relocation is the MoveZone (or absence of
// it) the applied replacement emits, so a log-only replay reproduces both
// branches. An answer with no parked competition is only reachable from a
// hand-built decision and degrades to a Note, the same totality stance as
// handleCmdZone.
func (e *Engine) handleReplacement(d *decision.Decision, in decision.Intent) {
	if len(d.Options) > 0 && strings.HasPrefix(d.Options[0].Kind, "madness_") {
		e.handleMadnessReplacement(d, in)
		return
	}
	if len(e.replChoices) == 0 {
		e.emit(events.Event{Kind: events.Note, Player: in.Player,
			Text: "replacement decision answered with no event parked"})
		return
	}
	rc := e.replChoices[0]
	e.replChoices = e.replChoices[1:]
	savedAnswerInRes := e.answerInResolution
	e.answerInResolution = savedAnswerInRes || rc.inResolution
	defer func() { e.answerInResolution = savedAnswerInRes }()
	chosen := d.Chosen(in)
	if rc.kind == replChoiceFaceUp {
		if len(chosen) > 0 && chosen[0].Index == 0 {
			m := rc.cands[rc.selected]
			e.turnUpMove = &rc.ev
			e.runReplaceWith(e.replCtx(m, rc.ev), rc.ev.Obj, m.repl.With, nil)
			e.turnUpMove = nil
		}
		if e.pending != nil {
			// The body parked the transition's re-emit on its continuation.
			return
		}
		prior := e.applyingReplacement
		e.applyingReplacement = true
		e.emit(rc.ev)
		e.applyingReplacement = prior
		return
	}
	if len(chosen) == 0 || ((rc.kind == replChoiceDamage || rc.kind == replChoiceCounter) && (chosen[0].Index < 0 || chosen[0].Index > len(rc.cands) ||
		(chosen[0].Index == len(rc.cands) && !(rc.kind == replChoiceDamage && hasOptionalReplacement(rc.cands))))) {
		e.emit(events.Event{Kind: events.Note, Player: in.Player,
			Text: "replacement answer had no choice"})
		return
	}
	before := e.triggerBefore
	e.triggerBefore = rc.before
	if rc.kind == replChoiceDraw {
		if chosen[0].Index == 1 {
			emitDeclinedDrawReplacement(e.emit, &e.applyingReplacement, rc.ev)
		}
		e.triggerBefore = before
		e.askNextReplacementChoice()
		return
	}
	if rc.kind == replChoiceDamage || rc.kind == replChoiceCounter {
		completed := true
		switch rc.kind {
		case replChoiceCounter:
			e.applyChosenCounterReplacement(rc, chosen[0].Index)
		case replChoiceDamage:
			completed = e.handleDamageReplacementChoice(rc, chosen[0].Index)
		}
		e.triggerBefore = before
		if completed && len(e.replChoices) > 0 {
			// The same parked resolution produced more than one replacement
			// choice: a multi-recipient DealDamage/DamageAll parks one per
			// recipient event before its enclosing chain suspends. The original
			// resume point must stay parked until the LAST of them is answered --
			// resuming after the first would run the remaining SA chain (and move
			// the spell off the stack) while a later recipient's damage is still
			// awaiting its CR 616.1 order choice, and the chained riders would
			// fire before the effect's own damage settled. If the application
			// itself posed a nested ask (e.resume no longer rp), chain this
			// frame's continuation behind the new one so nothing is dropped.
			for len(e.replChoices) > 0 {
				next := e.replChoices[0]
				// CR 616.1e: the affected player is recomputed from the parked
				// event at ask time, so a competition whose recipient changed
				// while parked asks the NEW affected player -- except that a
				// single-optional competition's "may" still belongs to its
				// OptionalDecider$ (replacementAskPlayer).
				if p, ok := e.damageAffectedPlayer(next.ev); ok && !e.G.Players[p].Lost {
					e.replChoices[0].player = e.replacementAskPlayer(next.cands, p)
					if e.pending == nil {
						e.askReplacementChoice(e.replChoices[0].player)
					}
					return
				}
				// CR 800.4a: an affected player who has left the game or lost
				// makes no choices. Its candidates apply in deterministic scan
				// order and the queue drains on.
				e.replChoices = e.replChoices[1:]
				switch {
				case next.kind == replChoiceCounter:
					e.applyChosenCounterReplacement(next, 0)
				case next.kind == replChoiceDamage:
					completed = e.handleDamageReplacementChoice(next, 0)
				default:
					e.applyReplacement(next.ev, next.cands[0])
				}
				if !completed {
					return
				}
			}
		}
		if completed && rc.combat && e.combatRound.assignments != nil {
			// A combat damage pass was parked at this assignment. Finish the
			// remaining precomputed assignments before running SBAs or the
			// regular pass; a newly parked replacement simply returns again.
			e.damageStep(false)
			if e.pending == nil && e.combatRound.assignments == nil && e.combatRound.active {
				e.completeCombatPass(e.combatRound.pass)
			}
		}
		return
	}
	if rc.life {
		// Apply the chosen replacement, then re-evaluate what still applies
		// to the modified event (CR 616.1e); a further non-commuting
		// competition asks again at the front of the queue.
		if chosen[0].Index >= len(rc.cands) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "life replacement answer out of range"})
			return
		}
		damaging, combat, override := e.damaging, e.combatDamaging, e.dmgSrcOverride
		e.damaging, e.combatDamaging, e.dmgSrcOverride = rc.damaging, rc.combatDamaging, rc.dmgSrcOverride
		priorExchange := e.lifeExchange
		e.lifeExchange = rc.exchange
		m := rc.cands[chosen[0].Index]
		if next, consumed := e.applyLifeReplacement(rc.ev, m); !consumed {
			applied := append(append([]replMatch(nil), rc.appliedRepls...), m)
			e.continueLifeReplacements(next, applied)
		} else {
			e.consumeExchangeLifeSide(rc.ev)
		}
		if rc.exchange != nil {
			if e.pending == nil && len(e.replChoices) == 0 {
				e.finishLifeExchange(rc.exchange)
			} else {
				// The chosen replacement's body itself suspended (a Dredge ask):
				// park the transaction for the drain that answers it.
				e.pendingLifeExchange = rc.exchange
			}
		}
		e.lifeExchange = priorExchange
		e.damaging, e.combatDamaging, e.dmgSrcOverride = damaging, combat, override
		e.triggerBefore = before
		e.askNextReplacementChoice()
		return
	}
	switch rc.kind {
	case replChoiceMana:
		if chosen[0].Index < 0 || chosen[0].Index >= len(rc.applicable) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "mana replacement answer out of range"})
			return
		}
		i := rc.applicable[chosen[0].Index]
		if manaReplacementNeedsColor(rc.cands[i]) {
			rc.kind = replChoiceManaColor
			rc.selected = i
			rc.applicable = nil
			e.replChoices = append([]replChoice{rc}, e.replChoices...)
			break
		}
		rc.ev = e.applyOneManaReplacementWithProducer(rc.ev, rc.cands[i], "", rc.manaProducer)
		rc.applied[i] = true
		e.continueManaReplacements(rc.ev, rc.cands, rc.applied, true, rc.manaTapped, rc.manaProducer)
	case replChoiceManaColor:
		// The chosen colour is structured data (Option.ManaSymbol); the
		// label is presentation-only.
		color := chosen[0].ManaSymbol
		if len(color) != 1 || !strings.Contains("WUBRG", color) ||
			rc.selected < 0 || rc.selected >= len(rc.cands) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "mana colour replacement answer out of range"})
			return
		}
		rc.ev = e.applyOneManaReplacementWithProducer(rc.ev, rc.cands[rc.selected], color, rc.manaProducer)
		rc.applied[rc.selected] = true
		e.continueManaReplacements(rc.ev, rc.cands, rc.applied, true, rc.manaTapped, rc.manaProducer)
	case replChoicePhaseOrder:
		if chosen[0].Index < 0 || chosen[0].Index >= len(rc.applicable) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "phase replacement answer out of range"})
			return
		}
		i := rc.applicable[chosen[0].Index]
		if rc.cands[i].repl.ParamStr(cards.PKOptional) == "True" {
			rc.kind = replChoicePhaseOptional
			rc.selected = i
			rc.applicable = nil
			e.replChoices = append([]replChoice{rc}, e.replChoices...)
		} else {
			e.finishParkedPhase(rc, i)
		}
	case replChoicePhaseOptional:
		if rc.selected < 0 || rc.selected >= len(rc.cands) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "optional phase replacement answer out of range"})
			return
		}
		if chosen[0].Kind == "apply" {
			e.finishParkedPhase(rc, rc.selected)
		} else {
			rc.applied[rc.selected] = true
			e.resumeParkedPhase(rc)
		}
	case replChoiceAddCounter:
		if chosen[0].Index < 0 || chosen[0].Index >= len(rc.cands) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "counter replacement-order answer out of range"})
			return
		}
		damaging, combat, override := e.damaging, e.combatDamaging, e.dmgSrcOverride
		e.damaging, e.combatDamaging, e.dmgSrcOverride = rc.damaging, rc.combatDamaging, rc.dmgSrcOverride
		m := rc.cands[chosen[0].Index]
		if n, ok := e.applyAddCounterBody(rc.ev, m, rc.ev.Amount); ok {
			rc.ev.Amount = n
		}
		rc.appliedRepls = append(rc.appliedRepls, m)
		e.continueAddCounterReplacements(rc)
		e.damaging, e.combatDamaging, e.dmgSrcOverride = damaging, combat, override
	case replChoiceEntryOrder:
		if chosen[0].Index < 0 || chosen[0].Index >= len(rc.cands) || rc.stage == nil {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "entry counter replacement-order answer out of range"})
			return
		}
		e.resumeEntryCounterOrder(rc, chosen[0].Index)
	case replChoiceToken:
		if chosen[0].Index < 0 || chosen[0].Index >= len(rc.applicable) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "token replacement-order answer out of range"})
			return
		}
		m := rc.cands[rc.applicable[chosen[0].Index]]
		rest := dropReplMatch(rc.cands, m)
		var plan []tokenPlanMint
		var parked bool
		if body := m.repl.With; body != nil &&
			(strings.EqualFold(strings.TrimSpace(body.ParamStr(cards.PKTokenScript)), "Chosen") ||
				strings.TrimSpace(body.ParamStr(cards.PKValidChoices)) != "") {
			// A chosen-copy match: the election the scan-order drive poses for
			// it (driveTokenReplacements' chosenShape arm), with the remaining
			// matches and the plan as they stand. idx -1 makes the pose's resume
			// cursor re-drive rest from 0 (m itself is already gone from rest).
			plan, parked = e.poseChosenTokenReplacement(rc.ev, rest, rc.tokenPlan, -1, m)
		} else {
			plan = e.applyTokenReplacementToPlan(rc.ev, rc.tokenPlan, m)
		}
		if !parked {
			plan, parked = e.driveTokenReplacements(rc.ev, rest, plan, 0)
		}
		if !parked {
			e.emitTokenPlan(rc.ev, plan)
		}
	case replChoiceUpdated:
		if chosen[0].Index < 0 || chosen[0].Index >= len(rc.cands) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "entry replacement-order answer out of range"})
			return
		}
		e.resumeUpdatedComposition(rc, chosen[0].Index)
	case replChoiceLoseMana:
		handleLoseManaChoice(rc, chosen[0].Index, in.Player, before, e.G, e, e.emit, func(v bool) { e.applyingReplacement = v }, func(v *triggerSnapshot) { e.triggerBefore = v })
	case replChoiceUntap:
		if chosen[0].Index < 0 || chosen[0].Index >= len(rc.cands) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "untap replacement-order answer out of range"})
			return
		}
		e.applySimpleReplacement(rc.ev, rc.cands[chosen[0].Index])
		if rc.untap != nil && e.pending == nil {
			e.finishUntapStep(rc.untap.next)
		}
	default:
		if chosen[0].Index < 0 || chosen[0].Index >= len(rc.cands) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "replacement-order answer out of range"})
			return
		}
		e.applyReplacement(rc.ev, rc.cands[chosen[0].Index])
	}
	e.triggerBefore = before
	// A mana replacement or colour decision can interrupt CR 601.2g's mana
	// window. Resume the parked cast only after the final rewrite is logged
	// and no next replacement decision is pending.
	if (rc.kind == replChoiceMana || rc.kind == replChoiceManaColor) && e.pending == nil && len(e.replChoices) == 0 && e.cast != nil {
		e.continueCast()
	}
	e.askNextReplacementChoice()
}

// askNextReplacementChoice hands over to either an ordinary replacement
// competition or a simultaneous Madness choice after the current answer.
func (e *Engine) askNextReplacementChoice() {
	if e.pending != nil {
		return
	}
	if len(e.replChoices) > 0 {
		if p, ok := e.replacementChoicePlayer(e.replChoices[0]); ok {
			e.askReplacementChoice(p)
		}
		return
	}
	if len(e.madnessChoices) > 0 {
		if o := e.G.Obj(e.madnessChoices[0].Obj); o != nil && int(o.Owner) < len(e.G.Players) {
			e.askMadnessReplacement(o.Owner)
		}
	}
	// No replacement-order decision remains outstanding: an exchange
	// transaction parked by a suspension (a consumed GainLife→Draw body whose
	// own draw parked a Dredge ask) has no other drain, so finish it here. A
	// competition asked just above re-set e.pending, which makes this inert
	// until that answer lands and calls this tail again.
	e.settlePendingLifeExchange()
}
