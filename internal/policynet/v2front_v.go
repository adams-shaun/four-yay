// Front end V (P§3.3, S0): the spellbench protocol-v2 Observation the agent
// role receives -> the same v2 entity table front end G builds from gorge's
// view.View. Extends v2agent's typed observation (the wire is the source of
// truth here; nothing reads game state), so a network trained on
// gorge-native transcripts serves an agent answering the wire protocol.
//
// Every S0 convention front end G applies has its mirror here; the parity
// test (P§3.9) holds the two to byte-identity over real transcripts.

package policynet

import (
	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
)

// V2VTable is front end V's product: the table plus a V2RefResolver over
// the observation's own object ids.
type V2VTable struct {
	Table   V2Table
	objRows map[string]int32 // object_id -> card row + 1
	plyRows map[string]int32 // seat word -> -(player row + 1)
}

// V2RefObject resolves a wire object id.
func (t *V2VTable) V2RefObject(id string) int32 { return t.objRows[id] }

// V2RefPlayer resolves a seat word.
func (t *V2VTable) V2RefPlayer(seat string) int32 { return t.plyRows[seat] }

// v2StackDatumFromEntry builds an ability stack entry's datum (the spell
// branch is v2CardFromSpellEntry).
func v2StackDatumFromEntry(se *v2agent.StackEntry, viewer string) (V2CardDatum, uint8) {
	kd := V2StackActivated
	if se.StackKind == "triggered_ability" {
		kd = V2StackTriggered
	}
	d := V2CardDatum{Zone: V2ZoneStack, StackKd: kd,
		CtlSeat: seatRel(se.ControllerSeat, viewer), OwnSeat: seatRel(se.OwnerSeat, viewer),
		Name: derefName(se.CardName), HasTgts: len(se.Targets) > 0}
	for _, tgt := range se.Targets {
		if tgt != nil && tgt.Player != nil {
			d.TgtPlyr = true
		}
	}
	return d, kd
}

// V2TableFromObservation builds the table from one wire observation. viewer
// may be empty, in which case the observation's own Viewer names the acting
// seat; the player rows follow the observation's order, which the protocol
// pins to seat order (spec 6.3).
func V2TableFromObservation(obs *v2agent.Observation, viewer string) *V2VTable {
	if viewer == "" {
		viewer = obs.Viewer
	}
	t := &V2VTable{objRows: map[string]int32{}, plyRows: map[string]int32{}}
	add := func(rec *v2agent.ObjectRecord, zone uint8, ctlSeat, ownSeat uint8) {
		d := v2CardFromRecord(rec, zone, ctlSeat, ownSeat)
		if rec.ObjectID != "" {
			t.objRows[rec.ObjectID] = int32(len(t.Table.Cards)) + 1
		}
		t.Table.Cards = append(t.Table.Cards, V2CardRow(d))
	}
	for i := range obs.Players {
		rec := &obs.Players[i]
		t.plyRows[rec.Seat] = -(int32(len(t.Table.Players)) + 1)
		for j := range rec.Battlefield {
			add(&rec.Battlefield[j], V2ZoneBattlefield, seatRel(rec.Battlefield[j].ControllerSeat, viewer), seatRel(rec.Battlefield[j].OwnerSeat, viewer))
		}
		if rec.Seat == viewer {
			for j := range rec.Hand {
				add(&rec.Hand[j], V2ZoneHand, v2SeatMine, seatRel(rec.Hand[j].OwnerSeat, viewer))
			}
		}
		for j := range rec.Graveyard {
			add(&rec.Graveyard[j], V2ZoneGraveyard, seatRel(rec.Graveyard[j].ControllerSeat, viewer), seatRel(rec.Graveyard[j].OwnerSeat, viewer))
		}
		for j := range rec.Exile {
			add(&rec.Exile[j], V2ZoneExile, seatRel(rec.Exile[j].ControllerSeat, viewer), seatRel(rec.Exile[j].OwnerSeat, viewer))
		}
		for j := range rec.Command {
			add(&rec.Command[j], V2ZoneCommand, seatRel(rec.Command[j].ControllerSeat, viewer), seatRel(rec.Command[j].OwnerSeat, viewer))
		}
		t.Table.Players = append(t.Table.Players, V2PlayerRow(V2PlayerDatum{
			Life:    rec.Life,
			Pool:    [6]int32{int32(rec.ManaPool.W), int32(rec.ManaPool.U), int32(rec.ManaPool.B), int32(rec.ManaPool.R), int32(rec.ManaPool.G), int32(rec.ManaPool.C)},
			Hand:    int(rec.HandCount),
			Library: int(rec.LibraryCount),
			IsMe:    rec.Seat == viewer,
		}))
	}
	for j := range obs.Stack {
		se := &obs.Stack[j]
		if se.StackKind == "spell" {
			d := v2CardFromSpellEntry(se, viewer)
			if se.ObjectID != "" {
				t.objRows[se.ObjectID] = int32(len(t.Table.Cards)) + 1
			}
			t.Table.Cards = append(t.Table.Cards, V2CardRow(d))
		} else {
			d, _ := v2StackDatumFromEntry(se, viewer)
			if se.ObjectID != "" {
				t.objRows[se.ObjectID] = int32(len(t.Table.Cards)) + 1
			}
			t.Table.Cards = append(t.Table.Cards, V2CardRow(d))
		}
	}
	// The global row. A pregame observation (turn 0) is not encoded: the
	// view and the observation disagree about the pregame starter seat, so
	// S0 leaves the active/priority bits unset there (H5 closes it).
	act := derefSeat(obs.ActiveSeat)
	prio := derefSeat(obs.PrioritySeat)
	t.Table.Global = V2GlobalRow(V2GlobalDatum{
		Turn:   int(obs.Turn),
		Step:   obs.PhaseStep,
		Stack:  len(obs.Stack),
		ActMe:  obs.Turn > 0 && act == viewer,
		PrioMe: obs.Turn > 0 && prio == viewer,
	})
	return t
}

