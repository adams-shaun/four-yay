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
// Adventure face is still "not dealt". A Room's other door (CR 709.5) binds
// the same way and is cast through the engine's "room_alt" offer.

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
	if f := roomAlternateCastFace(o); f != nil && f.Name == name {
		return "room_alt"
	}
	return ""
}

// oracleCastWantMode is the cast option Mode a cast step on o asks for: "kicked" for
// a kicked cast, else its explicit cast_mode, else the alternate face's mode
// when the step's ref names one of o's alternate spell faces, else "" (the
// ordinary cast).
func oracleCastWantMode(st oracleStep, o *state.Object) string {
	if st.Kicked {
		return "kicked"
	}
	if st.CastMode != "" {
		return st.CastMode
	}
	_, name, token, _, err := splitRef(st.Card)
	if err != nil || token {
		return ""
	}
	return oracleAltFaceMode(o, name)
}
