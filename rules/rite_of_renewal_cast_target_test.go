package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// CR 601.2c: "Rite of Renewal" -- "Return up to two target permanent cards
// from your graveyard to your hand. Target player shuffles up to four target
// cards from their graveyard into their library." The second sentence's
// targets are chosen as the spell is cast, and their legal set depends on the
// earlier "target player" choice (TargetsWithDefinedController$ ParentTarget).
// Before the fix the chain link was never announced on cast (castSubPreAskable
// deferred it because the census could not bind the parent at offer time) and
// the slot was asked at RESOLUTION, where XMage chose nothing -- the reported
// divergence. This pins the cast-time ask, the parent-relative offer and the
// resolution that moves exactly the announced card.
func TestRiteOfRenewalDependentGraveyardTargetsAnnouncedOnCast(t *testing.T) {
	t.Parallel()
	e, cfg, mine, theirs := cr601Board(t, 62004,
		map[string]state.Zone{
			"Rite of Renewal": state.ZHand,
			"Grizzly Bears":   state.ZGraveyard,
		},
		map[string]state.Zone{
			"Centaur Courser": state.ZGraveyard,
			"Craw Wurm":       state.ZGraveyard,
		})
	spell := mine["Rite of Renewal"]
	ownCard := mine["Grizzly Bears"]
	theirCard, theirOther := theirs["Centaur Courser"], theirs["Craw Wurm"]

	// Preconditions the assertions below depend on: the root's own graveyard
	// card, and seat 1's two graveyard cards.
	for id, want := range map[state.ObjID]state.PlayerID{ownCard: 0, theirCard: 1, theirOther: 1} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZGraveyard || o.Controller != want {
			t.Fatalf("precondition: card %d = %+v, want controller %d's graveyard", id, o, want)
		}
	}
	if ownCard == theirCard || ownCard == theirOther {
		t.Fatalf("precondition: the two seats' graveyard cards must be distinct (own %d, theirs %d/%d)", ownCard, theirCard, theirOther)
	}
	addMana(t, e, 0, "GGGC")
	edrSeatZeroPriority(t, e)

	cr601Cast(t, e, spell, "")

	// CR 601.2c: the root target -- up to two permanent cards from the
	// caster's own graveyard.
	root := e.Pending()
	if root == nil || root.Kind != decision.KTarget || root.ResumeKind == "cast_sub" {
		t.Fatalf("pending = %+v, want the root's own target ask", root)
	}
	answerTargetAsk(t, e, []state.ObjID{ownCard})

	// The DBPump player target, still before payment.
	answerCastSubPlayer(t, e, 1)

	// Then the dependent graveyard link: up to four cards in the TARGETED
	// player's graveyard.
	dep := castSubAsk(t, e)
	if dep.Player != 0 || dep.Min != 0 || dep.Max != 4 {
		t.Fatalf("dependent graveyard link ask = player %d bounds %d..%d, want the caster's up-to-four", dep.Player, dep.Min, dep.Max)
	}
	offered := map[state.ObjID]bool{}
	for _, o := range dep.Options {
		offered[o.Obj] = true
	}
	if !offered[theirCard] || !offered[theirOther] {
		t.Fatalf("dependent link ask offers %+v, want both of seat 1's graveyard cards", dep.Options)
	}
	if offered[ownCard] {
		t.Fatalf("dependent link offered seat 0's own graveyard card %d: TargetsWithDefinedController$ ParentTarget must restrict to the targeted player", ownCard)
	}
	// 601.2c precedes 601.2h: nothing has been paid while targets are chosen.
	if got := e.G.Players[0].Pool.Total(); got != 4 {
		t.Fatalf("mana was spent before the dependent targets were chosen: pool %d, want 4", got)
	}
	answerCastSubObj(t, e, theirCard)

	// Paid now, and the named card rides the stack object as a chain target.
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("the spell was not paid after its targets were chosen: pool %d", got)
	}
	so := e.G.Obj(spell)
	if so == nil {
		t.Fatal("precondition: the cast spell is not on the stack")
	}
	var gotPlayer, gotCard bool
	for _, tgt := range so.SubTargets {
		switch {
		case tgt.IsPlayer && tgt.Player == 1:
			gotPlayer = true
		case !tgt.IsPlayer && tgt.Obj == theirCard:
			gotCard = true
		}
	}
	if !gotPlayer || !gotCard {
		t.Fatalf("chain targets = %+v, want seat 1's player target and card %d named on cast", so.SubTargets, theirCard)
	}

	// Resolution asks nothing about targets -- every one was announced on
	// cast -- so pass priority until the stack empties, failing on any other
	// decision (a re-posed targeting ask would be the defect).
	cr601ResolveQuietly(t, e)

	if o := e.G.Obj(theirCard); o == nil || o.Zone != state.ZLibrary {
		t.Fatalf("the named card %d zone %v, want the library (shuffled from the graveyard)", theirCard, o)
	}
	if o := e.G.Obj(theirOther); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the un-named graveyard card %d zone %v, want the graveyard (it must not move)", theirOther, o)
	}
	if o := e.G.Obj(ownCard); o == nil || o.Zone != state.ZHand {
		t.Fatalf("the root's card %d zone %v, want the hand (returned)", ownCard, o)
	}
	replayCheck(t, e, cfg)
}

