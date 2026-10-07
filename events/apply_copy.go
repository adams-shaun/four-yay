// Event folds for copies and face state: StackCopy, Mutate, tokens, clones, face-up/down transitions.
//
// Split out of events/apply.go: code moved verbatim, no behaviour
// change. Each fold function is the body of the matching case in
// Apply (g, e) switch; see apply.go for the dispatch.
package events

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// foldMutate folds Kind Mutate into state.
func foldMutate(g *state.Game, e *Event) {
	// CR 702.140d: a mutate-spell resolution merges the mutating card's
	// card into the target permanent. Obj is the surviving target, IDs[0]
	// the mutating card's object (the resolving spell), Text "top"/"under"
	// the placement choice and Amount the mutated count to add. The
	// survivor's Card/FaceIdx always describe the TOP card; every
	// under-card lands in MergedCards, top-of-pile first, its object
	// parked in ZCeased (no membership list, so no battlefield scan sees
	// it as a second permanent). Nothing here reads a map or the clock, so
	// a log-only replay rebuilds the identical pile.
	survivor := g.Obj(e.Obj)
	if len(e.IDs) == 0 || survivor == nil || survivor.Card == nil ||
		survivor.Zone != state.ZBattlefield || e.Text == "" {
		return
	}
	src := g.Obj(e.IDs[0])
	if src == nil || src.Card == nil {
		return
	}
	srcCard, srcFace := src.Card, src.FaceIdx
	if e.Text == "top" {
		// The survivor's current top card becomes an under-card. Its own
		// card needs a parked object to move to a graveyard when the pile
		// dies, and the survivor's ID must keep naming the pile, so mint
		// one for the demoted card. Snapshot before AddObject (it may
		// reallocate g.Objs).
		oldCard, oldFace, owner := survivor.Card, survivor.FaceIdx, survivor.Owner
		parked := g.AddObject(oldCard, owner)
		parked.Zone = state.ZCeased
		parkedID := parked.ID
		survivor = g.Obj(e.Obj)
		if survivor == nil {
			return
		}
		survivor.SetCard(srcCard, srcFace)
		under := state.MergedCard{Obj: parkedID, Card: oldCard, FaceIdx: oldFace}
		survivor.MergedCards = append([]state.MergedCard{under}, survivor.MergedCards...)
		// The mutating spell's object is now redundant (its card data was
		// copied onto the survivor), so park it in ZCeased without adding
		// it to the pile: moving it to a graveyard later would duplicate
		// the top card.
		Move(g, src.ID, src.Zone, state.ZCeased)
	} else {
		// The mutating card goes under the target: park its object and
		// stack it beneath the survivor's existing cards (top-of-pile
		// first, so the newest under-card goes last).
		Move(g, src.ID, src.Zone, state.ZCeased)
		survivor = g.Obj(e.Obj)
		if survivor == nil {
			return
		}
		survivor.MergedCards = append(survivor.MergedCards, state.MergedCard{Obj: e.IDs[0], Card: srcCard, FaceIdx: srcFace})
	}
	survivor.TimesMutated += e.Amount
}

