//go:build manabrew

package mbtest

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/bench"
	manabrew "github.com/adams-shaun/gorge/internal/manabrew"
	"github.com/adams-shaun/gorge/internal/testutil"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// MBX-5: state parity and seat privacy for the ManaBrew GameView.
//
// TranslatingSeat drives a whole game through the translator, but only ever
// builds the DECIDING seat's prompt, so it never exercises Translator.State
// for the other seats. These tests project a native view for EVERY seat at
// every posed decision (the same view the native wire would hand that seat),
// run it through Translator.State, and compare the GameView field by field
// against the native view it came from. A second test asserts the GameView
// never reveals a card identity the seat's native view redacted.
//
// Intentional differences (each named at its check):
//
//   - Turn only: GameViewDto has no `phase` member, so view.View.Phase (the
//     coarse PhaseOf(step) bucket) is not projected; `step` is.
//   - The x_gorge_own_deck_v1 extension is native `OwnDeck`, not a zone, and
//     is checked by state_test.go; here it is ignored.
//   - ManaBrew omits native-only zones: the planar deck, the commander roster
//     and the viewer's own unordered `Library` contents are not emitted as
//     zone card lists (the library zone carries its count plus the visible top
//     card only), so a native card absent from the GameView is never a leak.
//   - A face-down permanent is redacted to an empty identity by the
//     translator even for the seat that may look at it; native reveals the
//     printed face to its controller. The translator is therefore STRICTER
//     than native (privacy-safe), and the privacy test pins that direction.
//   - CardDto.Power/Toughness are strings and keep the native int (a
//     non-creature's 0 becomes "0"); native cannot express null here.
//   - CardDto.AttachmentIDs is always []; native has no inverse attachment
//     list, so there is nothing to compare it against.
//   - ManaBrew CardDto.Identity.Name is the printed Printing.Name; a
//     layer-renamed card's live `CardView.Name` (a different field) is not
//     projected. The parity check compares Printing.Name.
//
// The ownerId of a stack object is the controller (the native StackView
// carries no owner); see stackObject's own comment.

// parityGamesPerSeatCount is the MB-8 census game count (censusGamesPerSeatCount),
// and the seeds below are the census's own (seats*1000+g for the parity run,
// seats*2000+g for the leak run), so these tests cover exactly the census games
// the brief names, not a different set.
const parityGamesPerSeatCount = censusGamesPerSeatCount

// gameViewOf extracts the GameViewDto from the translator's state message.
func gameViewOf(t *testing.T, msg mb.EngineMessage) mb.GameViewDto {
	t.Helper()
	su, ok := msg.Value.(mb.StateUpdate)
	if !ok {
		t.Fatalf("Translator.State returned %T, want mb.StateUpdate", msg.Value)
	}
	if su.Kind != "state" {
		t.Fatalf("StateUpdate.Kind = %q, want state", su.Kind)
	}
	return su.GameView
}

// zoneKey indexes one (zone, owner) bucket.
type zoneKey struct {
	zone  mb.ZoneKind
	owner string
}

func indexZones(t *testing.T, gv mb.GameViewDto) map[zoneKey]mb.ZoneDto {
	t.Helper()
	out := make(map[zoneKey]mb.ZoneDto, len(gv.Zones))
	for _, z := range gv.Zones {
		k := zoneKey{z.Zone, z.OwnerID}
		if _, dup := out[k]; dup {
			t.Errorf("duplicate zone bucket %v (zone %s owner %s)", k, z.Zone, z.OwnerID)
		}
		out[k] = z
	}
	return out
}

func visibleCards(t *testing.T, z mb.ZoneDto) []mb.CardDto {
	t.Helper()
	out := make([]mb.CardDto, 0, len(z.Cards))
	for _, cv := range z.Cards {
		vc, ok := cv.Value.(mb.VisibleCard)
		if !ok {
			t.Errorf("zone %s owner %s: card %#v is not the expected visible card", z.Zone, z.OwnerID, cv.Value)
			continue
		}
		out = append(out, vc.CardDto)
	}
	return out
}

