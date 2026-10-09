package templates

// Level-B observation of Petrified Hamlet's chosen-name statics (ticket
// levelb-static-named-enters): a land that names a card while its ETB trigger
// resolves, not through an as-enters replacement and not at cast time. The
// name is chosen mid-trigger-resolution (CR 603.3c), so the scenario plays
// the land, resolves the ETB trigger, and asserts at p0's next priority. The
// trigger's NameCard ask is self-answering: gorge's legacy no-universe
// fallback offers one name -- the top card of p0's library -- and the setup
// pins that to the probe land, which is exactly the trick the Spyglass family
// (cantBeActivatedNamedItem) uses.
//
//   - static#0.0 (CantBeActivated ValidCard$ Card.NamedCard): the named probe
//     land's non-mana activated ability is absent at p0's next priority; the
//     control -- the same board without the played land -- offers it. The
//     probe keeps a legendary creature on the battlefield so its own ability
//     is genuinely offered and the suppression is provably the static's, not
//     a target-feasibility accident.
//   - static#0.1 (Continuous Affected$ Land.NamedCard AddAbility$ <mana>):
//     the named probe land is offered the granted activation at the same
//     checkpoint; the control is not.

import (
	"fmt"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

const (
	// namedEntersProbe is the land whose name the ETB trigger chooses: the
	// recipient of both the suppression and the granted mana ability.
	namedEntersProbe = "Karakas"
	// namedEntersTarget is a legendary creature the probe's own activated
	// ability can return to hand, so the probe is offered it and the control
	// proves the offer without the static.
	namedEntersTarget = "Thalia, Guardian of Thraben"
	// namedEntersSuppressed is a fragment of the probe's non-mana activated
	// ability label ("Return target legendary creature to its owner's
	// hand."), the option the CantBeActivated static withholds.
	namedEntersSuppressed = "Return target legendary creature"
)

// namedCardManaGrant reports whether st is the chosen-name mana grant of the
// Petrified Hamlet shape: AddAbility$ naming a mana body, Affected$ naming
// the chosen name, on a land whose ETB trigger poses the NameCard ask. It is
// keyed to the land+NameCard shape so a named-grant on a castable source or
// a conspiracy's Hidden-agenda name (Secrets of Paradise) keeps its own row.
func namedCardManaGrant(f *cards.Face, st *cards.Static) bool {
	if !f.IsLand() || !strings.Contains(strings.ToLower(st.ParamStr(cards.PKAffected)), "namedcard") {
		return false
	}
	sa := cards.ResolveSVar(f.SVars, strings.TrimSpace(st.ParamStr(cards.PKAddAbility)))
	if sa == nil || sa.API != "Mana" {
		return false
	}
	for _, body := range f.SVars {
		if strings.Contains(body, "DB$ NameCard") {
			return true
		}
	}
	return false
}

// namedEntersScenario is the Petrified Hamlet shape's scenario: the land in
// p0's hand, the probe land and its legendary target on the battlefield, and
// the probe's name on top of p0's library -- the one name gorge's legacy
// no-universe NameCard ask offers.
func namedEntersScenario(f *cards.Face, name string, steps []oraclegen.Step) oraclegen.Scenario {
	sc := staticScenario(f, name, nil, []string{name}, steps)
	p0 := sc.Setup["p0"]
	p0.Battlefield = append(p0.Battlefield, namedEntersProbe, namedEntersTarget)
	p0.LibraryTop = []string{namedEntersProbe}
	sc.Setup["p0"] = p0
	return sc
}

// namedEntersControl drops the source from p0's hand and the play/resolve
// steps, keeping the final checkpoint with its want flipped to true: the
// same board as if the land were never played.
func namedEntersControl(sc oraclegen.Scenario, name string) oraclegen.Scenario {
	ctl := sc
	ctl.Setup = make(map[string]oraclegen.Seat, len(sc.Setup))
	for k, v := range sc.Setup {
		ctl.Setup[k] = v
	}
	p0 := ctl.Setup["p0"]
	p0.Hand = slices.DeleteFunc(slices.Clone(p0.Hand), func(n string) bool { return n == name })
	ctl.Setup["p0"] = p0
	ctl.Steps = append([]oraclegen.Step(nil), sc.Steps[len(sc.Steps)-1])
	ctl.Steps[0].Expect = append([]oraclegen.Expect(nil), ctl.Steps[0].Expect...)
	for i := range ctl.Steps[0].Expect {
		ctl.Steps[0].Expect[i].Want = boolPtr(true)
	}
	return ctl
}

// scriptNamedAnswers copies the name pick the replay recorded onto the
// item's XAnswers at the step that posed it: XMage offers every card name at
// a NameCard ask while gorge's legacy fallback offers one (the library top),
// so XMage's strict-choose driver needs the chosen name scripted.
func scriptNamedAnswers(res rules.OracleResult, it *oraclegen.Item) {
	for _, d := range res.Decisions {
		if d.Step >= 0 && len(d.PickKinds) == 1 && d.PickKinds[0] == "name" && len(d.Picks) == 1 {
			// The scenario's own answers may script nothing (the forced
			// one-option ask is dropped), so the slot is grown to the step.
			for len(it.XAnswers) <= d.Step {
				it.XAnswers = append(it.XAnswers, nil)
			}
			it.XAnswers[d.Step] = append(it.XAnswers[d.Step], oraclegen.XAnswer{Seat: d.Seat, Kind: "choice", Value: d.Picks[0]})
		}
	}
}

// cantBeActivatedNamedEntersItem serves Petrified Hamlet's chosen-name lock
// (CantBeActivated ValidCard$ Card.NamedCard, the name chosen while the ETB
// trigger resolves): p0 plays the land, the trigger's NameCard ask names the
// probe (library top), and the probe's non-mana activated ability is absent
// at p0's next priority while the control -- the same board without the land
// -- offers it.
func cantBeActivatedNamedEntersItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantBeActivated"
	if req.Face > 0 {
		return staticSkip(name, mode, "named-card observation plays the front face")
	}
	offered := func(want bool) []oraclegen.Expect {
		return []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: 0, Kind: "activate", Card: cardAt(0, namedEntersProbe), Label: namedEntersSuppressed}, Want: boolPtr(want)}}
	}
	steps := []oraclegen.Step{
		{Op: "play", Seat: 0, Card: "p0:" + name},
		{Op: "resolve"},
		{Op: "pass_to", Seat: 0, Decision: "priority", Expect: offered(false)},
	}
	sc := namedEntersScenario(f, name, steps)
	it, skip := serveOffered(reg, f, name, mode, req, "602.5", sc, func() string {
		res, ok := runStatic(reg, namedEntersControl(sc, name))
		if !ok {
			return "control does not replay"
		}
		if len(res.Fails) != 0 {
			return "control does not offer the probe ability: " + fmt.Sprint(res.Fails)
		}
		return ""
	})
	if skip != nil {
		return it, skip
	}
	res, _ := runStatic(reg, sc)
	scriptNamedAnswers(res, &it)
	return it, nil
}

