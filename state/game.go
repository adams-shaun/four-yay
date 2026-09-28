package state

import "github.com/adams-shaun/gorge/cards"

type Player struct {
	ID          PlayerID
	Name        string
	PlayerName  string
	Life        int32
	Lost        bool
	LandsPlayed int32
	Pool        Mana

	// RestrictedMana retains the spend restriction on mana produced by a
	// RestrictValid$ mana ability. It is cleared with the pool at step/phase
	// cleanup and is reconstructed from ManaAdd events.
	RestrictedMana []ManaRestriction

	// Counters records player counters (currently poison, used by Ward costs).
	Counters []Counter

	// LastUpkeepTurn records the turn of this seat's most recent upkeep,
	// written by events.Apply's StepChange case when the Draw step begins
	// (the turn's upkeep has just completed, so acquisitions made during
	// that upkeep itself still count as "since the beginning of your most
	// recent upkeep" — kw:Echo's gate, CR 702.35a — via the AcqStep half of
	// the acquisition tuple). Zero means no upkeep has been recorded (the
	// seat's first upkeep); kw:Echo's gate treats zero as vacuously true.
	// Written ONLY inside events.Apply so a live game and a replay derive it
	// identically; Clone copies it with the struct.
	LastUpkeepTurn int32

	// Snow parallels Pool slot for slot: Snow[i] counts how many of the
	// Pool[i] mana units were produced by a Snow permanent (CR 107.4h — a
	// snow unit can pay a {S} pip as well as anything else one mana pays).
	// It is written only by the ManaAdd event's "S<colour>" Counter form and
	// cleared with the pool by ManaClear, so Snow[i] <= Pool[i] always holds
	// and a replay derives both identically.
	Snow Mana

	// TypedMana retains the four historical public producer tallies: Treasure,
	// Cave, Desert and Artifact-only. Its first three tallies include mana
	// produced by artifacts of the same type; ArtifactTyped marks that subset.
	// Each unit is counted only once in the pool. ManaUnits derives the
	// exclusive payment partition; no rules-side state mutation is needed.
	TypedMana [4]Mana
	// ArtifactTyped is the Artifact subset of each Treasure/Cave/Desert
	// tally. Each unit appears once in TypedMana and, when produced by an
	// Artifact, also here. Both are folded only by events.Apply.
	ArtifactTyped [3]Mana

	// PersistentMana parallels Pool slot for slot: PersistentMana[i] counts
	// how many of the Pool[i] mana units carry PersistentMana$ True — mana
	// that does not empty as steps and phases end (CR 500.4 with the
	// producing card's exception: Rousing Refrain, Savage Ventmaw, 23 corpus
	// carriers). It is written only by events.Apply: the ManaAdd event's pm
	// Text suffix raises it with its pool unit, a spend that consumed
	// persistent units carries the attribution ON ITS EVENTS (a marked plain
	// negative, or a restricted spend's batch flags — the payment path
	// attributes a slot's units ordinary-first over its VISIBLE pool),
	// ManaClear clears only the non-persistent share, and the TurnChange
	// fold expires it (the cards' "until end of turn" bound — the units
	// become ordinary again and the next boundary's ManaClear empties them).
	// PersistentMana[i] <= Pool[i] always holds and a replay derives both
	// identically. No historical event ever carried the suffix, so every
	// pre-existing game reconstructs an all-zero tally byte-identically.
	PersistentMana Mana

	// Commanders lists this seat's commanders, in Config order, sized at
	// genesis and never grown. CmdCasts runs parallel to it: entry k counts
	// how many times Commanders[k] has been cast from the command zone.
	// CmdDamage records combat damage this seat has taken (a per-match
	// cumulative total), indexed by the match-wide dense commander index
	// assigned at genesis -- see rules.New. All three are populated by the
	// Commander genesis and written by other tasks (the commander tax, CR
	// 903.9 and commander damage hand off through them); Clone deep-copies
	// them so a cloned game's bookkeeping never aliases the live one's.
	Commanders []ObjID
	CmdCasts   []int32
	CmdDamage  []int32

	// DamageTakenByGame lists, in append order, every damage SOURCE that has
	// dealt this seat damage this game (never cleared -- it is game-long, not
	// the per-turn window). Appended by events.Apply's DamageProvenance case
	// with a dedup so the fold is idempotent and the slice stays small; the
	// wasDealtDamageThisGameBy player/object predicates read it as a
	// membership test. Clone deep-copies it so a cloned game's record never
	// aliases the live one's.
	DamageTakenByGame []ObjID

	// Speed is this seat's speed (CR 702.163, "Start your engines!"): it
	// starts at 0 (or 1 the first time an engine grants speed), rises by one
	// once on each of this seat's own turns when an opponent loses life,
	// caps at 4 (max speed), and never resets. Written only by events.Apply's
	// SpeedChange case, so a log-only reconstruction rebuilds it exactly.
	Speed int32

	// RingTempted is this seat's raw "the Ring has tempted you" count (CR
	// 701.54a): it rises by one each time the Ring tempts this seat and is
	// NOT capped — the Ring emblem's level abilities are gated on "tempted N
	// or more times", so a future emblem reader compares, never clamps.
	// Written only by events.Apply's RingTemptsYou case, so a log-only
	// reconstruction rebuilds it exactly.
	RingTempted int32

	// RingBearer is the ObjID of this seat's Ring-bearer permanent (CR
	// 701.54a/b: the creature chosen when the Ring last tempted this seat,
	// which keeps the designation until another creature becomes the
	// Ring-bearer, another player gains control of it, or it leaves the
	// battlefield). Zero means this seat has no Ring-bearer. The
	// designation's two event-derived clears live in events.Apply too
	// (ControlChange and the battlefield-leave path), so a replay derives
	// the designation identically.
	RingBearer ObjID

	// Blessing is this seat's one-way "the city's blessing" latch (CR
	// 702.131, Ascend): once true it stays true for the rest of the game
	// -- CR 702.131a grants it when a player controls an Ascend permanent
	// (or resolves an Ascend instant/sorcery) while controlling ten or
	// more permanents, and nothing ever removes it. Written only by
	// events.Apply's BlessingChange case, so a log-only reconstruction
	// rebuilds it exactly. A plain bool is carried for free by Clone's
	// per-player struct copy.
	Blessing bool

	// EnduringStory is the one-way CR 702.175 designation latch.
	EnduringStory bool

	// Notes is the set of player-notation labels this seat has noted, in the
	// order they were noted (Forge's `NoteCards$ ... | NoteCardsFor$ <label>`
	// on a DB$ Pump body appends <label> here; `Player.NotedFor<label>` reads
	// it). It is append-only and never re-ordered, so it is deterministic on
	// replay, and a re-note of the same label does not duplicate the entry.
	// Written ONLY by events.Apply's PlayerNoted case, so a live game and a
	// log-only reconstruction derive it identically. Clone deep-copies it
	// (the Notes slice is appended to in place, so sharing the backing array
	// would let either game corrupt the other). The sibling CARD notation
	// (`NoteCards$ Self` on a body whose label is read by `Card.NotedForX`)
	// is a different family and is not stored here.
	Notes []string
}