// compareStateParity is the field-by-field comparison the brief names: life,
// zones and counts, battlefield cards (id, tapped, counters, controller, P/T,
// attachments), stack, phase/step and the active/priority seats.
func compareStateParity(t *testing.T, where string, v view.View, gv mb.GameViewDto) {
	t.Helper()
	if gv.GameID == "" {
		t.Errorf("%s: GameID empty", where)
	}
	if gv.Turn != int(v.Turn) {
		t.Errorf("%s: turn=%d native=%d", where, gv.Turn, v.Turn)
	}
	if got, want := string(gv.Step), string(stepKindOf(v.Step)); got != want {
		t.Errorf("%s: step=%q native step %q maps to %q", where, got, v.Step, want)
	}
	// Phase is an intentional difference: GameViewDto has no phase member.
	if got, want := gv.ActivePlayerID, manabrew.PlayerID(v.Active); got != want {
		t.Errorf("%s: activePlayerId=%q native=%q", where, got, want)
	}
	if got, want := gv.PriorityPlayerID, manabrew.PlayerID(v.Priority); got != want {
		t.Errorf("%s: priorityPlayerId=%q native=%q", where, got, want)
	}
	if gv.GameOver != v.Over {
		t.Errorf("%s: gameOver=%v native=%v", where, gv.GameOver, v.Over)
	}
	switch {
	case v.Winner == nil && gv.WinnerID != nil:
		t.Errorf("%s: winnerId=%q native nil", where, *gv.WinnerID)
	case v.Winner != nil && gv.WinnerID == nil:
		t.Errorf("%s: winnerId nil native=%q", where, manabrew.PlayerID(*v.Winner))
	case v.Winner != nil && gv.WinnerID != nil && *gv.WinnerID != manabrew.PlayerID(*v.Winner):
		t.Errorf("%s: winnerId=%q native=%q", where, *gv.WinnerID, manabrew.PlayerID(*v.Winner))
	}

	if len(gv.Players) != len(v.Players) {
		t.Fatalf("%s: players=%d native=%d", where, len(gv.Players), len(v.Players))
	}
	for i, p := range v.Players {
		comparePlayer(t, where, p, gv.Players[i])
	}

	zones := indexZones(t, gv)
	// Every zone the translator emits must be one the native shape knows
	// about; the zone buckets are keyed by owner (battlefield by controller).
	for i := range v.Players {
		p := v.Players[i]
		compareZone(t, where, zones, mb.ZoneHand, p, nativeHandCount(p), len(p.Hand))
		compareZone(t, where, zones, mb.ZoneLibrary, p, p.LibrarySize, nativeLibraryVisible(p, v.Viewer))
		compareZone(t, where, zones, mb.ZoneGraveyard, p, len(p.Graveyard), len(p.Graveyard))
		compareZone(t, where, zones, mb.ZoneExile, p, len(p.Exile), len(p.Exile))
		compareZone(t, where, zones, mb.ZoneCommand, p, len(p.Command), len(p.Command))
		compareBattlefield(t, where, zones, p)
	}

	compareStack(t, where, v.Stack, gv.Stack)
	compareCombatAssignments(t, where, v, gv.CombatAssignments)
}

// nativeHandCount is the number of cards the hand zone's count reports: the
// viewer's own hand size is the projected slice, every other seat's is the
// HandSize scalar (the slice is nil).
func nativeHandCount(p view.PlayerView) int {
	if p.Hand != nil {
		return len(p.Hand)
	}
	return p.HandSize
}

// nativeLibraryVisible is how many library cards the native view exposes: the
// visible top card for the viewer's own seat only (ManaBrew does not project
// the own-library contents list).
func nativeLibraryVisible(p view.PlayerView, viewer state.PlayerID) int {
	if p.ID == viewer && p.LibraryTop != nil {
		return 1
	}
	return 0
}

func comparePlayer(t *testing.T, where string, p view.PlayerView, dto mb.PlayerDto) {
	t.Helper()
	if got, want := dto.ID, manabrew.PlayerID(p.ID); got != want {
		t.Errorf("%s: player id=%q native=%q", where, got, want)
	}
	if dto.Life != int(p.Life) {
		t.Errorf("%s: player %s life=%d native=%d", where, dto.ID, dto.Life, p.Life)
	}
	if dto.Name != p.Name {
		t.Errorf("%s: player %s name=%q native=%q", where, dto.ID, dto.Name, p.Name)
	}
	wantStatus := mb.PlayerStatus("active")
	if p.Lost {
		wantStatus = "lost"
	}
	if dto.Status != wantStatus {
		t.Errorf("%s: player %s status=%q native=%q", where, dto.ID, dto.Status, wantStatus)
	}
	// Commander casts (spec §6.2 re-keys the roster to object ids).
	wantCasts := map[string]int{}
	for i, c := range p.Commanders {
		n := 0
		if i < len(p.CommanderCasts) {
			n = int(p.CommanderCasts[i])
		}
		wantCasts[manabrew.CardID(c.ID)] = n
	}
	if !equalIntMap(dto.CommanderCasts, wantCasts) {
		t.Errorf("%s: player %s commanderCasts=%v native=%v", where, dto.ID, dto.CommanderCasts, wantCasts)
	}
	// Mana pool (public).
	wantPool := map[mb.ManaColor]int{}
	for color, n := range p.Pool {
		wantPool[mb.ManaColor(color)] = int(n)
	}
	if !equalIntMap(dto.ManaPool, wantPool) {
		t.Errorf("%s: player %s manaPool=%v native=%v", where, dto.ID, dto.ManaPool, wantPool)
	}
}

