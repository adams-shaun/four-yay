package state

import "math"

type (
	ObjID    uint32
	PlayerID uint8
	Zone     uint8
	Step     uint8
)

// playerRefBit marks an ObjID that does not name a real object at all, but
// instead carries a PlayerID -- for a caller (rules.pushTrigger, for a
// DeclareAttackers-driven Attacks trigger's defending-player entry) that
// needs to smuggle a player reference through a slot that can only ever
// hold object ids (events.Event.IDs, and therefore the TriggerPush event
// that persists a pending trigger's Remembered onto the stack). See
// Game.NextID/AddObject for why no real object id can ever collide with it.
const playerRefBit ObjID = 1 << 31

// PlayerRef encodes p as an ObjID carrying playerRefBit, so it can travel
// through an []ObjID (an events.Event's IDs field) alongside real object
// ids without a schema change. Decode it back with ObjID.PlayerRef.
func PlayerRef(p PlayerID) ObjID { return playerRefBit | ObjID(p) }

// PlayerRef reports whether id was produced by the package-level PlayerRef
// and, if so, the PlayerID it carries. A real object id -- everything
// AddObject ever hands out -- always reports false; see NextID's own
// comment for why the two ranges can never overlap.
func (id ObjID) PlayerRef() (PlayerID, bool) {
	if id&playerRefBit == 0 {
		return 0, false
	}
	return PlayerID(id &^ playerRefBit), true
}

const (
	ZLibrary Zone = iota
	ZHand
	ZBattlefield
	ZGraveyard
	ZExile
	ZStack
	// ZCommand is the command zone (CR 903.6). It is appended AFTER ZStack, not
	// in Magic's conventional order, so every earlier zone constant keeps its
	// numeric value -- no serialized event log and no chain head shifts from
	// renumbering. It is public information: every seat sees it, so Hidden()
	// stays false for it.
	ZCommand
	// ZCeased is an inert tombstone location for objects that have ceased to
	// exist. It is appended so every serialized value above remains stable.
	// Unlike game zones, it deliberately has no membership list; see Game.Zone.
	ZCeased
	// ZSideboard is appended after all historical values so serialized zones
	// and event hashes remain stable. Sideboards are private to their owner.
	ZSideboard
	// ZPlanarDeck is the private, face-down planar deck. Its current plane is
	// represented by the face-up first card in the ordered zone.
	ZPlanarDeck
	numZones = int(ZPlanarDeck) + 1
)

var zoneNames = [numZones]string{"library", "hand", "battlefield", "graveyard", "exile", "stack", "command", "ceased", "sideboard", "planar_deck"}

func (z Zone) String() string { return zoneNames[z] }

// Valid reports whether z is one of the defined zone constants. Zone is
// unsigned, so only the upper bound needs checking. Callers that index
// game state by a Zone coming from outside this package (an event, a
// network message) must check Valid first: an out-of-range Zone reaches
// Game's zone-index arithmetic and panics with index-out-of-range.
func (z Zone) Valid() bool { return int(z) < numZones }

// Hidden reports whether a zone's contents are private to its owner. View
// projection and event redaction both key off this.
func (z Zone) Hidden() bool {
	return z == ZLibrary || z == ZHand || z == ZSideboard || z == ZPlanarDeck
}

const (
	StepUntap Step = iota
	StepUpkeep
	StepDraw
	StepMain1
	StepBeginCombat
	StepDeclareAttackers
	StepDeclareBlockers
	StepCombatDamage
	StepEndCombat
	StepMain2
	StepEnd
	StepCleanup
	numSteps = int(StepCleanup) + 1
)

var stepNames = [numSteps]string{"untap", "upkeep", "draw", "main1",
	"begin-combat", "declare-attackers", "declare-blockers", "combat-damage",
	"end-combat", "main2", "end", "cleanup"}

// Valid reports whether s is one of the defined step constants. Mirrors
// Zone.Valid: a Step read back from an untrusted source (a tampered log,
// events/apply.go's StepChange case stores e.Step with no check of its own)
// must be validated before it indexes stepNames, or Project (view.go) and
// this package's own String below would panic on it.
func (s Step) Valid() bool { return int(s) < numSteps }

// String returns "unknown" for an out-of-range Step rather than panicking,
// the same total shape as events.Kind.String().
func (s Step) String() string {
	if !s.Valid() {
		return "unknown"
	}
	return stepNames[s]
}

// IsMain reports whether sorcery-speed actions are legal in this step.
func (s Step) IsMain() bool { return s == StepMain1 || s == StepMain2 }

