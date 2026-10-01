package searchprobe

import (
	"fmt"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// Skipping the per-deal observation probe on a hidden-blind board.
//
// Redealer.Deal checks every world it deals by projecting the seat's
// observation of it (Collector.probeBoundary) and comparing it with the base
// boundary's: board bytes and decision. On the search bench that probe was
// ~5% of all az CPU -- a whole view projection, the seat's potential-action
// walk and every seat's available mana, per simulation -- and it never
// refused a world.
//
// What a deal changes. A world is a CloneHypothetical of the base engine
// plus redealPlayer's Secret events: MoveZone between a player's hand and
// library and one LibraryOrder per player. So the world differs from the
// base only in (a) the hand and library lists' contents and order (their
// sizes are checked to land), (b) the moved objects' own move-reset fields
// and the per-turn zone-entry ledger and game clock that events.Move folds,
// (c) the log's appended Secret events and (d) the future chance stream. No
// public object, no stack object, no player field, no pending decision and
// no registered continuous effect is touched; the clone carries the rest.
//
// What the seat observes (Collector.observe, view.ProjectInto for the
// actor). Every field of the observation is public state, a zone SIZE, the
// actor's own hand, or one of the derived reads below:
//
//   - each visible object's derived characteristics (and, on the
//     battlefield, its ability costs): the layer system over the statics of
//     every object in a zone where they function, plus the registered
//     continuous effects;
//   - each seat's available mana: its battlefield mana abilities and whether
//     their costs can be paid;
//   - the actor's potential actions: the legal-offer walk over the actor's
//     own hand and public zones, plus any MayPlay grant's zones (the walk
//     reads the top of the actor's library only to test such a grant);
//   - the actor's library top, only under a MayLookAt grant;
//   - the round, folded from TurnChange/PlayerLost events only.
//
// A card's text is the only way any of those reads reaches into a hidden zone
// or the move-reset state: a static that functions from a hand or library,
// that affects cards there, that grants MayPlay/MayLookAt, or whose amount
// counts a hand, a library, this turn's zone entries or drawn cards; a mana
// ability whose cost discards, reveals or exiles from a hand; an offer-time
// ability parameter (a target zone, a condition, an X amount) that names a
// hidden zone. observationBlind scans every face any object of the game
// carries -- the redeal permutes the base's own objects, so the hidden cards
// a world can deal are among them -- and every registered continuous effect
// for any such text, conservatively (a case-insensitive substring match on
// every offer-time parameter and every SVar those parameters name). When
// none is found, and the actor's own hand is not re-dealt and no seat's
// hidden information is shared with the actor (CR 720.4 control), the
// observation of every world equals the base's, so Deal skips the probe.
//
// redealProbeVerify (on in this package's test binary, settable through
// SetRedealProbeVerify by other test binaries, or at link time) runs the
// probe anyway on every skip and panics if it would have refused the world.
// go build -ldflags "-X github.com/adams-shaun/gorge/internal/searchprobe.redealProbeVerifyFlag=1".
var redealProbeVerifyFlag string

var redealProbeVerify = redealProbeVerifyFlag != ""

// SetRedealProbeVerify turns the redeal probe-skip verify mode on or off and
// returns the previous setting. It is a test hook: a test binary outside this
// package that deals worlds calls it so every skipped probe is still run and
// checked.
func SetRedealProbeVerify(on bool) bool {
	prev := redealProbeVerify
	redealProbeVerify = on
	return prev
}

// observationBlind reports whether the seat's observation provably cannot
// tell any world r deals from the base (see above).
func observationBlind(e *rules.Engine, actor state.PlayerID, plans []redealPlan) bool {
	for i := range plans {
		if plans[i].player == actor && plans[i].handFree != 0 {
			return false
		}
	}
	if len(e.G.ControlledBy) > 0 {
		return false
	}
	for i, ces := 0, e.RegisteredContinuous(); i < len(ces); i++ {
		if !continuousBlind(&ces[i]) {
			return false
		}
	}
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.CopyFace != nil && !faceBlind(o.CopyFace) {
			return false
		}
		if o.Card == nil {
			continue
		}
		for _, f := range o.Card.Faces {
			if f != nil && !faceBlind(f) {
				return false
			}
		}
	}
	return true
}