func compareZone(t *testing.T, where string, zones map[zoneKey]mb.ZoneDto, kind mb.ZoneKind, p view.PlayerView, wantCount, wantCards int) {
	t.Helper()
	k := zoneKey{kind, manabrew.PlayerID(p.ID)}
	z, ok := zones[k]
	if !ok {
		t.Errorf("%s: player %s missing zone %s", where, manabrew.PlayerID(p.ID), kind)
		return
	}
	if z.Count != wantCount {
		t.Errorf("%s: zone %s owner %s count=%d native=%d", where, kind, z.OwnerID, z.Count, wantCount)
	}
	if len(z.Cards) != wantCards {
		t.Errorf("%s: zone %s owner %s cards=%d native=%d", where, kind, z.OwnerID, len(z.Cards), wantCards)
	}
}

// compareBattlefield checks the native battlefield bucket keyed by controller
// (all of a player's Battlefield entries are controlled by that player) field
// by field.
func compareBattlefield(t *testing.T, where string, zones map[zoneKey]mb.ZoneDto, p view.PlayerView) {
	t.Helper()
	k := zoneKey{mb.ZoneBattlefield, manabrew.PlayerID(p.ID)}
	z, ok := zones[k]
	if !ok {
		t.Errorf("%s: player %s missing battlefield zone", where, manabrew.PlayerID(p.ID))
		return
	}
	byID := map[string]mb.CardDto{}
	for _, c := range visibleCards(t, z) {
		byID[c.ID] = c
	}
	controlled := 0
	for _, c := range p.Battlefield {
		if c.Controller != p.ID {
			t.Errorf("%s: native battlefield of player %d carries a card controlled by %d", where, p.ID, c.Controller)
			continue
		}
		controlled++
		dto, ok := byID[manabrew.CardID(c.ID)]
		if !ok {
			t.Errorf("%s: battlefield card %s (native %q) missing from the GameView", where, manabrew.CardID(c.ID), c.Printing.Name)
			continue
		}
		compareBattlefieldCard(t, where, c, dto)
		delete(byID, manabrew.CardID(c.ID))
	}
	for id := range byID {
		t.Errorf("%s: GameView battlefield bucket %s carries %s, which native does not show", where, z.OwnerID, id)
	}
	if z.Count != controlled {
		t.Errorf("%s: battlefield owner %s count=%d native=%d", where, z.OwnerID, z.Count, controlled)
	}
}

