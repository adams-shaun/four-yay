package mzbridge

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// Port of StateEncoder.java (mage/player/ai/encoder/StateEncoder.java at
// the XMage fork 48e4918413): one decision's game state as a set of hashed
// feature ids. Every emission site below carries the Java line it mirrors,
// in the Java's own order. The hash, the namespace tree and the thermometer
// coding live in hash.go and features.go.
//
// What matches upstream is the SCHEME and, wherever gorge has the same
// fact, the feature NAME. Where XMage generates a string gorge does not
// have (an ability's rule, an effect's text, a cost's text) the name comes
// from the nearest gorge source; encode_face.go and encode_names.go say
// which. The sites with no gorge counterpart are marked "omitted".
//
// Hidden information. Upstream encodes a hand only for the player deciding
// at that state unless perfectInfo is set (StateEncoder.java:601; the
// comparison there is a Java reference comparison of two UUIDs, which holds
// whenever equals does because a copied player keeps the same UUID object:
// PlayerImpl.java:234, ComputerPlayerMCTS.java:381). Note what that rule
// means when the OTHER seat decides (an opponent node of upstream's
// clairvoyant search): the opponent's hand is encoded and the owner's is a
// count. Here every zone list comes from gorge's redacted projection for
// the DECIDING seat (view.ProjectLeanInto): the other seat's hand is a
// count, a library is a count, and a face-down card the seat may not look
// at is a nameless object. The encoder never reads a hand or a library from
// state.Game; it looks an object up in state.Game only by an id the
// projection listed. EncodeOptions.SeeOpponentHand is upstream's
// perfectInfo: the projection becomes the omniscient one (both hands,
// still no library order).

// EncodeOptions configures one Encode call.
type EncodeOptions struct {
	// SeeOpponentHand is upstream's perfectInfo (game.yml
	// hiddenInfo.see_opponent_hand): encode both hands. The zero value is
	// the DraftZero #2a setting.
	SeeOpponentHand bool
	// KeepDuplicatePermanents turns off an upstream artefact. The Java keys
	// a player's permanents by Permanent.getValue in a TreeMap
	// (StateEncoder.java:332-335), so permanents whose keys are equal --
	// five untapped copies of one basic land -- collapse to ONE encoded
	// permanent. The zero value reproduces that.
	KeepDuplicatePermanents bool
	// Player and Opponent are the micro-decision histories of the encoding
	// seat and the other seat (PlayerHistory, StateEncoder.java:520-542).
	// gorge has no such record; a caller that decomposes a decision into
	// steps supplies it.
	Player, Opponent MicroHistory
}

// MicroHistory is upstream's PlayerHistory: what a player has already
// chosen inside the decision in progress.
type MicroHistory struct {
	Targets []string // entity names, in order
	Choices []string
	Uses    []bool
	Amounts []int
}

// Encoder holds the reusable storage of Encode: the projected view and the
// per-face tables. It is not safe for concurrent use; one per worker.
type Encoder struct {
	faces map[*cards.Face]*faceInfo
	v     view.View

	// one Encode call's context
	e       *rules.Engine
	g       *state.Game
	d       *decision.Decision
	viewer  state.PlayerID
	decider state.PlayerID
	opts    EncodeOptions

	seen     map[state.ObjID]*view.CardView // every card the projection listed
	attached map[state.ObjID][]state.ObjID  // host -> attachments, id order
	linked   map[state.ObjID][]state.ObjID  // exiling permanent -> its exiled cards
	stack    []stackEntry                   // top first
	perms    []permEntry
	sorted   []*view.CardView
	words    []string
}

// NewEncoder returns an Encoder with empty caches.
func NewEncoder() *Encoder {
	return &Encoder{faces: map[*cards.Face]*faceInfo{}, seen: map[state.ObjID]*view.CardView{},
		attached: map[state.ObjID][]state.ObjID{}, linked: map[state.ObjID][]state.ObjID{}}
}

