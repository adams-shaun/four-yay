package mzenc

import (
	"regexp"
	"sort"
	"strconv"
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
	// emitted records the feature families actually produced. It is nil on the
	// hot path (ProcessState) so no map is allocated there; only
	// ProcessStateReport sets it. emit is the sole writer.
	emitted map[string]bool
}

// emit records that a feature family was produced. It is a no-op when the
// walker carries no emitted map (the ProcessState hot path), so the recording
// costs one nil check and allocates nothing.
func (w *walker) emit(family string) {
	if w.emitted != nil {
		w.emitted[family] = true
	}
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
	w.emit(famManaPool)
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
		w.emit(famIsActivePlayer)
	}
	if isDecisionPlayer {
		f.AddFeature("IsDecisionPlayer")
		w.emit(famIsDecisionPlayer)
	}
	f.AddNumericFeature("LifeTotal", int(pv.Life), true)
	w.emit(famLifeTotal)
	f.AddNumericFeature("LibraryCount", pv.LibrarySize, true)
	w.emit(famLibraryCount)
	w.processManaPool(f.SubFeatures("ManaPool", false), pv.Pool)

	// battlefield (StateEncoder.java:590-593): the per-permanent family,
	// nested under the player's subtree exactly as upstream.
	w.processBattlefield(f.SubFeatures("Battlefield", true), pv, w.ch)

	// graveyard then hand (StateEncoder.java:596-608). perfectInfo is always
	// true for the public entry, so every player's Hand is walked and no
	// CardsInHand scalar is emitted.
	w.processGraveyard(f.SubFeatures("Graveyard", true), pv, w.ch)
	w.processHand(f.SubFeatures("Hand", true), pv, w.ch)

	w.unsupported[famPlayerCounters] = true
	w.unsupported[famDayNight] = true
	w.unsupported[famCanPlayLand] = true
	w.unsupported[famInPayManaMode] = true
	w.unsupported[famActivating] = true
	w.unsupported[famMicroDecisions] = true
	w.unsupported[famAttachments] = true
	// global families the view exposes but no walker consumes yet.
	w.unsupported[famExile] = true
	w.unsupported[famCommandZone] = true
	w.unsupported[famGlobalWatchers] = true
}

