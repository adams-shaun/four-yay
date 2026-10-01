package rules

import (
	"strconv"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const openingHand = 7

// Spare is a finished game's reusable backing storage -- its event log array
// and its object arena -- handed from Engine.Release to the next game a batch
// runner builds (Config.Spare). Those two arrays are an engine's largest
// per-game allocations (~450 KB and ~200 KB for a 60-card 2-seat game), and a
// runner that plays thousands of games back to back otherwise allocates,
// zeroes and collects them once per game; the intent array and the Derived
// memo tables ride along. Reuse is invisible to the game: events.NewLogInto
// and state.NewGameInto re-cap their arrays to exactly the capacity a fresh
// allocation would have had, so growth points are unchanged; every slot of
// every array is cleared by Release and overwritten before it is read; the
// memo tables are a cache whose capacity no answer depends on; and the
// intent array is only ever appended to (Log.Clone caps it). The zero Spare
// is "none"; TestSpareReuseIsInvisible pins the contract.
type Spare struct {
	events  []events.Event
	objs    []state.Object
	intents []decision.Intent
	// Release recycles events and objs lazily: their first evDirty /
	// objDirty slots may still hold the spent engine's history (every slot
	// past them is zero), and the consumer zeroes whatever it does not
	// overwrite (events.Log.CloneIntoFrom, state.Game.CloneIntoDirty,
	// clearDirty). events[:evN] are the evN events of the array at evFrom
	// (the spent log's Provenance), so a clone of that same history copies
	// only what it lacks.
	evFrom            *events.Event
	evN               int
	evDirty, objDirty int
	// The Derived memo tables (derivedmemo.go): an ObjID index grown to the
	// arena's size plus the slots; cleared by Release, which is exactly the
	// zeroed never-written state derivedMemoTable.slot's growth relies on.
	memo, memoStack derivedMemoTable
	// The livelock watcher's signature and event windows (livelock.go):
	// filled from empty by every engine, so a recycled pair saves their
	// regrowth; the watcher reads only their length.
	loopSigs   []uint64
	loopRecent []events.Event
	// loopPrev / loopHeads are the watcher's candidate-index arrays
	// (prevPos, slotHead), recycled like the windows: prevPos is read only
	// below its length and slotHead is zeroed by the next watcher.
	loopPrev, loopHeads []uint32
	// snapObjs are the engine's recycled trigger-window snapshot arenas
	// (trigger_snapshot_pool.go), cleared when they were pooled; the next
	// engine's first look-back windows reuse them.
	snapObjs [][]state.Object
	// arena is a spent engine's cleared simulation arena (decision_arena.go),
	// adopted (switched off) by the next engine.
	arena *decisionArena
	// lookBack is the spent engine's zeroed look-back observer struct
	// (trigger_snapshot_pool.go), reused by the next engine.
	lookBack *Engine
	// legalOpts / manaAb are the offer walk's cleared scratch lists
	// (legalOptBuf, manaAbBuf), so the next engine's first walks append
	// into grown arrays instead of regrowing them from nil.
	legalOpts []decision.Option
	manaAb    []*cards.SA
	// hyp is the spent engine's hypothetical-clone and read scratch pool
	// (hypclone.go), adopted by the next engine.
	hyp *hypSparePool
	// static is a spent engine's cleared staticEffects memo storage, which
	// the next clone copies its parent's memo into (clone.go).
	static []ContinuousEffect
	// probe is a spent engine's layer-4 statics probe cells
	// (Engine.typesProbe), copied into by the next clone.
	probe []uint8
	// The emit path's per-engine working storage, recycled cleared: the
	// trigger and replacement zone summaries (every summary invalid, its id
	// lists emptied), active()'s two list arrays and the pending-trigger
	// queue's array. Each is overwritten before it is read.
	trigZones               []trigZoneSummary
	replZones               []replZoneSummary
	activeBuf, activeBufAlt []ContinuousEffect
	activeSrc               []*ContinuousEffect
	pending                 []pendingTrigger
}

// Release returns e's log and object-arena arrays as a Spare for the next
// game (pass its address as Config.Spare) and leaves e unusable (its Objs and Events are nil, so a stray later
// use fails loudly rather than reading a recycled array). It must be the
// LAST use of e and of anything sharing its arrays -- a Clone's log shares
// the Events prefix (events.Log.Clone) -- which is why only a batch runner
// that owns the finished engine outright calls it. The arrays are cleared so
// the Spare does not pin the finished game's cards, strings and slices --
// lazily for the event and object arrays, which the next consumer zeroes
// past what it overwrites (see Spare): a search recycles them once per
// simulation, and clearing a mid-game log and arena only for the next clone
// to copy over them again was most of Release's cost.
//
// A clone (Clone, CloneInto) may be released too -- that is the search loop
// CloneInto documents. A clone's Events and Intents start as its parent's
// backing arrays with cap == len (events.Log.Clone) and become its own only
// once an append regrows them, so a forked log's array with no spare
// capacity is left alone rather than cleared: recycling it would zero the
// parent's history. (An own array that happens to be exactly full is skipped
// too, which only forgoes one reuse.) The object arena and memo tables are
// always the clone's own.
func (e *Engine) Release() Spare {
	evs, ints := e.L.Events[:cap(e.L.Events)], e.L.Intents[:cap(e.L.Intents)]
	if e.L.Forked() {
		if cap(e.L.Events) == len(e.L.Events) {
			evs = nil
		}
		if cap(e.L.Intents) == len(e.L.Intents) {
			ints = nil
		}
	}
	sp := Spare{
		events:     evs,
		objs:       e.G.Objs[:cap(e.G.Objs)],
		intents:    ints,
		objDirty:   len(e.G.Objs),
		memo:       e.derivedMemo.release(),
		memoStack:  e.derivedMemoStack.release(),
		loopSigs:   e.loop.sigs[:0],
		loopRecent: e.loop.recent[:cap(e.loop.recent)],
		loopPrev:   e.loop.prevPos[:0],
		loopHeads:  e.loop.slotHead,
		snapObjs:   e.releaseSnapshotObjs(),
	}
	sp.arena = e.releaseArena()
	sp.hyp = e.releaseHypPool()
	// The walk scratch lists are cleared at the end of every walk (legal.go,
	// legal_walk_battlefield.go), so they hold no reference to recycle away.
	sp.legalOpts, sp.manaAb = e.legalOptBuf[:0], e.manaAbBuf[:0]
	e.legalOptBuf, e.manaAbBuf = nil, nil
	// The static memo's outer storage is always this engine's own (a build
	// writes into it, and a clone copies into its own), so it is recycled
	// cleared: the nested slices it held are never reached again.
	sp.static = e.staticContinuous[:cap(e.staticContinuous)]
	clear(sp.static)
	sp.static = sp.static[:0]
	e.staticContinuous = nil
	sp.probe, e.typesProbe, e.typesProbeReady = e.typesProbe[:0], nil, false
	for i := range e.trigZones {
		z := &e.trigZones[i]
		z.resetSummary()
	}
	for i := range e.replZones {
		z := &e.replZones[i]
		*z = replZoneSummary{ids: z.ids[:0], hotIDs: z.hotIDs[:0]}
	}
	sp.trigZones, sp.replZones, e.trigZones, e.replZones = e.trigZones[:0], e.replZones[:0], nil, nil
	sp.activeBuf, sp.activeBufAlt = clearedEffects(e.activeBuf), clearedEffects(e.activeBufAlt)
	e.activeBuf, e.activeBufAlt = nil, nil
	sp.activeSrc, e.activeSrc = e.activeSrc[:0], nil
	sp.pending = e.pendingTriggers[:cap(e.pendingTriggers)]
	clear(sp.pending)
	sp.pending, e.pendingTriggers = sp.pending[:0], nil
	if e.lookBackOwner == e && !e.lookBackBusy {
		sp.lookBack = e.lookBack
	}
	e.lookBack, e.lookBackOwner = nil, nil
	clear(sp.loopRecent)
	sp.loopRecent = sp.loopRecent[:0]
	e.loop.sigs, e.loop.recent, e.loop.prevPos, e.loop.slotHead = nil, nil, nil, nil
	// The event and object arrays are not cleared here: the next consumer
	// overwrites their live prefix anyway and zeroes the rest (see Spare).
	if evs != nil {
		sp.evFrom, sp.evN = e.L.Provenance()
		sp.evDirty = len(e.L.Events)
	}
	clear(sp.intents)
	e.L.Events, e.G.Objs, e.L.Intents = nil, nil, nil
	e.derivedMemo, e.derivedMemoStack, e.intentBuf = derivedMemoTable{}, derivedMemoTable{}, nil
	return sp
}

// clearDirty zeroes the spent history a lazily released Spare still holds
// in its event and object arrays, for a consumer that fills them from empty
// (genesis): afterwards both are zero throughout, as Release used to leave
// them.
func (sp *Spare) clearDirty() {
	clear(sp.events[:sp.evDirty])
	clear(sp.objs[:sp.objDirty])
	sp.evFrom, sp.evN, sp.evDirty, sp.objDirty = nil, 0, 0, 0
}

// clearedEffects zeroes a spent effect array to its capacity (so it pins
// none of the effects' slices and maps) and returns it empty.
func clearedEffects(b []ContinuousEffect) []ContinuousEffect {
	b = b[:cap(b)]
	clear(b)
	return b[:0]
}

// objectHeadroom is the extra Objs capacity newWithRNG reserves beyond the
// dealt decks and sideboards (see its use there).
const objectHeadroom = 128

func New(cfg Config) *Engine {
	return newWithRNG(cfg, newRNG(cfg.Seed), false)
}

// NewStartingPlayerChoice is the harness-facing constructor that offers CR
// 103.1's second half (rules/starting_player_choice.go): the toss winner
// CHOOSES who takes the first turn. Genesis (the resolved toss folded into
// G.StartingPlayer) is identical to New's, but startPostDealSetup -- the
// London mulligan round, the colour round, turn 1 -- is deferred until the
// choice is answered (Engine.AskStartingPlayer + Submit) or defaulted at the
// first Advance, so the pregame rounds open in the CHOSEN seat's turn order.
// A caller that poses no ask and never advances past genesis sees nothing;
// every other constructor (plain New) is the R-9 no-host fallback: the toss
// winner takes the first turn silently, byte-identical to the pre-choice
// engine. host, mtgsim and the acceptance driver use this constructor.
func NewStartingPlayerChoice(cfg Config) *Engine {
	return newWithRNG(cfg, newRNG(cfg.Seed), true)
}

func newWithRNG(cfg Config, random *rng, tossAsk bool) *Engine {
	life := int32(20)
	if cfg.StartingLife > 0 {
		life = cfg.StartingLife
	}
	initialObjects := 0
	for i, deck := range cfg.Decks {
		if i >= len(cfg.Names) {
			break
		}
		initialObjects += len(deck)
		if i < len(cfg.Sideboards) {
			initialObjects += len(cfg.Sideboards[i])
		}
		if i < len(cfg.PlanarDecks) {
			initialObjects += len(cfg.PlanarDecks[i])
		}
	}
	// Headroom past the dealt cards for the objects a game mints as it plays
	// (tokens, ability objects on the stack, copies): measured over the repo
	// deck matrix (botbench -pairs all, constructed and commander), a game
	// adds a median ~50 and a 99th-percentile ~125 objects to its dealt
	// cards, and without headroom EVERY game regrew Objs (a full doubling of
	// an ~800-byte-per-element array) on its first minted object.
	if initialObjects > 0 {
		initialObjects += objectHeadroom
	}
	var spare Spare
	if cfg.Spare != nil {
		spare, *cfg.Spare = *cfg.Spare, Spare{}
		spare.clearDirty()
	}
	e := &Engine{
		G:                 state.NewGameInto(cfg.Names, life, initialObjects, spare.objs),
		deckManifests:     make([]deck.Manifest, len(cfg.Names)),
		L:                 events.NewLogInto(cfg.Seed, spare.events),
		format:            cfg.Format,
		rng:               random,
		loop:              newLivelockWatcherInto(cfg.LoopGuard, spare.loopSigs, spare.loopRecent, spare.loopPrev, spare.loopHeads),
		compiledText:      newCompiledText(cfg),
		landTypeWords:     corpusLandTypeWords(cfg.NameUniverse),
		mulligans:         cfg.Mulligans,
		windowDiagnostics: cfg.WindowDiagnostics,
		startingLife:      life,
	}
	// The embedded turn ledger's per-turn slices (engine_turnledger.go).
	e.turnsTaken = make([]int32, len(cfg.Names))
	// The per-turn ManaExpend tally (rules/cast.go) starts empty; payCast
	// stamps and resets it lazily on e.G.Turn.
	e.manaExpended = make([]int32, len(cfg.Names))
	for i, name := range cfg.Names {
		var main, sideboard []*cards.Card
		if i < len(cfg.Decks) {
			main = cfg.Decks[i]
		}
		if i < len(cfg.Sideboards) {
			sideboard = cfg.Sideboards[i]
		}
		// Resolve commanders exactly as genesis does, so the manifest's
		// commander identities match the command zone even when Config names an
		// illegal set (legalCommandersFor rejects such a set whole). Outside
		// FormatCommander this is the range-filtered Config order.
		commanders := cfg.legalCommandersFor(i, len(main), main)
		archetype := ""
		if i < len(cfg.Archetypes) {
			archetype = cfg.Archetypes[i]
		}
		e.deckManifests[i] = deck.NewManifest(name, archetype, main, sideboard, commanders)
	}
	// The rest of a Spare: the memo tables start empty over the cleared
	// arrays (derivedMemoizedAt only reslices up into zeroed capacity), and
	// the intent array waits for the first Submit (the log's Intents stays
	// nil until an intent exists, as it always has).
	e.derivedMemo, e.derivedMemoStack = spare.memo, spare.memoStack
	e.adoptSnapshotObjs(spare.snapObjs)
	e.adoptArena(spare.arena)
	e.adoptHypPool(spare.hyp)
	e.legalOptBuf, e.manaAbBuf = spare.legalOpts, spare.manaAb
	if cap(spare.intents) > 0 {
		e.intentBuf = spare.intents[:0]
	}
	e.G.Tokens = cfg.Tokens
	e.setNameInPool = poolHasSetNameStatic(cfg)
	e.layer4InPool = poolHasLayer4Static(cfg)
	e.trigGrant.free = true // held per object as they appear (trigger_grantfree.go)
	e.G.NameUniverse = cfg.NameUniverse
	e.G.NameUniverseNames = append([]string(nil), cfg.NameUniverseNames...)
	if len(e.G.NameUniverseNames) == 0 && len(cfg.NameUniverse) > 0 {
		e.G.NameUniverseNames = effects.NameUniverseNames(cfg.NameUniverse)
	}
	e.manaExpendedTurn = e.G.Turn
	e.format = cfg.Format
	for i := range e.G.Players {
		if i < len(cfg.PlayerNames) && cfg.PlayerNames[i] != "" {
			e.G.Players[i].PlayerName = cfg.PlayerNames[i]
		}
	}
	e.emit(events.Event{Kind: events.GameStart, Amount: int32(len(cfg.Names))})
	// CR 103.1: the starting player is determined by a random method. Draw
	// the toss HERE, as the FIRST rng consumption of the game, before any
	// per-seat shuffle: the toss value is then a pure function of (seed,
	// seat count), independent of every deck size. The surviving-seat
	// resolution happens after the deal below (the toss draw is uniform over
	// every seat, so conditioned on naming a survivor it is uniform over the
	// survivors -- see the resolution site). CR 103.1's second half -- the
	// toss winner CHOOSES who takes the first turn -- is not implemented;
	// see the "Known approximations" row in AGENTS.md.
	toss := -1
	if len(cfg.Names) > 0 {
		toss = e.rng.IntN(len(cfg.Names))
	}
	// CR 103.1 precedes 103.2-103.4: the public toss announcement is emitted
	// HERE -- before the first shuffle and the opening hand (Forge's
	// GameAction and manabrew's game loop announce the toss before their deal
	// too), so the keep/mulligan decisions are made with the toss already
	// public. The Note names the seat the rng handed the toss to -- the true
	// CR 103.1 winner -- even if the deal below then eliminates them; who
	// actually takes the first turn is resolved after the deal has fixed the
	// survivors. The text carries the deck identity, never the display
	// PlayerName (F3 keeps display names out of the chain); view/describe.go
	// renders this one Note through player(), so a seated human still reads
	// their own name.
	if toss >= 0 {
		e.emit(events.Event{Kind: events.Note, Player: state.PlayerID(toss),
			Text: tossName(e.G, state.PlayerID(toss)) + " won the toss"})
	}
	// Match-wide dense commander indexing for Player.CmdDamage (assigned at
	// genesis): a commander's dense index is the sum of (valid commanders in
	// seats before its owner) + (its own position within its owner's
	// Commanders list, which is the order Commanders is built in the loop
	// below) -- both deterministically derivable from this Config, so no
	// separate index needs storing. Every seat's CmdDamage is sized to total
	// (the whole match's commander count) so it can be indexed by ANY
	// commander's match-wide dense index: seat B's damage holds a slot for
	// seat A's commander at A's commander's dense index. Sized here and
	// never grown; three small copy() calls per seat carry all three across
	// Game.Clone.
	totalCmd := 0
	for i := range cfg.Names {
		if i < len(cfg.Decks) {
			totalCmd += len(cfg.legalCommandersFor(i, len(cfg.Decks[i]), cfg.Decks[i]))
		}
	}
	// Opening hands are dealt as one genesis operation. Defer only the final
	// GameOver event: drawCard still emits losses and runs all other SBAs, but
	// the terminal marker must follow the public toss Note so the host can
	// recognize and persist the complete genesis burst.
	e.deferGameOver = true
	for i, deck := range cfg.Decks {
		if i >= len(cfg.Names) {
			// Ruling T22-m (fix round 2): a malformed Config with more
			// decks than named seats has nowhere to put the rest --
			// state.NewGame above sizes g.zones from len(cfg.Names) alone,
			// so PlayerID(i) here would index outside it and panic
			// (SetZone -> zoneIndex -> an out-of-range g.zones write).
			// Task 25 wires Config from a client, so a malformed one must
			// degrade, not crash the one goroutine running the whole
			// match; the excess decks are simply never dealt, the same
			// spirit as the zero-alive guard below for a Config with no
			// seats at all.
			break
		}
		p := state.PlayerID(i)
		ids := make([]state.ObjID, 0, len(deck))
		for _, c := range deck {
			ids = append(ids, e.G.AddObject(c, p).ID)
		}
		e.G.SetZone(state.ZLibrary, p, ids)
		if i < len(cfg.Sideboards) && len(cfg.Sideboards[i]) > 0 {
			sb := make([]state.ObjID, 0, len(cfg.Sideboards[i]))
			for _, c := range cfg.Sideboards[i] {
				o := e.G.AddObject(c, p)
				o.Zone = state.ZSideboard
				sb = append(sb, o.ID)
			}
			e.G.SetZone(state.ZSideboard, p, sb)
		}
		if i < len(cfg.PlanarDecks) && len(cfg.PlanarDecks[i]) > 0 {
			planes := make([]state.ObjID, 0, len(cfg.PlanarDecks[i]))
			for _, c := range cfg.PlanarDecks[i] {
				o := e.G.AddObject(c, p)
				planes = append(planes, o.ID)
			}
			planes = e.shufflePlanarDeck(p, planes)
			e.emit(events.Event{Kind: events.PlanarDeckShuffle, Player: p, IDs: planes, Secret: true})
			if len(planes) > 0 {
				e.emit(events.Event{Kind: events.PlanarReveal, Player: p, Obj: planes[0]})
			}
		}
		// Commanders leave the library for the command zone here, BEFORE the
		// shuffle and BEFORE the opening hand is dealt, so they are neither
		// shuffled into the library nor drawable. Emitted as real MoveZone
		// events (one per commander, in Config order) -- the log is the only
		// source of truth, and replay, which folds the logged events back
		// through this same New, reproduces the identical command zone. For a
		// non-Commander Config commandersFor is empty, so nothing is emitted
		// and the Shuffle below covers the whole library exactly as before.
		var myCmds []state.ObjID
		for _, idx := range cfg.legalCommandersFor(i, len(deck), deck) {
			id := ids[idx]
			myCmds = append(myCmds, id)
			e.emit(events.Event{Kind: events.MoveZone, Obj: id,
				From: state.ZLibrary, To: state.ZCommand})
		}
		// A commander list that failed the deck-construction validation seats
		// nothing; the rejection is on the log (rules/legal... engine.go's
		// legalCommandersFor) so a transcript shows why the command zone is
		// empty.
		if len(myCmds) == 0 && len(cfg.commandersFor(i, len(deck))) > 0 {
			e.emit(events.Event{Kind: events.Note, Player: p,
				Text: "commander configuration rejected under CR 903.4/903.13"})
		}
		e.G.Players[p].Commanders = myCmds
		if len(myCmds) > 0 {
			e.G.Players[p].CmdCasts = make([]int32, len(myCmds))
		}
		if totalCmd > 0 {
			e.G.Players[p].CmdDamage = make([]int32, totalCmd)
		}
		// Shuffle only what is left in the library -- the commanders have just
		// moved out, so a non-Commander seat's library and the original ids
		// are one and the same and the event is byte-identical to before.
		order := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, p)...)
		order = e.ShuffleLibrary(p, order)
		// Library order is hidden information: the event carries it because the
		// server needs it, and view projection redacts it for everyone else.
		e.emit(events.Event{Kind: events.Shuffle, Player: p, IDs: order, Secret: true})
		for j := 0; j < openingHand; j++ {
			e.drawCard(p)
		}
		if e.G.AliveCount() <= 1 {
			// Preserve T22-c's terminal-deal boundary: once at most one seat
			// remains, do not shuffle or deal a later hand. A later seat whose
			// configured library could not supply seven cards is nevertheless
			// also doomed by this same opening deal; account for that loss so
			// an all-undersized table truthfully reaches CR 104.4a's no-survivor
			// draw rather than accidentally crowning an undealt short deck.
			for next := i + 1; next < len(cfg.Decks) && next < len(cfg.Names); next++ {
				available := len(cfg.Decks[next]) - len(cfg.commandersFor(next, len(cfg.Decks[next])))
				if available < openingHand && !e.G.Players[next].Lost {
					e.playerLoses(state.PlayerID(next), loseReasonMilled, "drew from an empty library")
				}
			}
			if e.finishTerminalGenesis() {
				return e
			}
		}
	}
	if e.finishTerminalGenesis() {
		return e
	}
	e.deferGameOver = false
	alive := e.G.AliveFrom(0)
	// CR 103.1: the starting seat is the toss result, not seat 0.
	// Resolve the starting seat uniformly over the SURVIVORS. The pre-shuffle
	// toss draw is uniform over every seat, so CONDITIONED on naming a
	// survivor it is already uniform over the survivors -- it is the first
	// candidate and costs no further rng. Only when it named a seat the deal
	// eliminated does rejection sampling draw again: IntN over all seats,
	// retried until a survivor is hit, each round uniform over the survivors
	// once conditioned. In the no-elimination case -- every real game -- the
	// stream is exactly one IntN and the candidate is always the first. A
	// modulo over the survivor count instead would be BIASED: three seats
	// with seat 0 eliminated maps two of the three toss outcomes onto one
	// survivor (measured 395/205 over 600 seeds on the pre-fix code).
	start, _ := e.resolveToss(toss, alive, len(cfg.Names))
	// CR 103.1's SECOND half: the winner of the toss chooses who takes the
	// first turn, and that answer -- not the raw toss draw -- is the
	// starting seat. ONLY a tossAsk constructor offers the choice (see
	// NewStartingPlayerChoice): it is not posed by plain New, because a
	// genesis-genesis decision would appear in every test and fuzz log and
	// the R-9 no-host contract wants a fallback that completes without an
	// answer. The RESOLVED TOSS is folded below UNCONDITIONALLY either way,
	// so genesis (G.StartingPlayer, the view's pregame projection) exists
	// the moment New returns exactly as the pre-choice engine left it; with
	// the choice pending, startPostDealSetup is deferred until the answer
	// (or the default at the first Advance) resolves the choice, because the
	// London mulligan round must open in the CHOSEN seat's turn order
	// (CR 103.5 reads the starting player). A terminal deal (nobody or one
	// survivor) and a toss winner the deal eliminated have no chooser and
	// run startPostDealSetup here, byte-identical to the pre-choice engine.
	choicePending := tossAsk && !e.G.Over && len(alive) > 1 && toss >= 0 && toss < len(e.G.Players) &&
		!e.G.Players[toss].Lost
	if choicePending {
		e.tossChoice = tossChoice{active: true, winner: start}
	}
	// The resolved toss is authoritative genesis state, not merely a Note or
	// the later TurnChange: opening-hand effects and Count$StartingPlayer run
	// before turn one. Fold it through events.Apply without appending a new
	// event: genesis is replayed from Config (including its seeded toss), and
	// preserving the historic event stream keeps recorded matches replayable.
	events.Apply(e.G, events.Event{Kind: events.StartingPlayerChange, Player: start})
	// The starting seat is the toss winner resolved over the survivors --
	// never seat 0 (the pre-toss assumption Ruling T22-f removed) and never a
	// seat the deal eliminated: an early seat that decked out during its own
	// opening draw (Over still false, since other seats remain, but that
	// seat's own Lost is true) must not receive turn 1. A player already out
	// of the game is simply skipped in turn order everywhere else (NextAlive,
	// priority); resolveToss is genesis's own equivalent for the very first
	// turn.
	if !e.G.Over && !choicePending {
		// CR 103.1's resolution, now that the deal has fixed the survivors:
		// beginTurn records start in its ordinary TurnChange. The resolved seat
		// is also state.Game.StartingPlayer now (folded above without a new
		// event: genesis is replayed from Config, including its seeded toss, so
		// preserving the historic event stream keeps recorded matches
		// replayable), which is what view's pregame projection and the
		// Count$StartingPlayer head read. With the choice pending this is
		// deferred to resolveStartingPlayer (rules/starting_player_choice.go).
		e.startPostDealSetup()
	}
	return e
}