// ExtraTurn is one pending CR 500.7 turn. It is deliberately a queue entry,
// rather than a per-player flag: several grants can be pending in LIFO order
// and each grant can carry a different rider.
type ExtraTurn struct {
	Player    PlayerID
	SkipUntap bool
}

// ExtraPhase is one pending Forge AddPhaseEffect grant (DB$ AddPhase: "after
// this phase, there is an additional combat phase", 56 corpus SA lines). It
// is deliberately a queue entry, like ExtraTurn: several grants can be
// pending for the same splice point (Obeka, Splitter of Seconds's "that many
// additional upkeep steps" is several entries after the same end-of-combat
// step; two Aurelia triggers queue two extra combats the same way), and each
// grant can carry a different rider.
//
// The engine's turn walk is a linear Step advance (rules/turn.go's
// advanceStep), so an extra phase is spliced as a LIST insertion rather than
// a step renumbering: the extra phase begins when the walk leaves AfterStep
// (Consumed), runs Entry..RangeEnd, and on leaving RangeEnd the walk resumes
// at FollowedBy (default AfterStep+1, the phase that would naturally have
// followed the splice point -- Forge AddPhaseEffect's default followedBy, so
// an extra Beginning spliced after Main2 resumes at the end step, never back
// into Main1). RangeEnd is derived from Entry, except when the grant's
// ExtraPhase$ value names a multi-step phase whose range is not the entry's
// default -- then the +1 event carries the last step in its RANGEEND rider
// (events.ExtraPhaseRiders) and the fold stores it.
// Every field is folded from the ExtraPhase event (grant +1 / consume -1 /
// complete -2), cloned in Clone, and the whole queue clears at TurnChange --
// an extra phase is spliced into the CURRENT turn only and never survives
// into the next one.
type ExtraPhase struct {
	Player    PlayerID // the granting ability's controller (informational)
	AfterStep Step     // the step after which the extra phase is inserted
	Entry     Step     // the extra phase's first step
	RangeEnd  Step     // the extra phase's last step; completion when the walk leaves it
	// FollowedBy is the explicit FollowedBy$ resume point when HasFollowedBy
	// is set; otherwise the resume point is AfterStep+1 (see the type comment).
	HasFollowedBy bool
	FollowedBy    Step
	// Consumed marks a grant whose extra phase has been entered. It completes
	// when the walk leaves RangeEnd.
	Consumed bool
	// Delayed-trigger rider (Moraug's ExtraPhaseDelayedTrigger$/
	// ExtraPhaseDelayedTriggerExcute$ pair, the extra-turn Final-Fortune
	// precedent one level up): the SVar named by Execute registers a one-shot
	// delayed trigger at CONSUME time, firing at DelayedPhase in this same
	// turn (MinTurn = the current turn), gated by ValidPlayer when present.
	HasDelayedPhase bool
	DelayedPhase    Step
	ValidPlayer     string
	Execute         string
	Source          ObjID
}

