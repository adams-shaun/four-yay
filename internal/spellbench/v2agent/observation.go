package v2agent

// The observation of spec Section 6, typed. Every field the spec lists is
// always present on the wire; nullable ones are pointers or slices/maps
// (a JSON null decodes to nil, "[]" or "{}" to an empty non-nil value, so a
// decode followed by an encode reproduces the original exactly).
//
// Integer ranges follow spec 4.4: counters and identifiers are int64
// ([0, 2^53-1]), indices, counts and amounts uint32, life, power and
// toughness int32.

// ObjectRef is an object reference (spec 5.1). CardName is nil for an
// object whose identity is hidden from the observing seat.
type ObjectRef struct {
	ObjectID       string  `json:"object_id"`
	CardName       *string `json:"card_name"`
	OwnerSeat      string  `json:"owner_seat"`
	ControllerSeat string  `json:"controller_seat"`
	Zone           string  `json:"zone"`
}

// TargetRef is exactly one of {"player": seat} or {"object": ObjectRef}
// (spec 5.2).
type TargetRef struct {
	Player *string    `json:"player,omitempty"`
	Object *ObjectRef `json:"object,omitempty"`
}

// IsPlayer reports whether the reference names a player.
func (t *TargetRef) IsPlayer() bool { return t != nil && t.Player != nil }

// Characteristics are an object's current characteristics (spec 6.4).
// Keywords is nil when the engine does not declare the keywords flag.
type Characteristics struct {
	Supertypes []string `json:"supertypes"`
	Types      []string `json:"types"`
	Subtypes   []string `json:"subtypes"`
	Colors     []string `json:"colors"`
	ManaValue  uint32   `json:"mana_value"`
	Power      *int32   `json:"power"`
	Toughness  *int32   `json:"toughness"`
	Keywords   []string `json:"keywords"`
}

// HasType reports whether the characteristics list the card type t.
func (c *Characteristics) HasType(t string) bool {
	if c == nil {
		return false
	}
	for _, x := range c.Types {
		if x == t {
			return true
		}
	}
	return false
}

