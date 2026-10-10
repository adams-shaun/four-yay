package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// staticChosenType is the creature type a static's as-enters choice is led to
// when the probe must not already carry it. Gorge's creature-type ask lists
// the types of the creatures the chooser owns first and the default answer
// takes the first, so the unscripted choice is the probe's own subtype (Bear)
// and "AddType$ ChosenType" adds nothing the types comparison can see. No
// fixture creature (Grizzly Bears, Ornithopter, Llanowar Elves, ...) is a
// Wizard.
const staticChosenType = "Wizard"

// staticChosenTypeFixtures is the fixture for a continuous static that adds
// the source's chosen creature type (Leyline of Transformation's
// "AddType$ ChosenType"): the as-enters ask is answered with a type the
// probe lacks, so the added type is observable on it.
func staticChosenTypeFixtures(f *cards.Face, st cards.Static) []staticFixture {
	if !strings.EqualFold(st.ParamStr(cards.PKAddType), "ChosenType") {
		return nil
	}
	if _, ok := chosenTypeFixture(f); !ok {
		return nil
	}
	// reanswers: the scripted pick must reach XMage, whose cast-path XAnswers
	// were derived from the unscripted choice.
	return []staticFixture{{chosenType: staticChosenType, reanswers: true}}
}

// answerChosenType scripts the as-enters creature-type ask of the card's
// resolve step to want. It reports false when the scenario has no such
// single-answer resolve step to rewrite.
func answerChosenType(base *oraclegen.Item, want string) bool {
	for i, st := range base.Steps {
		if st.Op != "resolve" || len(st.Answers) != 1 || st.Answers[0].Kind != "choose" || len(st.Answers[0].Pick) != 1 {
			continue
		}
		base.Steps[i].Answers = []oraclegen.Answer{{Kind: "choose", Pick: []string{want}}}
		return true
	}
	return false
}
