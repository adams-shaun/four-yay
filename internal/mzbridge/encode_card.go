package mzbridge

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// encodeCard is processCard (134-175): the counters on the card's own node,
// then the static card features on the node's PARENT (the zone), from where
// they pool upward. A node that does not pass to its parent gets the
// counters only.
func (enc *Encoder) encodeCard(o *state.Object, f *cards.Face, n *Node) {
	// 138-141
	for _, c := range o.Counters {
		if name := counterName(c.Kind); name != "" {
			n.AddNumeric(name, int(c.N))
		}
	}
	// 143-145
	if !n.passToParent || n.parent == nil || f == nil {
		return
	}
	n = n.parent
	fi := enc.face(f)
	// 148
	n.Add("Card")
	// 150-152
	if fi.permanent {
		n.Add("Permanent")
	}
	// 154-156
	for _, ct := range fi.cardTypes {
		n.Add(ct)
	}
	// 158-164
	addColours(n, effects.ColorsOf(o), "")
	// 167-169
	for _, st := range fi.subTypes {
		n.Add(st)
	}
	// 170-171: processManaCosts (79-84) with no suffix
	n.AddNumeric("ManaValue", fi.manaValue) // 80
	for _, sym := range fi.manaSyms {
		n.Add(sym) // 82
	}
}

// addColours is the colour block of processCard (158-164) and, with the
// "_dynamic" suffix, of processPermBattlefield (190-196). colours is gorge's
// WUBRG letter set.
func addColours(n *Node, colours, suffix string) {
	has := func(c byte) bool { return strings.IndexByte(colours, c) >= 0 }
	if has('R') {
		n.Add("RedCard" + suffix)
	}
	if has('W') {
		n.Add("WhiteCard" + suffix)
	}
	if has('B') {
		n.Add("BlackCard" + suffix)
	}
	if has('G') {
		n.Add("GreenCard" + suffix)
	}
	if has('U') {
		n.Add("BlueCard" + suffix)
	}
	if colours == "" {
		n.Add("ColorlessCard" + suffix)
	}
	if len(colours) > 1 {
		n.Add("MultiColored" + suffix)
	}
}

// encodeCardInZone is processCardInZone (306-329): the card's static
// features, then a sub-node per static, activated and triggered ability
// that functions in the zone.
func (enc *Encoder) encodeCardInZone(o *state.Object, f *cards.Face, zone zoneMask, n *Node) {
	// 309
	enc.encodeCard(o, f, n)
	if f == nil {
		return
	}
	fi := enc.face(f)
	// 314-328: faceInfo lists statics, then activated, then triggered
	for i := range fi.abilities {
		a := &fi.abilities[i]
		if a.zones&zone == 0 {
			// a cast from another zone (flashback, a play-from-exile
			// grant) is XMage's spell ability in that zone
			if !(a.cast || a.playLand) || !enc.canActivate(o, a) {
				continue
			}
		}
		enc.encodeAbility(o, a, n.Sub(a.rule, true)) // 315-316, 320-321, 325-326
	}
}

// encodeAbility is processAbility (93-109) followed by the kind's own
// features: processActivatedAbility (110-122) or processTriggeredAbility
// (123-131).
func (enc *Encoder) encodeAbility(o *state.Object, a *abilityInfo, n *Node) {
	// 95-99: processCosts (85-92)
	if len(a.mana) > 0 {
		n.AddNumeric("ManaValue_dynamic", a.manaValue) // 80
		for _, sym := range a.mana {
			n.Add(sym + "_dynamic") // 82
		}
	}
	for _, c := range a.costs {
		n.Add(c) // 90
	}
	// 100-104: effect texts go to the ability node's parent
	if n.parent != nil {
		for _, t := range a.effects {
			n.parent.Add(t) // 102
		}
	}
	// 106-108: ability watchers -- omitted, gorge has no watcher keys
	switch a.kind {
	case abActivated:
		// 113
		if a.isMana {
			n.Add("ManaAbility")
		}
		// 114-121
		if o != nil && enc.canActivate(o, a) {
			n.Add("CanActivate")
		}
	case abTriggered:
		// 127-128: ReachedTriggerLimit, UsedAlready -- omitted
		// 129: the trigger event exists only for an ability on the stack;
		// encodeStackObject adds it
	}
}

