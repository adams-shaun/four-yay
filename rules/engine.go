// Package rules is the authoritative rules engine: turn structure, priority,
// the stack, combat and state-based actions. It owns the only Game instance a
// match has and mutates it exclusively through events.Emit.
//
// The event log is NOT a complete match description by itself. Genesis --
// state.NewGame and the initial AddObject calls that build each player's
// deck into the object arena, both in New below -- runs before the log
// exists and legitimately bypasses events. Replaying L.Events alone
// reconstructs everything that happens from that point on (turn structure,
// zone moves, life, damage, priority and so on), but it can never recover
// deck contents or player names: those are never logged. A faithful replay
// needs the original Config together with the log, not the log alone.
package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Format is the game's construction format. FormatConstructed is the zero
// value -- every existing Config that never sets it (all today's fixtures
// and every non-Commander game) is unchanged.
type Format int

const (
	FormatConstructed Format = iota
	FormatCommander
)

type Config struct {
	Seed  uint64
	Names []string
	// PlayerNames is a per-seat display name independent of the deck
	// (the wire's PlayerView.Name carries it; Names is the deck identity
	// the engine uses in event text, which is replay-hashed). nil or a
	// short slice falls back to Names, so every Config that never sets it
	// behaves byte-identically to today.
	PlayerNames []string
	Decks       [][]*cards.Card
	// Sideboards carries each seat's optional sideboard. It is genesis
	// configuration rather than an event, so replay receives the same cards
	// without changing any existing event schema.
	Sideboards [][]*cards.Card
	// PlanarDecks carries each seat's optional Planechase deck. It is genesis
	// configuration, like Sideboards, and a zero value emits no new events.
	PlanarDecks [][]*cards.Card
	// Archetypes carries each seat's deck authoring archetype (deck.File.
	// Archetype), parallel to Decks. It is genesis configuration like
	// Sideboards: it never reaches an event, so a Config that leaves it nil
	// (every Config before this field) produces byte-identical manifests with
	// an empty Archetype. A short or nil slice leaves the extra seats' archetype
	// empty.
	Archetypes []string
	// Format names the construction format. Zero means Constructed; the other
	// tasks in the Commander milestone (the tax, CR 903.9, commander damage)
	// read it. This task is plumbing: it reads Commanders and StartingLife
	// only.
	Format Format
	// StartingLife is a game's opening life total. 0 (the zero value) means
	// the existing 20, so every Config that never sets it observes exactly
	// what it always did.
	StartingLife int32
	// Commanders holds, for each seat i, the indices into Decks[i] that are
	// that seat's commanders; nil or absent means none. Genesis places those
	// objects in the command zone instead of the library. An index out of
	// range for its deck is a config error degraded the same way New degrades
	// more decks than seats: the offending entry is skipped, not a crash.
	Commanders [][]int
	// Mulligans is the number of London mulligans each player may take in the
	// pre-game round between the opening deal and turn 1. 0 (the zero value)
	// skips the round entirely, so every Config that never sets it is
	// unchanged (all standalone fixture Configs). It sits in the same Config
	// replay is handed, so a replay reproduces the round (Ruling R-8.4): the
	// acceptance config sets it so the 12-deck suite exercises keep/mulligan
	// and bottoming.
	Mulligans int
	// WindowDiagnostics opts this table into per-priority-option withholding
	// reasons. It is observer-only and emits no events.
	WindowDiagnostics bool
	// Tokens is the token definitions the decks in this match can create --
	// cards.Registry.Tokens. Copied onto Game.Tokens in New so
	// events.Apply's TokenCreate case has something to mint from. Replay
	// must pass the same table a live match's Config did.
	Tokens map[string]*cards.Card
	// NameUniverse is the compiled corpus used by NameCard decisions.
	NameUniverse []*cards.Card
	// NameUniverseNames pins a persisted match's sorted name list. A live
	// match leaves it nil and derives it from NameUniverse at genesis.
	NameUniverseNames []string
	// LoopGuard, when non-nil, overrides the livelock watcher's thresholds
	// for this game (rules/livelock.go): how many consecutive events a
	// repeating cycle must run before the engine aborts with a
	// *LivelockError, the longest cycle tracked, and the no-progress
	// runaway backstop. nil (the zero value every existing Config has) is
	// the defaults, so every game that never sets it is byte-identical to
	// an un-watched one. LoopGuard.Disabled (explicitly set) turns the
	// watcher off entirely -- the embedder's own opt-out for a supervised
	// non-terminating game; the host sets it whenever its
	// MaxDecisionsPerTurn opt-out is in force. The watcher is pure
	// observation either way: it emits no event and holds no state the
	// engine reads.
	LoopGuard *LoopGuard

	// LegacyResume opts this game out of the W3 resolution kernel
	// (rules/resolve), which is the default: the kernel checkpoints at the
	// pass that begins a resolution and answers its mid-resolution decisions
	// by re-executing it from the checkpoint with the recorded intents as an
	// answer tape, and it answers a park-and-continue ask in place (lasagna
	// spec §7.2), so a game logged on one path replays only on that path. The
	// GORGE_TAPE_KERNEL=0 environment switch opts every engine out.
	LegacyResume bool

	// Spare, when non-nil, is a finished game's storage (Engine.Release)
	// the new engine reuses for its log and object arena. It never changes
	// the game -- see Spare. It is consumed: New empties *Spare, so a copy
	// of this Config that builds a second engine (a replay, a coverage
	// rebuild) allocates fresh instead of sharing the first engine's arrays.
	Spare *Spare
}