func compareBattlefieldCard(t *testing.T, where string, c view.CardView, dto mb.CardDto) {
	t.Helper()
	if got, want := dto.ID, manabrew.CardID(c.ID); got != want {
		t.Errorf("%s: card id=%q native=%q", where, got, want)
	}
	if dto.Tapped != c.Tapped {
		t.Errorf("%s: card %s tapped=%v native=%v", where, dto.ID, dto.Tapped, c.Tapped)
	}
	if got, want := dto.ControllerID, manabrew.PlayerID(c.Controller); got != want {
		t.Errorf("%s: card %s controller=%q native=%q", where, dto.ID, got, want)
	}
	if got, want := dto.OwnerID, manabrew.PlayerID(c.Owner); got != want {
		t.Errorf("%s: card %s owner=%q native=%q", where, dto.ID, got, want)
	}
	// P/T: native ints, projected as strings (a non-creature's 0 becomes "0").
	if dto.Power == nil || *dto.Power != strconv.Itoa(int(c.Power)) {
		t.Errorf("%s: card %s power=%v native=%d", where, dto.ID, strPtr(dto.Power), c.Power)
	}
	if dto.Toughness == nil || *dto.Toughness != strconv.Itoa(int(c.Toughness)) {
		t.Errorf("%s: card %s toughness=%v native=%d", where, dto.ID, strPtr(dto.Toughness), c.Toughness)
	}
	// Counters.
	wantCounters := map[string]int{}
	for kind, n := range c.Counters {
		wantCounters[kind] = int(n)
	}
	if !equalIntMap(dto.Counters, wantCounters) {
		t.Errorf("%s: card %s counters=%v native=%v", where, dto.ID, dto.Counters, wantCounters)
	}
	// Attachments: AttachedTo is the permanent this card is attached to.
	wantAttached := ""
	if c.AttachedTo != 0 {
		wantAttached = manabrew.CardID(c.AttachedTo)
	}
	if dto.AttachedTo != wantAttached {
		t.Errorf("%s: card %s attachedTo=%q native=%q", where, dto.ID, dto.AttachedTo, wantAttached)
	}
	if dto.Damage != int(c.Damage) {
		t.Errorf("%s: card %s damage=%d native=%d", where, dto.ID, dto.Damage, c.Damage)
	}
	if dto.IsAttacking != c.Attacking {
		t.Errorf("%s: card %s isAttacking=%v native=%v", where, dto.ID, dto.IsAttacking, c.Attacking)
	}
	wantAttackingPlayer := ""
	if c.AttackingPlayer != nil {
		wantAttackingPlayer = manabrew.PlayerID(*c.AttackingPlayer)
	}
	if dto.AttackingPlayerID != wantAttackingPlayer {
		t.Errorf("%s: card %s attackingPlayerId=%q native=%q", where, dto.ID, dto.AttackingPlayerID, wantAttackingPlayer)
	}
	if dto.IsFaceDown != c.FaceDown {
		t.Errorf("%s: card %s isFaceDown=%v native=%v", where, dto.ID, dto.IsFaceDown, c.FaceDown)
	}
	if dto.IsCopy != (c.IsCopy && !c.FaceDown) {
		t.Errorf("%s: card %s isCopy=%v native=%v(faceDown=%v)", where, dto.ID, dto.IsCopy, c.IsCopy, c.FaceDown)
	}
	if dto.SummoningSick != c.SummonSick {
		t.Errorf("%s: card %s summoningSick=%v native=%v", where, dto.ID, dto.SummoningSick, c.SummonSick)
	}
	if dto.Identity.IsToken != c.IsToken && !c.FaceDown {
		t.Errorf("%s: card %s isToken=%v native=%v", where, dto.ID, dto.Identity.IsToken, c.IsToken)
	}
	// Identity name: Printing.Name (a layer-renamed live Name is not projected).
	wantName := c.Printing.Name
	if c.FaceDown {
		wantName = ""
	}
	if dto.Identity.Name != wantName {
		t.Errorf("%s: card %s identity.name=%q native printing=%q", where, dto.ID, dto.Identity.Name, wantName)
	}
	// Keywords are a defensive copy of the same set.
	if !equalStringsUnordered(dto.Keywords, c.Keywords) {
		t.Errorf("%s: card %s keywords=%v native=%v", where, dto.ID, dto.Keywords, c.Keywords)
	}
}

func compareStack(t *testing.T, where string, native []view.StackView, got []mb.StackObjectDto) {
	t.Helper()
	stackIDs := map[state.ObjID]bool{}
	for _, s := range native {
		stackIDs[s.ID] = true
	}
	if len(got) != len(native) {
		t.Errorf("%s: stack=%d native=%d", where, len(got), len(native))
		return
	}
	for i := range native {
		s, dto := native[i], got[i]
		if want := manabrew.StackID(s.ID); dto.ID != want {
			t.Errorf("%s: stack[%d] id=%q native=%q", where, i, dto.ID, want)
		}
		if want := manabrew.PlayerID(s.Controller); dto.ControllerID != want {
			t.Errorf("%s: stack[%d] controller=%q native=%q", where, i, dto.ControllerID, want)
		}
		if want := manabrew.CardID(s.Source); dto.SourceID != want {
			t.Errorf("%s: stack[%d] source=%q native=%q", where, i, dto.SourceID, want)
		}
		wantName := s.Name
		if s.Card != nil {
			wantName = s.Card.Printing.Name
		}
		if dto.Identity.Name != wantName {
			t.Errorf("%s: stack[%d] name=%q native=%q", where, i, dto.Identity.Name, wantName)
		}
		if dto.Text != s.Text {
			t.Errorf("%s: stack[%d] text=%q native=%q", where, i, dto.Text, s.Text)
		}
		if dto.IsPermanentSpell != (s.Kind == "spell") {
			t.Errorf("%s: stack[%d] isPermanentSpell=%v native kind=%q", where, i, dto.IsPermanentSpell, s.Kind)
		}
		if dto.IsCasting != (s.Kind == "spell") {
			t.Errorf("%s: stack[%d] isCasting=%v native kind=%q", where, i, dto.IsCasting, s.Kind)
		}
		if want := nativeStackTargets(s.Targets, stackIDs); !reflect.DeepEqual(dto.Targets, want) {
			t.Errorf("%s: stack[%d] targets=%v native=%v", where, i, dto.Targets, want)
		}
	}
}