// leanOmit is everything of the seat projection the encoder never reads.
const leanOmit = view.OmitLibrary | view.OmitAvailable | view.OmitArchetype | view.OmitAbilityCosts |
	view.OmitEffectiveCost | view.OmitDecision | view.OmitOwnDeck | view.OmitPotential

// Encode is StateEncoder.processState for one decision of a two-player
// game: it refreshes fs and fills it with the features of e's state as
// viewer's encoder sees it ("Player" is viewer, "Opponent" the other seat,
// StateEncoder.java:655-660). d is the pending decision and names the
// deciding player; a nil d means viewer decides and nothing is known to be
// activatable. at and decisionText are the two root features upstream's
// search supplies (MCTSPlayer.java:105: "priority" for a priority decision).
// fs.IDs() is the state's id set; with fs.SetDebug(true), fs.Describe()
// names every id.
//
// The caller must not Submit to e during the call.
func (enc *Encoder) Encode(e *rules.Engine, viewer state.PlayerID, d *decision.Decision, at ActionType, decisionText string, fs *FeatureSet, opts EncodeOptions) error {
	if e == nil || e.Game() == nil || fs == nil {
		return errors.New("mzbridge: Encode needs an engine and a feature set")
	}
	g := e.Game()
	if len(g.Players) != 2 || int(viewer) >= 2 {
		return errors.New("mzbridge: the state encoder is two-player (Player and Opponent)")
	}
	enc.e, enc.g, enc.d, enc.viewer, enc.opts = e, g, d, viewer, opts
	enc.decider = viewer
	if d != nil {
		if int(d.Player) >= 2 {
			return errors.New("mzbridge: decision for a seat that does not exist")
		}
		enc.decider = d.Player
	}
	e.BeginDerivedReads()
	defer e.EndDerivedReads()
	if opts.SeeOpponentHand {
		view.ProjectForInto(&enc.v, g, e, enc.decider, view.Omniscient, nil)
	} else {
		view.ProjectLeanInto(&enc.v, g, e, enc.decider, nil, leanOmit)
	}
	enc.index()

	// 630-631
	fs.Refresh()
	root := fs.Root()
	// 634-636: game.getTurnStepType().toString(), skipped before the game
	// has a phase.
	if g.Turn > 0 {
		if s := StepName(g.Step); s != "" {
			root.Add(s)
		}
	}
	// 639: decisionType.toString()
	root.Add(at.String())
	// 641: cleanString(decisionsText)
	root.Add(CleanString(decisionText))
	// 646-647: the stack does not pool into the root
	enc.encodeStack(root.Sub("Stack", false))
	// 650-651
	enc.encodeExile(root.Sub("Exile", true))
	// 656-657: PlayerA is the encoder's owner
	enc.encodePlayer(viewer, root.Sub("Player", true), &opts.Player)
	// 659-660
	enc.encodePlayer(1-viewer, root.Sub("Opponent", true), &opts.Opponent)
	return nil
}

// Encode is Encoder.Encode on a fresh Encoder. A caller encoding many
// states keeps one Encoder per worker instead.
func Encode(e *rules.Engine, viewer state.PlayerID, d *decision.Decision, at ActionType, decisionText string, fs *FeatureSet, opts EncodeOptions) error {
	return NewEncoder().Encode(e, viewer, d, at, decisionText, fs, opts)
}

// DescribeState is the debug dump: one "id<TAB>feature path" line per
// feature of the state, ordered by id.
func DescribeState(e *rules.Engine, viewer state.PlayerID, d *decision.Decision, at ActionType, decisionText string, opts EncodeOptions) ([]string, error) {
	fs := NewFeatureSet()
	fs.SetDebug(true)
	if err := Encode(e, viewer, d, at, decisionText, fs, opts); err != nil {
		return nil, err
	}
	return fs.Describe(), nil
}

func (enc *Encoder) face(f *cards.Face) *faceInfo {
	fi := enc.faces[f]
	if fi == nil {
		fi = buildFaceInfo(f)
		enc.faces[f] = fi
	}
	return fi
}

