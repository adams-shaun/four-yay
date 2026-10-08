package mzenc

import (
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// indexCards fills the walker's lookup tables from the view: every CardView
// by object id, and the attachment fan-out (the reverse of CardView.AttachedTo
// / AttachedPlayer, which is how the view carries the link while upstream walks
// a permanent's getAttachments()). The tables are only ever looked up by key,
// never ranged, so no map order reaches an id. A card listed in two places
// (the commander roster shadows the zone it currently sits in) resolves to the
// zone list's entry, which is indexed last.
func (w *walker) indexCards() {
	w.cards = map[state.ObjID]*view.CardView{}
	w.attachedTo = map[state.ObjID][]*view.CardView{}
	w.attachedPlayer = map[state.PlayerID][]*view.CardView{}
	v := w.v
	for i := range v.Players {
		pv := &v.Players[i]
		for j := range pv.Commanders {
			w.cards[pv.Commanders[j].ID] = &pv.Commanders[j]
		}
	}
	for i := range v.Players {
		pv := &v.Players[i]
		for _, zone := range [...][]view.CardView{pv.Command, pv.Hand, pv.Graveyard, pv.Exile, pv.Battlefield} {
			for j := range zone {
				w.cards[zone[j].ID] = &zone[j]
			}
		}
	}
	for i := range v.Stack {
		if c := v.Stack[i].Card; c != nil {
			w.cards[c.ID] = c
		}
	}
	for i := range v.Players {
		bf := v.Players[i].Battlefield
		for j := range bf {
			cv := &bf[j]
			switch {
			case cv.AttachedToPlayer:
				w.attachedPlayer[cv.AttachedPlayer] = append(w.attachedPlayer[cv.AttachedPlayer], cv)
			case cv.AttachedTo != 0:
				w.attachedTo[cv.AttachedTo] = append(w.attachedTo[cv.AttachedTo], cv)
			}
		}
	}
}

// sortedCards returns a (Name, ID)-ordered copy of a list of card pointers, the
// deterministic stand-in for upstream's attach-order / getCardsSorted lists.
func sortedCards(in []*view.CardView) []*view.CardView {
	out := make([]*view.CardView, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// cardTypeWords and superTypeWords are the words of a Forge type line that are
// NOT subtypes; every other word is one.
var cardTypeWords = map[string]bool{
	"Artifact": true, "Battle": true, "Conspiracy": true, "Creature": true,
	"Dungeon": true, "Enchantment": true, "Instant": true, "Kindred": true,
	"Tribal": true, "Land": true, "Phenomenon": true, "Plane": true,
	"Planeswalker": true, "Scheme": true, "Sorcery": true, "Vanguard": true,
	"Emblem": true, "Token": true,
}

var superTypeWords = map[string]bool{
	"Basic": true, "Legendary": true, "Ongoing": true, "Snow": true,
	"World": true, "Elite": true, "Host": true,
}

// subtypeWord reports whether a type-line word is a subtype (Elf, Aura,
// Equipment, Forest, ...): anything that is neither a card type nor a supertype.
func subtypeWord(t string) bool { return !cardTypeWords[t] && !superTypeWords[t] }

// processColors ports the colour block of StateEncoder.processCard (java:158-165).
// The view carries the printed mana cost, not a colour set, so the colours are
// the WUBRG symbols of that cost (hybrid and Phyrexian symbols count for each
// colour they name); a card with none is ColorlessCard. A colour indicator or
// a token's own colour is not in the view, so those read as colourless.
func (w *walker) processColors(f *Node, cv *view.CardView) {
	w.emit(famColors)
	var have [5]bool // W U B R G
	n := 0
	for _, r := range cv.ManaCost {
		i := strings.IndexRune("WUBRG", r)
		if i >= 0 && !have[i] {
			have[i] = true
			n++
		}
	}
	if have[3] {
		f.AddFeature("RedCard")
	}
	if have[0] {
		f.AddFeature("WhiteCard")
	}
	if have[2] {
		f.AddFeature("BlackCard")
	}
	if have[4] {
		f.AddFeature("GreenCard")
	}
	if have[1] {
		f.AddFeature("BlueCard")
	}
	if n == 0 {
		f.AddFeature("ColorlessCard")
	}
	if n > 1 {
		f.AddFeature("MultiColored")
	}
}

// hasKeyword reports whether the CardView carries keyword kw (case-folded).
func hasKeyword(cv *view.CardView, kw string) bool {
	for _, k := range cv.Keywords {
		if strings.EqualFold(k, kw) {
			return true
		}
	}
	return false
}

// canAttack derives upstream's p.canAttack(opponent, game) from the facts the
// view carries: untapped, not summoning sick unless it has haste, no defender.
// Restrictions from other permanents' statics ("can't attack") are not in the
// view and are not modelled.
func canAttack(cv *view.CardView) bool {
	return !cv.Tapped && (!cv.SummonSick || hasKeyword(cv, "haste")) && !hasKeyword(cv, "defender")
}

// canBlock derives upstream's p.canBlockAny(game): an untapped creature.
// "Can't block" statics are not in the view and are not modelled.
func canBlock(cv *view.CardView) bool { return !cv.Tapped }

// processPermAttachments ports the "attached" block (java:222-233): a
// non-pooling "attached" subtree, one subtree per attachment keyed by its name,
// each walked as a permanent in its own right.
func (w *walker) processPermAttachments(f *Node, cv *view.CardView, depth int) {
	list := w.attachedTo[cv.ID]
	if len(list) == 0 || depth >= maxAttachDepth {
		return
	}
	w.emit(famAttachments)
	af := f.SubFeatures("attached", false)
	for _, att := range sortedCards(list) {
		w.processPermDepth(af.SubFeatures(att.Name, true), att, depth+1)
	}
}

// processPlayerAttachments ports processPlayer's attachment block (java:
// 573-580): permanents attached to the player (Curses) are walked straight
// under a non-pooling "Attachments" subtree.
func (w *walker) processPlayerAttachments(f *Node, pv *view.PlayerView) {
	list := w.attachedPlayer[pv.ID]
	if len(list) == 0 {
		return
	}
	w.emit(famAttachments)
	af := f.SubFeatures("Attachments", false)
	for _, att := range sortedCards(list) {
		w.processPermDepth(af, att, 1)
	}
}

// processPlayerCounters ports processPlayer's counter block (java:582-586):
// one numeric feature per counter kind, in sorted kind order (upstream's map
// order is not a contract; the kind names differ, so only determinism matters).
func (w *walker) processPlayerCounters(f *Node, pv *view.PlayerView) {
	if len(pv.Counters) == 0 {
		return
	}
	kinds := make([]string, 0, len(pv.Counters))
	for k := range pv.Counters {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	w.emit(famPlayerCounters)
	for _, k := range kinds {
		f.AddNumericFeature(k, int(pv.Counters[k]), true)
	}
}

// processPermImprinted ports the "imprinted" block (java:234-243): each
// imprinted card, resolved from the view, as a processCard under its name. A
// card the view does not show (a hidden zone) is skipped, as upstream skips a
// null game.getCard.
func (w *walker) processPermImprinted(f *Node, cv *view.CardView) {
	if len(cv.Imprinted) == 0 {
		return
	}
	var imf *Node
	for _, id := range cv.Imprinted {
		c := w.cards[id]
		if c == nil {
			continue
		}
		if imf == nil {
			imf = f.SubFeatures("imprinted", false)
		}
		w.processCard(imf.SubFeatures(c.Name, true), c, true)
	}
	if imf != nil {
		w.emit(famImprinted)
	}
}

// processPermPaired ports the "paired" block (java:244-248).
func (w *walker) processPermPaired(f *Node, cv *view.CardView) {
	if cv.Paired == 0 {
		return
	}
	c := w.cards[cv.Paired]
	if c == nil {
		return
	}
	w.emit(famPaired)
	w.processCard(f.SubFeatures("paired", false), c, true)
}

// processPermExile ports the permanent's own exile zone (java:249-256, the
// Oblivion Ring link): a subtree named for the zone (gorGE: the permanent's
// name, as XMage names the zone for its source) holding the exiled cards in
// getCardsSorted order.
func (w *walker) processPermExile(f *Node, cv *view.CardView) {
	if len(cv.ExiledCards) == 0 {
		return
	}
	var cards []*view.CardView
	for _, id := range cv.ExiledCards {
		if c := w.cards[id]; c != nil {
			cards = append(cards, c)
		}
	}
	if len(cards) == 0 {
		return
	}
	w.emit(famPermanentExile)
	zf := f.SubFeatures(cleanString(cv.Name), false)
	for _, c := range sortedCards(cards) {
		w.processCardInZone(zf.SubFeatures(c.Name, true), c, "exile", w.ch)
	}
}

// processTargetedBy ports the "TargetedBy" block (java:257-269) over
// getSpellsTargetingPermanent (java:700-722), quirk included: the running
// index advances only past stack objects that HAVE targets, so StackDepth is
// the position among targeting objects, not the stack position.
func (w *walker) processTargetedBy(f *Node, cv *view.CardView) {
	var tb *Node
	idx := 1
	for i := range w.v.Stack {
		sv := &w.v.Stack[i]
		if len(sv.Targets) == 0 {
			continue
		}
		hit := false
		for _, t := range sv.Targets {
			if !t.IsPlayer && t.Obj == cv.ID {
				hit = true
				break
			}
		}
		if hit {
			if tb == nil {
				tb = f.SubFeatures("TargetedBy", false)
				w.emit(famTargetedBy)
			}
			tb.SubFeatures(cleanString(sv.Name), true).AddNumericFeature("StackDepth", idx, true)
		}
		idx++
	}
}

// processPermFlags ports the "unique flags" block (java:273-285) in upstream
// order. flipped has no gorGE counterpart (no flip cards), so it never fires.
func (w *walker) processPermFlags(f *Node, cv *view.CardView) {
	if cv.Flags == 0 {
		return
	}
	w.emit(famPermanentFlags)
	for _, fl := range [...]struct {
		bit  view.CardFlag
		name string
	}{
		{view.FlagHarnessed, "harnessed"},
		{view.FlagSolved, "solved"},
		{view.FlagSuspected, "suspected"},
		{view.FlagRingBearer, "RingBearer"},
		{view.FlagRenowned, "Renowned"},
		{view.FlagMonstrous, "Monstrous"},
		{view.FlagCloaked, "Cloaked"},
		{view.FlagDisguised, "disguised"},
		{view.FlagMorphed, "morphed"},
		{view.FlagLeftDoor, "Room-LeftDoor"},
		{view.FlagRightDoor, "Room-RightDoor"},
	} {
		if cv.Flags.Has(fl.bit) {
			f.AddFeature(fl.name)
		}
	}
}

// targetName is upstream game.getEntityName(id, playerId) for a stack target:
// the player's name, or the name of the object the view shows for that id.
func (w *walker) targetName(t view.TargetView) string {
	if t.IsPlayer {
		for i := range w.v.Players {
			if w.v.Players[i].ID == t.Player {
				if n := w.v.Players[i].Name; n != "" {
					return n
				}
			}
		}
		return "Player" + strconv.Itoa(int(t.Player))
	}
	if c := w.cards[t.Obj]; c != nil {
		return c.Name
	}
	for i := range w.v.Stack {
		if w.v.Stack[i].ID == t.Obj {
			return w.v.Stack[i].Name
		}
	}
	return ""
}

// processStackTargets ports the "targets" block of processStackObject
// (java:362-374): a non-pooling "targets" subtree, one subtree per chosen target
// keyed by its entity name, and a processCard of the target when it is a card.
func (w *walker) processStackTargets(f *Node, sv *view.StackView) {
	if len(sv.Targets) == 0 {
		return
	}
	w.emit(famStackTargets)
	tf := f.SubFeatures("targets", false)
	for _, t := range sv.Targets {
		tgt := tf.SubFeatures(w.targetName(t), true)
		if !t.IsPlayer {
			if c := w.cards[t.Obj]; c != nil {
				w.processCard(tgt, c, true)
			}
		}
	}
}