type triggerObjectLKI struct {
	object           *state.Object
	power, toughness int32
	ptValid          bool
}

// counterAddedThisTurn is engine-only provenance for positive object counter
// placements. The snapshot is captured before events.Apply mutates the object.
type counterAddedThisTurn struct {
	actor  state.PlayerID
	kind   string
	amount int32
	object state.Object
}

// Advance runs engine work until a decision is required or the game ends.
// A still-pending CR 103.1 winner-chooses choice (rules/starting_player_choice.go)
// is defaulted HERE, at the loop head -- never inside step(), which the
// resolution machinery calls mid-game; a hand-built mid-game engine must
// never find genesis work waiting for it.
func (e *Engine) Advance() {
	if e.tossChoice.active && e.pending == nil {
		e.resolveTossChoiceDefault()
	}
	for !e.G.Over && e.pending == nil {
		e.step()
	}
}

// Submit applies a client's answer. Anything the engine did not offer is
// rejected, which is what keeps the client rules-ignorant.
func (e *Engine) Submit(in decision.Intent) error {
	if e.derivedMemoDepth != 0 {
		panic("rules: Submit inside a Derived memo scope (BeginDerivedReads promises a pure read)")
	}
	// No engine frame is live here, so the last issued cast storage is free
	// unless it is still the cast in flight (cast_pool.go).
	e.recycleCast()
	if e.G.Over {
		return fmt.Errorf("game is over")
	}
	d := e.pending
	if d == nil {
		return fmt.Errorf("no decision pending")
	}
	if (in.Payment != nil || in.Announce != nil) && d.Kind == decision.KPriority {
		e.EnsurePaymentActions()
	}
	if err := submitValidate(e, d, in); err != nil {
		return err
	}
	dbgSubmit = string(d.Kind) + "/" + d.ResumeKind
	if len(in.Choices) > 0 && in.Choices[0] < len(d.Options) {
		dbgSubmit += "/" + d.Options[in.Choices[0]].Kind
	}
	// The resolution kernel (rules/resolve; Config.LegacyResume opts out): a
	// posed tape resolution re-executes from its checkpoint, and a pass that
	// begins a resolution takes the checkpoint first.
	if e.tape.On() && e.tape.Submit(asResolve(e), d, in) {
		return nil
	}
	submitCommit(e, d, in)
	return nil
}

