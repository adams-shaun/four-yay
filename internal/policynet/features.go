package policynet

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// FeatureSet selects the state/option encoding a model was trained under
// (ticket pn12, the feature axis). FeaturesV1 is the pinned encoder of
// policynet.go/option.go, byte for byte: every existing checkpoint, golden and
// digest reads it. The other sets are ADDITIVE: they emit the v1 encoding
// unchanged and then append further hashed rows into the same shared
// TableRows space, so the model geometry (dense widths, slot widths, InW) is
// identical for every set and only the encoder hash differs.
//
//   - FeaturesMZ ("mz"): MageZero-style per-card properties for both
//     battlefields and the viewer's hand (types, current P/T, keywords,
//     tapped, summoning sick, can-attack/can-block, counters, attached-to,
//     damage, attacking), per-seat available (untapped) mana, the stack's
//     spell APIs, and the option-side pn03 bits (the option card's spell API;
//     the decision-kind "other" routed to a hashed id instead of the v1 slot
//     that collides with the Land bit). Read from the redacted view only, so
//     a seat can encode it: checkpointable and playable.
//   - FeaturesMZOppHand ("mz-opphand"): FeaturesMZ plus the opponent's hand
//     (a Diag the label corpus records). DIAGNOSTIC ONLY: it reads hidden
//     information, so WriteCheckpoint refuses it and no seat can load it.
//   - FeaturesMZOracle ("mz-oracle"): FeaturesMZOppHand plus the next draws
//     of both libraries. DIAGNOSTIC ONLY, same refusal.
//   - FeaturesEntity ("entity", ticket pn14): FeaturesMZ plus one ENTITY
//     vector per visible card (entity.go: State.Cards, Option.EntA/EntB).
//     Unlike every set above it changes the model geometry: an entity model
//     carries a per-card encoder, a pooled projection into the state trunk
//     and 2·EntK extra hidden inputs per option (Model.EntK > 0). Read from
//     the redacted view only: checkpointable (schema version 4) and playable.
//   - FeaturesMZOwnLib ("mz-ownlib"): FeaturesMZ plus the seat's HONEST
//     remaining own-library composition (ownlib.go): one
//     "mz|ownlibleft|<name>" row per card name still in the library, valued
//     copies/library size, and "mz|ownlib|land" = the land fraction; when the
//     composition is not derivable from the view, one "mz|ownlib|unknown"
//     row instead. Folded by view.OwnLibrary off the seat's own deck list and
//     what it sees -- never the view's own Library list -- so it is
//     checkpointable and playable. Same geometry as mz (sparse rows only).
type FeatureSet uint8

const (
	FeaturesV1 FeatureSet = iota
	FeaturesMZ
	FeaturesMZOppHand
	FeaturesMZOracle
	FeaturesEntity
	// FeaturesMZOwnLib is appended after every earlier set: the ordinals are
	// part of what a checkpoint's feature set names, never renumbered.
	FeaturesMZOwnLib
)

var featureSetNames = [...]string{"v1", "mz", "mz-opphand", "mz-oracle", "entity", "mz-ownlib"}

// String is the flag spelling.
func (fs FeatureSet) String() string {
	if int(fs) < len(featureSetNames) {
		return featureSetNames[fs]
	}
	return fmt.Sprintf("featureset(%d)", uint8(fs))
}

// ParseFeatureSet maps a flag spelling onto a FeatureSet ("" is v1).
func ParseFeatureSet(s string) (FeatureSet, error) {
	if s == "" {
		return FeaturesV1, nil
	}
	for i, n := range featureSetNames {
		if n == s {
			return FeatureSet(i), nil
		}
	}
	return 0, fmt.Errorf("unknown feature set %q (want %s)", s, strings.Join(featureSetNames[:], ", "))
}

