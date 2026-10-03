package rules

// Regression coverage for the two `greatestPower` filter seams the round-t2
// review measured as still unbound: effSacrifice's SacValid$ pool walk
// (effects/zone.go) and conditionMet's defined-group walk /
// conditionNotPresentMet (effects/conditions.go). Both compare a candidate
// against its peers, so a continuous pump on a SMALLER creature must be able
// to displace the printed-greatest one. Each test asserts its own
// precondition (both creatures on the battlefield, derived and printed
// orderings in opposite directions) so a vacuous setup fails loudly.