// index builds the lookups one Encode needs from the projection: every
// listed card by id, attachments by host, linked exile by exiling
// permanent, and the stack top first.
func (enc *Encoder) index() {
	clear(enc.seen)
	clear(enc.attached)
	clear(enc.linked)
	g := enc.g
	for p := range enc.v.Players {
		pv := &enc.v.Players[p]
		for _, list := range [...][]view.CardView{pv.Battlefield, pv.Graveyard, pv.Exile, pv.Hand} {
			for i := range list {
				enc.seen[list[i].ID] = &list[i]
			}
		}
	}
	for p := range enc.v.Players {
		pv := &enc.v.Players[p]
		for i := range pv.Battlefield {
			cv := &pv.Battlefield[i]
			if cv.AttachedTo != 0 && enc.seen[cv.AttachedTo] != nil {
				enc.attached[cv.AttachedTo] = append(enc.attached[cv.AttachedTo], cv.ID)
			}
		}
		for i := range pv.Exile {
			cv := &pv.Exile[i]
			if o := g.Obj(cv.ID); o != nil && o.ExiledWith != 0 {
				if src := enc.seen[o.ExiledWith]; src != nil && g.Obj(src.ID).Zone == state.ZBattlefield {
					enc.linked[o.ExiledWith] = append(enc.linked[o.ExiledWith], cv.ID)
				}
			}
		}
	}
	// SpellStack is an ArrayDeque pushed at the head, so its iterator runs
	// top first (StateEncoder.java:414-423); the projection lists bottom
	// first.
	enc.stack = enc.stack[:0]
	for i := len(enc.v.Stack) - 1; i >= 0; i-- {
		enc.stack = append(enc.stack, enc.stackEntryOf(&enc.v.Stack[i]))
	}
}