// submitValidate is Submit's validation of in against the posed decision d,
// a pure read: a rejected intent leaves the decision posed and the engine
// untouched. The resolution kernel's served answers run through the same
// checks (resolveBoard.Validate).
func submitValidate(e *Engine, d *decision.Decision, in decision.Intent) error {
	if err := d.Validate(in); err != nil {
		return err
	}
	// The wire contract above is the ANSWERING seat's (d.Player); every
	// engine-side check below acts for the seat the decision is asked OF.
	// They differ only under a CR 722 redirect (actingView); submitCommit
	// logs the answering seat's intent exactly as submitted.
	d, in = actingView(d, in)
	if d.Kind == decision.KAttackers {
		// Ruling m34: the KAttackers option list offers every (attacker,
		// defender) pair, so an intent naming the same creature twice --
		// against two different defenders -- passes Validate's per-index
		// checks while declaring it attacking two players (CR 506.2).
		// Reject before the intent is recorded and the decision consumed,
		// so the pending attackers decision survives for a legal answer.
		if err := e.validateAttackers(d, in); err != nil {
			return err
		}
	}
	if d.Kind == decision.KBlockers {
		// CR 509.1a: the blockers option list similarly offers every legal
		// (blocker, attacker) pair. Reject choosing the same ordinary blocker
		// against multiple attackers while preserving the pending decision.
		if err := e.validateBlockers(d, in); err != nil {
			return err
		}
	}
	if d.Kind == decision.KChoose {
		// The cast flow's Convoke/Harmonize announcement (convokeAsk): the
		// static option list cannot express "only while the outstanding
		// cost can still absorb the contribution", so an over-selection
		// (two white creatures for one {W}) passes Validate's per-index and
		// group checks. Reject it here, before the intent is recorded and
		// the pending decision consumed, so a legal subset can be
		// resubmitted -- the same preserve-and-reject shape as
		// validateAttackers above.
		if err := e.validateCastContributions(d, in); err != nil {
			return err
		}
		// ShareLandType$ True (Myriad Landscape): a hidden-library search's
		// answer must name cards that all share one land type -- the option
		// list cannot express the pairwise constraint, so an answer naming
		// e.g. a Forest and a Mountain is rejected and the pending decision
		// survives for a legal (or smaller) answer. Single-card answers are
		// trivially legal.
		if err := e.validateSearch(d, in); err != nil {
			return err
		}
	}
	if d.Kind == decision.KPriority {
		if in.Announce != nil {
			action, ok := paymentActionFor(d, in.Announce.ActionID)
			if !ok {
				return fmt.Errorf("payment action is not offered") // defensive: Decision.Validate already checked.
			}
			if err := e.ValidateCastAnnounce(in.Player, action.Cast); err != nil {
				return err
			}
		}
		if in.Payment != nil {
			action, ok := paymentActionFor(d, in.Payment.ActionID)
			if !ok {
				return fmt.Errorf("payment action is not offered") // defensive: Decision.Validate already checked.
			}
			if err := e.ValidateCastPayment(in.Player, action.Cast, in.Payment.Plan); err != nil {
				return err
			}
		}
		// A priority answer whose handler would no-op at its first guard is
		// rejected before it is recorded (rules/priority_guard.go), so a
		// stale or mis-offered option errors instead of spinning.
		if in.Payment == nil && in.Announce == nil {
			if err := e.validatePriorityChoice(d, in); err != nil {
				return err
			}
		}
	}
	return nil
}