// Chosen is one public value chosen for a permanent (spec 6.4, 6.10).
type Chosen struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Permanent holds the battlefield-only fields of an object record (spec
// 6.4). Statuses, ClassLevel and Chosen are nil unless the engine declares
// permanent_details.
type Permanent struct {
	Tapped           bool              `json:"tapped"`
	SummoningSick    bool              `json:"summoning_sick"`
	Damage           uint32            `json:"damage"`
	Counters         map[string]uint32 `json:"counters"`
	AttachedTo       *TargetRef        `json:"attached_to"`
	Attacking        bool              `json:"attacking"`
	AttackTarget     *TargetRef        `json:"attack_target"`
	Blocking         bool              `json:"blocking"`
	BlockedAttackers []ObjectRef       `json:"blocked_attackers"`
	PhasedOut        bool              `json:"phased_out"`
	Statuses         []string          `json:"statuses"`
	ClassLevel       *uint32           `json:"class_level"`
	Chosen           []Chosen          `json:"chosen"`
}

// ObjectRecord is an object reference plus the record fields of spec 6.4.
type ObjectRecord struct {
	ObjectRef
	FullName        *string          `json:"full_name"`
	FaceDown        bool             `json:"face_down"`
	Token           bool             `json:"token"`
	Copy            bool             `json:"copy"`
	Characteristics *Characteristics `json:"characteristics"`
	Permanent       *Permanent       `json:"permanent"`
	ExiledBy        *ObjectRef       `json:"exiled_by"`
}

// ManaPool is a player's floating mana (spec 6.3); always all six keys.
type ManaPool struct {
	W uint32 `json:"W"`
	U uint32 `json:"U"`
	B uint32 `json:"B"`
	R uint32 `json:"R"`
	G uint32 `json:"G"`
	C uint32 `json:"C"`
}

// Total is the pool's mana of every symbol.
func (m ManaPool) Total() uint32 { return m.W + m.U + m.B + m.R + m.G + m.C }

// Progress is a player's optional progress (spec 6.3).
type Progress struct {
	Dungeon     *string `json:"dungeon"`
	DungeonRoom *string `json:"dungeon_room"`
	RingTempted uint32  `json:"ring_tempted"`
	Speed       *uint32 `json:"speed"`
}

// Player is one entry of observation.players (spec 6.3). Hand is nil for
// the other seat; Poison, Counters, Designations and Progress are nil when
// their flags are false.
type Player struct {
	Seat                string            `json:"seat"`
	Life                int32             `json:"life"`
	Poison              *uint32           `json:"poison"`
	Counters            map[string]uint32 `json:"counters"`
	ManaPool            ManaPool          `json:"mana_pool"`
	LandsPlayedThisTurn uint32            `json:"lands_played_this_turn"`
	MulligansTaken      uint32            `json:"mulligans_taken"`
	Designations        []string          `json:"designations"`
	Progress            *Progress         `json:"progress"`
	HandCount           uint32            `json:"hand_count"`
	LibraryCount        uint32            `json:"library_count"`
	Hand                []ObjectRecord    `json:"hand"`
	Battlefield         []ObjectRecord    `json:"battlefield"`
	Graveyard           []ObjectRecord    `json:"graveyard"`
	Exile               []ObjectRecord    `json:"exile"`
	Command             []ObjectRecord    `json:"command"`
}

// StackEntry is one entry of observation.stack (spec 6.5); index 0 of the
// stack is the bottom. Targets may hold nil for a target that left.
type StackEntry struct {
	ObjectRef
	StackKind       string           `json:"stack_kind"`
	Source          *ObjectRef       `json:"source"`
	FaceDown        bool             `json:"face_down"`
	Copy            bool             `json:"copy"`
	Characteristics *Characteristics `json:"characteristics"`
	Targets         []*TargetRef     `json:"targets"`
	Divided         []uint32         `json:"divided"`
	Modes           []uint32         `json:"modes"`
	XValue          *uint32          `json:"x_value"`
	Text            *string          `json:"text"`
}

// PendingTrigger is one entry of observation.pending_triggers (spec 6.6).
type PendingTrigger struct {
	Source         *ObjectRef `json:"source"`
	SourceName     *string    `json:"source_name"`
	ControllerSeat string     `json:"controller_seat"`
	Label          *string    `json:"label"`
	Optional       bool       `json:"optional"`
}

// KnownEntry is one entry of observation.known (spec 6.7): name-level
// knowledge of a card in a hidden zone.
type KnownEntry struct {
	OwnerSeat          string  `json:"owner_seat"`
	Zone               string  `json:"zone"`
	CardName           string  `json:"card_name"`
	ObjectID           *string `json:"object_id"`
	PositionFromTop    *uint32 `json:"position_from_top"`
	PositionFromBottom *uint32 `json:"position_from_bottom"`
	How                string  `json:"how"`
}

// Observation is the acting seat's information state (spec 6.2).
// PassedSeats, DayNight and PendingTriggers are nil when their flags are
// false; Known is always an array.
type Observation struct {
	Viewer          string           `json:"viewer"`
	Turn            int64            `json:"turn"`
	PhaseStep       string           `json:"phase_step"`
	ActiveSeat      *string          `json:"active_seat"`
	PrioritySeat    *string          `json:"priority_seat"`
	PassedSeats     []string         `json:"passed_seats"`
	DayNight        *string          `json:"day_night"`
	Players         []Player         `json:"players"`
	Stack           []StackEntry     `json:"stack"`
	PendingTriggers []PendingTrigger `json:"pending_triggers"`
	Known           []KnownEntry     `json:"known"`
}

// Player returns the entry for seat, or nil.
func (o *Observation) Player(seat string) *Player {
	if o == nil {
		return nil
	}
	for i := range o.Players {
		if o.Players[i].Seat == seat {
			return &o.Players[i]
		}
	}
	return nil
}

// Me is the viewer's own player entry, or nil.
func (o *Observation) Me() *Player {
	if o == nil {
		return nil
	}
	return o.Player(o.Viewer)
}

// Opponent is the other seat's player entry, or nil.
func (o *Observation) Opponent() *Player {
	if o == nil {
		return nil
	}
	return o.Player(OtherSeat(o.Viewer))
}

// Object finds the record with objectID in any zone array, or nil. Stack
// entries and known cards are not object records; see StackEntry and
// KnownEntry.
func (o *Observation) Object(objectID string) *ObjectRecord {
	if o == nil {
		return nil
	}
	for i := range o.Players {
		p := &o.Players[i]
		for _, zone := range [][]ObjectRecord{p.Hand, p.Battlefield, p.Graveyard, p.Exile, p.Command} {
			for j := range zone {
				if zone[j].ObjectID == objectID {
					return &zone[j]
				}
			}
		}
	}
	return nil
}

// OtherSeat maps "p0" to "p1" and anything else to "p0".
func OtherSeat(seat string) string {
	if seat == "p0" {
		return "p1"
	}
	return "p0"
}