// nativeStackTargets maps a native stack object's chosen targets into the
// ManaBrew TargetRef shape: a player target is a RefPlayer named by the player
// id, an object target is a RefCard named by the object id (the native
// TargetView carries no distinction between a permanent and a stack object, so
// RefSpell is never minted here).
func nativeStackTargets(ts []view.TargetView, stackIDs map[state.ObjID]bool) []mb.TargetRef {
	out := make([]mb.TargetRef, 0, len(ts))
	for _, t := range ts {
		if t.IsPlayer {
			out = append(out, mb.TargetRef{Kind: mb.RefPlayer, ID: manabrew.PlayerID(t.Player)})
			continue
		}
		if stackIDs[t.Obj] {
			out = append(out, mb.TargetRef{Kind: mb.RefSpell, ID: manabrew.StackID(t.Obj)})
			continue
		}
		out = append(out, mb.TargetRef{Kind: mb.RefCard, ID: manabrew.CardID(t.Obj)})
	}
	return out
}

// compareCombatAssignments checks the GameView's blocker→attacker pairing list
// against the native attackers' BlockedBy lists, as a set (the wire order is
// not specified).
func compareCombatAssignments(t *testing.T, where string, v view.View, got []mb.CombatAssignmentDto) {
	t.Helper()
	want := map[string]bool{}
	for _, p := range v.Players {
		for _, attacker := range p.Battlefield {
			for _, blocker := range attacker.BlockedBy {
				want[manabrew.CardID(blocker)+"->"+manabrew.CardID(attacker.ID)] = true
			}
		}
	}
	if len(got) != len(want) {
		t.Errorf("%s: combatAssignments=%d native=%d", where, len(got), len(want))
	}
	for _, a := range got {
		if !want[a.BlockerID+"->"+a.AttackerID] {
			t.Errorf("%s: combatAssignment %s->%s not in native BlockedBy", where, a.BlockerID, a.AttackerID)
		}
	}
}

// --- privacy -----------------------------------------------------------------

// compareSeatPrivacy asserts the GameView reveals no card identity the seat's
// native view redacted: every visible card in the GameView must correspond to a
// non-hidden card the native view exposes, with the same identity; a native
// face-down card must appear only as a blank-identity, isFaceDown entry; and no
// card from another seat's hand or any library may surface as a visible
// identity.
func compareSeatPrivacy(t *testing.T, where string, v view.View, gv mb.GameViewDto) {
	t.Helper()
	native := map[state.ObjID]view.CardView{}
	addNative := func(cs []view.CardView) {
		for _, c := range cs {
			native[c.ID] = c
		}
	}
	for _, p := range v.Players {
		addNative(p.Hand)
		addNative(p.Library)
		addNative(p.Battlefield)
		addNative(p.Graveyard)
		addNative(p.Exile)
		addNative(p.Command)
		addNative(p.PlanarDeck)
		addNative(p.Commanders)
		if p.LibraryTop != nil {
			native[p.LibraryTop.ID] = *p.LibraryTop
		}
	}
	for _, z := range gv.Zones {
		for _, cv := range z.Cards {
			vc, ok := cv.Value.(mb.VisibleCard)
			if !ok {
				continue
			}
			id, ok := parseCardID(vc.ID)
			if !ok {
				t.Errorf("%s: visible card has non-obj id %q", where, vc.ID)
				continue
			}
			n, visible := native[id]
			if !visible {
				t.Errorf("%s: GameView reveals visible card %s (%q) that the native view does not show",
					where, vc.ID, vc.Identity.Name)
				continue
			}
			if n.FaceDown {
				if vc.Identity.Name != "" || vc.Identity.SetCode != "" || vc.Identity.CardNumber != "" {
					t.Errorf("%s: face-down card %s leaked identity %+v", where, vc.ID, vc.Identity)
				}
				if !vc.IsFaceDown {
					t.Errorf("%s: face-down card %s not marked isFaceDown", where, vc.ID)
				}
				continue
			}
			if vc.Identity.Name != n.Printing.Name {
				t.Errorf("%s: card %s identity.name=%q native printing=%q", where, vc.ID, vc.Identity.Name, n.Printing.Name)
			}
		}
	}
}