// Counter returns this player's count of kind.
func (p *Player) Counter(kind string) int32 {
	for _, c := range p.Counters {
		if c.Kind == kind {
			return c.N
		}
	}
	return 0
}

// AddCounter changes one player-counter kind, clamping at zero.
func (p *Player) AddCounter(kind string, n int32) {
	for i := range p.Counters {
		if p.Counters[i].Kind == kind {
			p.Counters[i].N += n
			if p.Counters[i].N < 0 {
				p.Counters[i].N = 0
			}
			return
		}
	}
	if n > 0 {
		p.Counters = append(p.Counters, Counter{Kind: kind, N: n})
	}
}

// ManaUnits derives the exclusive payment partition from overlapping type
// and Artifact-bit tallies without mutating state.
func (p Player) ManaUnits() [7]Mana {
	var units [7]Mana
	for t := range p.TypedMana {
		units[t] = p.TypedMana[t]
	}
	for t := 0; t < 3; t++ {
		units[t+TypedArtifactTreasure] = p.ArtifactTyped[t]
		for i := range units[t] {
			units[t][i] -= p.ArtifactTyped[t][i]
		}
	}
	return units
}

// Game is the complete authoritative state. Everything a client sees is a
// projection of this. Only the events package may mutate it.
type Game struct {
	Players []Player
	// Objs is a dense arena: Objs[i] has ID i+1, so ObjID 0 is "no object".
	Objs     []Object
	Stack    []ObjID
	Turn     int32
	Active   PlayerID
	Priority PlayerID
	// StartingPlayer is the seat that takes the first turn. HasStartingPlayer
	// keeps seat zero distinct from a game whose opening determination has not
	// completed (for example terminal genesis with no survivors). It is folded
	// only by events.StartingPlayerChange, so replay, Clone and snapshots retain
	// opening-hand effects that replace the toss result.
	StartingPlayer    PlayerID
	HasStartingPlayer bool
	Step              Step
	Passes            int32
	Over              bool
	Winner            PlayerID
	// Draw marks a game that ended with no surviving seats (CR 104.4a).
	// Winner's zero value is PlayerID(0), a real seat, so Over alone cannot
	// distinguish "seat 0 won" from "nobody did" -- Draw is what does.
	Draw bool
	// ExtraTurns counts, per seat, how many EXTRA turns (CR 500.7) that seat
	// still takes after the seat's current one, before turn order resumes
	// normally. Added by an api:AddTurn effect's ExtraTurn event (+Amount),
	// consumed by the turn structure (rules advanceStep emits Amount -1 and
	// repeats the same seat) -- so a log-only reconstruction folds the same
	// grants and consumptions to the same totals.
	ExtraTurns map[PlayerID]int
	// ExtraTurnQueue is the ORDERED pending extra turns, in creation order:
	// one entry per un-consumed ExtraTurn grant (+Amount event), appended on
	// the grant and removed (the seat's LAST entry) on the -1 consumption.
	// CR 500.7 takes multiple extra turns MOST RECENTLY CREATED FIRST, so the
	// turn structure consumes the queue from its end. Each entry retains the
	// grant's turn-specific rider (currently SkipUntap), which a per-seat count
	// cannot express. Every mutation rides the same events. Empty when no extra
	// turn is pending.
	ExtraTurnQueue []ExtraTurn
	// ExtraPhases is the ORDERED pending Forge AddPhaseEffect grants (DB$
	// AddPhase), in creation order, with the same fold discipline as
	// ExtraTurnQueue: appended by an api:AddPhase grant (+1 ExtraPhase event),
	// marked Consumed when the turn structure enters the extra phase (-1),
	// removed when the extra phase completes (-2), and cleared wholesale at
	// TurnChange -- an extra phase is spliced into the CURRENT turn only and
	// never survives into the next one. Empty when no extra phase is pending.
	ExtraPhases []ExtraPhase
	// CombatsThisTurn counts the combat phases BEGUN this turn (one per
	// BeginCombat StepChange, folded in events/apply.go's StepChange case,
	// reset at TurnChange): 1 through the ordinary combat, 2 through an extra
	// combat. It exists for ConditionFirstCombat$ (Raiyuu's "if it's the first
	// combat phase of the turn" AddPhase gate, effects/conditions.go), which
	// without it reads no state and would fail open, re-granting an extra
	// combat from every combat the first one created (a measured livelock). No
	// event or behaviour changes for any game without such a condition.
	CombatsThisTurn int32
	// ResolvedThisTurn counts, per resolving ability, how many times THAT
	// ability has resolved this turn, INCLUDING the resolution whose tally is
	// being read. It backs Forge's Count$ResolvedThisTurn (Sephiroth, Fabled
	// SOLDIER's "if this is the fourth time this ability has resolved this
	// turn, transform", Prowl's back face, Victor, Nissa, Tannuk, ...). The
	// key is events.ResolvedAbilityKey (source ObjID + the root Ability$ body's
	// content), so two T: lines sharing one Execute$ SVar count into one
	// tally exactly as the oracle's "this ability" means. It is incremented by
	// events.Apply's Resolve case -- folded from the existing Resolve event, no
	// new Kind or field -- and zeroed at TurnChange, the same replay-exact
	// shape CombatsThisTurn uses, so a log-only replay rebuilds the identical
	// tally without a new event. Nil until the first resolution of a game.
	ResolvedThisTurn map[string]int32
	// Monarch is the current monarch when HasMonarch is true. The presence bit
	// keeps seat zero distinct from no monarch.
	Monarch    PlayerID
	HasMonarch bool
	// TurnStartMonarch is the seat that held the monarch designation when the
	// CURRENT turn began, with HasTurnStartMonarch its presence bit. It is
	// snapshotted from Monarch/HasMonarch at every TurnChange (events/apply.go),
	// exactly the CombatsThisTurn shape, so it is rebuilt identically by replay
	// without any new event or event field. It backs the trig:BecomeMonarch
	// intervening-if BeginTurn$ You -- "if you were the monarch as the turn
	// began" (CR 603.4: the condition is evaluated as the turn began, not when
	// the trigger later resolves) -- whose only corpus carrier is Knights of
	// the Black Rose.
	TurnStartMonarch    PlayerID
	HasTurnStartMonarch bool
	// NextID hands out object ids one at a time, starting at 1 (see NewGame)
	// and incrementing by exactly one per AddObject call below -- it can
	// never reach playerRefBit (1<<31, ids.go): a single match would need
	// over two billion objects created before that collision, far beyond
	// anything this engine's own bounds (maxTriggerFires, turn/priority
	// limits, ...) let happen. That gap is what makes PlayerRef/ObjID.PlayerRef
	// a safe way to smuggle a PlayerID through an []ObjID slot. Note that
	// int(id) of such a sentinel is negative on 32-bit builds (1<<31
	// overflows int there), so g.Obj must compare id as an ObjID/uint32 --
	// the id&playerRefBit check in its bounds test below -- never via
	// int(id) alone.
	NextID ObjID
	// Clock is a monotonic timestamp source for continuous-effect ordering.
	Clock uint32

	// Tokens is the token definitions this match may create, keyed by
	// Forge script stem; set at genesis, never mutated, so Clone shares it.
	Tokens map[string]*cards.Card
	// NameUniverse is the immutable compiled card-name universe used by
	// NameCard choices. It is supplied by the embedder and shared by clones.
	NameUniverse []*cards.Card
	// NameUniverseNames is its sorted, distinct primary-face-name snapshot.
	// A persisted match supplies it on replay so a later corpus update cannot
	// renumber a NameCard decision's options.
	NameUniverseNames []string

	// Delayed holds delayed-trigger registrations (CR 603.7: Mode$ Phase, or
	// the event-matched shape a DelayedTrigger.EventMode names) that have not
	// yet fired. It is game state -- a delayed trigger is
	// registered during one resolution and fires later, in general a
	// different turn, so the registration has to survive the event log to
	// survive replay, and only events.Apply may write it. Each entry records
	// the phase it fires in, the source object whose SVar table carries the
	// Execute$ ability, the controller, and the Remembered captured at
	// registration. Firing is one-shot: the DelayedPush event removes the
	// entry, so a delayed trigger fires exactly once. DelayedNext hands out
	// the registration IDs monotonically so a firing can remove precisely the
	// registration it belongs to (a source may register several).
	Delayed     []DelayedTrigger
	DelayedNext uint32

	// zones is indexed by zoneIndex(z, p); the stack lives in Stack instead.
	zones [][]ObjID

	// Entered holds, in move order, every zone entry made this turn -- the
	// per-add list Forge keeps per zone (CardUtil.getThisTurnEntered reads
	// it). It is derived exclusively inside events.Apply (each Move appends
	// one entry; the TurnChange reset clears it) so a replay folds the
	// identical list. Objects that ceased (ZCeased) stay listed: Forge's
	// list keeps the card too, and validity is evaluated against the object
	// wherever it now lives.
	Entered []ZoneEntry
}

