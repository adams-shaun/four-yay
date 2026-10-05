package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// CR 601.2c: a spell's SubAbility$ ChangeZone link with an explicit Origin$
// Graveyard declares real targets ("shuffle up to four target cards from your
// graveyard into your library") and they are chosen while the spell is cast,
// not as it resolves. These tests pin Cathartic Parting end to end -- the
// cast-time announcement, the payment ordering, and the resolution moving
// exactly the announced cards -- and pin the corpus census that says which
// targeted ChangeZone chain links the cast census can and cannot resolve.

// TestCatharticPartingGraveyardTargetsAnnouncedOnCast is the defect's own
// card. Seat 0 casts Cathartic Parting (SP$ ChangeZone ... SubAbility$
// DBChangeZone, the graveyard link TargetMin$ 0 / TargetMax$ 4). The root
// target is the opponent's artifact; the graveyard link's up-to-four targets
// must be announced while the spell is still unpaid (a cast_sub KTarget), and
// resolution must move exactly the cards named on cast.
func TestCatharticPartingGraveyardTargetsAnnouncedOnCast(t *testing.T) {
	t.Parallel()
	e, cfg, mine, theirs := cr601Board(t, 61001,
		map[string]state.Zone{
			"Cathartic Parting": state.ZHand,
			"Grizzly Bears":     state.ZGraveyard,
			"Centaur Courser":   state.ZGraveyard,
			"Craw Wurm":         state.ZGraveyard,
		},
		map[string]state.Zone{"Sol Ring": state.ZBattlefield})
	spell := mine["Cathartic Parting"]
	named1, named2, unnamed := mine["Grizzly Bears"], mine["Centaur Courser"], mine["Craw Wurm"]
	ring := theirs["Sol Ring"]
	addMana(t, e, 0, "G1")
	edrSeatZeroPriority(t, e)

	// Preconditions the assertions below depend on: the three graveyard cards
	// really are in seat 0's graveyard, and the opponent's artifact is on the
	// battlefield as the root's legal target.
	for _, id := range []state.ObjID{named1, named2, unnamed} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("precondition: card %d zone %v, want the graveyard", id, o)
		}
	}
	if o := e.G.Obj(ring); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Sol Ring zone %v, want the opponent's battlefield", o)
	}

	cr601Cast(t, e, spell, "")

	// CR 601.2c: the root target first.
	root := e.Pending()
	if root == nil || root.Kind != decision.KTarget || root.ResumeKind == "cast_sub" {
		t.Fatalf("pending = %+v, want the root's own target ask", root)
	}
	answerTargetAsk(t, e, []state.ObjID{ring})

	// Then the graveyard link's ask, still before payment.
	sub := castSubAsk(t, e)
	if sub.Player != 0 || sub.Min != 0 || sub.Max != 4 {
		t.Fatalf("graveyard link ask = player %d bounds %d..%d, want the caster's up-to-four", sub.Player, sub.Min, sub.Max)
	}
	offered := map[state.ObjID]bool{}
	for _, o := range sub.Options {
		offered[o.Obj] = true
	}
	if !offered[named1] || !offered[named2] || !offered[unnamed] {
		t.Fatalf("graveyard link ask offers %+v, want every card in the caster's graveyard", sub.Options)
	}
	if offered[ring] {
		t.Fatal("the graveyard link offered a battlefield permanent: it must offer only the graveyard")
	}
	// 601.2c precedes 601.2h: nothing has been paid while targets are chosen.
	if got := e.G.Players[0].Pool.Total(); got != 2 {
		t.Fatalf("mana was spent before the graveyard targets were chosen: pool %d, want 2", got)
	}
	// Answer the one up-to-four ask with BOTH named cards in a single intent
	// (the wire contract rejects a second ask: the whole selection is one
	// CR 601.2c announcement).
	var pick []int
	for _, want := range []state.ObjID{named1, named2} {
		found := false
		for _, o := range sub.Options {
			if o.Obj == want {
				pick = append(pick, o.Index)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("graveyard card %d was not offered: %+v", want, sub.Options)
		}
	}
	if err := e.Submit(decision.Intent{Seq: sub.Seq, Player: sub.Player, Choices: pick}); err != nil {
		t.Fatalf("submit graveyard targets: %v", err)
	}

	// Paid now, and both named cards ride the stack object as chain targets.
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("the spell was not paid after its targets were chosen: pool %d", got)
	}
	so := e.G.Obj(spell)
	if so == nil || len(so.SubTargets) != 2 {
		t.Fatalf("chain targets = %+v, want the two cards named on cast", so)
	}
	gotNamed := map[state.ObjID]bool{so.SubTargets[0].Obj: true, so.SubTargets[1].Obj: true}
	if !gotNamed[named1] || !gotNamed[named2] || gotNamed[unnamed] {
		t.Fatalf("chain targets = %+v, want exactly the two cards named on cast", so.SubTargets)
	}
	evs := subTargetEvents(e, spell)
	if len(evs) != 2 {
		t.Fatalf("chain TargetsChosen events = %+v, want one per graveyard card named on cast", evs)
	}
	eventIDs := map[state.ObjID]bool{}
	for _, ev := range evs {
		for _, id := range ev.IDs {
			eventIDs[id] = true
		}
	}
	if !eventIDs[named1] || !eventIDs[named2] || eventIDs[unnamed] {
		t.Fatalf("chain TargetsChosen events name %v, want exactly the two cards named on cast", eventIDs)
	}

	// Resolution asks nothing about TARGETS (every one was announced on cast),
	// but the link's ShuffleNonMandatory$ True poses its own yes/no shuffle
	// ask; answer it and finish resolving. Any OTHER non-priority decision
	// would be a re-posed targeting ask -- the defect this test exists to
	// rule out.
	for i := 0; i < 40 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		switch {
		case d.Kind == decision.KPriority:
			submitChoices(t, e, passIndex(t, d))
		case string(d.ResumeKind) == "search_mayshuffle":
			submitChoices(t, e, 0) // Yes -- shuffle
		default:
			t.Fatalf("the resolution posed %+v: every target was already announced on cast (CR 601.2c)", d)
		}
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("the stack did not empty: %v", e.G.Stack)
	}
	for _, id := range []state.ObjID{named1, named2} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZLibrary {
			t.Fatalf("card %d zone %v, want the library (the named card moved)", id, o)
		}
	}
	if o := e.G.Obj(unnamed); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the un-named graveyard card zone %v, want the graveyard (it must not move)", o)
	}
	replayCheck(t, e, cfg)
}

