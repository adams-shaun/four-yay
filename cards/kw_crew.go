// Keyword expansion split out of keywords.go so that tickets touching
// different keywords stop colliding on one file. Each expander is
// registered by head; a duplicate head panics (registerKeyword).

package cards

import "strings"

// kwCrew expands K:Crew (CR 702.122) into the activated ability the keyword
// prints: "Tap any number of other untapped creatures you control with total
// power N or greater: This Vehicle becomes an artifact creature until end of
// turn." The expansion mints ONE AB$ Animate whose
//
//   - Cost$ is the tap-any-number cost the corpus already spells for this
//     exact shape (Mossbridge Troll's `tapXType<Any/Creature.Other+
//     withTotalPowerGE10>` -- "tap any number of untapped creatures you
//     control other than CARDNAME with total power 10 or greater"): the
//     Creature.Other spec excludes the Vehicle itself, the tap-cost
//     machinery only ever offers untapped candidates, and the
//     withTotalPowerGE<N> group predicate -- the SET-level "total power N or
//     greater" clause -- rides the spec verbatim; rules/mana.go's
//     stripGroupPowerFloor moves it into CostPart.MinPower, where the offer
//     gate and the tap election (Decision.MinSum over each candidate's
//     power) enforce it. Crew is NOT sorcery speed (unlike Equip), and
//     paying the crew cost has no summoning-sickness interaction: CR 302.6
//     restricts attacking and the {T} SYMBOL, not tapping as a cost, so the
//     expansion adds no sickness gate and the machinery reads none.
//   - Defined$ Self / Types$ Artifact,Creature is the CR 702.122b animation:
//     the same AB$ Animate shape the corpus's own printed crew-style
//     abilities carry (Kylox's Voltstrider), with no Power$/Toughness$ so
//     the Vehicle keeps its printed 4/4 (effAnimate's zero default is inert
//     when neither is present). No Duration$: Animate's default is
//     until-end-of-turn, which reverts the animation at the end step.
//
// param is "<N>" optionally followed by colon fields. The corpus carries
// exactly one rider: Luxurious Locomotive's `K:Crew:1:ActivationLimit$ 1`
// ("activate only once each turn"), passed through to the minted SA verbatim
// the way the Equip expansion passes ReduceCost$/ActivationLimit$/
// AlternateCost$ riders -- rules/legal.go's offer loop and
// rules/activate.go's abilityAlternateCost already read them. Any
// space-bearing trailing field is display prose and stays dropped.
func kwCrew(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	if has("A", k) {
		return
	}
	if sa := crewAbilitySA(param); sa != nil {
		sa.Params["KeywordLine"] = k
		f.Abilities = append(f.Abilities, sa)
	}
}

func crewAbilitySA(param string) *SA {
	fields := strings.Split(param, ":")
	n := strings.TrimSpace(fields[0])
	if n == "" {
		n = "1"
	}
	var riders []string
	for _, fld := range fields[1:] {
		fld = strings.TrimSpace(fld)
		if fld == "" {
			continue
		}
		head, _, isParam := strings.Cut(fld, " ")
		if isParam && strings.HasSuffix(head, "$") && (head == "ReduceCost$" || head == "ActivationLimit$" || head == "AlternateCost$") {
			riders = append(riders, fld)
		}
	}
	saStr := "AB$ Animate | Cost$ tapXType<Any/Creature.Other+withTotalPowerGE" + n +
		"> | Defined$ Self | Types$ Artifact,Creature | Keyword$ Crew" +
		" | SpellDescription$ Crew " + n +
		" (Tap any number of other untapped creatures you control with total power " + n +
		" or greater: This Vehicle becomes an artifact creature until end of turn.)"
	for _, r := range riders {
		saStr += " | " + r
	}
	sa, _ := parseSA("", saStr)
	return sa
}

// GrantedCrewAbility synthesizes the activated ability for a derived Crew line.
func GrantedCrewAbility(line string) *SA {
	if KeywordHead(line) != "Crew" {
		return nil
	}
	param := ""
	if j := strings.IndexByte(line, ':'); j >= 0 {
		param = strings.TrimSpace(line[j+1:])
	}
	sa := crewAbilitySA(param)
	if sa != nil {
		sa.Params["KeywordLine"] = line
	}
	return sa
}

func init() { registerKeyword(kwCrew, "Crew") }