// hiddenIDsFor is the true, engine-level object ids in a player's hidden
// zones: every seat's library (CR 400.2 hides library order from every seat)
// and every OTHER seat's hand. These are exactly the objects the native view
// of `viewer` must have redacted, so none may appear as a visible GameView
// card id. Matching is by OBJECT ID, never by name: two cards in different
// zones can share a name, so a name check would report a public graveyard card
// as a leak of a same-named hidden library card.
func hiddenIDsFor(e *rules.Engine, viewer state.PlayerID, playerCount int) map[state.ObjID]bool {
	out := map[state.ObjID]bool{}
	add := func(ids []state.ObjID) {
		for _, id := range ids {
			out[id] = true
		}
	}
	for i := 0; i < playerCount; i++ {
		add(e.G.Zone(state.ZLibrary, state.PlayerID(i)))
	}
	for i := 0; i < playerCount; i++ {
		if state.PlayerID(i) == viewer {
			continue
		}
		add(e.G.Zone(state.ZHand, state.PlayerID(i)))
	}
	return out
}

func parseCardID(id string) (state.ObjID, bool) {
	rest, ok := strings.CutPrefix(id, "o")
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseUint(rest, 10, 64)
	if err != nil {
		return 0, false
	}
	return state.ObjID(n), true
}

// --- harness -----------------------------------------------------------------

// parityObserver drives both tests: at every posed decision it runs the parity
// and privacy comparisons for every seat.
type parityObserver struct {
	t             *testing.T
	e             *rules.Engine
	decisions     int
	seenStack     bool
	seenBattle    bool
	seenHidden    bool
	privacyChecks int
}

func (o *parityObserver) setup(e *rules.Engine) { o.e = e }

func (o *parityObserver) observe(seatIdx int, d *decision.Decision, _ decision.Intent, _ *botpolicy.Board) error {
	if o.e == nil {
		return fmt.Errorf("parity observer: engine not captured")
	}
	o.decisions++
	translator := manabrew.New("parity", int64(seatIdx), nil)
	for sid := 0; sid < len(o.e.G.Players); sid++ {
		v := view.ProjectFor(o.e.G, o.e, state.PlayerID(sid), view.Seat, d)
		v.Round = view.RoundOf(o.e.G, o.e.L.Events)
		gv := gameViewOf(o.t, translator.State(v))
		compareStateParity(o.t, fmt.Sprintf("seq %d seat %d", d.Seq, sid), v, gv)
		compareSeatPrivacy(o.t, fmt.Sprintf("seq %d seat %d", d.Seq, sid), v, gv)
		o.privacyChecks++
		o.note(v, state.PlayerID(sid))
	}
	return nil
}

func (o *parityObserver) note(v view.View, _ state.PlayerID) {
	if len(v.Stack) > 0 {
		o.seenStack = true
	}
	for _, p := range v.Players {
		if len(p.Battlefield) > 0 {
			o.seenBattle = true
		}
		if p.Hand == nil && p.HandSize > 0 {
			o.seenHidden = true
		}
	}
}

// playParityGame plays one repo-deck game with the parity observer installed,
// through the regular TranslatingSeat/MockClient path.
func playParityGame(t *testing.T, reg *cards.Registry, seats int, seed uint64, obs *parityObserver) {
	t.Helper()
	names := testutil.LegacyDeckNames()
	playerNames := make([]string, seats)
	decks := make([][]*cards.Card, seats)
	for i := 0; i < seats; i++ {
		playerNames[i] = names[(int(seed)+i)%len(names)]
		decks[i] = testutil.RepoDeck(t, reg, playerNames[i])
	}
	cfg := rules.Config{Seed: seed, Names: playerNames, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.AllCards()}
	client := NewFirstLegalClient()
	seatList := make([]seat.Seat, seats)
	for i := range seatList {
		seatList[i] = NewTranslatingSeat("parity", int64(seed), client, NewCensus())
	}
	hooks := bench.Hooks{
		Setup:    obs.setup,
		Decision: obs.observe,
	}
	outcome, _, err := bench.PlayGame(cfg, seatList, censusMaxTurns, censusMaxIntents, hooks)
	if err != nil {
		t.Fatalf("seats=%d seed=%d: PlayGame: %v", seats, seed, err)
	}
	if bench.IsAbort(outcome.StallOn) {
		t.Fatalf("seats=%d seed=%d: engine abort (%s): %s", seats, seed, outcome.StallOn, outcome.Livelock)
	}
}

