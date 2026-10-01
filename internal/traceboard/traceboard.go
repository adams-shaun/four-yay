// Package traceboard is the decision-trace board schema: the redacted,
// deterministic per-seat board snapshot `cmd/botbench` writes into every
// decision-trace record and `cmd/searchteacher` writes into every label
// record. It is deliberately ONE shared type so a consumer can decode both
// corpora with the same Go type; the schema (field names, tags, ordering
// guarantees) is v1 and must not drift between the two writers.
//
// Every list is built in sorted, map-free order, so a projection of the same
// board is byte-identical run to run.
package traceboard

import (
	"sort"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/state"
)

// SchemaVersion is the board schema version carried by every trace and label
// record; it bumps only when the field set below changes meaningfully.
const SchemaVersion = 1

// Board is the redacted per-seat snapshot `botpolicy.BoardFromGameInto`
// builds for one deciding seat.
type Board struct {
	SchemaVersion int         `json:"schema_version"`
	IsMain        bool        `json:"is_main"`
	Mana          [6]int32    `json:"mana_wubrgc"`
	Cards         []Card      `json:"cards"`
	Life          []Life      `json:"life"`
	Creatures     []Creature  `json:"creatures"`
	Commanders    []Commander `json:"commanders"`
	Stack         []Stack     `json:"stack"`
}

// Card is one card the deciding seat knows about.
type Card struct {
	CardID        state.ObjID `json:"card_id"`
	Creature      bool        `json:"creature"`
	Power         int32       `json:"power"`
	ManaValue     int32       `json:"mana_value"`
	Basic         bool        `json:"basic"`
	AttachedTo    state.ObjID `json:"attached_to,omitempty"`
	Activated     int32       `json:"activated"`
	ManaCost      string      `json:"mana_cost"`
	Castable      bool        `json:"castable"`
	OnBattlefield bool        `json:"on_battlefield"`
	InstantSpeed  bool        `json:"instant_speed"`
	Produces      [6]int32    `json:"produces_wubrgc"`
	ProducesAny   bool        `json:"produces_any"`
	Indeterminate bool        `json:"produces_indeterminate"`
	Counter       bool        `json:"counter"`
}

// Life is one seat's life total.
type Life struct {
	Player state.PlayerID `json:"player"`
	Life   int32          `json:"life"`
}

// Creature is one creature's combat-visible characteristics.
type Creature struct {
	Object     state.ObjID    `json:"object"`
	Controller state.PlayerID `json:"controller"`
	Power      int32          `json:"power"`
	Toughness  int32          `json:"toughness"`
	Damage     int32          `json:"damage"`
	Tapped     bool           `json:"tapped"`
	Keywords   []string       `json:"keywords"`
}

// CommanderDamage is one player's commander damage from one commander.
type CommanderDamage struct {
	Player state.PlayerID `json:"player"`
	Damage int32          `json:"damage"`
}

// Commander is one commander's bookkeeping.
type Commander struct {
	Object        state.ObjID       `json:"object"`
	Casts         int32             `json:"casts"`
	InCommandZone bool              `json:"in_command_zone"`
	Damage        []CommanderDamage `json:"damage"`
}

// Stack is one object on the stack.
type Stack struct {
	Object     state.ObjID    `json:"object"`
	Controller state.PlayerID `json:"controller"`
	IsSpell    bool           `json:"is_spell"`
}

// Project copies b into the schema's value form. It allocates fresh slices
// (the source Board is reused in place by BoardFromGameInto, so the caller
// must not alias it), sorts every map-derived list by key, and copies the
// keyword slices, so the result is immutable and byte-stable.
func Project(b *botpolicy.Board) Board {
	r := Board{SchemaVersion: SchemaVersion, IsMain: b.IsMain, Mana: b.Pool}
	cardIDs := sortedObjIDs(b.Cards)
	for _, id := range cardIDs {
		c := b.Cards.Get(id)
		r.Cards = append(r.Cards, Card{CardID: id, Creature: c.Creature, Power: c.Power, ManaValue: c.CMC, Basic: c.Basic, AttachedTo: c.AttachedTo, Activated: c.Activated, ManaCost: c.ManaCost, Castable: c.Castable, OnBattlefield: c.OnBattlefield, InstantSpeed: c.InstantSpeed, Produces: c.Produces.Colour, ProducesAny: c.Produces.Any, Indeterminate: c.Produces.Indeterminate, Counter: c.Counter})
	}
	players := make([]int, 0, b.Life.Len())
	for p := range b.Life.All() {
		players = append(players, int(p))
	}
	sort.Ints(players)
	for _, p := range players {
		r.Life = append(r.Life, Life{Player: state.PlayerID(p), Life: b.Life.Get(state.PlayerID(p))})
	}
	for _, id := range sortedObjIDs(b.Creatures) {
		c := b.Creatures.Get(id)
		r.Creatures = append(r.Creatures, Creature{Object: id, Controller: c.Controller, Power: c.Power, Toughness: c.Toughness, Damage: c.Damage, Tapped: c.Tapped, Keywords: append([]string(nil), c.Keywords...)})
	}
	for _, id := range sortedObjIDs(b.Commanders) {
		c := b.Commanders.Get(id)
		v := Commander{Object: id, Casts: c.Casts, InCommandZone: c.InCommandZone}
		ps := make([]int, 0, len(c.Damage))
		for p := range c.Damage {
			ps = append(ps, int(p))
		}
		sort.Ints(ps)
		for _, p := range ps {
			v.Damage = append(v.Damage, CommanderDamage{Player: state.PlayerID(p), Damage: c.Damage[state.PlayerID(p)]})
		}
		r.Commanders = append(r.Commanders, v)
	}
	for _, s := range b.Stack {
		r.Stack = append(r.Stack, Stack{Object: s.ID, Controller: s.Controller, IsSpell: s.IsSpell})
	}
	return r
}

func sortedObjIDs[V any](t botpolicy.IDTable[state.ObjID, V]) []state.ObjID {
	ids := append([]state.ObjID(nil), t.Keys()...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
