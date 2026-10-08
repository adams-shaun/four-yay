package mzenc

import (
	"regexp"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// DefaultTableSize is MageZero v0.2's Features.TABLE_SIZE (Integer.MAX_VALUE).
const DefaultTableSize int64 = 2_147_483_647

// actionTypeNames mirrors ActionEncoder.ActionType.toString(), the six
// decision-type feature names (StateEncoder.processState:639).
var actionTypeNames = [6]string{"PRIORITY", "CHOOSE_NUM", "BLANK", "CHOOSE_TARGET", "MAKE_CHOICE", "CHOOSE_USE"}

// stepName maps the projected view step string (state.Step.String()) to the
// upstream TurnStepType name used as a global phase feature. Keys are exactly
// state.Step.String()'s spellings, held there by TestStepNameCoversAllSteps,
// so they cannot drift from the state package. Only the steps StateEncoder
// emits a name for are listed.
var stepName = map[string]string{
	"untap":             "UNTAP",
	"upkeep":            "UPKEEP",
	"draw":              "DRAW",
	"main1":             "PRECOMBAT_MAIN",
	"begin-combat":      "BEGIN_COMBAT",
	"declare-attackers": "DECLARE_ATTACKERS",
	"declare-blockers":  "DECLARE_BLOCKERS",
	"combat-damage":     "COMBAT_DAMAGE",
	"end-combat":        "END_COMBAT",
	"main2":             "POSTCOMBAT_MAIN",
	"end":               "END_TURN",
	"cleanup":           "CLEANUP",
}

var (
	uuidTagRE = regexp.MustCompile(` \[[0-9a-f]+\]`)
	angleRE   = regexp.MustCompile("<[^>]*>")
)

// cleanString ports StateEncoder.cleanString (StateEncoder.java:682-689):
// remove " [hex]" UUID tags and every <...> span.
func cleanString(s string) string {
	if s == "" {
		return s
	}
	s = uuidTagRE.ReplaceAllString(s, "")
	return angleRE.ReplaceAllString(s, "")
}

type walker struct {
	e           *Encoder
	seat        state.PlayerID
	active      state.PlayerID
	ch          view.Chars
	names       map[state.ObjID]string
	unsupported map[string]bool
}

// manaPoolKeys and manaPoolNames are the fixed colour order processMana
// (StateEncoder.java:438-444) reads a pool in, spelled as gorGE's Pool map
// keys (internal/policynet's poolKeys: "W","U","B","R","G","C").
var (
	manaPoolKeys  = [...]string{"W", "U", "B", "R", "G", "C"}
	manaPoolNames = [...]string{"WhiteMana", "BlueMana", "BlackMana", "RedMana", "GreenMana", "ColorlessMana"}
)

// processManaPool ports StateEncoder.processMana (StateEncoder.java:438-444):
// the six fixed colour features under the caller's "ManaPool" subtree. The map
// is iterated in the fixed key order, never ranged, so the emission order (and
// therefore nothing hashed) cannot vary with map iteration order.
func (w *walker) processManaPool(f *Node, pool map[string]int32) {
	for i, k := range manaPoolKeys {
		f.AddNumericFeature(manaPoolNames[i], int(pool[k]), true)
	}
}

// processPlayer ports the scalar half of StateEncoder.processPlayer
// (StateEncoder.java:543-619): the active/decision flags, life total, library
// count, hand count and mana pool. Families Task 2 cannot expose are recorded
// in w.unsupported and emit nothing. isDecisionPlayer is pv.ID == seat.
func (w *walker) processPlayer(f *Node, pv *view.PlayerView, isDecisionPlayer bool) {
	if pv.ID == w.active {
		f.AddFeature("IsActivePlayer")
	}
	if isDecisionPlayer {
		f.AddFeature("IsDecisionPlayer")
	}
	f.AddNumericFeature("LifeTotal", int(pv.Life), true)
	f.AddNumericFeature("LibraryCount", pv.LibrarySize, true)
	f.AddNumericFeature("CardsInHand", pv.HandSize, true)
	w.processManaPool(f.SubFeatures("ManaPool", false), pv.Pool)

	// battlefield (StateEncoder.java:590-593): the per-permanent family,
	// nested under the player's subtree exactly as upstream.
	w.processBattlefield(f.SubFeatures("Battlefield", true), pv, w.ch)

	w.unsupported["PlayerCounters"] = true
	w.unsupported["DayNight"] = true
	w.unsupported["CanPlayLand"] = true
	w.unsupported["InPayManaMode"] = true
	w.unsupported["Activating"] = true
	w.unsupported["MicroDecisions"] = true
	w.unsupported["Attachments"] = true
}

// processBattlefield ports StateEncoder.processBattlefield
// (StateEncoder.java:330-340): walk one seat's permanents under the fixed
// "Battlefield" subtree. Upstream sorts by Permanent.getValue—a name+id key—
// so gorGE sorts a fresh copy by (Name, ID) and never relies on the incoming
// slice order. The copy is deliberate: it reorders nothing the caller holds,
// and it is what makes the "#1"/"#2" duplicate-name occurrence keys stable.
func (w *walker) processBattlefield(f *Node, pv *view.PlayerView, ch view.Chars) {
	if len(pv.Battlefield) == 0 {
		return
	}
	perms := make([]view.CardView, len(pv.Battlefield))
	copy(perms, pv.Battlefield)
	sort.Slice(perms, func(i, j int) bool {
		if perms[i].Name != perms[j].Name {
			return perms[i].Name < perms[j].Name
		}
		return perms[i].ID < perms[j].ID
	})
	for i := range perms {
		cv := &perms[i]
		w.processPerm(f.SubFeatures(cv.Name, true), cv, ch)
	}
}

// processPerm ports the view-exposed subset of StateEncoder.processPermBattlefield
// (StateEncoder.java:177-305). Upstream first walks the static card via
// processCard (Task 4); until that lands this emits the type words directly as
// the gorGE analogue of CardType.name(). The dynamic type/subtype/colour list,
// the dynamic ability list, the engine-only CanAttack/CanBlock predicates, the
// colour set and every unique permanent flag are not expressible through
// view.CardView, so they are recorded in w.unsupported and emit nothing.
func (w *walker) processPerm(f *Node, cv *view.CardView, ch view.Chars) {
	if cv.Tapped {
		f.AddFeature("Tapped")
	}
	// static card type words (ct.name(), lowercased for gorGE)
	for _, t := range strings.Fields(cv.Types) {
		f.AddFeature(strings.ToLower(t))
	}
	if isCreatureType(cv.Types) {
		if cv.SummonSick {
			f.AddFeature("SummoningSick")
		}
		if cv.Attacking {
			f.AddFeature("Attacking")
			for _, bid := range cv.BlockedBy {
				if name := w.names[bid]; name != "" {
					f.AddFeature(name + " Blocking")
				}
			}
		}
		f.AddNumericFeature("Damage", int(cv.Damage), true)
		f.AddNumericFeature("Power", int(cv.Power), true)
		f.AddNumericFeature("Toughness", int(cv.Toughness), true)
	}
	// keywords (the ability-rule token analogue): lowercased, in view order.
	for _, kw := range cv.Keywords {
		f.AddFeature(strings.ToLower(kw))
	}

	// families view.CardView cannot express. Names match the Task 6 register.
	w.unsupported["Subtypes"] = true
	w.unsupported["Colors"] = true
	w.unsupported["DynamicTypes"] = true
	w.unsupported["DynamicAbilities"] = true
	w.unsupported["CanAttack"] = true
	w.unsupported["CanBlock"] = true
	w.unsupported["PermanentFlags"] = true
	w.unsupported["Attachments"] = true
	w.unsupported["Imprinted"] = true
	w.unsupported["Paired"] = true
	w.unsupported["TargetedBy"] = true
	w.unsupported["PermanentExile"] = true
}

// isCreatureType reports whether a CardView's space-joined type line carries
// the Creature card type, the view-analogue of upstream p.isCreature(game).
func isCreatureType(types string) bool {
	for _, t := range strings.Fields(types) {
		if t == "Creature" {
			return true
		}
	}
	return false
}

// keywordOf is a case-insensitive membership test over cv.Keywords. Task 4's
// processCard reads it for keyword-derived static features.
func keywordOf(cv *view.CardView, kw string) bool {
	for _, k := range cv.Keywords {
		if strings.EqualFold(k, kw) {
			return true
		}
	}
	return false
}

// ProcessState walks an omniscient gorGE view and returns the MageZero
// feature-id set for the given decision.
func ProcessState(v view.View, ch view.Chars, seat state.PlayerID, decisionType int, decisionsText string) map[int32]struct{} {
	e := NewEncoder(DefaultTableSize)
	w := &walker{e: e, seat: seat, active: v.Active, ch: ch, unsupported: map[string]bool{}}
	root := w.e.Root()
	// globals (StateEncoder.java:634-641)
	if name, ok := stepName[v.Step]; ok {
		root.AddFeature(name)
	}
	if decisionType >= 0 && decisionType < len(actionTypeNames) {
		root.AddFeature(actionTypeNames[decisionType])
	}
	root.AddFeature(cleanString(decisionsText))

	// each player, in v.Players order: the seat under "Player", every other
	// seat under "Opponent" (StateEncoder.java:657-661).
	for i := range v.Players {
		pv := &v.Players[i]
		if pv.ID == seat {
			w.processPlayer(root.SubFeatures("Player", true), pv, true)
		} else {
			w.processPlayer(root.SubFeatures("Opponent", true), pv, false)
		}
	}
	return e.IDs()
}
