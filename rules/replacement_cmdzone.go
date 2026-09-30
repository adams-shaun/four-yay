package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	// kw:etbCounter and kw:ETBReplacement are implemented wholly by the
	// machinery above: both are R:Event$ Moved replacements (expanded from a
	// K: line by cards/keywords.go) matched and applied here. Reading a card's
	// own tags is what a replacement registration means -- nothing elsewhere
	// in the tree registers them.
	//
	// kw:Devour and kw:Ravenous (CR 702.148) are the same idea for a K: line
	// that expands to an ETB trigger instead of a replacement (kw:Devour's
	// optional sacrifice + counter put, kw:Ravenous's X +1/+1-counter put
	// plus the X>=5 conditional draw, both in cards/kw_*.go). The marker
	// exists only so the coverage ratchet sees the head as supported; the
	// machinery it needs (trig:ChangesZone, api:PutCounter, api:Draw, the
	// SVar-condition gate) is all registered under its own primitives.
	//
	// The four turn/mana replacement events register the same way: repl:Untap
	// (the Basalt Monolith class), repl:BeginPhase (the Necropotence class),
	// repl:Transform (the Sephiroth class) and repl:ProduceMana (the Virtue
	// of Strength class, whose ReplaceWith$ body DB$ ReplaceMana is a
	// registered API). Each is matched by replacementMatches's per-event
	// branch above and applied by applyReplacements's dispatch.
	//
	// The life classes register too: repl:GainLife (the Prevent$ GainLife
	// shape plus the CantGainLife static's replacement arm) and
	// repl:LifeReduced (the ReplaceCount$Amount/Twice shape), both applied by
	// applyLifeReplacements. repl:DamageDone and repl:Counter are this
	// ticket's own additions, matched by replacementMatches's DamageDone case
	// and CounterAllowed respectively.
	// kw:Bloodthirst (CR 702.54) is implemented by this file's
	// bloodthirstEntryMatch: the keyword line (printed K:Bloodthirst:<N> or a
	// layer-6 AddKeyword$ grant) is read at MoveZone→Battlefield collection
	// time from the entering object's DERIVED keyword list, so one read
	// covers both shapes -- a printed carrier and Twins of Discord's
	// `Affected$ Creature.Other+YouCtrl+Colorless | AddKeyword$ Bloodthirst:2`
	// grant, which cards-side expansion could never see. No cards-side
	// expansion exists: bloodthirst is a static ability whose whole meaning
	// is an entry-time conditional counter put, which is exactly what the
	// synthetic Repl below expresses.
	effects.RegisterNonAPI("kw:etbCounter", "kw:ETBReplacement", "kw:Devour", "kw:Ravenous", "kw:Bloodthirst", "kw:Sunburst",
		"repl:Untap", "repl:BeginPhase", "repl:Transform", "repl:ProduceMana",
		"repl:GainLife", "repl:LifeReduced", "repl:DamageDone", "repl:Counter",
		"repl:CreateToken", "repl:RollPlanarDice", "repl:Explore", "repl:Attached", "repl:Scry", "api:ReplaceToken",
		"repl:AddCounter", "api:ReplaceCounter",
		// repl:GameLoss / repl:GameWin (task fdn-repl-cant-lose) are the
		// "you can't lose the game" / "your opponents can't win the game"
		// CantHappen class (Herald of Eternal Dawn, the Platinum Angel family,
		// Abyssal Persecutor, Lich's Mastery). Matching and application live in
		// rules/cantlose.go; this registration is what makes the coverage
		// ratchet see the heads as supported.
		"repl:GameLoss", "repl:GameWin",
		// repl:TurnFaceUp (task cli-20260924T031747Z-6d0658fc) is the "as this
		// is turned face up" class (Hooded Hydra's five +1/+1 counters, Karlov
		// Watchdog's CantHappen, Gift of Doom's attach), matched by
		// replacementMatches's TurnFaceUp case and applied by
		// applyTurnFaceUpReplacements.
		"repl:TurnFaceUp",
		// api:ReplaceDamage is handled inline by applyReplaceDamageBody (this
		// file) via the ReplaceDamage intercept in applyReplacements, never
		// through effects.Resolve/runReplaceWith -- this registration is the
		// census token only; a stub effects.Register handler would be dead code.
		"api:ReplaceDamage")
}

