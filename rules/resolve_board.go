package rules

// resolve_board.go is the rules side of the W3 resolution kernel (lasagna
// spec §7; rules/resolve): the Engine cluster the kernel's state lives in,
// and resolveBoard, which implements resolve.Board over the Engine itself so
// the kernel drives the engine without holding it.

import (
	"sync"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/resolve"
	"github.com/adams-shaun/gorge/state"
)

// engineResolveKernel is the resolution kernel's Engine cluster.
type engineResolveKernel struct {
	// tape is the kernel's per-engine state (resolve.Kernel carries its own
	// per-field clone tags; cloneWith copies tape.ForClone()).
	tape resolve.Kernel `clone:"deep"`
	// tapeSpare recycles the storage of a checkpoint dropped unused (its
	// resolution finished, or went legacy, without posing a tape ask -- no
	// clone can have shared it), so the next checkpoint allocates nothing.
	// Release carries it in Spare.tapeCkpt, so a search's per-simulation
	// engines recycle it too.
	tapeSpare Spare `clone:"reset"`
	// tapeHeldHook and tapeHeldStats are the harness observers a restore
	// parked while the recorded prefix re-executes (spec §7.5): the prefix
	// was already observed once. resolveBoard.Observe re-attaches them.
	tapeHeldHook  func(p state.PlayerID, source state.ObjID, sa *cards.SA) `clone:"hook"`
	tapeHeldStats *PaymentPlanStats                                        `clone:"hook"`
	// tapeGranted is set when a converted engine-posed ask (the CR 603.5
	// optional-trigger yes/no) completed its resolution in line through the
	// legacy answer's continuation, whose tail already logged the CR 117.3b
	// grant: handlePriority consumes it instead of logging a second one.
	// Transient within one Submit.
	tapeGranted bool `clone:"reset"`
	// tapeWindowAsking is set while a resolution-time payment window asks
	// through the kernel (windowAsk): its own holder is open by design, so
	// Busy looks past it. Transient within one ask.
	tapeWindowAsking bool `clone:"reset"`
	// hostAsking is nonzero inside Engine.Ask (effects.Host.Ask): the one
	// ask whose continuation is a resume point, never a boundary decision.
	hostAsking int `clone:"reset"`
	// tapeOffStackAsking is set while an off-stack mana ability's colour
	// choice asks through the kernel (askOffStackMana): its own frame is open
	// by design, so Busy looks past it.
	tapeOffStackAsking bool `clone:"reset"`
	// tapeEpoch counts the kernel's restores of this engine. A restore
	// rewinds the engine's state in place under the same Game and Log
	// pointers and the re-run then logs past the recorded prefix again, so a
	// reader that caches derived facts by log position (botpolicy's
	// incremental board, through BoardReadKey) cannot see the rewind; the
	// epoch is part of that key.
	tapeEpoch uint32 `clone:"reset"`
}

// resolveBoard is the Engine itself under the kernel's method set: asResolve
// is a pointer conversion, so handing the kernel its Board allocates nothing.
type resolveBoard Engine

var _ resolve.Board = (*resolveBoard)(nil)

func asResolve(e *Engine) *resolveBoard { return (*resolveBoard)(e) }

// TapeAnswer implements effects' ask seam (effects.AskTape) for the
// converted ask sites: inside a tape run the decision is posed and either
// answered from the tape or the run unwinds; with a synchronous answerer it
// is posed and answered inline (an UnlessCost$ election included: it settles
// in line, unlessAnswerSettle). ok false: no run serves it, and the asking
// site takes its deterministic default.
func (e *Engine) TapeAnswer(d *decision.Decision) (decision.Intent, bool) {
	return e.tape.Answer(asResolve(e), d)
}

// SetTapeAnswerer installs (nil removes) e's synchronous answerer: an engine
// whose every seat is a policy answers a converted mid-resolution ask inline,
// so the kernel takes no checkpoint and re-executes nothing. A clone never
// inherits it. Inline answers bypass per-decision observers, so a training
// collector must not install one.
func SetTapeAnswerer(e *Engine, f resolve.Answerer) { e.tape.SetAnswerer(f) }

// TapePosed reports whether e has a tape resolution posed (S0 held).
func TapePosed(e *Engine) bool { return e.tape.Posed() }

func (b *resolveBoard) Log() *events.Log { return b.L }

func (b *resolveBoard) Pending() *decision.Decision { return b.pending }