// processBattlefield ports StateEncoder.processBattlefield
// (StateEncoder.java:330-340): walk one seat's permanents under the fixed
// "Battlefield" subtree. Upstream sorts by Permanent.getValue—a name+id key—
// so gorGE sorts a fresh copy by (Name, ID) and never relies on the incoming
// slice order. The copy is deliberate: it reorders nothing the caller holds,
// and it is what makes the "#1"/"#2" duplicate-name occurrence keys stable.
func (w *walker) processBattlefield(f *Node, pv *view.PlayerView, ch view.Chars) {
	w.emit(famBattlefield)
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
	w.emit(famPermanent)
	if cv.Tapped {
		f.AddFeature("Tapped")
	}
	// static card type words (ct.name(), lowercased for gorGE)
	for _, t := range strings.Fields(cv.Types) {
		f.AddFeature(strings.ToLower(t))
	}
	if isCreatureType(cv.Types) {
		w.emit(famCreature)
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

	// families view.CardView cannot express. Keys are the coverage register's
	// canonical spelling.
	w.unsupported[famSubtypes] = true
	w.unsupported[famColors] = true
	w.unsupported[famDynamicTypes] = true
	w.unsupported[famDynamicAbilities] = true
	w.unsupported[famCanAttack] = true
	w.unsupported[famCanBlock] = true
	w.unsupported[famPermanentFlags] = true
	// Attachments is already registered by processPlayer (player-level
	// upstream family, Task 2); permanent attachments share the family name.
	w.unsupported[famImprinted] = true
	w.unsupported[famPaired] = true
	w.unsupported[famTargetedBy] = true
	w.unsupported[famPermanentExile] = true
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

// permanentTypes are the card types that make a card a permanent
// (upstream c.isPermanent()).
var permanentTypes = [...]string{"Creature", "Artifact", "Enchantment", "Land", "Planeswalker", "Battle"}

// isPermanentType reports whether a CardView's space-joined type line carries
// any permanent card type, the view-analogue of upstream c.isPermanent().
func isPermanentType(types string) bool {
	for _, t := range strings.Fields(types) {
		for _, p := range permanentTypes {
			if t == p {
				return true
			}
		}
	}
	return false
}

// manaBraceForm strips the optional {..} braces Forge sometimes renders a
// cost in, so "..{U} {U}.." tokenises as the space-separated notation the
// counter reads. It mirrors policynet's manaBraceForm.
var manaBraceForm = strings.NewReplacer("{", " ", "}", " ")

// manaValue is a minimal port of internal/policynet's mvOf (option.go): the
// mana value of a Forge-notation cost ("U U" -> 2, "R" -> 1, "" -> 0). It
// handles the shapes view.CardView.ManaCost carries: a plain pip, a generic
// integer, X (0 off the stack, CR 202.3b), and a monocolour hybrid twobrid
// ("2/W"/"2W" -> its generic face, CR 202.4b); every other symbolic token
// (hybrid, Phyrexian) counts as one pip.
func manaValue(cost string) int {
	cost = strings.TrimSpace(manaBraceForm.Replace(cost))
	if cost == "" || strings.EqualFold(cost, "no cost") {
		return 0
	}
	mv := 0
	for _, tok := range strings.Fields(cost) {
		if tok == "X" {
			continue
		}
		if len(tok) == 1 && strings.ContainsRune("WUBRGC", rune(tok[0])) {
			mv++
			continue
		}
		if n, err := strconv.Atoi(tok); err == nil && n >= 0 {
			mv += n
			continue
		}
		if v, ok := twobridManaValue(tok); ok {
			mv += v
			continue
		}
		mv++
	}
	return mv
}

// twobridManaValue recognises Forge's concatenated ("2W") and slash ("2/W")
// monocolour-hybrid spellings and returns the generic face, copied from
// policynet's twobridManaValue.
func twobridManaValue(sym string) (int, bool) {
	generic, col := "", ""
	if left, right, ok := strings.Cut(sym, "/"); ok {
		generic, col = left, right
	} else {
		i := 0
		for i < len(sym) && sym[i] >= '0' && sym[i] <= '9' {
			i++
		}
		if i == 0 {
			return 0, false
		}
		generic, col = sym[:i], sym[i:]
	}
	if len(col) != 1 || !strings.ContainsRune("WUBRGC", rune(col[0])) {
		return 0, false
	}
	v, err := strconv.Atoi(generic)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

// processCard ports the view-exposed subset of StateEncoder.processCard
// (StateEncoder.java:134-175): the universal "Card" tag, the "Permanent" tag
// for a permanent card type, each card-type word (lowercased for gorGE), the
// printed mana value, and the card's name recorded in w.names (Task 3's
// BlockedBy walk reads it).
//
// Subtypes are not exposed by view.CardView, so none are emitted and the
// family is recorded unsupported. passToParent mirrors upstream's
// `if(!f.passToParent) return` guard: a card node not created pass-to-parent
// emits nothing.
func (w *walker) processCard(f *Node, cv *view.CardView, passToParent bool) {
	if !passToParent {
		return
	}
	w.emit(famCard)
	w.names[cv.ID] = cv.Name
	f.AddFeature("Card")
	if isPermanentType(cv.Types) {
		f.AddFeature("Permanent")
	}
	for _, t := range strings.Fields(cv.Types) {
		f.AddFeature(strings.ToLower(t))
	}
	f.AddNumericFeature("ManaValue", manaValue(cv.ManaCost), true)
	w.unsupported[famSubtypes] = true
}

// processCardInZone ports StateEncoder.processCardInZone (StateEncoder.java:306-329):
// processCard, then the zone's static/activated/triggered ability walks. The
// view carries no ability list, so the ability walks are not exposable and the
// CardAbilities family is recorded unsupported; only the card features are
// emitted.
func (w *walker) processCardInZone(f *Node, cv *view.CardView, zone string, ch view.Chars) {
	w.processCard(f, cv, f.passToParent)
	w.unsupported[famCardAbilities] = true
}

// processGraveyard ports StateEncoder.processGraveyard
// (StateEncoder.java:341-345): walk the sorted graveyard cards under the
// caller's "Graveyard" subtree. Upstream uses getCardsSorted, so gorGE sorts a
// fresh copy by (Name, ID) and never relies on the incoming slice order.
func (w *walker) processGraveyard(f *Node, pv *view.PlayerView, ch view.Chars) {
	w.emit(famGraveyard)
	w.processCardList(f, pv.Graveyard, "graveyard", ch)
}

// processHand ports StateEncoder.processHand (StateEncoder.java:347-351): walk
// the sorted hand cards under the caller's "Hand" subtree.
func (w *walker) processHand(f *Node, pv *view.PlayerView, ch view.Chars) {
	w.emit(famHand)
	w.processCardList(f, pv.Hand, "hand", ch)
}

// processCardList shares processGraveyard/processHand: sort a fresh copy of
// cards by (Name, ID) (upstream getCardsSorted), create the card's name
// subtree, and walk it in zone. The copy is deliberate: it reorders nothing
// the caller holds and gives a deterministic traversal independent of the
// incoming list order.
func (w *walker) processCardList(f *Node, cards []view.CardView, zone string, ch view.Chars) {
	if len(cards) == 0 {
		return
	}
	sorted := make([]view.CardView, len(cards))
	copy(sorted, cards)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].ID < sorted[j].ID
	})
	for i := range sorted {
		cv := &sorted[i]
		w.processCardInZone(f.SubFeatures(cv.Name, true), cv, zone, ch)
	}
}

