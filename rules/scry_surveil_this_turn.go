package rules

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// ScriedThisTurn satisfies effects.Host's ScriedThisTurn for
// Count$YouScryThisTurn (Desperate Futurescribe, Proctor of Potential,
// Surveillance Phantasm: "if you've scried ... this turn"): every events.Scry
// record naming p since the last TurnChange. One record is logged per
// completed scry instruction per scrying player (rules' handleArrange, or the
// no-ask stand-in through EmitScryRecord), so this counts scries the way
// Forge's per-turn tally does, and a replay derives the same number.
func (e *Engine) ScriedThisTurn(p state.PlayerID) int32 {
	return e.kindThisTurn(events.Scry, p)
}

// SurveilledThisTurn satisfies effects.Host's SurveilledThisTurn for
// Count$YouSurveilThisTurn (Darkblade Agent's "as long as you've surveilled
// this turn" and the scry-or-surveil readers): every events.Surveil marker
// naming p since the last TurnChange. api:Surveil logs exactly one marker per
// surveil instruction per acting player -- also when the library is empty,
// matching Forge's Player.surveil, which counts the instruction either way.
func (e *Engine) SurveilledThisTurn(p state.PlayerID) int32 {
	return e.kindThisTurn(events.Surveil, p)
}

// kindThisTurn counts the log's events of kind k whose Player is p since the
// last TurnChange (the per-turn log fold CardsDrawnThisTurn uses).
func (e *Engine) kindThisTurn(k events.Kind, p state.PlayerID) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := &e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == k && ev.Player == p {
			n++
		}
	}
	return n
}