// TestTargetsWithDefinedControllerParentTargetCensus walks every front-face
// ability's SubAbility$ chain and classifies each link carrying
// TargetsWithDefinedController$ ParentTarget/ParentTargetedController by
// whether the cast flow announces its targets on cast (CR 601.2c) or defers
// them to resolution. It is a ratchet in the same sense as
// knownUnjudgedChangeZoneSubTargets: a card that newly stops being announced
// is a regression, and a card that newly starts being announced has a stale
// entry here.
//
// The deferred set is the MANDATORY links (and choosers / dynamic bounds) that
// read an earlier target: the root's offer census does not yet require the
// link to have a legal pool, so asking them on cast would abort castable
// spells (TestChainTargetOfferCensusAgreesWithCastFlow). Every optional
// (TargetMin$ 0) link -- the graveyard-shuffle family (Rite of Renewal,
// Krosan Reclamation, Memory's Journey, ...) -- is announced on cast.
func TestTargetsWithDefinedControllerParentTargetCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	admitted, deferred := 0, map[string]string{}
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
			for sa := root; sa != nil; sa = sa.Sub {
				ref := strings.TrimSpace(effects.TargetsOf(sa).DefinedController)
				if ref != "ParentTarget" && ref != "ParentTargetedController" {
					continue
				}
				if castSubChangeZoneAnnounceable(sa) && !subTargetingRootUnbindable(sa, f.SVars) {
					admitted++
					continue
				}
				if !seen[f.Name] {
					seen[f.Name] = true
					deferred[f.Name] = ref
				}
			}
		}
	}
	if admitted != 7 {
		t.Fatalf("%d TargetsWithDefinedController$ parent links are announced on cast, want exactly 7 (including every optional literal-bound link, excluding unresolvable dynamic parent-relative bounds)", admitted)
	}
	if len(reg.Cards) < 30000 {
		t.Fatalf("census corpus has %d cards, want the full ~33667", len(reg.Cards))
	}
	for name := range knownDeferredParentTargetLinks {
		if _, ok := deferred[name]; !ok {
			t.Errorf("%s: its TargetsWithDefinedController$ parent link is now announced on cast; delete the ratchet entry", name)
		}
	}
	for name, ref := range deferred {
		if _, ok := knownDeferredParentTargetLinks[name]; !ok {
			t.Errorf("%s: TargetsWithDefinedController$ %s link is not announced on cast; a new gap", name, ref)
		}
	}
	t.Logf("census: %d parent-relative links announced on cast, %d cards deferred: %v", admitted, len(deferred), deferred)
}

// knownDeferredParentTargetLinks pins the corpus cards whose
// TargetsWithDefinedController$ ParentTarget/ParentTargetedController link the
// cast flow still does NOT announce (their declaration reads an earlier target
// through a chooser or a dynamic bound the cast census cannot bind). It is a
// ratchet: a card that newly becomes announceable is stale and fails by name,
// and a card that newly stops being announced is a new gap that fails too.
var knownDeferredParentTargetLinks = map[string]string{
	"Drafna's Restoration":       "ParentTarget",
	"Breaking of the Fellowship": "ParentTargetedController",
	"Goblin Welder":              "ParentTargetedController",
	"Mutiny":                     "ParentTargetedController",
	"Suffer the Past":            "ParentTarget",
}