// foldStackCopy folds Kind StackCopy into state.
func foldStackCopy(g *state.Game, e *Event) {
	if !validPlayer(g, e.Player) {
		return
	}
	src := g.Obj(e.Obj)
	if e.Text == "copy card for play" {
		// Cipher's CopyCard$ True on Play: mint a copy of the encoded
		// card in the temporary library holding zone. The cast flow
		// then moves this copy onto the stack. The event, rather than
		// the rules caller, owns the mutation so replay derives its ID.
		if src == nil || src.Face() == nil {
			return
		}
		card, faceIdx := src.Card, src.FaceIdx
		o := g.AddObject(card, e.Player)
		o.SetFaceIdx(faceIdx)
		o.IsCopy = true
		return
	}
	if src == nil || src.Zone != state.ZStack {
		return
	}
	// Snapshot everything read from src into locals before AddObject:
	// AddObject appends to g.Objs and may reallocate its backing array,
	// and src is a pointer into that array (g.Obj returns &g.Objs[id-1])
	// -- so a *src field read after AddObject would come from whatever
	// the old backing array still holds, not necessarily kept in sync
	// with the live object src names. Correct today only because
	// nothing between AddObject and the reads below mutates src; this
	// is the engine's one mutation path, so it does not get to rely on
	// that happening to remain true.
	card, faceIdx, ability, source := src.Card, src.FaceIdx, src.Ability, src.Source
	// The as-cast battlefield is immutable, so the copy shares the pointer: it
	// answers "as you cast this spell" with the ORIGINAL cast's facts.
	castBattlefield := src.CastBattlefield
	stackKind, stackKindKnown := src.StackKind, src.StackKindKnown
	// A copy of a HAS-ALL-ABILITIES-OF wrapper keeps the minted foreign-face
	// provenance (r3): the copy resolves the same compiled SA, so it reads
	// the same owning face.
	gainedFace, gainedFrom := src.GainedFace, src.GainedFrom
	copyNonLegendary := src.CopyNonLegendary
	copyLoyalty, copyLoyaltySet := src.CopyLoyalty, src.CopyLoyaltySet
	// The copy inherits the original's CastFlags -- a copy of a fused,
	// bestowed or kicked spell resolves as one -- EXCEPT the cast
	// provenance a later reader turns into an "if you cast it"
	// obligation. A copy is put on the stack, not cast (CR 707.10), so
	// state.CastProvenanceFlags is stripped here, at the mint: the copy
	// resolves, Move turns it into a token and clears IsCopy, and
	// rules/altcast.go's battlefield-entry hook has no IsCopy left to
	// tell a never-cast token from the real cast.
	x, castFlags := src.X, src.CastFlags&^state.CastProvenanceFlags
	// Teamwork is a paid additional cost, like Conspire: copies carry the
	// cost-conditioned bit AND the boolean its count/filter readers inspect.
	teamworkPaid := src.TeamworkPaid
	// CR 708.4: a stack COPY of a face-down (morph-family) spell stays
	// face down. The original's stack marker was folded by the MoveZone
	// branch above from the face-down entry Counter, but a copy is minted
	// by AddObject and never passes through that fold -- so derive the
	// face-down status from the morph-family cast flags it inherits (the
	// same family rules/resolution.go's entry hook re-carries). The flags
	// are read here, before AddObject may reallocate g.Objs. An ordinary
	// (face-up) spell carries none of these bits, so its copy stays
	// unmarked and the view's redaction (view/view.go stackViews) is
	// unchanged for it. Cloaked mirrors the Disguise entry's marker so a
	// copied disguised spell keeps the same state bit the cloak machinery
	// reads.
	morphFlags := castFlags & (state.FlagMorphed | state.FlagMegamorphed | state.FlagDisguised)
	// Deep-copy, never alias: the copy's Targets/Remembered must be
	// able to change independently of the original's once both sit on
	// the stack.
	targets := append([]state.Target(nil), src.Targets...)
	remembered := append([]state.Target(nil), src.Remembered...)
	// Mode announcements are copiable characteristics (CR 707.10): a
	// modal copy must resolve the same chosen modes, not ask for new ones.
	chosenModes := state.CloneChosenModes(src.ChosenModes)
	// A DefinedTarget$ copy names its own targets (the StackCopy doc): the
	// event's IDs replace the inherited list with object targets. The
	// ids are not re-validated here beyond existence -- the copy's own CR
	// 608.2b resolution recheck judges legality, exactly as it does for
	// every other stack object's targets.
	if len(e.IDs) > 0 {
		targets = targets[:0]
		for _, id := range e.IDs {
			if g.Obj(id) != nil {
				targets = append(targets, state.Target{Obj: id})
			}
		}
	}

	o := g.AddObject(card, e.Player)
	Move(g, o.ID, state.ZLibrary, state.ZStack)
	o.SetFaceIdx(faceIdx)
	o.Ability, o.Source = ability, source
	o.CastBattlefield = castBattlefield
	o.StackKind, o.StackKindKnown = stackKind, stackKindKnown
	o.GainedFace, o.GainedFrom = gainedFace, gainedFrom
	o.Targets = targets
	o.Remembered = remembered
	o.ChosenModes = chosenModes
	o.X, o.CastFlags, o.IsCopy = x, castFlags, true
	o.TeamworkPaid = teamworkPaid
	// A copy was never cast (CR 707.10), so it carries no rider grants:
	// the Spell.MayPlaySource/AddsCounters provenance is a statement about
	// the original's cast, and FlagAddsCounters is stripped from castFlags
	// by CastProvenanceFlags above.
	o.ManaAddsCounterGrants = nil
	if morphFlags != 0 {
		o.FaceDown = true
		o.Cloaked = morphFlags&state.FlagDisguised != 0
	}
	// CR 707.10c: Amount is the creating CopySpellAbility's
	// MayChooseTarget$ discriminator (1 = true). It rides the event so the
	// permission travels with the COPY instance -- an external copier
	// (Mirari, Cloven Casting, Storm, Replicate) whose SA is not part of
	// the copied spell's own text still grants the election on replay,
	// and effects/copy.go never has to reach into rules to ask.
	o.CopyMayChooseTarget = e.Amount == 1
	// The creating CopySpellAbility's rider payload (Counter, the
	// StackCopyCounter grammar): NonLegendary$ True strips the Legendary
	// supertype and SetLoyalty$ overrides the entry's starting loyalty
	// (Ob Nixilis, the Adversary's Casualty:X script). Both changed
	// characteristics are copiable: a later copy of this copy inherits
	// them even without its own payload (CR 707.2), the event's own
	// payload winning when present. Snapshot before AddObject, which may
	// reallocate g.Objs.
	if cc := ParseStackCopyCounter(e.Counter); cc.NonLegendary || cc.HasLoyalty {
		if cc.NonLegendary {
			o.CopyNonLegendary = true
		}
		if cc.HasLoyalty {
			o.CopyLoyalty, o.CopyLoyaltySet = cc.Loyalty, true
		}
	} else {
		o.CopyNonLegendary = copyNonLegendary
		if copyLoyaltySet {
			o.CopyLoyalty, o.CopyLoyaltySet = copyLoyalty, true
		}
	}
}

