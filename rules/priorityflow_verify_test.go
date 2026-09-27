package rules

// The rules test binary runs every priority grant in verify mode
// (priorityFlowVerify, rules/turn.go): a grant made while a cast proposal is
// still open or a choose-flow marker is still armed panics.
func init() { priorityFlowVerify = true }