// Diagnostic reports whether the set reads hidden information (the opponent's
// hand or future draws). A diagnostic model is a measurement vehicle only:
// it is never checkpointed and never played by a seat.
func (fs FeatureSet) Diagnostic() bool { return fs == FeaturesMZOppHand || fs == FeaturesMZOracle }

// Diag is the hidden information a diagnostic feature set reads: recorded by
// cmd/searchteacher -label-extras (LabelExtras) and never available to a
// seat.
type Diag struct {
	OppHand       []view.CardView
	OppLibraryTop []string
	OwnLibraryTop []string
}

// SplitOmniscientView turns an omniscient projection (view.ProjectFor with
// view.Omniscient, viewer = seat) into what a diagnostic training record
// holds: the seat's view with every OTHER seat's hand removed, plus a Diag
// whose OppHand is those hands concatenated in player order -- the order
// cmd/searchteacher's label extras record them in. The state encoders read
// only the seat's own hand, so the removal changes no base feature; it keeps
// the returned view's hands shaped like a Seat projection's. Library tops are
// not in any view (an omniscient projection never shows library order), so
// the Diag carries none: it is complete for FeaturesMZOppHand only.
//
// v is not modified: the Players slice is copied before a hand is cleared.
func SplitOmniscientView(v view.View, seat state.PlayerID) (view.View, *Diag) {
	diag := &Diag{OppHand: []view.CardView{}}
	players := make([]view.PlayerView, len(v.Players))
	copy(players, v.Players)
	for i := range players {
		if players[i].ID == seat {
			continue
		}
		diag.OppHand = append(diag.OppHand, players[i].Hand...)
		players[i].Hand = nil
	}
	v.Players = players
	return v, diag
}

