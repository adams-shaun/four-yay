package rules

import "github.com/adams-shaun/gorge/state"

// oracle_run_faces.go binds an oracle scenario's step ref that names an
// ALTERNATE spell face of a physical card. A scenario deals the physical
// card under its parent (front) name -- the name both engines deal it by --
// and a cast step may name the face it casts ("p0:Stomp" for a Bonecrusher
// Giant in hand). Only the Adventure spell face is bound (CR 715): it is cast
// from the hand through the engine's own "adventure_alt" offer
// (rules/adventure.go), so the runner chooses exactly the offer a player
// would. Setup names stay physical-card names; a setup that names an
// Adventure face is still "not dealt".

// oracleAltFaceMode returns the cast mode that casts name as an alternate
// spell face of o's card, or "" when name is not such a face of o (or is
// the face o already shows).
func oracleAltFaceMode(o *state.Object, name string) string {
	if o == nil || o.Face() == nil || o.Face().Name == name {
		return ""
	}
	if f := adventureSpellFace(o); f != nil && f.Name == name {
		return "adventure_alt"
	}
	return ""
}

// faceCastMode is the cast mode a cast step's ref implies: the alternate
// face's mode when the ref names one of id's alternate spell faces, else ""
// (the ordinary cast).
func (r *oracleRun) faceCastMode(ref string, id state.ObjID) string {
	_, name, token, _, err := splitRef(ref)
	if err != nil || token {
		return ""
	}
	return oracleAltFaceMode(r.e.G.Obj(id), name)
}