// foldPair folds Kind Pair into state.
func foldPair(g *state.Game, e *Event) {
	// CR 702.103: a Soulbond pairing. Obj is the pairing permanent and
	// IDs[0] its chosen partner; both fields are set reciprocally when
	// both are battlefield permanents. Neither half is written when a
	// pairing ends (the paired field is reset by each object's own
	// Move when one leaves the battlefield).
	if len(e.IDs) > 0 {
		applyPair(g, e.Obj, e.IDs[0])
	}
}

// foldCopyToken folds Kind CopyToken into state.
func foldCopyToken(g *state.Game, e *Event) {
	// DB$ CopyPermanent's mint (task copyp1: Flamerush Rider, Molten
	// Echoes, the populate family). Mirrors MyriadCopy's discipline: the
	// copy is the SOURCE CARD + face snapshot taken BEFORE AddObject
	// (which may reallocate g.Objs), the token's printed characteristics
	// are the copied card's, and the entry-state riders ride the Amount
	// bitmask so a replay derives the identical object. Like MyriadCopy
	// this only MINTS (in the untracked ZLibrary state AddObject leaves
	// it in); the caller follows with a genuine MoveZone so the entry is
	// an ordinary ChangesZone-matchable event. AtEOT$ ExileCombat flags
	// IsMyriad so the existing end-of-combat cleanup -- the same fold and
	// the same rules-side emit gate Myriad tokens already use -- exiles
	// the copy with identical semantics (end of combat, battlefield
	// only). Totality like every case: a missing source or an invalid
	// player mints nothing.
	if !validPlayer(g, e.Player) {
		return
	}
	// A DefinedName$ copy has no battlefield source object: Text names a
	// card in the game's NameUniverse, resolved through the same
	// state.Game.NamedCard the effect's reach check used. The named-card
	// branch and the object branch share every line below, so the two mint
	// identically.
	var card *cards.Card
	var faceIdx uint8
	srcAtEOTTrigBody := ""
	if e.Obj == 0 {
		if e.Text == "" {
			return
		}
		card = g.NamedCard(e.Text)
		if card == nil {
			return
		}
	} else {
		src := g.Obj(e.Obj)
		if src == nil || src.Card == nil {
			return
		}
		card, faceIdx = src.Card, src.FaceIdx
		srcAtEOTTrigBody = src.AtEOTTrigBody
	}
	o := g.AddObject(card, e.Player)
	o.IsToken = true
	o.IsCopy = true
	o.SetFaceIdx(faceIdx)
	// AtEOTTrig$ is a copiable value (CR 707.2): the mint's own body when
	// the copying spell carries one (Counter), else the source object's --
	// a token copy of an AtEOTTrig$ token still sacrifices itself at the
	// end step. See state.Object.AtEOTTrigBody. A named-card copy has no
	// source object, so only the spell's own body applies.
	o.AtEOTTrigBody = e.Counter
	if o.AtEOTTrigBody == "" {
		o.AtEOTTrigBody = srcAtEOTTrigBody
	}
	if e.Amount&CopyTokenTapped != 0 {
		o.Tapped = true
	}
	if e.Amount&CopyTokenAttacking != 0 {
		o.IsAttacking = true
		if len(e.IDs) > 0 {
			o.Attacking = state.PlayerID(e.IDs[0])
		}
	}
	if e.Amount&CopyTokenExileCombat != 0 {
		o.IsMyriad = true
	}
}