// startPostDealSetup opens the pregame rounds between the opening deal and
// turn 1. The CR 903.4b commander colour-choice round runs FIRST when a
// qualifying commander exists (the choice is made "before the game begins",
// and the London mulligan round is also pregame); otherwise it hands straight
// to startMulliganOrTurn. Both genesis and the colour round's end call it, so
// a game with no qualifying commander is byte-identical to the pre-round
// engine.
func (e *Engine) startPostDealSetup() {
	if round := e.newColorRound(); len(round.asks) > 0 {
		e.coloring = true
		e.colorRound = round
		e.stepColorRound()
		return
	}
	e.startMulliganOrTurn()
}

// startMulliganOrTurn opens whichever round follows the colour round: the
// London mulligan round (Config.Mulligans > 0), the optional opening-hand
// effects round, or turn 1 directly.
func (e *Engine) startMulliganOrTurn() {
	if e.mulligans > 0 {
		// Ruling R-8.4: the London mulligan round lives between the deal
		// and turn 1. e.pregame makes step() dispatch to stepPregame
		// (rules/mulligan.go) instead of the ordinary turn steps; the
		// round's end calls beginTurn below. Over is already false (the
		// per-seat deck-out guard above returned early) -- a game that
		// ended during the deal never starts a round.
		// CR 103.5: the starting player declares first, then each other
		// player in turn order -- AliveFrom(e.G.StartingPlayer) is that
		// order, which is also beginTurn's seat at the round's end. The
		// opening-hand effects round runs after this round (a Gemstone
		// Caverns may not be used from a hand its owner later mulliganed
		// away), and an accepted Impatient Iguana there replaces the
		// recorded designation before turn one.
		e.pregame = true
		e.mulligan = newMulliganRound(e.G.AliveFrom(e.G.StartingPlayer), e.mulligans)
	} else {
		e.opening = e.newOpeningRound(e.G.StartingPlayer, 0)
		if len(e.opening.effects) > 0 {
			e.stepOpening()
			return
		}
		e.beginTurn(e.G.StartingPlayer)
	}
}