// canActivate stands in for ActivatedAbility.canActivate (116): the engine
// offers this exact action to the deciding player in the pending priority
// decision. A mana ability is not a priority option in gorge; it counts as
// activatable when the decider controls its untapped source.
func (enc *Encoder) canActivate(o *state.Object, a *abilityInfo) bool {
	d := enc.d
	if d == nil || a.kind != abActivated {
		return false
	}
	if a.isMana && !a.cast {
		if o.Controller != enc.decider || o.Zone != state.ZBattlefield || len(a.mana) > 0 {
			return false
		}
		if !a.tap {
			return true
		}
		if o.Tapped {
			return false
		}
		return !(o.SummonSick && enc.e.IsCreature(o.ID) && !enc.e.HasKeyword(o.ID, "Haste"))
	}
	if d.Kind != decision.KPriority {
		return false
	}
	for i := range d.Options {
		op := &d.Options[i]
		if op.Obj != o.ID {
			continue
		}
		switch {
		case a.cast && op.Kind == "cast", a.playLand && op.Kind == "play_land":
			return true
		case !a.cast && !a.playLand && op.Kind == "ability" && op.Ability == a.index:
			return true
		}
	}
	return false
}

// maxAttachDepth bounds the attachment recursion (an Aura on an Aura on ...).
const maxAttachDepth = 4