// foldCardToken folds Kind CardToken into state.
func foldCardToken(g *state.Game, e *Event) {
	// A battlefield token that is a copy of the CARD object Obj names
	// (encore's "create a token copy" per opponent). Mirrors StackCopy's
	// snapshot discipline: every read from src is taken into a local
	// BEFORE AddObject, because AddObject may reallocate g.Objs and a
	// src pointer read after it would read the old backing array.
	// Totality like every case: a missing source (already ceased to
	// exist) or an invalid player mints nothing.
	if !validPlayer(g, e.Player) {
		return
	}
	src := g.Obj(e.Obj)
	if src == nil || src.Card == nil {
		return
	}
	card, faceIdx := src.Card, src.FaceIdx
	o := g.AddObject(card, e.Player)
	o.IsToken = true
	o.SetFaceIdx(faceIdx)
	Move(g, o.ID, state.ZLibrary, state.ZBattlefield)
	applyEntryCounterPairs(o, e.Pairs)
	// Encore encodes its required defender as seat+1; zero remains the
	// ordinary CardToken shape. The current turn is folded here so replay
	// reconstructs the same one-turn attack requirement.
	if e.Amount > 0 {
		defender := state.PlayerID(e.Amount - 1)
		if validPlayer(g, defender) {
			o.EncoreAttackTurn = g.Turn
			o.EncoreAttackDefender = defender
		}
	}
}

// foldTokenCreate folds Kind TokenCreate into state.
func foldTokenCreate(g *state.Game, e *Event) {
	if !validPlayer(g, e.Player) {
		return
	}
	def, ok := g.Tokens[e.Text]
	if !ok || def == nil {
		return
	}
	o := g.AddObject(def, e.Player)
	o.IsToken = true
	Move(g, o.ID, state.ZLibrary, state.ZBattlefield)
	applyEntryCounterPairs(o, e.Pairs)
}

// foldCloneStatic folds Kind CloneStatic into state.
func foldCloneStatic(g *state.Game, e *Event) {
	if o := g.Obj(e.Obj); o != nil && o.CopyFace != nil {
		if statics, ok := cards.ParseStaticLines(e.Text); ok {
			face := *o.CopyFace
			face.Statics = append(append([]cards.Static(nil), face.Statics...), statics...)
			o.SetCopyFace(&face)
		}
	}
}

