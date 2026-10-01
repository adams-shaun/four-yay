// Front end G (P§3.3, S0): gorge's own view.View plus its pending decision
// surface -> the v2 entity table. Extends entity.go; never replaces it.
//
// Row order (P§3.4): players in seat order, and per player the zones
// battlefield, hand (the viewer's own seat only), graveyard, exile,
// command; then the stack, bottom to top. Front end V iterates the same
// order off the observation, so the parity test (P§3.9) can hold both to
// byte-identity.

package policynet

import (
	"strconv"

	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// V2GTable is front end G's product: the table plus the pointer maps a
// gorge decision's option encodings resolve through.
type V2GTable struct {
	Table   V2Table
	ObjRows map[state.ObjID]int32    // object -> card row + 1
	PlyRows map[state.PlayerID]int32 // player -> -(player row + 1)
	// SemIDRows maps the v2 semantics' object ids (the posing's "card-..."
	// construction) to card rows; the parity test and the dump join it from
	// the posing's refs. Unfilled, only the decimal fallback applies.
	SemIDRows map[string]int32
	// SpellPT carries a stack spell's power/toughness for objects whose
	// row the view cannot derive it for: a stack object's PT is not a
	// battlefield derived state, so view.View exposes none, while the
	// observation's spell characteristics carry the printed face (CR
	// 708.4's derived shape for a face-down spell). The pairing fills it
	// from the posed observation (JoinSemIDs); a standalone front-end G
	// build leaves it nil and the rows read 0, which is the honest view
	// projection.
	SpellPT map[state.ObjID][2]int32
}

// V2RefObject resolves a semantic object reference: a posed id first, then
// the decimal string of a gorge object id.
func (t *V2GTable) V2RefObject(id string) int32 {
	if r, ok := t.SemIDRows[id]; ok {
		return r
	}
	if n, err := strconv.Atoi(id); err == nil {
		if r, ok := t.ObjRows[state.ObjID(n)]; ok {
			return r
		}
	}
	return 0
}

// V2RefPlayer resolves a seat word to a player row.
func (t *V2GTable) V2RefPlayer(seat string) int32 {
	// "p0" .. "p9" in seat order; the v2 seats are exactly the gorge seats.
	if len(seat) == 2 && seat[0] == 'p' && seat[1] >= '0' && seat[1] <= '9' {
		p := state.PlayerID(seat[1] - '0')
		if r, ok := t.PlyRows[p]; ok {
			return r
		}
	}
	return 0
}

// V2TableFromView builds the table from one seat's projection. priorityAsk
// is whether the pending decision is a priority ask: the observation poses
// its priority seat only on those (spec 6.3), so the S0 table encodes the
// priority bit only on those too.
func V2TableFromView(v view.View, viewer state.PlayerID, priorityAsk bool, into *V2GTable) *V2GTable {
	t := into
	if t == nil {
		t = &V2GTable{}
	}
	if t.ObjRows == nil {
		t.ObjRows = map[state.ObjID]int32{}
	}
	if t.PlyRows == nil {
		t.PlyRows = map[state.PlayerID]int32{}
	}
	add := func(cv *view.CardView, zone uint8, ownSeat uint8) {
		d := V2CardFromView(cv, zone, viewer, v2SeatNone, ownSeat)
		t.ObjRows[cv.ID] = int32(len(t.Table.Cards)) + 1
		t.Table.Cards = append(t.Table.Cards, V2CardRow(d))
	}
	for i := range v.Players {
		pv := &v.Players[i]
		seat := pv.ID
		t.PlyRows[seat] = -(int32(len(t.Table.Players)) + 1)
		for j := range pv.Battlefield {
			add(&pv.Battlefield[j], V2ZoneBattlefield, viewSeatOf(seat, viewer))
		}
		if seat == viewer {
			for j := range pv.Hand {
				add(&pv.Hand[j], V2ZoneHand, v2SeatMine)
			}
		}
		for j := range pv.Graveyard {
			add(&pv.Graveyard[j], V2ZoneGraveyard, viewSeatOf(seat, viewer))
		}
		for j := range pv.Exile {
			add(&pv.Exile[j], V2ZoneExile, viewSeatOf(seat, viewer))
		}
		for j := range pv.Command {
			add(&pv.Command[j], V2ZoneCommand, viewSeatOf(seat, viewer))
		}
		t.Table.Players = append(t.Table.Players, V2PlayerRow(V2PlayerDatum{
			Life:    pv.Life,
			Pool:    [6]int32{pv.Pool["W"], pv.Pool["U"], pv.Pool["B"], pv.Pool["R"], pv.Pool["G"], pv.Pool["C"]},
			Hand:    pv.HandSize,
			Library: pv.LibrarySize,
			IsMe:    seat == viewer,
		}))
	}
	for j := range v.Stack {
		sv := &v.Stack[j]
		d := V2CardDatum{Zone: V2ZoneStack,
			CtlSeat: viewSeatOf(sv.Controller, viewer), HasTgts: len(sv.Targets) > 0}
		for _, tgt := range sv.Targets {
			if tgt.IsPlayer {
				d.TgtPlyr = true
			}
		}
		if sv.Kind == "spell" {
			d.StackKd = V2StackSpell
			// A face-down spell's band carries no Card for a viewer other
			// than its caster (CR 708.4): the datum keeps FaceDown with no
			// characteristics, the same shape front end V encodes from the
			// observation's 708.4 spell entry.
			if sv.Card != nil {
				cv := V2CardFromView(sv.Card, V2ZoneStack, viewer, d.CtlSeat, v2SeatNone)
				d.Name, d.Types, d.Supers, d.Keywords = cv.Name, cv.Types, cv.Supers, cv.Keywords
				d.MV, d.Power, d.Tough = cv.MV, cv.Power, cv.Tough
				d.OwnSeat = cv.OwnSeat
				// A stack spell's PT rides the wire characteristics when the
				// pairing supplied them (see SpellPT); the view exposes none.
				if pt, ok := t.SpellPT[sv.ID]; ok {
					d.Power, d.Tough = pt[0], pt[1]
				}
			} else {
				d.Facedown = true
				d.OwnSeat = d.CtlSeat
			}
		} else {
			// An ability band: an activated ability (the view's "ability")
			// or a triggered one ("trigger"), named after its source — the
			// same convention the observation's ability entry poses. The
			// view attaches the source's cardView only for display; the
			// datum carries none of it, exactly like front end V.
			d.Name = sv.Name
			d.OwnSeat = d.CtlSeat
			if sv.Kind == "trigger" {
				d.StackKd = V2StackTriggered
			} else {
				d.StackKd = V2StackActivated
			}
		}
		t.ObjRows[sv.ID] = int32(len(t.Table.Cards)) + 1
		t.Table.Cards = append(t.Table.Cards, V2CardRow(d))
	}
	t.Table.Global = V2GlobalRow(V2GlobalDatum{
		Turn:   int(v.Turn),
		Step:   V2PhaseStep(parseViewStep(v.Step)),
		Stack:  len(v.Stack),
		ActMe:  v.Turn > 0 && v.Active == viewer,
		PrioMe: v.Turn > 0 && priorityAsk && v.Priority == viewer})
	return t
}

// parseViewStep maps a view's step string back to the engine step (the
// reverse mapping state.ParseStep owns); an unknown word degrades to main.
func parseViewStep(s string) state.Step {
	if st, ok := state.ParseStep(s); ok {
		return st
	}
	return state.StepMain1
}