// encodePermanent is processPermBattlefield (177-305). p is the player
// whose battlefield is being encoded (the Java's playerId).
func (enc *Encoder) encodePermanent(cv *view.CardView, p state.PlayerID, n *Node, depth int) {
	g, e := enc.g, enc.e
	o := g.Obj(cv.ID)
	if o == nil {
		return
	}
	hidden := hiddenFace(cv)
	// 179: a PermanentCard (not a token) is also encoded as its card
	if !o.IsToken && !hidden {
		enc.encodeCardInZone(o, printedFace(o), inBattlefield, n)
	}
	// 181
	if cv.Tapped {
		n.Add("Tapped")
	}
	// 184-189: current types and subtypes
	isCreature := false
	for _, w := range e.Derived(cv.ID).Types {
		class, name := classifyType(w)
		if class == typeSuper || name == "" {
			continue
		}
		isCreature = isCreature || name == "CREATURE"
		n.Add(name + "_dynamic") // 185, 188
	}
	// 190-196
	addColours(n, e.ObjectColors(o), "_dynamic")

	// 199-213: every ability the permanent currently has. Keywords come
	// from the derived list (printed and granted); the rest from its
	// current face.
	var fi *faceInfo
	if f := o.Face(); f != nil && !hidden {
		fi = enc.face(f)
	}
	// A token has no SpellAbility or PlayLandAbility: it was never a card.
	skip := func(a *abilityInfo) bool { return o.IsToken && (a.cast || a.playLand) }
	nonKeyword := 0
	if fi != nil {
		for i := len(o.Face().Keywords); i < len(fi.abilities); i++ {
			if !skip(&fi.abilities[i]) {
				nonKeyword++
			}
		}
	}
	if len(cv.Keywords) > 0 || nonKeyword > 0 {
		dyn := n.Sub("DynamicPermAbilities", false) // 201
		for _, kw := range cv.Keywords {
			dyn.Sub(keywordRule(kw), true) // 203, 209
		}
		if fi != nil {
			for i := len(o.Face().Keywords); i < len(fi.abilities); i++ {
				if a := &fi.abilities[i]; !skip(a) {
					enc.encodeAbility(o, a, dyn.Sub(a.rule, true)) // 203-209
				}
			}
		}
	}

	// 216-227: attachments, recursively; the "attached" node does not pool
	if ids := enc.attached[cv.ID]; len(ids) > 0 && depth < maxAttachDepth {
		att := n.Sub("attached", false) // 218
		for _, id := range ids {
			acv := enc.seen[id]
			enc.encodePermanent(acv, p, att.Sub(acv.Name, true), depth+1) // 223-224
		}
	}
	// 229-239: imprinted cards, as far as the deciding seat can see them
	if len(o.Imprinted) > 0 {
		var imp *Node
		for _, id := range o.Imprinted {
			icv := enc.seen[id]
			io := g.Obj(id)
			if icv == nil || io == nil || hiddenFace(icv) {
				continue
			}
			if imp == nil {
				imp = n.Sub("imprinted", false) // 231
			}
			enc.encodeCard(io, printedFace(io), imp.Sub(icv.Name, true)) // 235-236
		}
	}
	// 241-245: paired (soulbond); the node does not pool, so processCard
	// adds the paired card's counters and nothing else
	if o.Paired != 0 {
		if po := g.Obj(o.Paired); po != nil && enc.seen[o.Paired] != nil {
			enc.encodeCard(po, printedFace(po), n.Sub("paired", false)) // 243-244
		}
	}
	// 247-253: the exile zone this permanent owns
	if ids := enc.linked[cv.ID]; len(ids) > 0 {
		list := make([]view.CardView, 0, len(ids))
		for _, id := range ids {
			list = append(list, *enc.seen[id])
		}
		enc.encodeCards(list, inExile, n.Sub(enc.exileZoneName(cv.ID), false)) // 251-252
	}
	// 255-267: stack objects targeting this permanent, with their position
	// among the stack objects that have targets (getSpellsTargetingPermanent,
	// 699-722, advances its index only past objects with targets)
	var targeted *Node
	index := 0
	for i := range enc.stack {
		se := &enc.stack[i]
		if len(se.o.Targets) == 0 {
			continue
		}
		index++
		hit := false
		for _, t := range se.o.Targets {
			hit = hit || (!t.IsPlayer && t.Obj == cv.ID)
		}
		if !hit {
			continue
		}
		if targeted == nil {
			targeted = n.Sub("TargetedBy", false) // 258
		}
		targeted.Sub(se.name, true).AddNumeric("StackDepth", index) // 261-262
	}

	// 275-286: flags. flipped, harnessed, solved, disguised, morphed and
	// the two Room doors are omitted (gorge does not model them apart).
	if o.Suspected {
		n.Add("suspected") // 278
	}
	if g.IsRingBearer(o.Controller, o.ID) {
		n.Add("RingBearer") // 279
	}
	if o.Renowned {
		n.Add("Renowned") // 280
	}
	if o.Monstrous {
		n.Add("Monstrous") // 281
	}
	if o.Cloaked {
		n.Add("Cloaked") // 282
	}

	// 289-304
	if isCreature {
		haste := hasWord(cv.Keywords, "Haste")
		sick := cv.SummonSick && !haste
		// 290: hasSummoningSickness is false for a creature with haste
		if sick {
			n.Add("SummoningSick")
		}
		// 291: Permanent.canAttack -- untapped, not summoning sick, no
		// restriction. gorge's own check is unexported; Defender is the
		// one restriction read here.
		if !cv.Tapped && !sick && !hasWord(cv.Keywords, "Defender") {
			n.Add("CanAttack")
		}
		// 292: Permanent.canBlockAny -- untapped and unrestricted
		if !cv.Tapped {
			n.Add("CanBlock")
		}
		// 294-300
		if cv.Attacking {
			n.Add("Attacking") // 295
			for _, id := range cv.BlockedBy {
				n.Add(enc.entityName(state.Target{Obj: id}) + " Blocking") // 298
			}
		}
		n.AddNumeric("Damage", int(cv.Damage))       // 301
		n.AddNumeric("Power", int(cv.Power))         // 302
		n.AddNumeric("Toughness", int(cv.Toughness)) // 303
	}
}

func hasWord(list []string, w string) bool {
	for _, s := range list {
		if s == w {
			return true
		}
	}
	return false
}