// foldClonePermanent folds Kind ClonePermanent into state.
func foldClonePermanent(g *state.Game, e *Event) {
	// CR 613.1a's layer-1 copy basis (DB$ Clone, api:Clone). Obj is the
	// object that becomes the copy and IDs[0] the object copied from; an
	// empty or zero id CLEARS the basis. The synthetic face is a value
	// copy of the source's PRINTED face taken here, inside Apply, so a
	// replay derives the identical characteristics from the same event.
	// Modifier parameters (AddTypes$/SetColor$/AddKeywords$/SetPower$/
	// SetToughness$/RemoveCardTypes$/RemoveCreatureTypes$) are separate
	// layer-4/5/6/7 continuous effects the primitive registered; they are
	// deliberately NOT folded into this face, so the CR 613 layer walk
	// stays the one place exceptions settle. NewName$ rides Text and the
	// GainThisAbility$ rider rides Counter.
	o := g.Obj(e.Obj)
	if o == nil {
		return
	}
	if e.Counter != "chosen-name" && (len(e.IDs) == 0 || e.IDs[0] == 0) {
		o.SetCopyFace(nil)
		o.CopyGainThisAbility = false
		return
	}
	var face *cards.Face
	if e.Counter == "chosen-name" {
		if i, ok := g.NameUniverse.FirstByName(e.Text); ok {
			face = g.NameUniverse.Card(i).Faces[0]
		}
	} else if src := g.Obj(e.IDs[0]); src != nil {
		face = src.Face()
	}
	if face == nil {
		return
	}
	sf := *face
	if e.Counter != "chosen-name" && e.Text != "" {
		sf.Name = e.Text
	}
	// GainThisAbility$ True: "...except it has this ability". New
	// events carry a one-based index of the resolving ability, or -- for
	// a DB$/SVar-under-trigger body whose root is a TRIGGER -- of the
	// resolving trigger (Counter "gain-this-trigger"); old events without
	// one retain their original whole-list replay semantics. The
	// original face's SVar table is still merged to retain references used
	// by the granted ability.
	if e.Counter == "gain-this-ability" || e.Counter == "gain-this-trigger" {
		if of := o.Face(); of != nil {
			if e.Counter == "gain-this-trigger" {
				// Amount is a one-based index into the become object's face
				// TRIGGERS: Forge appends exactly root.getTrigger().copy(...),
				// so the copy keeps the recurring trigger that makes a
				// recurring Copy carrier recur.
				if e.Amount > 0 && int(e.Amount) <= len(of.Triggers) {
					sf.Triggers = append(append([]cards.Trigger(nil), sf.Triggers...), of.Triggers[e.Amount-1])
				}
			} else if e.Amount > 0 && int(e.Amount) <= len(of.Abilities) {
				// Amount is a one-based index into the become object's face
				// abilities. Zero retains the legacy whole-list form for old
				// logs; new Clone effects identify the resolving ability.
				sf.Abilities = append(append([]*cards.SA(nil), sf.Abilities...), of.Abilities[e.Amount-1])
			} else if e.Amount == 0 && len(of.Abilities) > 0 {
				sf.Abilities = append(append([]*cards.SA(nil), sf.Abilities...), of.Abilities...)
			}
			if len(of.SVars) > 0 {
				merged := make(map[string]string, len(sf.SVars)+len(of.SVars))
				for k, v := range sf.SVars {
					merged[k] = v
				}
				for k, v := range of.SVars {
					merged[k] = v
				}
				sf.SVars = merged
			}
		}
		o.CopyGainThisAbility = true
	} else {
		o.CopyGainThisAbility = false
	}
	o.SetCopyFace(&sf)
}

// foldFlipFace folds Kinds FlipFace, Specialize into state.
func foldFlipFace(g *state.Game, e *Event) {
	if o := g.Obj(e.Obj); o != nil && o.Card != nil &&
		e.Amount >= 0 && int(e.Amount) < len(o.Card.Faces) {
		o.SetFaceIdx(uint8(e.Amount))
	}
}

// foldTurnFaceDown folds Kind TurnFaceDown into state.
func foldTurnFaceDown(g *state.Game, e *Event) {
	if o := g.Obj(e.Obj); o != nil && o.Zone == state.ZBattlefield && !o.FaceDown {
		setType, power, toughness, hasPT, _ := FaceDownEntryFields(e.Counter)
		o.FaceDown = true
		o.FaceDownSetType = setType
		o.FaceDownPower, o.FaceDownToughness = power, toughness
		o.FaceDownHasPT = hasPT
	}
}

// foldTurnFaceUp folds Kind TurnFaceUp into state.
func foldTurnFaceUp(g *state.Game, e *Event) {
	// CR 708.6: turning a face-down permanent face up reveals the face it
	// already had -- no FaceIdx change -- and retires the CR 708.5
	// face-down characteristic set (the folded FaceDownSetType/Power/
	// Toughness payload). Gated on the battlefield, the same way
	// faceDownEffective reads it: a marker stranded on a card that left
	// the battlefield is not a turn-up.
	if o := g.Obj(e.Obj); o != nil && o.Zone == state.ZBattlefield && o.FaceDown {
		o.FaceDown = false
		o.FaceDownSetType = ""
		o.FaceDownPower = 0
		o.FaceDownToughness = 0
		o.FaceDownHasPT = false
	}
}