// ZoneEntry is one zone entry made this turn: the object that moved, the
// zone it entered (To) and the zone it actually came from (From -- the real
// pre-move zone, the same value events.Move recorded on the object's
// EnteredFrom). A card that entered a zone twice (bounced and replayed) is
// listed once per entry, exactly like Forge's per-zone
// getCardsAddedThisTurn lists, which the Count$ThisTurnEntered_* heads and
// the ThisTurnEntered* filter predicates read.
type ZoneEntry struct {
	Obj  ObjID
	To   Zone
	From Zone
	// Owner and PermanentCard capture descend provenance at the move. A
	// later control change, copy effect or zone change cannot rewrite which
	// player's graveyard received a permanent card this turn (CR 700.11).
	Owner         PlayerID
	PermanentCard bool
	// Sacrificed marks a battlefield departure made under the sacrifice
	// action marker (events.IsSacrifice, CR 701.21a), and Sacrificer is the
	// permanent's controller at that instant -- the player who sacrificed
	// it (a stolen permanent its taker sacrifices is the taker's). Both are
	// folded by events.Apply's MoveZone case, so the
	// PlayerCount*$SacrificedThisTurn heads read a replay-derived record.
	Sacrificed bool
	Sacrificer PlayerID
}

// DelayedTrigger is one registered delayed triggered ability awaiting its
// phase -- or, for an event-matched registration (EventMode set), awaiting its
// triggering event. It is reconstructed from the event log by events.Apply's
// DelayedRegister case, so a replay that folds the logged registrations
// arrives at the same set. Firing (events.Apply's DelayedPush case) removes
// the entry, which is what keeps a delayed trigger one-shot.
type DelayedTrigger struct {
	ID         uint32
	Phase      Step
	Source     ObjID
	Controller PlayerID
	Execute    string // the SVar name of the ability to run when it fires
	Remembered []Target
	// MinTurn is the earliest game turn the trigger may fire in (zero = no
	// bound). rules' delayed-trigger scan skips an entry whose MinTurn is
	// still ahead of the current turn, which is how an extra-turn grant's
	// end-step trigger (Final Fortune) skips the granting turn's own end
	// step and fires in the granted turn instead. Folded from the
	// registering event's Amount, so a replay rebuilds it.
	MinTurn int32
	// EventMode and Trigger extend the registration to the non-phase
	// (event-matched) shape: EventMode is the trigger Mode$ the registration
	// fires on ("SpellCast" -- the first spell cast whose event satisfies the
	// body's validity clauses) and Trigger is the SVar name on the source's
	// face holding that trigger body, re-parsed at fire time so ValidCard$/
	// ValidActivatingPlayer$ are evaluated against the actual cast. Both are
	// empty for a Mode$ Phase registration, so every already-registered shape
	// is unchanged; the pair travels inside the DelayedRegister event's Text
	// field ("<Mode>:<Trigger>") because the event gains no fields.
	EventMode string
	Trigger   string
	// EffectRepeat marks a turn-scoped api:Effect trigger. Unlike a CR 603.7
	// one-shot promise, its registration survives each firing until MaxTurn.
	EffectRepeat bool
	// ValidPlayer is the registering DelayedTrigger SA's ValidPlayer$ value
	// when it has one (Necropotence's "You": "put that card into your hand
	// at the beginning of YOUR next end step"). The rules-side delayed
	// trigger scan gates the fire on it -- the phase occurrence must satisfy
	// the spec against the registration's controller -- and a phase the gate
	// fails leaves the one-shot registration pending for the first later
	// occurrence that matches. Empty keeps the ungated fire every earlier
	// registration had. It rides in the DelayedRegister event's Text
	// ("<Phase>|VP=<value>") because the event gains no field (Ruling T20-a's
	// field-reuse precedent); the SpellCast decode never collides because a
	// Phase$ value contains no "|".
	ValidPlayer string
	// PresentSpec, PresentZone and PresentCompare are the registering
	// DelayedTrigger SA's IsPresent$/PresentZone$/PresentCompare$ condition
	// when it has one (Bank Job's "at the beginning of the next end step, if
	// that card is still exiled"). The rules-side delayed-trigger scan gates
	// the fire on it at the phase occurrence: the count of objects in the
	// named zone matching PresentSpec with this registration's Remembered
	// bound as the IsTriggerRemembered referent must satisfy PresentCompare
	// (default GE1). A gate the step fails leaves the one-shot registration
	// pending for the first later occurrence that matches, exactly like
	// ValidPlayer. Empty PresentSpec keeps the ungated fire every earlier
	// registration had. The triple rides the DelayedRegister event's Text
	// ("<Phase>|VP=<value>|IP=<spec>|PZ=<zone>|PC=<compare>") because the
	// event gains no field (Ruling T20-a's field-reuse precedent); the values
	// contain no "|", so the LastIndex strips are exact and an already-logged
	// registration without the suffixes decodes exactly as before (all three
	// empty, every presence gate skipped).
	PresentSpec    string
	PresentZone    string
	PresentCompare string
	// MaxTurn is the LATEST game turn the trigger may fire in (zero = no
	// bound) — the mirror of MinTurn for a ThisTurn$ True registration
	// (Mistrise Village's "the next spell you cast this turn can't be
	// countered"): a registration whose turn has passed without firing is
	// skipped by rules' delayed-trigger scans forever after, never removed
	// (removal would need an event of its own to keep a replay folding the
	// same set, so the expired entry just stays inert). It rides the
	// registering event's Text ("|TT=<turn>") because the event gains no
	// field (Ruling T20-a's field-reuse precedent).
	MaxTurn int32
	// OptionalSpec is an api:Effect Triggers$ body's OptionalDecider$ spec
	// (Beck's "whenever a creature enters this turn, you may draw a card").
	// An Effect trigger whose body is optional must be REGISTERED like any
	// other Effect trigger and ask its yes/no when the minted ability
	// resolves; withholding it fires the body mandatorily, the opposite of
	// the card text. The spec has to be captured at registration time rather
	// than re-read from the trigger body at fire time because a Mode$ Phase
	// registration's body is never re-parsed (checkDelayedTriggers fires it
	// on the step alone), so it rides the DelayedRegister event's Text
	// ("|OD=<spec>") the same way ValidPlayer$ and MaxTurn do. Empty for
	// every registration with no election.
	OptionalSpec string
	// Effect-created trigger lifetime riders, folded from DelayedRegister.Text.
	// BirthTurn distinguishes a later turn belonging to the effect controller
	// from the turn in which the promise was created.
	EffectDuration string
	BirthTurn      int32
	ForgetOnMoved  string
	ExileOnMoved   string
	ForgetCounter  string
	ForgetOnCast   string
	ImprintOnHost  bool
	// SourceIncarnation is captured for keyword promises whose effect applies
	// to that exact permanent (dash/warp). Ordinary CR 603.7 delayed triggers,
	// including Encore's group cleanup, intentionally leave TrackSource false:
	// they exist independently of their source after registration.
	SourceIncarnation uint32
	TrackSource       bool
	// SourceBattlefield records whether the source was a battlefield
	// permanent at REGISTRATION time. A Duration$ Permanent api:Effect
	// trigger's source-relative ending (CR 611.2: the effect ends when its
	// source leaves the battlefield) applies only to such a source; an
	// opening-hand Effect (Chancellor of the Annex, source in hand) or an
	// emblem/command-zone source has no battlefield incarnation to lose, so
	// its Permanent promise is unbounded and must not be killed by the
	// battlefield liveness rule. It rides the DelayedRegister event's Text
	// ("|SB") because the event gains no field (Ruling T20-a).
	SourceBattlefield bool
}

