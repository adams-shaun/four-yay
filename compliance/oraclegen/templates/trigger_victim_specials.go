// Victims for the dies-other filters a placed-and-destroyed creature cannot
// satisfy (ticket g17, wave 3 "trigger with no recipe"): a victim with
// toughness below 1, a face-down victim, an attacking victim and a Clue
// victim. Each shape has a turn-1 cause:
//
//   - toughnessLT1: the victim starts at the gate, at zero effective
//     toughness, and dies to the setup state-based action before step zero;
//     the trigger is on the stack at the first priority, so the cause carries
//     the two passes that surface it. The engine decides the LKI toughness,
//     not this table.
//   - facedown: a Disguise probe is cast face down and destroyed face down.
//   - attacking: the victim attacks and is destroyed mid-combat; a source
//     that must itself attack is declared alongside it.
//   - Clue: a corpus card that creates a Clue when it dies supplies the
//     token, which an artifact-destroy probe then kills.
//
// The detector is diesVictimSkip, the one home for these qualifiers, so a
// row this file cannot serve keeps its named skip.
package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// clueTokenProbes are corpus cards whose own death creates a Clue token,
// tried in order; the token dies to an artifact-destroy probe and the
// trigger's own matcher accepts it.
var clueTokenProbes = []string{"Knowledge Seeker", "Messenger Hawk"}

// auraBearerProbes are the bearers an Aura dies-cause is cast onto, tried in
// order for the Enchant$ keyword's target type: a plain creature, then an
// artifact creature for "Enchant artifact".
var auraBearerProbes = []string{bearsProbe, "Gingerbrute"}

// castList is the funded cast steps of probes with targets, each named in the
// cause's hand; ok is false when any probe cannot be funded. seat and mana
// override the step's seat ("pN:probe") and pool; mode sets a face-down cast.
func castList(reg *cards.Registry, specs ...castSpec) (steps []oraclegen.Step, hand []string, ok bool) {
	for _, sp := range specs {
		st, ok := castProbe(reg, sp.probe, sp.targets...)
		if !ok {
			return nil, nil, false
		}
		if sp.mana != "" {
			st.Mana = sp.mana
		}
		if sp.mode != "" {
			st.CastMode = sp.mode
		}
		if sp.seat != 0 {
			st.Seat = sp.seat
			st.Card = "p" + strconv.Itoa(sp.seat) + ":" + sp.probe
		}
		steps = append(steps, st)
		hand = append(hand, sp.probe)
	}
	return steps, hand, true
}

// castSpec is one castList entry.
type castSpec struct {
	probe   string
	targets []string
	seat    int
	mana    string
	mode    string
}

