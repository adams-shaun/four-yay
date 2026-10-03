package cards

// IsManaAbilityAPI reports whether api is one of the two supported activated
// mana ability APIs (CR 605.1a): AB$ Mana and AB$ ManaReflected. It is a pure
// function of the ability's API word, so it lives beside the IR rather than in
// the engine; rules' activation walks and trigmatch's ValidSA$ Activated.Mana
// gate share it.
func IsManaAbilityAPI(api string) bool { return api == "Mana" || api == "ManaReflected" }