// encodePlayer is processPlayer (543-619).
func (enc *Encoder) encodePlayer(p state.PlayerID, n *Node, hist *MicroHistory) {
	g, d := enc.g, enc.d
	pl := &g.Players[p]
	pv := &enc.v.Players[p]

	// 546: isInPayManaMode -- the announced mana-payment window
	if d != nil && d.Player == p && d.ManaPayment != nil {
		n.AddFeature("InPayManaMode", false)
	}
	// 547: isActivating -- omitted, gorge keeps no such flag

	// 550: processMicroDecisions (520-542). The four chains exist for every
	// player, empty or not.
	chain := n.Sub("ChosenTargets", false) // 523
	for _, t := range hist.Targets {
		chain = chain.Sub(t, true) // 525
	}
	chain = n.Sub("ChosenChoices", false) // 528
	for _, c := range hist.Choices {
		chain = chain.Sub(c, true) // 530
	}
	chain = n.Sub("UseChoices", false) // 533
	for _, u := range hist.Uses {
		chain = chain.Sub(strconv.FormatBool(u), true) // 535
	}
	chain = n.Sub("AmountChoices", false) // 538
	for _, a := range hist.Amounts {
		chain = chain.Sub(strconv.Itoa(a), true) // 540
	}

	// 555
	if g.Turn > 0 && g.Active == p {
		n.Add("IsActivePlayer")
	}
	// 556
	if enc.decider == p {
		n.Add("IsDecisionPlayer")
	}
	// 557
	n.AddNumeric("LifeTotal", int(pv.Life))
	// 558: landsPlayed < landsPerTurn. gorge exposes no land-drop limit, so
	// this is "no land played yet this turn", or the engine offering a land
	// play to this player right now.
	if pl.LandsPlayed < 1 || enc.offersLandPlay(p) {
		n.Add("CanPlayLand")
	}
	// 559-566: day/night -- omitted, gorge has no day/night designation

	// 569
	n.AddNumeric("LibraryCount", pv.LibrarySize)

	// 573-579: permanents attached to the player (a Curse)
	var curses *Node
	for q := range enc.v.Players {
		bf := enc.v.Players[q].Battlefield
		for i := range bf {
			o := g.Obj(bf[i].ID)
			if o == nil || !o.HasAttachedPlayer || o.AttachedPlayer != p {
				continue
			}
			if curses == nil {
				curses = n.Sub("Attachments", false) // 575
			}
			enc.encodePermanent(&bf[i], p, curses, 0) // 577
		}
	}
	// 581-584: player counters
	for _, c := range pl.Counters {
		if name := counterName(c.Kind); name != "" {
			n.AddNumeric(name, int(c.N))
		}
	}

	// 587-588: mana pool, processManaPool (446-457) and processMana (438-445)
	mp := n.Sub("ManaPool", false)
	mp.AddNumeric("GreenMana", int(pl.Pool[state.MG]))     // 439
	mp.AddNumeric("RedMana", int(pl.Pool[state.MR]))       // 440
	mp.AddNumeric("BlueMana", int(pl.Pool[state.MU]))      // 441
	mp.AddNumeric("WhiteMana", int(pl.Pool[state.MW]))     // 442
	mp.AddNumeric("BlackMana", int(pl.Pool[state.MB]))     // 443
	mp.AddNumeric("ColorlessMana", int(pl.Pool[state.MC])) // 444
	// 449-456: conditional mana is gorge's restricted floating mana
	if len(pv.PoolRestrictions) > 0 {
		cm := mp.Sub("ConditionalMana", false) // 451
		for _, r := range pv.PoolRestrictions {
			c := cm.Sub(r.Text, true) // 453
			if name := manaColourFeature(r.Color); name != "" {
				c.AddNumeric(name, int(r.Amount)) // 454
			}
		}
	}

	// 591-593
	enc.encodeBattlefield(p, n.Sub("Battlefield", true))
	// 596-598
	enc.encodeCards(pv.Graveyard, inGraveyard, n.Sub("Graveyard", true))
	// 601-608: the projection lists a hand exactly when this player is the
	// decider, or the encoding is perfect-information
	if pv.Hand != nil {
		enc.encodeCards(pv.Hand, inHand, n.Sub("Hand", true)) // 603-604
	} else {
		n.AddNumeric("CardsInHand", pv.HandSize) // 607
	}
	// 610-611: processCommandZone (458-497) -- emblems and commanders are
	// omitted; the node itself is always emitted
	n.Sub("CommandZone", false)
	// 614-615: processWatchers (498-519). XMage registers the spell-cast and
	// life-lost watchers in every game (GameImpl.initGameDefaultWatchers);
	// the life-gained and token watchers exist only when a card in the game
	// brings them. gorge counts all four from the log in every game.
	gw := n.Sub("GlobalWatchers", false)
	cast, gained, lost, tokens := enc.thisTurn(p)
	gw.AddNumeric("SpellsCastThisTurn", cast)      // 502
	gw.AddNumeric("LifeGainedThisTurn", gained)    // 507
	gw.AddNumeric("LifeLostThisTurn", lost)        // 512
	gw.AddNumeric("TokensCreatedThisTurn", tokens) // 517
}

func manaColourFeature(sym string) string {
	if sym == "" {
		return ""
	}
	switch sym[len(sym)-1] {
	case 'G':
		return "GreenMana"
	case 'R':
		return "RedMana"
	case 'U':
		return "BlueMana"
	case 'W':
		return "WhiteMana"
	case 'B':
		return "BlackMana"
	case 'C':
		return "ColorlessMana"
	}
	return ""
}

