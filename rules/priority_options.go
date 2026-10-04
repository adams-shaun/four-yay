package rules

// The priority decision's option kinds (decision.Option.Kind): the closed
// vocabulary the legal walk offers and handlePriority dispatches. A switch
// over an option's Kind cases on these names directly (a native string
// switch: no table lookup on the priority path).
const (
	optPass       = "pass"
	optConcede    = "concede"
	optPlayLand   = "play_land"
	optCast       = "cast"
	optActivate   = "activate"
	optAbility    = "ability"
	optGranted    = "granted"
	optStation    = "station"
	optUnlock     = "unlock"
	optSpecialize = "specialize"
	optTurnFaceUp = "turn_face_up"
)