// staticGrantedNamedCardItem serves the mana grant side of the same land
// (Continuous Affected$ Land.NamedCard AddAbility$ <mana body>): after the
// same play/resolve/name steps, the named probe land is offered the granted
// activation, and the control without the land is not.
func staticGrantedNamedCardItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st *cards.Static) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "AddAbility"
	sa := cards.ResolveSVar(f.SVars, strings.TrimSpace(st.ParamStr(cards.PKAddAbility)))
	if sa == nil || sa.API != "Mana" {
		return staticSkip(name, mode, "grant body is not a mana ability")
	}
	label := grantedManaLabel(sa)
	offered := func(want bool) []oraclegen.Expect {
		return []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: 0, Kind: "activate", Card: cardAt(0, namedEntersProbe), Label: label}, Want: boolPtr(want)}}
	}
	steps := []oraclegen.Step{
		{Op: "play", Seat: 0, Card: "p0:" + name},
		{Op: "resolve"},
		{Op: "pass_to", Seat: 0, Decision: "priority", Expect: offered(true)},
	}
	sc := namedEntersScenario(f, name, steps)
	it, skip := serveOffered(reg, f, name, mode, req, "613", sc, func() string {
		res, ok := runStatic(reg, namedEntersControl(sc, name))
		if !ok {
			return "control does not replay"
		}
		if len(res.Fails) == 0 {
			return "the granted activation is still offered without the source"
		}
		return ""
	})
	if skip != nil {
		return it, skip
	}
	res, _ := runStatic(reg, sc)
	scriptNamedAnswers(res, &it)
	return it, nil
}