// thisTurn folds the public event log since the last turn change into the
// four counts upstream's watchers keep.
func (enc *Encoder) thisTurn(p state.PlayerID) (cast, gained, lost, tokens int) {
	if enc.e.L == nil {
		return
	}
	evs := enc.e.L.Events
	for i := len(evs) - 1; i >= 0; i-- {
		ev := &evs[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Player != p {
			continue
		}
		switch ev.Kind {
		case events.PutOnStack:
			cast++
		case events.LifeChange:
			if ev.Amount > 0 {
				gained += int(ev.Amount)
			} else {
				lost -= int(ev.Amount)
			}
		case events.TokenCreate, events.CardToken, events.CopyToken:
			tokens++
		}
	}
	return
}

func (enc *Encoder) offersLandPlay(p state.PlayerID) bool {
	d := enc.d
	if d == nil || d.Player != p || d.Kind != decision.KPriority {
		return false
	}
	for i := range d.Options {
		if d.Options[i].Kind == "play_land" {
			return true
		}
	}
	return false
}

// permEntry is one permanent with the key upstream sorts and collapses by.
type permEntry struct {
	cv  *view.CardView
	key string
}

// encodeBattlefield is processBattlefield (330-340): the player's
// permanents in the order of Permanent.getValue(game, playerId)
// (PermanentImpl.java:287-314), one per distinct key.
func (enc *Encoder) encodeBattlefield(p state.PlayerID, n *Node) {
	bf := enc.v.Players[p].Battlefield
	enc.perms = enc.perms[:0]
	for i := range bf {
		enc.perms = append(enc.perms, permEntry{cv: &bf[i], key: enc.permanentKey(&bf[i])})
	}
	slices.SortStableFunc(enc.perms, func(a, b permEntry) int { return strings.Compare(a.key, b.key) })
	for i := range enc.perms {
		// TreeMap.put: a later permanent with the same key replaces the
		// earlier one, so the last of each run is the one encoded.
		if !enc.opts.KeepDuplicatePermanents && i+1 < len(enc.perms) && enc.perms[i+1].key == enc.perms[i].key {
			continue
		}
		cv := enc.perms[i].cv
		enc.encodePermanent(cv, p, n.Sub(cv.Name, true), 0) // 337-338
	}
}

// permanentKey is Permanent.getValue(game, playerId): controller flag,
// name, tapped, damage, subtypes, supertypes, power, toughness, abilities,
// attachment names, combat state, counters. The abilities term is the
// permanent's current face name and keywords, the nearest gorge has to
// Abilities.getValue.
func (enc *Encoder) permanentKey(cv *view.CardView) string {
	var b strings.Builder
	b.WriteString("true")
	b.WriteString(cv.Name)
	b.WriteString(strconv.FormatBool(cv.Tapped))
	b.WriteString(strconv.Itoa(int(cv.Damage)))
	var sub, super []string
	isCreature := false
	for _, w := range enc.e.Derived(cv.ID).Types {
		switch class, _ := classifyType(w); class {
		case typeSub:
			sub = append(sub, w)
		case typeSuper:
			super = append(super, w)
		default:
			isCreature = isCreature || w == "Creature"
		}
	}
	b.WriteString("[" + strings.Join(sub, ", ") + "][" + strings.Join(super, ", ") + "]")
	b.WriteString(strconv.Itoa(int(cv.Power)))
	b.WriteString(strconv.Itoa(int(cv.Toughness)))
	if o := enc.g.Obj(cv.ID); o != nil && o.Face() != nil && cv.Name != "" {
		b.WriteString(o.Face().Name)
	}
	b.WriteString(strings.Join(cv.Keywords, ","))
	names := enc.words[:0]
	for _, id := range enc.attached[cv.ID] {
		names = append(names, enc.seen[id].Name)
	}
	slices.Sort(names)
	for _, s := range names {
		b.WriteString(s)
	}
	if isCreature && cv.Attacking {
		b.WriteString("Attacking")
		names = names[:0]
		for _, id := range cv.BlockedBy {
			names = append(names, enc.entityName(state.Target{Obj: id}))
		}
		slices.Sort(names)
		for _, s := range names {
			b.WriteString(s + "Blocking")
		}
	}
	enc.words = names[:0]
	if len(cv.Counters) > 0 {
		o := enc.g.Obj(cv.ID)
		cs := make([]string, 0, len(o.Counters))
		for _, c := range o.Counters {
			if name := counterName(c.Kind); name != "" && c.N > 0 {
				cs = append(cs, name+strconv.Itoa(int(c.N)))
			}
		}
		slices.Sort(cs)
		for _, s := range cs {
			b.WriteString(s)
		}
	}
	return b.String()
}

// encodeCards is processGraveyard (341-346) and processHand (347-352):
// the zone's cards in name order (Cards.getCardsSorted), each a sub-node.
func (enc *Encoder) encodeCards(list []view.CardView, zone zoneMask, n *Node) {
	enc.sorted = enc.sorted[:0]
	for i := range list {
		enc.sorted = append(enc.sorted, &list[i])
	}
	slices.SortStableFunc(enc.sorted, func(a, b *view.CardView) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return int(a.ID) - int(b.ID)
	})
	// a copy: encoding a card can re-enter (a permanent's linked exile zone)
	list2 := append([]*view.CardView(nil), enc.sorted...)
	for _, cv := range list2 {
		cn := n.Sub(cv.Name, true) // 343, 349, 427
		if o := enc.g.Obj(cv.ID); o != nil && !hiddenFace(cv) {
			enc.encodeCardInZone(o, printedFace(o), zone, cn) // 344, 350, 428
		}
	}
}