// cmdZoneMove is one parked commander zone change (CR 903.9, Task m32): the
// MoveZone event a commander is about to undergo, deferred until its owner
// decides whether to put it into the command zone instead. The answer
// re-emits the park as a real MoveZone -- to ZCommand on an accept, to the
// parked destination verbatim on a decline -- so the event log always
// carries the zone change that actually happened and a log-only replay
// reproduces it. Plain value data (an events.Event plus the moving object's
// id), so Clone copies the queue with one slice copy.
type cmdZoneMove struct {
	ev     events.Event
	obj    state.ObjID
	before *triggerSnapshot // immutable SBA look-back, safe to share in Clone
}

// commanderZoneReplacementApplies is CR 903.9's match predicate: a
// commander, in a Commander-format game, about to be put into its owner's
// graveyard, hand or library -- from ANYWHERE -- or about to be exiled from
// anywhere. The five points of the rule each live in exactly one place here
// (mutation guards, Task m32 brief item 7):
//
//   - FormatCommander gate: no format check, no mechanic. A Constructed
//     game never runs any of this.
//   - The four destinations: graveyard, hand, library, exile -- a
//     commander moving to the battlefield or the stack is not replaced.
//   - "From anywhere": ev.From is never consulted. The battlefield, the
//     stack, the graveyard, a hand a library or exile are all sources.
//   - Ownership, not control: the lookup is the owner's Commanders list,
//     not the controller's -- a commander stolen by an opponent goes to its
//     owner's command zone and its owner is asked.
func (e *Engine) commanderZoneReplacementApplies(ev events.Event) bool {
	if e.format != FormatCommander {
		return false
	}
	switch ev.To {
	case state.ZGraveyard, state.ZHand, state.ZLibrary, state.ZExile:
	default:
		return false
	}
	o := e.G.Obj(ev.Obj)
	if o == nil || int(o.Owner) >= len(e.G.Players) {
		return false
	}
	for _, c := range e.G.Players[o.Owner].Commanders {
		if c == ev.Obj {
			return true
		}
	}
	return false
}

// parkCommanderZoneMove applies the CR 903.9 replacement to one matching
// zone change: the event is deferred (never logged, never applied -- the
// commander stays where it is) and its owner is asked whether to put the
// commander into the command zone instead. handleCmdZone emits the parked
// move for real when the answer lands.
//
// Dedup by object: while a commander's decision is outstanding the only
// engine work that can run is (a) the resolution-chain tail that already
// emitted the first park and (b) Submit's repeating state-based-action pass.
// A second park for the same commander is therefore either the SAME zone
// change being re-offered (the SBA pass re-finding a commander it already
// had tried -- the no-progress shape a replacement must not loop on, the
// stalledCastLimit lesson: the already-pending decision covers it, so the
// duplicate is dropped) or a second effect in the same chain that in the
// real rules would resolve AFTER the commander has already moved (its own
// CR 608.2b Origin$-driven recheck would then skip it, so dropping is the
// more-correct outcome, not merely safe). The move itself always happens
// exactly once, through the front decision.
//
// An owner who has left the game cannot exercise a "may" choice (CR 800.4a:
// a departed player makes no choices), so the unexercised choice is a
// decline: the original move happens unchanged, emitted here under
// applyingReplacement so the commander check that just matched cannot
// re-park it (CR 616.1, a replacement applies only once).
func (e *Engine) parkCommanderZoneMove(ev events.Event) {
	o := e.G.Obj(ev.Obj)
	if o == nil {
		return
	}
	owner := o.Owner
	if e.G.Players[owner].Lost {
		saved := e.applyingReplacement
		e.applyingReplacement = true
		e.emit(ev)
		e.applyingReplacement = saved
		return
	}
	for _, pm := range e.cmdZone {
		if pm.obj == ev.Obj {
			return
		}
	}
	e.cmdZone = append(e.cmdZone, cmdZoneMove{ev: ev, obj: ev.Obj, before: e.retainTriggerBefore()})
	if e.pending == nil {
		e.askCommandZone(owner)
	}
}