const startingLife = 20

// NewGame builds a game with the default starting life total (20). It keeps
// the every-existing-caller spelling: NewGameLife is the life-taking variant,
// so callers that never set a life total observe exactly what they always
// did.
func NewGame(names []string) *Game { return NewGameLife(names, startingLife) }

// NewGameLife is NewGame with an explicit starting life total (Config.
// StartingLife's 0-means-20 convention is resolved by the caller).
func NewGameLife(names []string, life int32, objectCapacity ...int) *Game {
	capacity := 0
	if len(objectCapacity) > 0 && objectCapacity[0] > 0 {
		capacity = objectCapacity[0]
	}
	return NewGameInto(names, life, capacity, nil)
}

// NewGameInto is NewGameLife with an explicit object capacity, backed by a
// spent object arena a batch runner recycled from a finished game
// (rules.Engine.Release). The arena is reused only when it can hold capacity
// objects, and it is re-capped to exactly capacity, so Objs grows at the same
// points a fresh arena would. AddObject overwrites every slot it claims with
// a fresh Object, so spare's contents are never read; its caller must hold no
// other reference into it.
func NewGameInto(names []string, life int32, capacity int, spare []Object) *Game {
	objs := spare[:0]
	if capacity > 0 && cap(objs) >= capacity {
		objs = objs[:0:capacity]
	} else {
		objs = make([]Object, 0, capacity)
	}
	g := &Game{NextID: 1, Objs: objs, zones: make([][]ObjID, numZones*len(names))}
	for i, n := range names {
		g.Players = append(g.Players, Player{ID: PlayerID(i), Name: n, Life: life})
	}
	return g
}