// hiddenFace reports a face-down card the projection blanked: the deciding
// seat may not look at it.
func hiddenFace(cv *view.CardView) bool { return cv.FaceDown && cv.Name == "" }

// printedFace is the face of the physical card (PermanentCard.getCard),
// which is what the static card features describe; a copy effect changes
// the object's current face, not its card.
func printedFace(o *state.Object) *cards.Face {
	if o.Card != nil && int(o.FaceIdx) < len(o.Card.Faces) && o.Card.Faces[o.FaceIdx] != nil {
		return o.Card.Faces[o.FaceIdx]
	}
	return o.Face()
}

// encodeExile is processExile (431-437): one sub-node per exile zone, each
// holding its cards by name (processExileZone, 425-430). XMage always has
// the "Permanent - Exile" zone; a permanent that exiles cards "until it
// leaves" owns a zone named after it. XMage's exile is one shared zone set,
// so both players' cards are listed together.
func (enc *Encoder) encodeExile(n *Node) {
	var plain []view.CardView
	type zone struct {
		name  string
		src   state.ObjID
		cards []view.CardView
	}
	var zones []zone
	for p := range enc.v.Players {
		ex := enc.v.Players[p].Exile
		for i := range ex {
			o := enc.g.Obj(ex[i].ID)
			if o == nil {
				continue
			}
			if o.ExiledWith == 0 || enc.linked[o.ExiledWith] == nil {
				plain = append(plain, ex[i])
				continue
			}
			at := -1
			for z := range zones {
				if zones[z].src == o.ExiledWith {
					at = z
				}
			}
			if at < 0 {
				zones = append(zones, zone{name: enc.exileZoneName(o.ExiledWith), src: o.ExiledWith})
				at = len(zones) - 1
			}
			zones[at].cards = append(zones[at].cards, ex[i])
		}
	}
	slices.SortStableFunc(zones, func(a, b zone) int {
		if c := strings.Compare(a.name, b.name); c != 0 {
			return c
		}
		return int(a.src) - int(b.src)
	})
	enc.encodeCards(plain, inExile, n.Sub("Permanent - Exile", true)) // 434-435
	for _, z := range zones {
		enc.encodeCards(z.cards, inExile, n.Sub(z.name, true)) // 434-435
	}
}

// exileZoneName is cleanString(ExileZone.getName()) for the zone a
// permanent owns: XMage names it "<idName> [<zcc>] - Exile"
// (CardUtil.createObjectRelatedWindowTitle, Exile.createZone), and
// cleanString strips both bracketed runs.
func (enc *Encoder) exileZoneName(src state.ObjID) string {
	name := ""
	if cv := enc.seen[src]; cv != nil {
		name = cv.Name
	}
	return name + " - Exile"
}