func (b *resolveBoard) Busy() bool {
	e := (*Engine)(b)
	// An off-stack mana resolution (CR 605.3: a mana ability never uses the
	// stack) routes its asks through its own activation continuation
	// (askOffStackMana), not the resolution's: a converted site inside one
	// asks through the legacy path.
	if e.tapeWindowAsking && (e.offStackMana == nil || e.tapeOffStackAsking) &&
		(e.UnlessPayment == nil || e.UnlessPayment.Tape) {
		// windowAsk's own window: the holder it asks for is open by design.
		return false
	}
	if u := e.UnlessPayment; u != nil && u.Tape && e.offStackMana == nil &&
		e.cumulative == nil && e.triggerCost == nil && e.echo == nil {
		// A tape-driven unless payment is the kernel's own, not a legacy
		// suspension: an ask its component walk reaches (a discard's
		// madness election) is served in place.
		return false
	}
	return e.Suspended() || (e.offStackMana != nil && !offStackTapeServed(e))
}

func (b *resolveBoard) StartsResolution(d *decision.Decision, in decision.Intent) bool {
	e := (*Engine)(b)
	if d.Kind == decision.KChoose && e.choosing == chooseOpening && !e.Suspended() {
		// A pregame "begin the game with" effect: its entry is asked inside
		// this one Submit.
		return len(in.Choices) > 0 && firstChosen(d, in).Kind == "opening_yes"
	}
	if in.Payment != nil || in.Announce != nil || e.Suspended() {
		return false // a legacy suspension is in flight; never nest a tape run in it
	}
	if d.Kind == decision.KChoose {
		// A turn-up's cost answer whose TurnFaceUp replacement may ask
		// (turnup_tape.go).
		return e.choosing == chooseTurnUp && e.turnUp != nil && tapeTurnUpReplMayAsk(e)
	}
	if d.Kind != decision.KPriority {
		return false
	}
	switch first := firstChosen(d, in); first.Kind {
	case optPlayLand:
		return true
	case optActivate:
		// A mana ability whose rider may ask (offstack_mana_rider_tape.go).
		return tapeManaRiderMayAsk(e, in.Player, first.Obj)
	case optPass:
		if e.G.Passes+1 < int32(e.G.AliveCount()) {
			return false
		}
		return len(e.G.Stack) > 0 || tapeStepMayAsk(e)
	case optTurnFaceUp:
		return len(in.Choices) > 0 && tapeTurnUpReplMayAsk(e)
	}
	return false
}

// modalLandMode is the priority option's back-face selection (legal_walk_hand.go).
const modalLandMode = "modal_land"

func (b *resolveBoard) MayAsk(d *decision.Decision, in decision.Intent) bool {
	e := (*Engine)(b)
	if d.Kind == decision.KChoose && e.choosing == chooseTurnUp {
		return true // a turn-up: StartsResolution already proved it may ask
	}
	first := firstChosen(d, in)
	switch first.Kind {
	case optActivate, optTurnFaceUp:
		return true // StartsResolution already proved the rider may ask
	case optPlayLand:
		// A modal land flips to its selected face before continueCast checks
		// ManaConvert. The current face may fail a ValidCard$ filter that the
		// selected face passes; never exempt that pre-flip payment window.
		if first.Mode == modalLandMode {
			return true
		}
		// in.Player, not the option's Player field: a play_land option
		// leaves Player at its zero value, so a seat-1 land play judged a
		// You-scoped ManaConvert static (North Star) as seat 0's.
		return tapeLandMayAsk(e, in.Player, first.Obj)
	}
	if len(e.G.Stack) == 0 {
		return tapeLandMayAsk(e, in.Player, first.Obj)
	}
	return tapeMayAsk(e)
}

func (b *resolveBoard) Checkpoint() resolve.Snapshot {
	e := (*Engine)(b)
	if e.tapeSpare.objs == nil {
		// No checkpoint of this engine's own to recycle (its first
		// resolution, or its last one is still shared): take a dropped one
		// from any engine, so a run of short games recycles too.
		if h, _ := tapeSparePool.Get().(*Spare); h != nil {
			e.tapeSpare, *h = *h, Spare{}
			tapeHolders.Put(h)
		}
	}
	return e.CloneInto(&e.tapeSpare)
}

// Drop recycles S0 into the process-wide pool rather than into the engine,
// so storage an engine dropped outlives the engine (a game's last checkpoint
// seeds the next game's first).
func (b *resolveBoard) Drop(s resolve.Snapshot) { tapePut(s.(*Engine).Release()) }

// tapeSparePool holds dropped checkpoints' storage (*Spare) any engine's next
// checkpoint may adopt; tapeHolders recycles the empty *Spare holders. Reuse
// is invisible to the game (Spare's contract), so which engine's storage a
// checkpoint adopts never reaches an event.
var tapeSparePool, tapeHolders sync.Pool

func tapePut(sp Spare) {
	h, _ := tapeHolders.Get().(*Spare)
	if h == nil {
		h = new(Spare)
	}
	*h = sp
	tapeSparePool.Put(h)
}