// Mana indices. A fixed array rather than a map keeps the pool copyable and
// iteration order deterministic.
const (
	MW = iota
	MU
	MB
	MR
	MG
	MC
	numMana
)

type Mana [numMana]int32

// The first four typed pool slots retain their historical indices. Slots 4-6
// encode the orthogonal Artifact bit on a Treasure/Cave/Desert mana unit;
// these are exclusive per-unit slots, never additional pool units.
const (
	TypedTreasure = 0
	TypedCave     = 1
	TypedDesert   = 2
	TypedArtifact = 3
	// The high bit of a producer tag is orthogonal Artifact provenance.
	TypedArtifactTreasure = 4
	TypedArtifactCave     = 5
	TypedArtifactDesert   = 6
)

// TypedManaTags names the four public producer-type predicates, in stable
// order. ManaUnitTags below is the exclusive pool/event encoding.
var TypedManaTags = [4]string{"Treasure", "Cave", "Desert", "Artifact"}

// ManaUnitTags partitions the pool into exclusive units. The final three
// encode an Artifact bit alongside the original Treasure/Cave/Desert type.
// TypedManaTags remains the four public type names, not these wire spellings.
var ManaUnitTags = [7]string{"Treasure", "Cave", "Desert", "Artifact", "ArtifactTreasure", "ArtifactCave", "ArtifactDesert"}

// ManaUnitTypes maps an exclusive unit to every type it represents.
func ManaUnitTypes(unit int) (base int, artifact bool) {
	if unit >= TypedArtifactTreasure && unit <= TypedArtifactDesert {
		return unit - TypedArtifactTreasure, true
	}
	return unit, unit == TypedArtifact
}

// TypedManaSpentByTag maps a TypedManaTags index to the carried spent count
// on an object. It is the ONE mapping the filtered Count$CastTotalManaSpent
// <Type> head reads, so a type is "modelled" exactly when it appears in
// TypedManaTags: adding a tag there extends the head automatically, and a
// type outside the table stays the fail-closed 0.
func (o *Object) TypedManaSpentByTag(tag int) int32 {
	switch tag {
	case TypedTreasure:
		return o.ManaTreasureSpent
	case TypedCave:
		return o.ManaCaveSpent
	case TypedDesert:
		return o.ManaDesertSpent
	case TypedArtifact:
		return o.ManaArtifactSpent
	}
	return 0
}

// TypedManaCounter reports the TypedMana index and pool slot of a
// "<Tag><colour>" tagged ManaAdd counter (ManaAdd's typed form: "TreasureC",
// "CaveW", "DesertR"). ok is false for every other counter shape -- a bare
// WUBRGC letter, the empty default, or the "S<colour>" snow form.
func TypedManaCounter(counter string) (tag int, slot int, ok bool) {
	for ti, t := range ManaUnitTags {
		if len(counter) == len(t)+1 && counter[:len(t)] == t {
			return ti, ManaIndex(counter[len(t)]), true
		}
	}
	return 0, 0, false
}

// ManaSlot maps a ManaAdd counter form to its pool slot: a bare WUBRGC
// letter (or the empty default), a "S<colour>" snow unit, or a
// "<Tag><colour>" typed unit. It is the ONE slot reader for a counter that
// may carry any of the three historically encodable forms -- a restricted
// batch stores its producing counter verbatim, so state.ManaIndex(c[0])
// would read a tagged unit's type letter as a colour.
func ManaSlot(counter string) int {
	if counter == "" {
		return MC
	}
	if len(counter) == 2 && counter[0] == 'S' {
		return ManaIndex(counter[1])
	}
	if _, slot, ok := TypedManaCounter(counter); ok {
		return slot
	}
	return ManaIndex(counter[0])
}

// ManaIndex maps a WUBRGC symbol to its pool slot. Returns MC for anything
// unrecognised, which is the safe default for colourless-producing lands.
func ManaIndex(sym byte) int {
	switch sym {
	case 'W':
		return MW
	case 'U':
		return MU
	case 'B':
		return MB
	case 'R':
		return MR
	case 'G':
		return MG
	}
	return MC
}

// Total returns the sum of the six mana slots, saturated to the int32 range.
// A projection may intentionally set several slots to math.MaxInt32 to
// represent an indeterminate source's unbounded production; accumulating in
// int64 keeps that conservative bound from wrapping negative during a cost
// affordability check.
func (m Mana) Total() int32 {
	var n int64
	for _, v := range m {
		n += int64(v)
	}
	if n >= math.MaxInt32 {
		return math.MaxInt32
	}
	if n <= math.MinInt32 {
		return math.MinInt32
	}
	return int32(n)
}