// EncoderHashFor is the encoder digest a checkpoint of feature set fs
// carries. FeaturesV1 is EncoderHash() unchanged (every existing checkpoint
// keeps loading); every other set digests the v1 hash plus its own
// name and token-format version, so a checkpoint cannot be read under the
// wrong feature set.
func EncoderHashFor(fs FeatureSet) uint64 {
	if fs == FeaturesV1 {
		return EncoderHash()
	}
	s := fmt.Sprintf("gorge-policynet-features\x1fbase=%#016x\x1fset=%s\x1fmz-format=1\x1fpins=%d,%d",
		EncoderHash(), fs, hashID("mz|bf-opp|canblock"), hashID("mz|me|avail"))
	if fs == FeaturesEntity {
		s += entityHashSuffix()
	}
	if fs == FeaturesMZOwnLib {
		s += ownLibHashSuffix()
	}
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

// FeaturesForHash maps a checkpoint's encoder hash back to the non-diagnostic
// feature set it was written under.
func FeaturesForHash(h uint64) (FeatureSet, bool) {
	for _, fs := range []FeatureSet{FeaturesV1, FeaturesMZ, FeaturesEntity, FeaturesMZOwnLib} {
		if EncoderHashFor(fs) == h {
			return fs, true
		}
	}
	return 0, false
}

// Features is the feature set the scorer's model was trained under; a seat
// encodes with EncodeStateWith/EncodeOptionWith of this set.
func (sc *Scorer) Features() FeatureSet { return sc.m.Features }

// EncodeStateWith is EncodeState under feature set fs. FeaturesV1 is exactly
// EncodeState. diag is read only by the diagnostic sets (nil = none known).
func EncodeStateWith(fs FeatureSet, v view.View, seat state.PlayerID, diag *Diag) State {
	st := EncodeState(v, seat)
	if fs == FeaturesV1 {
		return st
	}
	var extra []Feature
	push := func(s string, val float32) { extra = append(extra, Feature{Row: hashID(s), Value: val}) }
	mzState(v, seat, push)
	if fs == FeaturesMZOwnLib {
		ownLibState(v, seat, push)
	}
	if fs.Diagnostic() && diag != nil {
		mzDiag(v, seat, fs, diag, push)
	}
	st.Sparse = append(st.Sparse, extra...)
	sort.SliceStable(st.Sparse, func(i, j int) bool { return st.Sparse[i].Row < st.Sparse[j].Row })
	if fs == FeaturesEntity {
		st.Cards, _ = entityCards(v, seat)
	}
	return st
}

// EncodeOptionWith is EncodeOption under feature set fs. FeaturesV1 is
// exactly EncodeOption. Every other set (a) routes a decision kind outside
// decision.Kinds' named slots to a hashed "mz|deckind|other" id instead of
// the v1 "other" slot, which sits one past the 13-slot block ON the Land
// type bit (the pn03 finding: slot 24+len(decision.Kinds) == typeOffset),
// and (b) adds the option card's spell API as a hashed id (pn03's counter
// vs. other-instant signal).
func EncodeOptionWith(fs FeatureSet, v view.View, seat state.PlayerID, d decision.Kind, o decision.Option, idx, n int) Option {
	opt := EncodeOption(v, seat, d, o, idx, n)
	if fs == FeaturesV1 {
		return opt
	}
	if decKindIndex(d) < 0 {
		// Remove the colliding v1 "other" bit. EncodeOption emits the
		// decision-kind slot before the card-type block, so the FIRST slot
		// at that row is the decision kind's; a real Land's own type bit, set
		// later, survives.
		opt.Slots = dropSlot(opt.Slots, uint16(decKindOffset+len(decision.Kinds)))
		opt.Hashed = append(opt.Hashed, Feature{Row: hashID("mz|deckind|other"), Value: 1})
	}
	if o.Obj != 0 {
		if ref, ok := cardIndex(v, seat)[o.Obj]; ok && ref.cv.SpellAPI != "" {
			opt.Hashed = append(opt.Hashed, Feature{Row: hashID("mz|api|" + ref.cv.SpellAPI), Value: 1})
		}
	}
	if fs == FeaturesEntity {
		opt.EntA, opt.EntB = entityOptionRefs(v, seat, o)
	}
	return opt
}

// dropSlot removes the first slot at row r (a v1 "other" slot is set once).
func dropSlot(slots []Feature, r uint16) []Feature {
	for i, f := range slots {
		if f.Row == r {
			return append(slots[:i:i], slots[i+1:]...)
		}
	}
	return slots
}

// mzBucket clamps a small integer into 0..hi for a categorical token.
func mzBucket(v int32, hi int32) string {
	if v < 0 {
		v = 0
	}
	if v > hi {
		v = hi
	}
	return formatInt(int64(v))
}

func hasKeyword(cv view.CardView, kw string) bool {
	for _, k := range cv.Keywords {
		if strings.EqualFold(k, kw) {
			return true
		}
	}
	return false
}

// seatAvailable is a seat's floating pool plus its available (tappable)
// mana, in the fixed WUBRG C order — public information for every seat
// (view.PlayerView.Pool/Available).
func seatAvailable(pv *view.PlayerView) int32 {
	var t int32
	for _, k := range poolKeys {
		t += pv.Pool[k] + pv.Available[k]
	}
	return t
}

// mzState emits the MZ per-card and per-seat tokens in a fixed order: the
// viewer's battlefield, every other battlefield (seat order), the viewer's
// hand, the stack, then the seat aggregates.
func mzState(v view.View, seat state.PlayerID, push func(string, float32)) {
	var me *view.PlayerView
	var others []*view.PlayerView
	for i := range v.Players {
		if v.Players[i].ID == seat {
			me = &v.Players[i]
		} else {
			others = append(others, &v.Players[i])
		}
	}
	names := map[state.ObjID]string{}
	for i := range v.Players {
		for _, cv := range v.Players[i].Battlefield {
			names[cv.ID] = cv.Name
		}
	}
	type agg struct{ canAttack, canAttackPower, canBlock, canBlockTough, flyers int32 }
	perm := func(z string, cv view.CardView, a *agg) {
		for _, t := range strings.Fields(cv.Types) {
			push("mz|"+z+"|type|"+t, 1)
		}
		creature := hasTypeWord(cv.Types, "Creature")
		if cv.Tapped {
			push("mz|"+z+"|tapped", 1)
		} else {
			push("mz|"+z+"|untapped", 1)
		}
		if cv.SummonSick {
			push("mz|"+z+"|sick", 1)
		}
		if cv.Damage > 0 {
			push("mz|"+z+"|damaged", 1)
		}
		if cv.Attacking {
			push("mz|"+z+"|attacking", 1)
		}
		if len(cv.BlockedBy) > 0 {
			push("mz|"+z+"|blocked", 1)
		}
		for _, k := range cv.Keywords {
			push("mz|"+z+"|kw|"+strings.ToLower(k), 1)
		}
		keys := make([]string, 0, len(cv.Counters))
		for k := range cv.Counters {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			push("mz|"+z+"|ctr|"+k, float32(cv.Counters[k]))
		}
		if cv.AttachedTo != 0 {
			push("mz|"+z+"|attached", 1)
			if host, ok := names[cv.AttachedTo]; ok {
				push("mz|"+z+"|attached|"+host, 1)
				push("mz|"+cv.Name+"|on|"+host, 1)
			}
		}
		if !creature {
			return
		}
		p, t := mzBucket(cv.Power, 9), mzBucket(cv.Toughness-cv.Damage, 9)
		push("mz|"+z+"|cr|p"+p+"|t"+t, 1)
		push("mz|"+z+"|cr|p"+p, 1)
		push("mz|"+z+"|cr|t"+t, 1)
		push("mz|"+cv.Name+"|"+z+"|p"+p+"|t"+t, 1)
		for _, k := range cv.Keywords {
			push("mz|"+z+"|cr|kw|"+strings.ToLower(k), 1)
		}
		if hasKeyword(cv, "Flying") {
			a.flyers++
		}
		canAttack := !cv.Tapped && (!cv.SummonSick || hasKeyword(cv, "Haste")) && !hasKeyword(cv, "Defender")
		if canAttack {
			push("mz|"+z+"|canattack", 1)
			push("mz|"+z+"|canattack|p"+p, 1)
			a.canAttack++
			a.canAttackPower += cv.Power
		}
		if !cv.Tapped {
			push("mz|"+z+"|canblock", 1)
			push("mz|"+z+"|canblock|t"+t, 1)
			a.canBlock++
			a.canBlockTough += cv.Toughness - cv.Damage
		}
	}
	var mine, theirs agg
	if me != nil {
		for _, cv := range me.Battlefield {
			perm("bf-me", cv, &mine)
		}
	}
	for _, pv := range others {
		for _, cv := range pv.Battlefield {
			perm("bf-opp", cv, &theirs)
		}
	}
	var myAvail int32
	if me != nil {
		myAvail = seatAvailable(me)
		for _, cv := range me.Hand {
			handCard("hand", cv, myAvail, push)
		}
	}
	for _, sv := range v.Stack {
		if sv.Card == nil {
			continue
		}
		side := "opp"
		if sv.Card.Controller == seat {
			side = "me"
		}
		push("mz|stack|"+side, 1)
		if sv.Card.SpellAPI != "" {
			push("mz|stack|"+side+"|api|"+sv.Card.SpellAPI, 1)
		}
		for _, t := range strings.Fields(sv.Card.Types) {
			push("mz|stack|"+side+"|type|"+t, 1)
		}
	}
	// Seat aggregates: numeric-valued rows (the embedding row times the count)
	// plus a bucketed one-hot, so both a linear and a threshold read exist.
	num := func(key string, val int32, hi int32) {
		push("mz|"+key, float32(val))
		push("mz|"+key+"="+mzBucket(val, hi), 1)
	}
	num("me|avail", myAvail, 8)
	var oppAvail int32
	for _, pv := range others {
		oppAvail += seatAvailable(pv)
	}
	num("opp|avail", oppAvail, 8)
	num("me|canattack-n", mine.canAttack, 6)
	num("me|canattack-power", mine.canAttackPower, 12)
	num("opp|canblock-n", theirs.canBlock, 6)
	num("opp|canblock-tough", theirs.canBlockTough, 12)
	num("opp|canattack-n", theirs.canAttack, 6)
	num("opp|canattack-power", theirs.canAttackPower, 12)
	num("me|canblock-n", mine.canBlock, 6)
	num("me|flyers", mine.flyers, 4)
	num("opp|flyers", theirs.flyers, 4)
	if me != nil && len(others) > 0 {
		diff := me.Life - others[0].Life
		push("mz|lifediff="+mzBucket(diff/3+7, 14), 1)
		push("mz|opp|life="+mzBucket(others[0].Life/2, 12), 1)
		push("mz|me|life="+mzBucket(me.Life/2, 12), 1)
		// Lethal reads: can my untapped attackers' power alone kill through no
		// blocks; can theirs kill me.
		if mine.canAttackPower >= others[0].Life {
			push("mz|me|lethal-unblocked", 1)
		}
		if theirs.canAttackPower >= me.Life {
			push("mz|opp|lethal-unblocked", 1)
		}
	}
}

// handCard emits one hand card's tokens under zone tag z; avail is the
// holder's available mana (castability is mana-value-only, an approximation:
// colours are not checked).
func handCard(z string, cv view.CardView, avail int32, push func(string, float32)) {
	mv := int32(mvOf(cv.ManaCost))
	for _, t := range strings.Fields(cv.Types) {
		push("mz|"+z+"|type|"+t, 1)
	}
	push("mz|"+z+"|mv"+mzBucket(mv, 8), 1)
	instant := hasTypeWord(cv.Types, "Instant") || hasKeyword(cv, "Flash")
	if instant {
		push("mz|"+z+"|instant-speed", 1)
	}
	land := hasTypeWord(cv.Types, "Land")
	if !land && mv <= avail {
		push("mz|"+z+"|castable", 1)
		for _, t := range strings.Fields(cv.Types) {
			push("mz|"+z+"|castable|type|"+t, 1)
		}
		if instant {
			push("mz|"+z+"|castable|instant-speed", 1)
		}
	}
	if cv.SpellAPI != "" {
		push("mz|"+z+"|api|"+cv.SpellAPI, 1)
	}
	for _, k := range cv.Keywords {
		push("mz|"+z+"|kw|"+strings.ToLower(k), 1)
	}
	if hasTypeWord(cv.Types, "Creature") {
		push("mz|"+z+"|cr|p"+mzBucket(cv.Power, 9)+"|t"+mzBucket(cv.Toughness, 9), 1)
	}
}

// mzDiag emits the diagnostic hidden-information tokens.
func mzDiag(v view.View, seat state.PlayerID, fs FeatureSet, diag *Diag, push func(string, float32)) {
	var oppAvail int32
	for i := range v.Players {
		if v.Players[i].ID != seat {
			oppAvail += seatAvailable(&v.Players[i])
		}
	}
	for _, cv := range diag.OppHand {
		push("mz|opphand|"+cv.Name, 1)
		handCard("opphand", cv, oppAvail, push)
	}
	if fs < FeaturesMZOracle {
		return
	}
	for i, n := range diag.OppLibraryTop {
		push("mz|opplib"+formatInt(int64(i+1))+"|"+n, 1)
		push("mz|opplib|"+n, 1)
	}
	for i, n := range diag.OwnLibraryTop {
		push("mz|ownlib"+formatInt(int64(i+1))+"|"+n, 1)
		push("mz|ownlib|"+n, 1)
	}
}