// Restore makes the engine a fresh copy of the checkpoint in place. The
// copy is written into the live engine's own storage (the live state it
// replaces is dead: it is released, minus its log arrays, and S0 is cloned
// into what it freed), and the live log keeps its arrays: S0's history is
// their prefix, so the log rewinds to S0's chain state with a verify window
// over the recorded events (events.Log.RewindTo). The Game and Log pointers
// hosts and tests hold, the kernel's own state and the per-engine pools keep
// their identity.
//
// A hypothetical world's checkpoint carries an RNG splice (cp.World): the
// re-run draws S0's own generator for exactly the draws the prefix had made
// when the world was forked, then switches to the world's reseeded generator,
// so nothing after the fork reads S0's true future (spec §7.3).
func (b *resolveBoard) Restore(cp *resolve.Checkpoint, evEnd int) {
	e := (*Engine)(b)
	s0 := cp.S0.(*Engine)
	g, l := e.G, e.L
	arenaOn := e.decArena != nil && e.decArena.owner == e && e.decArena.on
	kernel, epoch := e.tape, e.tapeEpoch+1
	hook, stats := e.ManaAbilityHook, e.PaymentStats
	if hook == nil && stats == nil {
		hook, stats = e.tapeHeldHook, e.tapeHeldStats // already parked
	}
	evs, ints := l.Events, l.Intents
	l.Events, l.Intents = nil, nil
	// The decision arena stays out of the release: Release clears its
	// chunks for reuse, and every decision it posed must keep its Options
	// for the engine's whole life (a host holds them past the answer).
	arena := e.decArena
	if arena != nil && arena.owner != e {
		arena = nil
	}
	e.decArena = nil
	sp := e.Release()
	l.Events, l.Intents = evs, ints
	sc := s0.CloneInto(&sp)
	if w, ok := cp.World.(*rngSplice); ok {
		sc.rng.splice = w
		// Stay a hypothetical engine across the prefix (SubmitHypothetical
		// requires a chance state); the splice replaces it with the world's
		// own at the fork point, discarding the prefix's records.
		sc.rng.chance = w.next.chance.clone()
	}
	l.RewindTo(s0.L, evEnd)
	*g = *sc.G
	sc.L = l
	*e = *sc
	e.G, e.L = g, l
	e.tape, e.tapeEpoch = kernel, epoch
	if arena != nil {
		e.decArena = arena
	}
	e.tapeHeldHook, e.tapeHeldStats = hook, stats
	tapeRebindOwner(e, sc, arenaOn)
}

// tapeRebindOwner moves the per-engine pools a restore adopted (cloneWith
// bound them to the scratch copy sc) onto e, which took sc's contents by
// value, and keeps a host-enabled decision arena on: without this every
// re-execution dropped the pools and the arena and allocated afresh (S3b
// restore fix 3). Owner-keyed caches (static scan reuse, walk block reuse,
// the activation index) only miss and rebuild.
func tapeRebindOwner(e *Engine, sc *Engine, arenaOn bool) {
	if e.walkClsOwner == sc {
		e.walkClsOwner = e
	}
	if e.actIndex.owner == sc {
		e.actIndex.owner = e
	}
	if e.lookBackOwner == sc {
		e.lookBackOwner = e
	}
	if e.previewOwner == sc {
		e.previewOwner = e
	}
	if e.decArena != nil && e.decArena.owner == sc {
		e.decArena.owner = e
	}
	if e.hypSpares != nil && e.hypSpares.owner == sc {
		e.hypSpares.owner = e
	}
	if e.snapPool != nil && e.snapPool.owner == sc {
		e.snapPool.owner = e
	}
	if arenaOn {
		e.SetDecisionArena(true)
	}
}

func (b *resolveBoard) Observe() {
	if b.tapeHeldHook != nil || b.tapeHeldStats != nil {
		b.ManaAbilityHook, b.PaymentStats = b.tapeHeldHook, b.tapeHeldStats
		b.tapeHeldHook, b.tapeHeldStats = nil, nil
	}
}

func (b *resolveBoard) Validate(d *decision.Decision, in decision.Intent) error {
	return submitValidate((*Engine)(b), d, in)
}

func (b *resolveBoard) Commit(d *decision.Decision, in decision.Intent) {
	submitCommit((*Engine)(b), d, in)
}

func (b *resolveBoard) Submit(in decision.Intent) error { return (*Engine)(b).Submit(in) }

func (b *resolveBoard) Pose(d *decision.Decision) { (*Engine)(b).ask(d) }