// knownUnjudgedChangeZoneSubTargets pins the corpus carriers whose targeted
// SubAbility$ ChangeZone link the cast census does NOT resolve, keyed by card
// name to the link's Origin$. It is a ratchet: the cast flow announces such a
// link on cast only when castSubChangeZoneAnnounceable admits it (a
// player-target link, or an explicit Origin$ Graveyard object target), so a
// card that newly becomes resolvable is stale and fails the census by name,
// and a card that newly becomes unresolvable is a new gap that fails too.
//
// Every entry here is a Battlefield-origin object link -- a bounce spell's
// second "target creature" (Peel from Reality, Withdraw), an exile/return
// rider (Expel the Unworthy, Grip of Desolation). Those ARE the same
// CR 601.2c announcement class as Cathartic Parting's graveyard link; the
// cast census resolves their zone (the battlefield default) correctly, but
// admitting them is a wider change than this ticket scopes. They are filed as
// a follow-up (see the report's Issues section) and pinned here so the count
// can only shrink.
var knownUnjudgedChangeZoneSubTargets = map[string]string{
	"Aether Tradewinds":         "Battlefield",
	"Churning Eddy":             "Battlefield",
	"Cruel Alliance":            "Battlefield",
	"Expel the Unworthy":        "Battlefield",
	"Fiery Annihilation":        "Battlefield",
	"Geth's Summons":            "Graveyard",
	"Grip of Desolation":        "Battlefield",
	"Into the Flood Maw":        "Battlefield",
	"Karn's Temporal Sundering": "Battlefield",
	"Kaya, Spirits' Justice":    "Battlefield",
	"Lost in the Mist":          "Battlefield",
	"Peel from Reality":         "Battlefield",
	"Rise":                      "Battlefield",
	"Rite of Undoing":           "Battlefield",
	"Tear Asunder":              "Battlefield",
	"Time Out":                  "Battlefield",
	"Withdraw":                  "Battlefield",
}

// TestSubAbilityChangeZonePreAskCensus walks every front-face ability's
// SubAbility$ chain and classifies each targeted ChangeZone link by whether
// the cast census resolves it. The admitted set is exactly the corpus's
// player-target links and explicit Origin$ Graveyard object links (Cathartic
// Parting among them); the Battlefield-origin object links -- the same
// announcement class, but out of this ticket's scope -- are left to the
// mid-resolution ask and pinned in the ratchet above.
func TestSubAbilityChangeZonePreAskCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	admitted, unjudged := 0, map[string]string{}
	seen := map[string]bool{}
	for _, c := range reg.Cards {
		if len(c.Faces) == 0 || c.Faces[0] == nil {
			continue
		}
		f := c.Faces[0]
		for _, root := range f.Abilities {
			if root == nil {
				continue
			}
			for sa := root.Sub; sa != nil; sa = sa.Sub {
				if sa.API != "ChangeZone" || !effects.TargetsOf(sa).Targeted() {
					continue
				}
				if castSubChangeZoneAnnounceable(sa) {
					admitted++
					continue
				}
				key := f.Name + "\x00" + strings.TrimSpace(sa.ParamStr(cards.PKOrigin))
				if !seen[key] {
					seen[key] = true
					unjudged[f.Name] = strings.TrimSpace(sa.ParamStr(cards.PKOrigin))
				}
			}
		}
	}
	if admitted < 50 {
		t.Fatalf("only %d targeted ChangeZone sub-links are announced on cast; want the corpus's ~60 (a regression in castSubChangeZoneAnnounceable)", admitted)
	}
	t.Logf("census: %d targeted ChangeZone sub-links announced on cast, %d cards still unjudged", admitted, len(unjudged))
	for name, origin := range knownUnjudgedChangeZoneSubTargets {
		if got, ok := unjudged[name]; !ok {
			t.Errorf("%s: its targeted ChangeZone sub-link is now resolved by the cast census (origin was %q); delete the ratchet entry", name, origin)
		} else if got != origin {
			t.Errorf("%s: ChangeZone sub-link origin %q, want the pinned %q", name, got, origin)
		}
	}
	for name, origin := range unjudged {
		if _, ok := knownUnjudgedChangeZoneSubTargets[name]; !ok {
			t.Errorf("%s: targeted ChangeZone sub-link with Origin$ %q is not announced on cast; a new gap", name, origin)
		}
	}
	// The census must see the corpus's several hundred chain shapes, or it is
	// silently walking nothing (a missing .cards corpus skips the registry).
	if len(reg.Cards) < 30000 {
		t.Fatalf("census corpus has %d cards, want the full ~33667", len(reg.Cards))
	}
}
