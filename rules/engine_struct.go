package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

type Engine struct {
	G                *state.Game     `clone:"deep"`
	deckManifests    []deck.Manifest `clone:"share"`
	endTurnRequested bool            `clone:"reset"`
	L                *events.Log     `clone:"deep"`
	compiledText     *compiledText   `clone:"share"`
	landTypeWords    []string        `clone:"share"`

	// ManaAbilityHook, when non-nil, is called once per mana ability
	// activation the engine resolves (resolveManaAbilityRefOriginal, the one
	// choke point every activation path -- the priority "activate" option,
	// the cast payment window, the unless-cost and attack/block-cost windows
	// -- funnels through), after the activation is judged payable and before
	// its cost and effect are applied, and once per triggered mana ability
	// (CR 605.1b, a Static$ True TapsForMana trigger) the batch after it
	// resolves off the stack (resolveTriggeredManaAbilities). sa is the
	// ability's compiled identity: the printed Face().Abilities pointer
	// (never the colour-pinned copy a Combo pick resolves through), the
	// foreign card's pointer for a gained ability, or the printed
	// Trigger.Effect body for a triggered one. It is a harness-only
	// OBSERVER (cmd/cardfuzz credits mana-ability use with it, because a
	// mana ability never uses the stack and ManaAdd carries no source): it
	// emits nothing, mutates nothing, is
	// not copied by Clone, and a nil hook -- every host, replay and test --
	// leaves the event stream and every chain head byte-identical.
	ManaAbilityHook func(p state.PlayerID, source state.ObjID, sa *cards.SA) `clone:"hook"`

	// format is the construction format New was configured with (Config.
	// Format). It is the explicit gate the Commander rules (the tax, CR
	// 903.9, commander damage) check -- "in a non-Commander game none of
	// this runs at all" -- rather than inferring the format from the
	// incidental shape of the zones. It is read long after New returns: at
	// cast time for the CR 903.8 tax, at combat-damage time and in the
	// state-based-action pass for CR 903.10. Plain Format value; Clone
	// copies it so a cloned Commander engine still gates its command-zone
	// rules and keeps its commander-damage loss condition.
	format Format `clone:"deep"`

	rng     *rng               `clone:"deep"`
	pending *decision.Decision `clone:"deep"`

	// deferGameOver is true only while New processes the opening deal. A
	// library-empty draw still emits PlayerLost and runs every other SBA, but
	// checkGameOver waits until New has recorded the CR 103.1 toss. That keeps
	// GameOver as the final event of a terminal genesis burst (the host's
	// persisted-boundary contract) and lets an all-undersized opening deal
	// reach the truthful no-survivor draw instead of accidentally crowning an
	// undealt short deck.
	deferGameOver bool `clone:"reset"`

	// continuous holds every registered continuous effect, live or expired.
	// The layer system (layers.go) is the only reader and writer.
	continuous   []ContinuousEffect       `clone:"deep"`
	lifeExchange *lifeExchangeTransaction `clone:"reset"`
	// pendingLifeExchange parks an exchange transaction whose first or second
	// life change suspended on a decision that is NOT a replacement-order ask
	// (a consumed GainLife→Draw body that itself parked a Dredge ask). No
	// static caller references the transaction once the synchronous emit
	// returns, so without this slot it is orphaned and NEITHER side ever
	// applies. settlePendingLifeExchange re-drives it from every engine-idle
	// drain, and finishLifeExchange re-parks it whenever it suspends again.
	// It is only ever non-nil while a decision or a replacement-order queue is
	// outstanding -- an intent boundary -- so (like resume/replChoices/pending)
	// Clone copies it.
	pendingLifeExchange *lifeExchangeTransaction `clone:"deep"`
	// controlGrants holds the GainControl effects that can still end (see
	// rules/control.go). It is engine continuation state only; every take and
	// return is a ControlChange event, so the log alone rebuilds Game state.
	controlGrants []controlGrant `clone:"deep"`
	// expiringControl guards expireControl against re-entry through the
	// ControlChange events it emits.
	expiringControl bool `clone:"reset"`
	// reconcilingControlStatics guards reconcileControlStatics (rules/
	// control_static.go) against re-entry through the ControlChange events
	// IT emits; the same intent-boundary discipline as expiringControl.
	reconcilingControlStatics bool `clone:"reset"`

	// mulligans is Config.Mulligans carried past genesis: the colour round's
	// end (rules/commander_color.go) must re-enter the same mulligan/opening
	// hand-off the genesis branch would have taken, and cfg is not otherwise
	// retained. Plain int, so Clone copies it.
	mulligans int `clone:"deep"`
	// windowDiagnostics is the default-off, observer-only priority sidecar
	// gate copied from genesis Config.
	windowDiagnostics bool `clone:"deep"`
	// skipPass is the hypothetical-only auto-pass of quiet priority windows
	// (skip_pass.go); set by the search's world setup, never cloned.
	skipPass uint8 `clone:"reset"`
	// startingLife is Config.StartingLife with the 0-means-20 convention
	// already resolved at genesis — the value state.NewGameLife opened the
	// game with. It is the effects.Host StartingLife backing (the
	// PlayerCountDefinedPlayer.PlayerUID_RelativePlayerUID$StartingLife read
	// behind Anya, Merciless Angel's and Game Over's relative
	// half-starting-life thresholds). Plain int32, so Clone copies it.
	startingLife int32 `clone:"deep"`
	// setNameInPool is a genesis-time fact: does any card this match can put
	// on the battlefield print a SetName$ static? False for almost every
	// match, which reduces the per-event refresh to one predictable branch.
	setNameInPool bool `clone:"deep"`
	// layer4InPool is the same genesis-time fact for a layer-4 type-changing
	// effect (cards.ChangesTypes). False for most matches, which reduces the
	// per-event refresh to one predictable branch.
	layer4InPool bool `clone:"deep"`
	// controlStaticInPool is the same genesis-time fact for a GainControl$
	// static (cards.MayCarryControlStatic). False for most matches, which
	// reduces the per-event static-control reconcile to one branch.
	controlStaticInPool bool `clone:"deep"`
	// continuousVersion is bumped by every direct mutation of e.continuous
	// (layers.go's AddContinuous and EndOfTurnCleanup). It stands in for the
	// events a board change would signal through the log head: while
	// AddContinuous also emits a ClockTick, EndOfTurnCleanup rewrites
	// e.continuous in place with no event, and the active() cache must see
	// that drop (an UntilEOT pump expiring) even though the log head did not
	// move. A zero value is never taken as a valid cache hit across rebuilds
	// because active() guards hits on version as well.
	continuousVersion int `clone:"reset"`

	// searchingBy tracks the library search in flight, for the Opposition
	// Agent class: repl:Moved's FoundSearchingLibrary$ matches only while a
	// search's own moves are being emitted, and the search-control static
	// redirects the search pick's decision. The depth counter keeps a nested
	// search's flag alive until the outer search leaves applyLibrarySearch.
	// Synchronous engine-runtime state (set and cleared around one
	// synchronous applyLibrarySearch), never folded from an event and never
	// read across a suspension.
	searchingBy state.PlayerID `clone:"reset"`
	searchDepth int            `clone:"reset"`

	// loop is the livelock watcher (rules/livelock.go): pure observation of
	// the event stream this engine is logging, configured by Config.
	// LoopGuard. It reads nothing and is read by nothing else; it panics
	// with a *LivelockError when the stream looks non-terminating. Clone
	// copies the guard thresholds and resets the run/quiet state (a clone
	// only happens at an intent boundary, where the watcher is idle
	// anyway), so the clone and the original watch their own streams
	// independently.
	loop livelockWatcher `clone:"reset"`

	// The struct's remaining field contracts live in the embedded clusters
	// below: plain value sub-structs, each declared in its own focused file
	// next to this one and named for its cluster. They are embedded as
	// ANONYMOUS fields on purpose: that is what promotes every member field,
	// so all existing e.<field> access keeps compiling unchanged and
	// Clone's per-field copy classes (rules/clone.go) are unchanged. Only the
	// fields some composite literal must set stay at this top level:
	// genesis.go's New (G, deckManifests, L, format, rng, loop, compiledText,
	// landTypeWords, mulligans, windowDiagnostics, startingLife),
	// trigger_match.go's observer Engine literal (G, L, continuous,
	// continuousVersion, setNameInPool, layer4InPool) and the test
	// constructors in rules/*_test.go. Never introduce e.<cluster>.<field>
	// access anywhere -- internal/archtest's resumeFieldWriters guard reads
	// the selector text of assignment targets.
	engineTurnLedger
	engineLayerCaches
	engineRounds
	engineDerivedTables
	engineScratch
	engineTriggerMaps
	engineResolution
	engineDrain
	engineTokenMint
	engineTriggerBatches
	engineContinuation
	engineParked
	engineCastWindows
	engineEmitCtx
	engineResolveKernel

	// paySession is the payment layer's engine-owned state (pay.Session,
	// lasagna spec §9.2): embedded so its fields read as Engine fields, and
	// handed to rules/pay through pay.Engine's Session.
	paySession
}