func (b *resolveBoard) Record(d *decision.Decision, in decision.Intent) decision.Intent {
	e := (*Engine)(b)
	logged := cloneIntentForLog(in)
	e.potentialAskSerial++
	e.tape.LogIntent(e.L, logged)
	e.emit(events.Event{Kind: events.DecisionMade, Player: logged.Player, Text: decisionMadeText(d.Kind, logged.Choices)})
	e.pending = nil
	// An answered converted ask is an ask this engine took (effects' askSeam
	// AskCount): a primitive that asks whether its body asked -- the legacy
	// path's suspension -- sees the served one too.
	e.askCount++
	// The asking code acts for the seat the decision is asked OF (a CR 722
	// redirect), exactly as submitCommit re-seats a handler's intent.
	ad, _ := actingView(d, logged)
	acted := logged
	acted.Player = ad.Player
	tapeAnswerRecord(e, ad, acted)
	return acted
}

func (b *resolveBoard) Emit(ev events.Event) { events.Emit(b.G, b.L, ev) }

// tapeMissObserver receives one class string per predicate miss (a legacy
// ask during a resolution the ask-free predicate exempted): the cardfuzz
// miss census. Process-wide; nil (every production engine) costs one load.
var tapeMissObserver atomic.Pointer[func(class string)]

// SetTapeMissObserver installs (nil removes) the process-wide predicate-miss
// observer and returns the previous one. f must be safe for concurrent use.
func SetTapeMissObserver(f func(class string)) func(class string) {
	var prev *func(string)
	if f == nil {
		prev = tapeMissObserver.Swap(nil)
	} else {
		prev = tapeMissObserver.Swap(&f)
	}
	if prev == nil {
		return nil
	}
	return *prev
}

// tapeLegacyObserver receives one class string per legacy ask that ends a
// tape run (resolve.Stats' LegacySwitch and Aborts): the census of ask sites
// not yet converted onto the kernel (W3 step 2). Process-wide, like
// tapeMissObserver.
var tapeLegacyObserver atomic.Pointer[func(class string)]

// SetTapeLegacyObserver installs (nil removes) the process-wide legacy-ask
// observer and returns the previous one. f must be safe for concurrent use.
func SetTapeLegacyObserver(f func(class string)) func(class string) {
	var prev *func(string)
	if f == nil {
		prev = tapeLegacyObserver.Swap(nil)
	} else {
		prev = tapeLegacyObserver.Swap(&f)
	}
	if prev == nil {
		return nil
	}
	return *prev
}

// tapeLegacyAsked reports a legacy ask inside a tape run to the observer,
// classed by switch/abort and the decision (kind/resume kind, or the
// prompt's first words for an engine-posed ask with no resume kind).
func tapeLegacyAsked(e *Engine, d *decision.Decision, aborts bool) {
	f := tapeLegacyObserver.Load()
	if f == nil {
		return
	}
	class := "switch "
	if aborts {
		class = "abort  "
	}
	class += string(d.Kind)
	if d.ResumeKind != "" {
		class += "/" + d.ResumeKind
	} else {
		p := d.Prompt
		if len(p) > 40 {
			p = p[:40]
		}
		class += " \"" + p + "\""
	}
	(*f)(class + "  [" + tapeShape(e) + "]")
}

// tapeMissed reports a predicate miss to the observer, classed by the
// resolving object's shape and the decision that reached the ask choke
// point.
func tapeMissed(e *Engine, d *decision.Decision) {
	f := tapeMissObserver.Load()
	if f == nil {
		return
	}
	ask := string(d.Kind)
	if d.ResumeKind != "" {
		ask += "/" + d.ResumeKind
	}
	nm := ""
	if n := len(e.G.Stack); n > 0 {
		if o := e.G.Obj(e.G.Stack[n-1]); o != nil {
			if so := e.G.Obj(o.Source); so != nil && so.Face() != nil {
				nm = so.Face().Name
			}
		}
	}
	(*f)(tapeShape(e) + " -> " + ask + " " + nm + " " + d.Prompt)
}

// tapeShape names the top of the stack's shape: its kind and SubAbility$
// API chain.
func tapeShape(e *Engine) string {
	if len(e.G.Stack) == 0 {
		return "nostack"
	}
	o := e.G.Obj(e.G.Stack[len(e.G.Stack)-1])
	if o == nil {
		return "nil"
	}
	if o.Ability != nil {
		return "ab:" + saChainShape(o.Ability)
	}
	f := o.Face()
	if f == nil {
		return "spell:noface"
	}
	kind := "spell:"
	if f.IsPermanent() {
		kind = "perm:"
	}
	if sa := f.SpellAbility(); sa != nil {
		return kind + saChainShape(sa)
	}
	return kind
}

func saChainShape(sa *cards.SA) string {
	out := ""
	for s, n := sa, 0; s != nil && n < 8; s, n = s.Sub, n+1 {
		if n > 0 {
			out += ">"
		}
		out += s.API
	}
	return out
}