func (g *Game) zoneIndex(z Zone, p PlayerID) int { return int(p)*numZones + int(z) }

func (g *Game) Zone(z Zone, p PlayerID) []ObjID {
	if z == ZCeased {
		return nil
	}
	if z == ZStack {
		return g.Stack
	}
	return g.zones[g.zoneIndex(z, p)]
}

func (g *Game) SetZone(z Zone, p PlayerID, ids []ObjID) {
	if z == ZCeased {
		return
	}
	if z == ZStack {
		g.Stack = ids
		return
	}
	g.zones[g.zoneIndex(z, p)] = ids
}

// Obj returns the object with this ID, or nil for ObjID 0, a PlayerRef
// sentinel (ids.go), and out-of-range IDs. The sentinel check must compare
// id as an ObjID/uint32: int(id) of a PlayerRef is negative on 32-bit
// builds, so a bare int(id) > len(g.Objs) test would let it through and
// index Objs with a huge negative offset.
func (g *Game) Obj(id ObjID) *Object {
	if id == 0 || id&playerRefBit != 0 || int(id) > len(g.Objs) {
		return nil
	}
	return &g.Objs[id-1]
}

func (g *Game) AddObject(card *cards.Card, owner PlayerID) *Object {
	o := Object{ID: g.NextID, Card: card, Owner: owner, Controller: owner, Zone: ZLibrary}
	g.NextID++
	g.Objs = append(g.Objs, o)
	return &g.Objs[len(g.Objs)-1]
}