// askCommandZone poses the CR 903.9 choice for the FRONT parked move to its
// owner, following askTriggerOptional's shape: Min == Max == 1 over two
// options, first the "change the outcome" one, then the "let it happen"
// one. Only the front of the queue is ever asked -- see handleCmdZone's
// resumption for how the queue hands from one decision to the next.
func (e *Engine) askCommandZone(owner state.PlayerID) {
	pm := e.cmdZone[0]
	name := "this commander"
	if o := e.G.Obj(pm.obj); o != nil && o.Face() != nil && o.Face().Name != "" {
		name = o.Face().Name
	}
	dest := pm.ev.To.String()
	into := "Put " + name + " into the command zone"
	d := &decision.Decision{Player: owner, Kind: decision.KCommanderZone, Min: 1, Max: 1,
		Prompt: name + " would go to the " + dest + ": put it into the command zone instead?",
		Source: pm.obj,
		Options: []decision.Option{
			{Index: 0, Kind: "command_zone", Label: into, Obj: pm.obj, Player: owner},
			{Index: 1, Kind: "leave", Label: "Let it go to the " + dest, Obj: pm.obj, Player: owner},
		}}
	e.ask(d)
}

// handleCmdZone applies an answered CR 903.9 decision: the front parked move
// is emitted for real -- to the command zone if the owner chose
// "command_zone", verbatim (the destination it was heading for) if they
// chose "leave" -- and, if more moves are parked, the next one's owner is
// asked. The de-park emit runs under applyingReplacement: the CR 903.9
// replacement has already applied to this zone change, and CR 616.1 lets a
// replacement effect apply only once, so the final move is never re-parked
// and never re-asked -- a decline therefore cannot spin the engine by
// re-offering the same choice, and the SBA pass that re-finds the commander
// after this answer degrades the same way it would for any other completed
// replacement (the dedup in parkCommanderZoneMove handled its in-flight
// copy).
//
// The owner's choice is in the log as the Intents entry plus the
// DecisionAsk/DecisionMade events every decision emits; the outcome is the
// MoveZone event below, so a log-only replay reproduces both branches from
// the log alone. An answer with no parked move (only reachable from a
// hand-built decision -- every real ask parks one) degrades to a Note, the
// same totality stance as handleModes.
func (e *Engine) handleCmdZone(d *decision.Decision, in decision.Intent) {
	if len(e.cmdZone) == 0 {
		e.emit(events.Event{Kind: events.Note, Player: in.Player,
			Text: "commander-zone decision answered with no move parked"})
		return
	}
	pm := e.cmdZone[0]
	e.cmdZone = e.cmdZone[1:]
	to := pm.ev.To
	if opts := d.Chosen(in); len(opts) == 1 && opts[0].Kind == "command_zone" {
		to = state.ZCommand
	}
	saved, before := e.applyingReplacement, e.triggerBefore
	e.applyingReplacement, e.triggerBefore = true, pm.before
	e.emit(events.Event{Kind: events.MoveZone, Obj: pm.ev.Obj, From: pm.ev.From,
		To: to, Player: pm.ev.Player, Text: pm.ev.Text})
	e.applyingReplacement, e.triggerBefore = saved, before
	if len(e.cmdZone) > 0 && e.pending == nil {
		// More commanders were parked in the same burst (a board wipe, a
		// multiple-SBA pass): hand the front of the queue to its owner.
		if o := e.G.Obj(e.cmdZone[0].obj); o != nil && int(o.Owner) < len(e.G.Players) {
			e.askCommandZone(o.Owner)
		}
	}
}
