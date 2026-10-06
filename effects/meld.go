package effects

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Meld performs CR 701.42a's "exile them, then meld them into <result>" for
// the two permanents a and b on behalf of controller, and returns the melded
// permanent's object. It is the primitive a DB$/AB$ Meld handler calls (the
// api:Meld registration itself is separate work); the representation is
// documented in state/meld.go. It lives here rather than on rules.Engine so
// that handler reaches it through the ordinary Host roles, with no new Host
// or Engine method.
//
// The sequence is all ordinary events, so live play and a log-only replay
// agree:
//  1. both permanents are exiled as ONE zone batch (two MoveZone events, so
//     leaves-the-battlefield triggers, exile replacements and the CantExile
//     restriction see them);
//  2. if both cards are now in exile and form a meld pair (CR 701.42c: one
//     carries the meld-result face, the other is a meld card, neither is a
//     token or copy, and every name opts gives matches), a Meld event pairs
//     them -- the result card turns to its meld face and the partner is
//     parked;
//  3. the result card's exile->battlefield MoveZone is the melded
//     permanent's entry (ETB triggers, entry counters such as a meld
//     planeswalker's loyalty, and entry replacements all see a normal
//     entry), under controller's control;
//  4. opts.Tapped / opts.Attacking deliver Mishra's "enters tapped and
//     attacking" through the same Tap / TokenAttacks events every other
//     entry rider uses.
//
// ok is false when nothing melded: either permanent was not on the
// battlefield (nothing happens), an exile did not land or the pair is not a
// meld pair (the exiled cards stay in exile, CR 701.42c), or the entry did
// not complete.
func Meld(h Host, controller state.PlayerID, a, b state.ObjID, opts state.MeldOptions) (state.ObjID, bool) {
	g := h.Game()
	oa, ob := g.Obj(a), g.Obj(b)
	if a == b || oa == nil || ob == nil || oa.Zone != state.ZBattlefield || ob.Zone != state.ZBattlefield ||
		int(controller) >= len(g.Players) {
		return 0, false
	}
	res, partner, face, paired := meldRoles(oa, ob, opts)
	h.BeginZoneBatch()
	for _, id := range [2]state.ObjID{a, b} {
		if h.ExileBlocked(id, false) {
			continue
		}
		h.Emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZExile,
			Player: controller, Text: events.MeldText})
	}
	h.EndZoneBatch()
	if !paired || !meldInExile(g, res) || !meldInExile(g, partner) {
		return 0, false
	}
	h.Emit(events.Event{Kind: events.Meld, Obj: res, IDs: []state.ObjID{partner}, Player: controller,
		Amount: int32(face), Text: events.MeldText})
	if o := g.Obj(res); o == nil || o.MeldedWith != partner {
		return 0, false
	}
	h.Emit(events.Event{Kind: events.MoveZone, Obj: res, From: state.ZExile, To: state.ZBattlefield,
		Player: controller})
	o := g.Obj(res)
	if o == nil || o.Zone != state.ZBattlefield {
		return 0, false
	}
	switch {
	case opts.Attacking && int(opts.Defender) < len(g.Players) && opts.Defender != controller:
		ids := []state.ObjID{state.ObjID(opts.Defender)}
		if opts.DefenderBattle != 0 {
			ids = append(ids, opts.DefenderBattle)
		}
		h.Emit(events.Event{Kind: events.TokenAttacks, Obj: res, Player: controller, IDs: ids,
			Text: "entered attacking"})
	case (opts.Tapped || opts.Attacking) && !o.Tapped:
		// An "enters tapped and attacking" meld with no defender still
		// enters tapped (attackingEntryNoDefender's shape).
		h.EmitTap(res, controller, true)
	}
	return res, true
}

// meldRoles picks the result (the card carrying the meld-result face) and
// the partner out of a pair, and reports whether they form a meld pair at
// all (CR 701.42c): exactly one result face, the partner itself a meld card,
// neither a token or copy, and every name opts gives matches.
func meldRoles(oa, ob *state.Object, opts state.MeldOptions) (res, partner state.ObjID, face uint8, paired bool) {
	res, partner = oa.ID, ob.ID
	fa, okA := state.MeldResultFace(oa.Card)
	fb, okB := state.MeldResultFace(ob.Card)
	switch {
	case okA && !okB:
		face = fa
	case okB && !okA:
		res, partner, face = ob.ID, oa.ID, fb
		oa, ob = ob, oa
	default:
		return res, partner, 0, false
	}
	if !state.IsMeldCard(ob.Card) || oa.IsToken || ob.IsToken || oa.IsCopy || ob.IsCopy {
		return res, partner, 0, false
	}
	if opts.ResultName != "" && oa.Card.Faces[face].Name != opts.ResultName {
		return res, partner, 0, false
	}
	if opts.PrimaryName != "" || opts.SecondaryName != "" {
		na, nb := meldFrontName(oa), meldFrontName(ob)
		if (na != opts.PrimaryName || nb != opts.SecondaryName) && (na != opts.SecondaryName || nb != opts.PrimaryName) {
			return res, partner, 0, false
		}
	}
	return res, partner, face, true
}

// meldFrontName is a card's front-face name ("" for a faceless object).
func meldFrontName(o *state.Object) string {
	if o.Card == nil || len(o.Card.Faces) == 0 || o.Card.Faces[0] == nil {
		return ""
	}
	return o.Card.Faces[0].Name
}

// meldInExile reports whether id is an object currently in exile.
func meldInExile(g *state.Game, id state.ObjID) bool {
	o := g.Obj(id)
	return o != nil && o.Zone == state.ZExile
}