// v2CardFromRecord converts one ObjectRecord. The facedown convention
// mirrors front end G: a face-down object's printed face is not encoded in
// S0 (no name, no types, no keywords, mana value and power/toughness 0);
// its counters, damage and combat state still are.
func v2CardFromRecord(rec *v2agent.ObjectRecord, zone, ctlSeat, ownSeat uint8) V2CardDatum {
	perm := rec.Permanent
	if perm == nil {
		perm = &v2agent.Permanent{}
	}
	d := V2CardDatum{Zone: zone, CtlSeat: ctlSeat, OwnSeat: ownSeat,
		Facedown: rec.FaceDown, Damage: int32(perm.Damage),
		Tapped: perm.Tapped, Sick: perm.SummoningSick,
		Att: perm.Attacking, BlockN: len(perm.BlockedAttackers),
		Attached: perm.AttachedTo != nil,
	}
	if perm.AttackTarget != nil && perm.AttackTarget.Player != nil {
		d.AttPlyr = true
	}
	if rec.CardName != nil && *rec.CardName != "" && !rec.FaceDown {
		d.Name = *rec.CardName
	}
	if c := rec.Characteristics; c != nil && !rec.FaceDown {
		d.Types = append(d.Types, c.Types...)
		d.Supers = append(d.Supers, c.Supertypes...)
		for _, k := range c.Keywords {
			if w := V2KeywordName(k); w != "" {
				d.Keywords = append(d.Keywords, w)
			}
		}
		d.MV = int(c.ManaValue)
	}
	if perm.Counters != nil {
		d.Counters = make(map[string]int32, len(perm.Counters))
		for k, n := range perm.Counters {
			d.Counters[k] = int32(n)
		}
	}
	return d
}

// v2CardFromSpellEntry converts one stack spell entry. The 708.4
// characteristics the observation carries for a face-down spell (a nameless
// 2/2 colourless creature for every viewer) are not encoded: front end G
// has no signal to reproduce the derived shape, so S0 leaves a face-down
// spell's characteristics blank on both sides. The X a stack spell paid is
// likewise dropped: the view carries only the printed cost, whose mana
// value counts X as 0, so the observation's folded value is reduced back.
func v2CardFromSpellEntry(se *v2agent.StackEntry, viewer string) V2CardDatum {
	d := V2CardDatum{Zone: V2ZoneStack, StackKd: V2StackSpell,
		CtlSeat: seatRel(se.ControllerSeat, viewer), OwnSeat: seatRel(se.OwnerSeat, viewer),
		Facedown: se.FaceDown, HasTgts: len(se.Targets) > 0}
	for _, tgt := range se.Targets {
		if tgt != nil && tgt.Player != nil {
			d.TgtPlyr = true
		}
	}
	if !se.FaceDown {
		if se.CardName != nil && *se.CardName != "" {
			d.Name = *se.CardName
		}
		if c := se.Characteristics; c != nil {
			d.Types = append(d.Types, c.Types...)
			d.Supers = append(d.Supers, c.Supertypes...)
			for _, k := range c.Keywords {
				if w := V2KeywordName(k); w != "" {
					d.Keywords = append(d.Keywords, w)
				}
			}
			d.MV = int(c.ManaValue)
			if se.XValue != nil {
				d.MV -= int(*se.XValue)
			}
			if c.Power != nil {
				d.Power = *c.Power
			}
			if c.Toughness != nil {
				d.Tough = *c.Toughness
			}
		}
	}
	return d
}

// seatRel maps a wire seat word to the relative seat of a row.
func seatRel(seat, viewer string) uint8 {
	switch {
	case seat == viewer && seat != "":
		return v2SeatMine
	case seat == "":
		return v2SeatNone
	default:
		return v2SeatOther
	}
}

// derefSeat is a nil-safe seat pointer read.
func derefSeat(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// derefName is a nil-safe name pointer read.
func derefName(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