// submitCommit is Submit past validation: it records in, emits DecisionMade
// and runs the answer's handler and Submit's idle tail. Validation (above) is
// a pure read; from here the Submit commits, which ends the posed decision's
// rest window (potential_walk_cache.go).
func submitCommit(e *Engine, d *decision.Decision, in decision.Intent) {
	// The handlers act for the seat the decision is asked OF (actingView, a
	// CR 722 redirect); the log keeps the answering seat's intent.
	logged := in
	d, _ = actingView(d, in)
	e.potentialAskSerial++
	if e.L.Intents == nil && e.intentBuf != nil {
		// A recycled intent array (Config.Spare) backs the log from its first
		// intent on; Log.Clone caps Intents, so no clone ever shares its
		// spare capacity.
		e.L.Intents, e.intentBuf = e.intentBuf, nil
	}
	// The caller owns its intent. Keep a private witness before it becomes
	// replay history, so a client-side mutation after Submit cannot alter it.
	logged = cloneIntentForLog(logged)
	e.tape.LogIntent(e.L, logged)
	// The handlers read the same private witness, re-seated on the acting
	// seat (d is the acting view, so d.Player is that seat either way).
	in = logged
	in.Player = d.Player
	made := decisionMadePaymentText(d.Kind, in.Choices, in.Payment)
	if in.Announce != nil {
		made = decisionMadeAnnounceText(d.Kind, in.Choices, in.Announce)
	}
	e.emit(events.Event{Kind: events.DecisionMade, Player: logged.Player, Text: made})
	e.pending = nil
	if in.Announce != nil {
		action, _ := paymentActionFor(d, in.Announce.ActionID)
		// The same Priority marker the planned route emits, then the ordinary
		// cast transaction with no witness: the caster pays in the announced
		// CR 601.2g window (announce_pay.go).
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
		e.beginCastAnnounced(in.Player, decision.Option{Kind: "cast", Obj: action.Cast.Object})
	} else if in.Payment != nil {
		e.paymentStats.recordPlannedSubmission()
		action, _ := paymentActionFor(d, in.Payment.ActionID)
		// Match the ordinary cast priority action exactly, then enter the same
		// cast transaction.  The plan is only an immutable payment continuation;
		// it never represents a second casting implementation.
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
		e.beginCastWithPayment(in.Player, decision.Option{Kind: "cast", Obj: action.Cast.Object}, in.Payment)
	} else {
		e.handle(d, in)
	}
	// A decision posed while a commander-zone choice was outstanding waited
	// behind it (ask's CR 903.9 arm); pose it now that the answer landed,
	// before anything below can treat the engine as idle and advance.
	drainDeferredAsks(e)
	// A mana ability whose cost payment posed a decision (the CR 903.9
	// commander-zone choice for a sacrificed commander) resolves its mana
	// effect once that decision -- and any it handed on to -- is answered.
	if e.pending == nil && e.manaAfterCost != nil {
		e.resumeManaAfterCost()
	}
	// A CR 616.1 competition that arose while THIS decision was outstanding
	// was parked on the queue without an ask (poseLifeReplacementChoice's
	// queued arm, poseDamageReplacementChoice's multi-recipient batch, the
	// AddCounter/token/Updated poses): ask it now, before anything else
	// reads the parked event's unresolved state. A handler that already
	// asked (handleReplacement's own tails) set pending again, and this
	// drain is inert for it.
	if e.pending == nil && !e.Suspended() {
		e.askNextReplacementChoice()
	}
	// A CR 708.6 turn-face-up special action whose cost events parked on a
	// decision (any kind: commander zone, replacement order, madness, an ask
	// inside a replacement body) finishes paying and turns face up once every
	// such decision has landed (rules/morph_turnup.go).
	e.resumeTurnUpAfterCost()
	// An opening-hand round parked behind a decision its own effect posed
	// (an "as this enters" choice of a card beginning the game on the
	// battlefield) steps on now that the engine is idle again.
	e.resumeOpening()
	// CR 704.4: nobody receives priority in the middle of a resolution. A
	// handler may have resumed an effect only far enough to pose another
	// mid-resolution decision; in that case state-based actions wait until
	// the resolution finishes. The answer that finishes it clears resume, so
	// this same boundary performs the deferred check before Advance can grant
	// priority.
	//
	// CR 704.3: state-based actions are checked only when a player would
	// receive priority, and CR 601.2 explicitly withholds priority for the
	// whole cast: a mana window (CR 601.2g) re-enters payCast for each mana
	// ability activated, so an activation that drops the payer to 0 or less
	// (Ancient Tomb, a pain land, a pay-life source) leaves e.cast set with
	// the cast still mid-payment. The loss must wait for the priority
	// boundary the completed cast reaches, never fire between payment steps --
	// otherwise a manual window that taps the costly source first is
	// unpayable at 1 life although the same window in reverse order
	// (TestManaPaymentContinuesBelowZeroLife) settles. A pending activation's
	// cost payment reuses this same pendingCast flow, so e.cast != nil covers
	// both. The settled cast clears e.cast (payCast's tail) and the abort
	// path clears it too, so the check runs at the boundary either way.
	if !e.Suspended() && e.cast == nil {
		e.checkStateBased()
	}
	e.Advance()
}

// drawCard draws for the turn structure, sharing effects.DrawFor with the
// Draw primitive so the draw step and a card that says "draw a card" can
// never disagree about what drawing means.
//
// Ruling T22-d: this used to call checkGameOver alone -- correct as far as
// it went (an empty-library draw is itself a loss, via the PlayerLost
// DrawFor emits directly), but it skipped destroyLethalDamage and, now,
// checkLoseConditions' own permanent-removal sweep. checkStateBased runs
// both of those in addition to checkGameOver, so a player who decks out
// here has their battlefield cleaned up the same way one who hits 0 life
// does, rather than only on whatever later checkStateBased call happens to
// come next.
func (e *Engine) drawCard(p state.PlayerID) {
	effects.DrawFor(e, p)
	e.checkStateBased()
}

// drawCardTurn is drawCard for the draw step's turn-based draw.
func (e *Engine) drawCardTurn(p state.PlayerID) {
	effects.DrawForTurn(e, p)
	e.checkStateBased()
}

// cardsKeywordHead lets layers.go strip a keyword's parameters ("Equip:2" ->
// "Equip") without importing cards itself for one call.
func cardsKeywordHead(k string) string { return cards.KeywordHead(k) }

var dbgSubmit string // TEMP
