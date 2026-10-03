// The "whenever this creature evolves" trigger family (Forge Mode$ Evolved,
// CR 702.99b, task trig:Evolved).
//
// Evolve is a keyword (K:Evolve expands in cards/kw_evolve.go into an
// ordinary ChangesZone counter trigger), and Mode$ Evolved is the distinct
// notification ability 2 corpus carriers print beside it -- Watchful Radstag
// ("create a token that's a copy of it") and Renegade Krasis ("put a +1/+1
// counter on each other creature you control with a +1/+1 counter on it").
// rules' resolveTop emits the events.Evolved marker once the keyword
// ability's counter actually lands, and rules/trigmatch/cards.go's
// trigmatch.evolvedMatches consumes it. The mode is implemented in rules rather than
// as an effect function, so the coverage key is declared here from the
// effects side (the gift.go trig:GiveGift precedent) -- otherwise
// cards.Registry.Coverage would keep reporting trig:Evolved as an
// unimplemented primitive for both cards.
package effects

func init() { RegisterNonAPI("trig:Evolved") }