// ProcessState walks an omniscient gorGE view and returns the MageZero
// feature-id set for the given decision. It passes a nil emitted map so the
// hot path allocates no coverage bookkeeping; ProcessStateReport is the
// recording entry point.
func ProcessState(v view.View, ch view.Chars, seat state.PlayerID, decisionType int, decisionsText string) map[int32]struct{} {
	ids, _ := processState(v, ch, seat, decisionType, decisionsText, nil)
	return ids
}

// processState is the shared walk. emitted is nil on the hot path and the
// walker's emit calls are no-ops then; ProcessStateReport supplies a map and
// reads w.unsupported back.
func processState(v view.View, ch view.Chars, seat state.PlayerID, decisionType int, decisionsText string, emitted map[string]bool) (map[int32]struct{}, map[string]bool) {
	e := NewEncoder(DefaultTableSize)
	w := &walker{e: e, seat: seat, active: v.Active, ch: ch,
		names: map[state.ObjID]string{}, unsupported: map[string]bool{}, emitted: emitted}
	// Pre-register every battlefield permanent's name before any player or
	// permanent is walked: processPerm resolves a BlockedBy id through
	// w.names, and a blocker may sit on a later player's battlefield (or be
	// walked after its attacker within one), so the lookup must not depend on
	// walk order. Plain deterministic loops, no map range.
	for i := range v.Players {
		bf := v.Players[i].Battlefield
		for j := range bf {
			w.names[bf[j].ID] = bf[j].Name
		}
	}

	root := w.e.Root()
	// globals (StateEncoder.java:634-641)
	if name, ok := stepName[v.Step]; ok {
		root.AddFeature(name)
		w.emit(famTurnStep)
	}
	if decisionType >= 0 && decisionType < len(actionTypeNames) {
		root.AddFeature(actionTypeNames[decisionType])
		w.emit(famDecisionType)
	}
	root.AddFeature(cleanString(decisionsText))
	w.emit(famDecisionsText)

	// stack (StateEncoder.java:647): the root "Stack" subtree, always created
	// even when empty, walked bottom to top.
	w.processStack(root, &v)

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
	return e.IDs(), w.unsupported
}