// selfDiesNonCreatureCauses is the dies cause of a non-creature permanent:
// the removal spell its type admits destroys the placed card. A permanent
// that resists destroy dies to its own sacrifice ability's activation
// instead. An Aura is cast onto a bearer first (setup cannot place an
// unattached Aura) and destroyed there.
func selfDiesNonCreatureCauses(reg *cards.Registry, f *cards.Face, name string) ([]triggerCause, string) {
	if resistsDestroy(f) {
		if c, ok := selfSacrificeDiesCause(reg, f, name); ok {
			return []triggerCause{c}, ""
		}
		return nil, "dies probe cannot destroy it (it resists destroy)"
	}
	if oraclegen.HasType(f, "Aura") {
		return auraDiesCauses(reg, f, name)
	}
	var out []triggerCause
	for _, p := range diesDestroyProbes(f) {
		if c, ok := castCause(reg, name, p, "p0:"+name); ok {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, "dies probe not in corpus"
	}
	return out, ""
}

// selfSacrificeCause activates the face's own sacrifice ability, so the card
// is put into a graveyard from the battlefield by its controller. An
// indestructible permanent's dies trigger has no removal spell, but its own
// "{T}, Sacrifice this permanent" cost does.
func selfSacrificeDiesCause(reg *cards.Registry, f *cards.Face, name string) (triggerCause, bool) {
	prefixes, why := oraclegen.XMageAbility(f)
	if why != "" {
		return triggerCause{}, false
	}
	for idx, sa := range f.Abilities {
		if !sa.IsActivated() || !sacrificesSelf(sa) {
			continue
		}
		cost := sa.ParamStr(cards.PKCost)
		mana, gap := activationCostIn(cost, "battlefield", "")
		if gap != "" {
			continue
		}
		prefix, okp := prefixes[idx]
		if !okp {
			continue
		}
		i := idx
		setup := oraclegen.Seat{}
		addActivationCostFixtures(&setup, "", cost)
		return triggerCause{
			battlefield:  append([]string(nil), setup.Battlefield...),
			hand:         append([]string(nil), setup.Hand...),
			graveyard:    append([]string(nil), setup.Graveyard...),
			steps:        []oraclegen.Step{{Op: "activate", Seat: 0, Card: "p0:" + name, Mana: mana, AbilityIndex: &i, Answers: activationXAnswers(cost)}},
			xability:     []string{prefix},
			activateCost: cost,
		}, true
	}
	return triggerCause{}, false
}

// sacrificesSelf reports whether an activated ability's cost sacrifices its
// own source (a Sac<1/CARDNAME> or Sac<1/Self> cost token).
func sacrificesSelf(sa *cards.SA) bool {
	for _, tok := range costTokens(sa.ParamStr(cards.PKCost)) {
		payload, ok := strings.CutPrefix(tok, "Sac<")
		if !ok {
			continue
		}
		fields := strings.Split(strings.TrimSuffix(payload, ">"), "/")
		if len(fields) == 2 && fields[0] == "1" &&
			(strings.EqualFold(fields[1], "CARDNAME") || strings.EqualFold(fields[1], "Self")) {
			return true
		}
	}
	return false
}

// auraDiesCauses casts the Aura onto a bearer of the type its Enchant$
// keyword names, then destroys the Aura, so the Aura is put into a graveyard
// from the battlefield. Each bearer is its own cause: the cast's target
// legality, not this table, decides which one serves.
func auraDiesCauses(reg *cards.Registry, f *cards.Face, name string) ([]triggerCause, string) {
	enchant := ""
	if raw, ok := f.KeywordParam("Enchant"); ok {
		enchant = raw
	}
	bearers := auraBearerProbes
	switch strings.ToLower(enchant) {
	case "artifact":
		bearers = []string{"Gingerbrute"}
	case "creature":
		bearers = []string{bearsProbe}
	}
	var out []triggerCause
	for _, bearer := range bearers {
		for _, p := range diesDestroyProbes(f) {
			casts, hand, ok := castList(reg,
				castSpec{probe: name, targets: []string{"p0:" + bearer}},
				castSpec{probe: p, targets: []string{"p0:" + name}})
			if !ok {
				continue
			}
			out = append(out, triggerCause{
				selfInHand:  true,
				hand:        hand,
				battlefield: []string{bearer},
				steps:       []oraclegen.Step{casts[0], {Op: "resolve"}, casts[1]},
			})
		}
	}
	if len(out) == 0 {
		return nil, "dies aura bearer not in corpus"
	}
	return out, ""
}

// becomesTargetNonCreatureCauses targets the placed permanent with the
// opponent's removal spell its type admits; the ward ask is declined as
// every becomes-target cause's is.
func becomesTargetNonCreatureCauses(reg *cards.Registry, f *cards.Face, name string) ([]triggerCause, string) {
	var out []triggerCause
	for _, p := range diesDestroyProbes(f) {
		st, ok := castProbe(reg, p, "p0:"+name)
		if !ok {
			continue
		}
		st.Seat, st.Card = 1, "p1:"+p
		out = append(out, triggerCause{
			opponentHand: []string{p},
			steps:        []oraclegen.Step{{Op: "pass", Seat: 0}, st},
		})
	}
	if len(out) == 0 {
		return nil, "becomes-target probe not in corpus"
	}
	return out, ""
}

// toughLT1Probe is the {2}{B} instant that puts four -1/-1 counters on a
// creature. One cast on a 2/2 victim takes it to 0/0, the state-based action
// sends it to the graveyard, and the LKI the trigger's filter reads carries
// the counters, so the toughness the predicate sees is the counters'.
var toughLT1Probe = "Blight Rot"

// diesSpecialVictimCauses builds the causes of a dies-other trigger whose
// victim filter names a qualifier no placed victim carries. ok is false when
// the filter's qualifier is none of the shapes here (the generic walk then
// runs as before).
func diesSpecialVictimCauses(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) ([]triggerCause, string, bool) {
	switch diesVictimSkip(t) {
	case "dies victim must have toughness less than 1":
		return toughnessLT1VictimCauses(reg, name, t), "", true
	case "dies victim must be facedown":
		return faceDownVictimCauses(reg, name)
	case "dies victim must be attacking":
		return attackingVictimCauses(reg, f, name)
	case "dies victim must be a Clue token":
		return clueVictimCauses(reg, name)
	}
	return nil, "", false
}

// toughnessLT1VictimCauses puts four -1/-1 counters on a vanilla 2/2 victim
// on the side the filter's OppCtrl names: one {2}{B} Blight Rot cast,
// resolved by the detection's passes, leaves the victim a 0/0 the
// state-based action sends to the graveyard. The engine decides the LKI
// toughness, not this table.
func toughnessLT1VictimCauses(reg *cards.Registry, name string, t *cards.Trigger) []triggerCause {
	side := 0
	if strings.Contains(strings.ToLower(levelb.ZoneChangeFilter(t)), "oppctrl") {
		side = 1
	}
	if toughLT1Probe == name || !oraclegen.XMageKnown(toughLT1Probe) {
		return nil
	}
	if _, ok := reg.Lookup(toughLT1Probe); !ok {
		return nil
	}
	casts, hand, ok := castList(reg,
		castSpec{probe: toughLT1Probe, targets: []string{victimRef(side, bearsProbe)}})
	if !ok {
		return nil
	}
	c := triggerCause{hand: hand, steps: casts}
	if side == 1 {
		c.opponentBattlefield = []string{bearsProbe}
	} else {
		c.battlefield = []string{bearsProbe}
	}
	return []triggerCause{c}
}

// victimRef is a victim as a step target on side.
func victimRef(side int, name string) string {
	return "p" + strconv.Itoa(side) + ":" + name
}

// faceDownVictimCauses casts a Disguise probe face down and destroys it while
// it is still face down, so the victim's LKI carries the face-down state.
func faceDownVictimCauses(reg *cards.Registry, name string) ([]triggerCause, string, bool) {
	var out []triggerCause
	for _, probe := range turnedFaceUpDisguiseProbes {
		if probe == name || !oraclegen.XMageKnown(probe) {
			continue
		}
		card, ok := reg.Lookup(probe)
		if !ok || len(card.Faces) == 0 {
			continue
		}
		_, _, mode, _, why := morphFamilyCost(card.Faces[0])
		if why != "" {
			continue
		}
		casts, hand, ok := castList(reg,
			castSpec{probe: probe, mana: faceDownCastPool, mode: mode},
			castSpec{probe: destroyProbes[0], targets: []string{"p0:" + probe}})
		if !ok {
			continue
		}
		out = append(out, triggerCause{
			hand:  hand,
			steps: []oraclegen.Step{casts[0], {Op: "resolve"}, casts[1]},
		})
	}
	if len(out) == 0 {
		return nil, "dies victim must be facedown", true
	}
	return out, "", true
}

// attackingVictimCauses attacks with the victim and destroys it mid-combat,
// so its LKI still carries the attacking state. A creature source that must
// attack each combat is declared alongside the victim so the declaration is
// legal; the engine's own attack requirements are the authority.
func attackingVictimCauses(reg *cards.Registry, f *cards.Face, name string) ([]triggerCause, string, bool) {
	attackers := []string{"p0:" + bearsProbe}
	battlefield := []string{bearsProbe}
	if f.IsCreature() {
		attackers = append([]string{"p0:" + name}, attackers...)
	}
	attack := oraclegen.Step{Op: "attack", Seat: 0, Defender: "p1", Attackers: attackers}
	casts, hand, ok := castList(reg, castSpec{probe: destroyProbes[0], targets: []string{"p0:" + bearsProbe}})
	if !ok {
		return nil, "dies victim must be attacking", true
	}
	return []triggerCause{{
		battlefield: battlefield,
		hand:        hand,
		steps:       []oraclegen.Step{attack, casts[0]},
	}}, "", true
}

// clueVictimCauses kills a corpus card whose death creates a Clue token, then
// destroys the Clue (an artifact token) with the artifact-destroy probe.
func clueVictimCauses(reg *cards.Registry, name string) ([]triggerCause, string, bool) {
	var out []triggerCause
	for _, probe := range clueTokenProbes {
		if probe == name || !oraclegen.XMageKnown(probe) {
			continue
		}
		if _, ok := reg.Lookup(probe); !ok {
			continue
		}
		casts, hand, ok := castList(reg,
			castSpec{probe: destroyProbes[0], targets: []string{"p0:" + probe}},
			castSpec{probe: artifactProbes[0], targets: []string{"p0:token:Clue"}})
		if !ok {
			continue
		}
		out = append(out, triggerCause{
			battlefield: []string{probe},
			hand:        hand,
			steps:       []oraclegen.Step{casts[0], {Op: "resolve"}, casts[1]},
		})
	}
	if len(out) == 0 {
		return nil, "dies victim must be a Clue token", true
	}
	return out, "", true
}