// finishTerminalGenesis finalizes a game whose opening deal left at most one
// survivor. The toss was already announced before the first shuffle (the
// pre-deal Note in New); terminal genesis therefore needs no additional Note.
// Ruling T22-e: nobody survived genesis is CR 104.4a's draw; one survivor is
// CR 104.2a's winner. GameOver remains the final genesis event for the host's
// persistence/replay burst boundaries.
func (e *Engine) finishTerminalGenesis() bool {
	if e.G.AliveCount() > 1 {
		return false
	}
	e.deferGameOver = false
	e.checkGameOver()
	return true
}

// resolveToss maps the pre-shuffle random determination onto the seats that
// survived the opening deal. The original candidate is already uniform over
// every configured seat; rejection sampling an eliminated candidate preserves
// uniformity over survivors without consuming another draw in ordinary games.
func (e *Engine) resolveToss(toss int, alive []state.PlayerID, seats int) (state.PlayerID, bool) {
	if toss < 0 || len(alive) == 0 || seats <= 0 {
		return 0, false
	}
	candidate := state.PlayerID(toss)
	for {
		for _, p := range alive {
			if p == candidate {
				return candidate, true
			}
		}
		candidate = state.PlayerID(e.rng.IntN(seats))
	}
}

// tossName is the identity the toss Note's text carries: the deck-identity
// Name, else "seat N". F3 (TestPlayerNamesDoNotReachTheChain) keeps the
// per-seat PlayerName -- a display name -- out of the event chain entirely,
// and the Note is event text, so it uses the same deck identity every other
// event text already carries. (view/describe.go's player label may prefer
// PlayerName; that is a view projection, not chain text.)
func tossName(g *state.Game, p state.PlayerID) string {
	pl := g.Players[p]
	if pl.Name != "" {
		return pl.Name
	}
	return "seat " + strconv.Itoa(int(p))
}