// cloneObjectHeadroom is the spare Objs capacity Clone gives a copy.
const cloneObjectHeadroom = 8

// Clone deep-copies the game. Everything is slices of value types, so this is
// a handful of copy() calls rather than a graph walk. Player's Commander
// bookkeeping slices (Commanders/CmdCasts/CmdDamage) are deep-copied as well
// -- they are sized once at genesis and written throughout a match, so a
// clone that shared the live one's backing arrays would let either evolve
// and silently corrupt the other. Game.Clone is the hottest path in the
// engine; three small copy() calls per seat (nil slices cost nothing) is the
// whole price.
func (g *Game) Clone() *Game {
	c := *g
	c.Players = make([]Player, len(g.Players))
	for i := range g.Players {
		c.Players[i] = g.Players[i]
		c.Players[i].Counters = append([]Counter(nil), g.Players[i].Counters...)
		c.Players[i].Commanders = append([]ObjID(nil), g.Players[i].Commanders...)
		c.Players[i].CmdCasts = append([]int32(nil), g.Players[i].CmdCasts...)
		c.Players[i].CmdDamage = append([]int32(nil), g.Players[i].CmdDamage...)
		c.Players[i].DamageTakenByGame = append([]ObjID(nil), g.Players[i].DamageTakenByGame...)
		c.Players[i].RestrictedMana = append([]ManaRestriction(nil), g.Players[i].RestrictedMana...)
		c.Players[i].Notes = append([]string(nil), g.Players[i].Notes...)
	}
	// A little spare capacity: a clone that mints even one object (a token,
	// a copy on the stack) would otherwise regrow the whole arena through
	// append -- a second copy of every ~1 KB Object, double-sized -- and the
	// search clones a mid-game root once per simulation (measured: that
	// regrow was 11% of all bytes the AlphaZero search allocated). Capacity
	// only; AddObject's IDs and every reader's view of Objs are unchanged.
	c.Objs = make([]Object, len(g.Objs), len(g.Objs)+cloneObjectHeadroom)
	for i := range g.Objs {
		c.Objs[i] = g.Objs[i].CloneDeep()
	}
	c.Stack = append([]ObjID(nil), g.Stack...)
	// Entered is appended to in place by every Move (one entry per zone
	// entry this turn), so a clone must own its own copy -- sharing the
	// backing array would let either evolve and corrupt the other (the same
	// rule as Delayed below and Stack above).
	c.Entered = append([]ZoneEntry(nil), g.Entered...)
	// Delayed is written in place (append on registration, remove on firing),
	// so a clone must own its own registrations -- sharing the live one's
	// backing array would let either evolve and corrupt the other (the same
	// rule as every other mutated slice in this struct). Each entry's
	// Remembered slice is deep-copied too. DelayedNext is a plain value and
	// travels with the struct copy above.
	if g.Delayed != nil {
		c.Delayed = make([]DelayedTrigger, len(g.Delayed))
		for i, dt := range g.Delayed {
			dt.Remembered = append([]Target(nil), dt.Remembered...)
			c.Delayed[i] = dt
		}
	}
	c.zones = make([][]ObjID, len(g.zones))
	for i, z := range g.zones {
		if z != nil {
			c.zones[i] = append([]ObjID(nil), z...)
		}
	}
	if g.ExtraTurns != nil {
		c.ExtraTurns = make(map[PlayerID]int, len(g.ExtraTurns))
		for p, n := range g.ExtraTurns {
			c.ExtraTurns[p] = n
		}
	}
	// ResolvedThisTurn is incremented in place by events.Apply's Resolve case,
	// so a clone must own its own map -- sharing the live one's backing map
	// would let a clone's resolution corrupt the original's per-ability tally
	// (the same rule as ExtraTurns above and every other in-place slice/map).
	if g.ResolvedThisTurn != nil {
		c.ResolvedThisTurn = make(map[string]int32, len(g.ResolvedThisTurn))
		for k, n := range g.ResolvedThisTurn {
			c.ResolvedThisTurn[k] = n
		}
	}
	c.ExtraTurnQueue = append([]ExtraTurn(nil), g.ExtraTurnQueue...)
	c.ExtraPhases = append([]ExtraPhase(nil), g.ExtraPhases...)
	return &c
}