// TestManaBrewStateParity compares Translator.State(v) with the native
// view.View it came from, for every seat at every posed decision of the MB-8
// census games (2 and 4 seats).
func TestManaBrewStateParity(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	obs := &parityObserver{t: t}
	for _, seats := range []int{2, 4} {
		for g := 0; g < parityGamesPerSeatCount; g++ {
			seed := uint64(seats)*1000 + uint64(g)
			playParityGame(t, reg, seats, seed, obs)
		}
	}
	if obs.decisions == 0 || obs.privacyChecks == 0 {
		t.Fatal("parity observer saw zero decisions -- the .cards corpus or the decision loop is not reachable")
	}
	// Preconditions: the run must actually have exercised a stack, a
	// battlefield card and a hidden hand, or the field-by-field comparison
	// passed on empty views.
	if !obs.seenStack {
		t.Error("no decision observed with a non-empty stack")
	}
	if !obs.seenBattle {
		t.Error("no decision observed with a battlefield permanent")
	}
	if !obs.seenHidden {
		t.Error("no decision observed with another seat's hidden hand")
	}
	t.Logf("state parity: %d decisions, %d seat-views checked", obs.decisions, obs.privacyChecks)
}

// TestManaBrewNoLeak asserts the GameView never reveals a card identity the
// seat's native view redacted, over the same games.
func TestManaBrewNoLeak(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	obs := &leakObserver{t: t}
	for _, seats := range []int{2, 4} {
		for g := 0; g < parityGamesPerSeatCount; g++ {
			seed := uint64(seats)*2000 + uint64(g)
			playLeakGame(t, reg, seats, seed, obs)
		}
	}
	if obs.checks == 0 {
		t.Fatal("leak observer saw zero decisions -- the .cards corpus or the decision loop is not reachable")
	}
	if !obs.sawHidden {
		t.Error("no decision observed with another seat's hidden hand; the leak assertion is vacuous")
	}
	checkSyntheticFaceDownPrivacy(t)
	t.Logf("no-leak: %d seat-views checked", obs.checks)
}

// checkSyntheticFaceDownPrivacy pins the face-down invariant on a hand-built
// view, so it is never vacuous when the repo decks produce no morph/manifest
// permanent: a face-down permanent must reach the GameView with a blank
// identity and isFaceDown set, revealing no more than the native view (which
// itself blanks the face for every viewer that may not look).
func checkSyntheticFaceDownPrivacy(t *testing.T) {
	t.Helper()
	v := view.View{Viewer: 0, Turn: 1, Step: "main1", Active: 0, Priority: 0,
		Players: []view.PlayerView{
			{ID: 0, Name: "A", Life: 20, Hand: []view.CardView{}, HandSize: 0, LibrarySize: 0,
				Battlefield: []view.CardView{{ID: 7, FaceDown: true, Printing: view.Printing{Name: "Secret Morph"}, Types: "Creature — Shapeshifter", Owner: 0, Controller: 0}}},
			{ID: 1, Name: "B", Life: 20, Hand: nil, HandSize: 3, LibrarySize: 20, Battlefield: nil},
		}}
	// Precondition: the fixture must actually carry a face-down permanent with
	// a printed name, or the assertion below would pass vacuously.
	if len(v.Players[0].Battlefield) != 1 || !v.Players[0].Battlefield[0].FaceDown || v.Players[0].Battlefield[0].Printing.Name == "" {
		t.Fatal("synthetic face-down precondition failed")
	}
	gv := gameViewOf(t, manabrew.New("leak", 1, nil).State(v))
	compareSeatPrivacy(t, "synthetic face-down", v, gv)
	found := false
	for _, z := range gv.Zones {
		for _, cv := range z.Cards {
			vc, ok := cv.Value.(mb.VisibleCard)
			if !ok || vc.ID != "o7" {
				continue
			}
			found = true
			if vc.Identity.Name != "" || !vc.IsFaceDown {
				t.Errorf("synthetic face-down permanently leaked identity %+v (isFaceDown=%v)", vc.Identity, vc.IsFaceDown)
			}
		}
	}
	if !found {
		t.Fatal("synthetic face-down permanent was not projected at all")
	}
}

type leakObserver struct {
	t          *testing.T
	e          *rules.Engine
	checks     int
	sawHidden  bool
	sawLibrary bool
}

func (o *leakObserver) setup(e *rules.Engine) { o.e = e }