// hiddenWord reports whether s names a hidden zone or a hidden-zone
// readable fact: a hand, a library, a drawn card, a look/play permission.
// Case-insensitive and substring-based, so it errs toward "names one" (a
// false hit costs a probe, never a wrong world). This turn's zone entries
// need no word of their own: the redeal's entries are into a hand or a
// library, which only a head naming that zone counts.
func hiddenWord(s string) bool {
	for _, w := range [...]string{"hand", "library", "drawn", "maylook", "mayplay"} {
		if containsFold(s, w) {
			return true
		}
	}
	return false
}

// containsFold is a case-insensitive strings.Contains for an ASCII
// lower-case needle, allocation-free.
func containsFold(s, lower string) bool {
	n := len(lower)
	for i := 0; i+n <= len(s); i++ {
		j := 0
		for ; j < n; j++ {
			c := s[i+j]
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != lower[j] {
				break
			}
		}
		if j == n {
			return true
		}
	}
	return false
}

// manaCostHiddenWord reports whether a mana ability's cost reads a hidden
// zone's contents: a discard, a reveal, an exile or behold from a hand, a
// mill. Every seat's available mana is projected, so an opponent's mana
// ability whose payability depends on which cards its hand holds would make
// the observation depend on the deal.
func manaCostHiddenWord(s string) bool {
	for _, w := range [...]string{"discard", "reveal", "behold", "mill", "draw"} {
		if containsFold(s, w) {
			return true
		}
	}
	return hiddenWord(s)
}

// faceBlindMemo caches faceBlind per face: a pure function of the immutable
// compiled face, shared by every engine and worker.
var faceBlindMemo sync.Map // *cards.Face -> bool

// faceBlind reports whether nothing on f can make the observation read a
// hidden zone (see observationBlind).
func faceBlind(f *cards.Face) bool {
	if v, ok := faceBlindMemo.Load(f); ok {
		return v.(bool)
	}
	ok := faceBlindScan(f)
	faceBlindMemo.Store(f, ok)
	return ok
}

func faceBlindScan(f *cards.Face) bool {
	sv := svarScan{f: f}
	for _, kw := range f.Keywords {
		if hiddenWord(kw) && !strings.HasPrefix(kw, "MayEffectFromOpeningHand") {
			// MayEffectFromOpeningHand (Leyline Axe) acts in the pregame
			// only, before any boundary a search is rooted at.
			return false
		}
	}
	for i := range f.Statics {
		st := &f.Statics[i]
		if hiddenWord(st.Mode) {
			return false
		}
		if _, ok := st.Params["MayPlay"]; ok {
			// A MayPlay grant is blind only with an explicit, public
			// AffectedZone$ (the hidden-zone names fail the scan below).
			if z, ok := st.Params["AffectedZone"]; !ok || strings.TrimSpace(z) == "" {
				return false
			}
		}
		for k, v := range st.Params {
			if k == "MayPlay" || k == "Description" {
				// Description$ is display text: no rule reads it.
				continue
			}
			if hiddenWord(k) || hiddenWord(v) || !sv.refs(v) {
				return false
			}
		}
	}
	for _, sa := range f.Abilities {
		if sa != nil && !saBlind(&sv, sa.API, sa.Params) {
			return false
		}
	}
	return true
}

// saPayloadKey reports whether an ability parameter is read only when the
// ability resolves (or is display text), never by the offer walk, the
// available-mana read or the cost projection. Origin$ is payload only for an
// untargeted ability: a targeted one draws its targets from it. (A
// parameter's KEY is never scanned: every corpus key naming a hidden zone or
// a drawn card -- the LibraryPosition family, WithMayLook, RememberDrawn,
// QuasiLibrarySearch -- is resolution payload; the zone an offer reads is
// always a value.)
func saPayloadKey(k string, targeted bool) bool {
	switch k {
	case "Destination", "DestinationZone", "DestinationZone2", "DestinationAlternative",
		"RevealedDestination", "FoundDestination", "ChangeType", "ChangeNum", "ChangeTypeDesc",
		"ChangeValid", "DigNum", "SubAbility",
		// The statics, triggers and replacements an Effect or Animate
		// ability installs exist only once it resolves (and are then
		// registered continuous effects, which continuousBlind scans).
		"StaticAbilities", "Triggers", "ReplacementEffects",
		"SpellDescription", "StackDescription", "PrecostDesc", "CostDesc", "AILogic":
		return true
	case "Origin":
		return !targeted
	}
	return false
}