// AliveFrom lists surviving seats in APNAP order starting at start.
func (g *Game) AliveFrom(start PlayerID) []PlayerID {
	n := PlayerID(len(g.Players))
	out := make([]PlayerID, 0, n)
	for i := PlayerID(0); i < n; i++ {
		p := (start + i) % n
		if !g.Players[p].Lost {
			out = append(out, p)
		}
	}
	return out
}

func (g *Game) AliveCount() int { return len(g.AliveFrom(0)) }

// IsMonarch reports whether p currently holds the monarch designation.
func (g *Game) IsMonarch(p PlayerID) bool { return g.HasMonarch && g.Monarch == p }

// WasMonarchAtTurnStart reports whether p held the monarch designation when
// the current turn began (the trig:BecomeMonarch BeginTurn$ intervening-if).
func (g *Game) WasMonarchAtTurnStart(p PlayerID) bool {
	return g.HasTurnStartMonarch && g.TurnStartMonarch == p
}

// IsRingBearer reports whether id is p's Ring-bearer (CR 701.54e: the
// creature is "your Ring-bearer" exactly while it is on the battlefield
// under your control and carries the designation — the zone and control
// halves are enforced by events.Apply's clears, so a live check is just the
// id comparison, and a zero id is never a bearer).
func (g *Game) IsRingBearer(p PlayerID, id ObjID) bool {
	return id != 0 && g.Players[p].RingBearer == id
}

// IsStartingPlayer reports whether p currently holds the CR 103.1 first-turn
// designation. The presence bit makes the zero seat unambiguous.
func (g *Game) IsStartingPlayer(p PlayerID) bool {
	return g.HasStartingPlayer && g.StartingPlayer == p
}

// NextAlive returns the next surviving seat after p, or p itself if none is.
func (g *Game) NextAlive(p PlayerID) PlayerID {
	n := PlayerID(len(g.Players))
	for i := PlayerID(1); i <= n; i++ {
		q := (p + i) % n
		if !g.Players[q].Lost {
			return q
		}
	}
	return p
}