func (o *leakObserver) observe(_ int, d *decision.Decision, _ decision.Intent, _ *botpolicy.Board) error {
	if o.e == nil {
		return fmt.Errorf("leak observer: engine not captured")
	}
	translator := manabrew.New("leak", 1, nil)
	playerCount := len(o.e.G.Players)
	for sid := 0; sid < playerCount; sid++ {
		viewer := state.PlayerID(sid)
		v := view.ProjectFor(o.e.G, o.e, viewer, view.Seat, d)
		v.Round = view.RoundOf(o.e.G, o.e.L.Events)
		gv := gameViewOf(o.t, translator.State(v))
		compareSeatPrivacy(o.t, fmt.Sprintf("seq %d seat %d", d.Seq, sid), v, gv)
		// Direct engine-level check: no true hidden card (another seat's hand,
		// any library) may appear as a visible identity, matched by object id.
		hidden := hiddenIDsFor(o.e, viewer, playerCount)
		for _, z := range gv.Zones {
			for _, cv := range z.Cards {
				vc, ok := cv.Value.(mb.VisibleCard)
				if !ok {
					continue
				}
				id, ok := parseCardID(vc.ID)
				if ok && hidden[id] {
					o.t.Errorf("seq %d seat %d: GameView leaked hidden card %s (%q) in zone %s owner %s",
						d.Seq, sid, vc.ID, vc.Identity.Name, z.Zone, z.OwnerID)
				}
			}
		}
		if v.Players[viewer].Hand == nil {
			o.t.Errorf("seq %d seat %d: viewer's own hand is nil in the native view", d.Seq, sid)
		}
		for i, p := range v.Players {
			if state.PlayerID(i) != viewer && p.Hand != nil {
				o.t.Errorf("seq %d seat %d: another seat's hand is non-nil in the native view", d.Seq, sid)
			}
		}
		for i := range v.Players {
			if len(o.e.G.Zone(state.ZLibrary, state.PlayerID(i))) > 0 {
				o.sawLibrary = true
			}
			if state.PlayerID(i) != viewer && len(o.e.G.Zone(state.ZHand, state.PlayerID(i))) > 0 {
				o.sawHidden = true
			}
		}
		o.checks++
	}
	return nil
}

func playLeakGame(t *testing.T, reg *cards.Registry, seats int, seed uint64, obs *leakObserver) {
	t.Helper()
	names := testutil.LegacyDeckNames()
	playerNames := make([]string, seats)
	decks := make([][]*cards.Card, seats)
	for i := 0; i < seats; i++ {
		playerNames[i] = names[(int(seed)+i)%len(names)]
		decks[i] = testutil.RepoDeck(t, reg, playerNames[i])
	}
	cfg := rules.Config{Seed: seed, Names: playerNames, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.AllCards()}
	client := NewFirstLegalClient()
	seatList := make([]seat.Seat, seats)
	for i := range seatList {
		seatList[i] = NewTranslatingSeat("leak", int64(seed), client, NewCensus())
	}
	hooks := bench.Hooks{Setup: obs.setup, Decision: obs.observe}
	outcome, _, err := bench.PlayGame(cfg, seatList, censusMaxTurns, censusMaxIntents, hooks)
	if err != nil {
		t.Fatalf("seats=%d seed=%d: PlayGame: %v", seats, seed, err)
	}
	if bench.IsAbort(outcome.StallOn) {
		t.Fatalf("seats=%d seed=%d: engine abort (%s): %s", seats, seed, outcome.StallOn, outcome.Livelock)
	}
}

// --- small helpers -----------------------------------------------------------

func strPtr(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func equalIntMap[K comparable](a, b map[K]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func equalStringsUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

// stepKindOf is the expected native-name → StepKind mapping, mirroring
// internal/manabrew's unexported stepKind so the parity test can assert it
// without exporting the function. TestStepMapTotal in package manabrew keeps
// the real map total; a native step with no entry here yields "" and fails the
// comparison.
func stepKindOf(step string) mb.StepKind {
	switch step {
	case "untap":
		return mb.StepUntap
	case "upkeep":
		return mb.StepUpkeep
	case "draw":
		return mb.StepDraw
	case "main1":
		return mb.StepMain1
	case "begin-combat":
		return mb.StepCombatBegin
	case "declare-attackers":
		return mb.StepCombatDeclareAttackers
	case "declare-blockers":
		return mb.StepCombatDeclareBlockers
	case "combat-damage":
		return mb.StepCombatDamage
	case "end-combat":
		return mb.StepCombatEnd
	case "main2":
		return mb.StepMain2
	case "end":
		return mb.StepEndOfTurn
	case "cleanup":
		return mb.StepCleanup
	}
	return mb.StepKind("")
}