// saBlind reports whether an ability's offer-time parameters (and the SVars
// they name) read no hidden zone. A mana ability's cost is held to the
// stricter manaCostHiddenWord; any other ability's cost reads only its own
// controller's hand (the actor's, never re-dealt) and public zones. A
// hand-activated ability (ActivationZone$ Hand) is the actor's own hand card
// or an opponent's card the actor's walk never offers -- unless it is a mana
// ability, which every seat's available mana could count.
func saBlind(sv *svarScan, api string, params map[string]string) bool {
	mana := strings.Contains(api, "Mana")
	_, tgts := params["ValidTgts"]
	_, tz := params["TgtZone"]
	targeted := tgts || tz
	for k, v := range params {
		switch {
		case saPayloadKey(k, targeted):
			continue
		case k == "Cost":
			if (mana && manaCostHiddenWord(v)) || (!mana && containsFold(v, "library")) || !sv.refs(v) {
				return false
			}
			continue
		case k == "ActivationZone" && !mana && strings.TrimSpace(v) == "Hand":
			continue
		}
		if hiddenWord(v) || !sv.refs(v) {
			return false
		}
	}
	return true
}

// svarScan follows the SVars an offer-time parameter names, once each.
type svarScan struct {
	f    *cards.Face
	seen []string
}

// refs reports whether every SVar named in v (any identifier token that is
// one of the face's SVar names) is blind: an ability body (DB$/AB$/SP$) by
// saBlind's rules, anything else (a Count$ amount, a static body) by a raw
// hiddenWord scan; the SVars it names are followed in turn.
func (sv *svarScan) refs(v string) bool {
	if len(sv.f.SVars) == 0 {
		return true
	}
	for start := 0; start < len(v); {
		for start < len(v) && !identByte(v[start]) {
			start++
		}
		end := start
		for end < len(v) && identByte(v[end]) {
			end++
		}
		if end > start && !sv.ref(v[start:end]) {
			return false
		}
		start = end
	}
	return true
}

func identByte(c byte) bool {
	return c == '_' || ('0' <= c && c <= '9') || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func (sv *svarScan) ref(name string) bool {
	body, ok := sv.f.SVars[name]
	if !ok {
		return true
	}
	for _, s := range sv.seen {
		if s == name {
			return true
		}
	}
	sv.seen = append(sv.seen, name)
	if head, _, _ := strings.Cut(body, "|"); strings.Contains(head, "$") {
		k, api, _ := strings.Cut(head, "$")
		switch strings.TrimSpace(k) {
		case "DB", "AB", "SP":
			return saBlind(sv, strings.TrimSpace(api), parseSVarParams(body))
		}
	}
	return !hiddenWord(body) && sv.refs(body)
}

// parseSVarParams splits an ability-body SVar ("DB$ Api | Key$ Value | ...")
// into its parameters (the head is the API, not a parameter).
func parseSVarParams(body string) map[string]string {
	out := make(map[string]string)
	for i, part := range strings.Split(body, "|") {
		if i == 0 {
			continue
		}
		k, v, ok := strings.Cut(part, "$")
		if !ok {
			out[strings.TrimSpace(part)] = ""
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

// continuousBlind reports whether a registered continuous effect reads no
// hidden zone: no MayPlay or MayLookAt grant, and no hidden word anywhere in
// its values (its affected spec, amounts, zone, rider parameters and SVars)
// or on a face it grants.
func continuousBlind(ce *rules.ContinuousEffect) bool {
	if ce.MayPlay || ce.MayLookAt {
		return false
	}
	for _, g := range ce.GainedFaces {
		if g.Face != nil && !faceBlind(g.Face) {
			return false
		}
	}
	for _, g := range ce.GainedTriggerFaces {
		if g.Face != nil && !faceBlind(g.Face) {
			return false
		}
	}
	// Every value at once (fmt prints maps in key order; field names are not
	// printed, so a name like SetMaxHandSize cannot match).
	return !hiddenWord(fmt.Sprint(*ce))
}
