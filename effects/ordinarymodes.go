// Ordinary event-backed trigger modes implemented in rules/trigmatch.
package effects

func init() {
	RegisterNonAPI("trig:TurnBegin", "trig:LosesGame", "trig:ChangesController", "trig:Exiled")
}
