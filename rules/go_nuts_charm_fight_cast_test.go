package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestGoNutsTeamworkedCharmAnnouncesFightTargetBeforePayment(t *testing.T) {
	t.Parallel()
	e, cfg, mine, theirs := cr601Board(t, 61035,
		map[string]state.Zone{"Go Nuts!": state.ZHand, "Goblin Piker": state.ZBattlefield, "Grizzly Bears": state.ZBattlefield, "Hill Giant": state.ZBattlefield},
		map[string]state.Zone{"Grizzly Bears": state.ZBattlefield, "Craw Wurm": state.ZBattlefield})
	spell, paymentA, paymentB, modeRoot := mine["Go Nuts!"], mine["Goblin Piker"], mine["Grizzly Bears"], mine["Hill Giant"]
	linkedTarget, otherOpponent := theirs["Grizzly Bears"], theirs["Craw Wurm"]
	for _, check := range []struct {
		name string
		id   state.ObjID
		zone state.Zone
	}{{"spell", spell, state.ZHand}, {"Teamwork creature", paymentA, state.ZBattlefield},
		{"second Teamwork creature", paymentB, state.ZBattlefield}, {"fight mode root", modeRoot, state.ZBattlefield}, {"linked target", linkedTarget, state.ZBattlefield},
		{"other opponent creature", otherOpponent, state.ZBattlefield}} {
		if o := e.G.Obj(check.id); o == nil || o.Zone != check.zone {
			t.Fatalf("precondition: %s %d zone=%v, want %v", check.name, check.id, o, check.zone)
		}
	}
	if modeRoot == linkedTarget || linkedTarget == otherOpponent ||
		e.G.Obj(modeRoot).Controller != 0 || e.G.Obj(linkedTarget).Controller != 1 || e.G.Obj(otherOpponent).Controller != 1 {
		t.Fatalf("precondition: distinct own root and two opponent candidates required: root=%+v linked=%+v other=%+v",
			e.G.Obj(modeRoot), e.G.Obj(linkedTarget), e.G.Obj(otherOpponent))
	}
	if e.G.Obj(modeRoot).Face().Power() == e.G.Obj(linkedTarget).Face().Power() {
		t.Fatalf("precondition: fight creatures need distinguishable power, root=%d linked=%d",
			e.G.Obj(modeRoot).Face().Power(), e.G.Obj(linkedTarget).Face().Power())
	}
	addMana(t, e, 0, "G")
	cr601Cast(t, e, spell, "teamworked")
	d := teamworkAskOptions(t, e)
	if !d.AllowNone || d.MinSum != 3 || len(d.Options) < 2 {
		t.Fatalf("precondition: Go Nuts! Teamwork must be payable and declinable: %+v", d)
	}
	submitChoices(t, e, teamworkOption(t, d, paymentA), teamworkOption(t, d, paymentB))
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "cast_modes" {
		t.Fatalf("pending=%+v, want cast KModes after paying-choice declaration", d)
	}
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("paid Teamwork mode bounds=%d..%d, want both Charm modes (offered %d modes)", d.Min, d.Max, len(d.Options))
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)

	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind == "cast_sub" || d.Min != 2 || d.Max != 2 {
		t.Fatalf("pending=%+v, want the combined two-mode root target ask before payment", d)
	}
	rootChoices := make([]int, 0, 2)
	rootObjects := map[string]bool{}
	fightRootOffered := false
	for _, option := range d.Options {
		if option.Kind == "player" || e.G.Obj(option.Obj) == nil || e.G.Obj(option.Obj).Controller != 0 {
			continue
		}
		if option.Group == "charm-mode-1" && option.Obj == modeRoot {
			fightRootOffered = true
		}
		if !rootObjects[option.Group] && (option.Group != "charm-mode-1" || option.Obj == modeRoot) {
			rootObjects[option.Group] = true
			rootChoices = append(rootChoices, option.Index)
		}
	}
	if len(rootChoices) != 2 || !fightRootOffered {
		t.Fatalf("Charm mode roots lack legal own-creature choices including fight root %d: %+v", modeRoot, d.Options)
	}
	if links := castSubAskLinks(e, e.cast, e.castStageSA(e.cast, e.G.Obj(spell), e.G.Obj(spell).Face())); len(links) != 1 {
		t.Fatalf("selected Charm mode cast-time chain links=%d, want DBFight: %+v", len(links), links)
	}
	submitChoices(t, e, rootChoices...)
	d = castSubAsk(t, e)
	foundLinked, foundOther := false, false
	for _, option := range d.Options {
		if option.Obj == linkedTarget {
			foundLinked = true
		}
		if option.Obj == otherOpponent {
			foundOther = true
		}
	}
	if !foundLinked || !foundOther {
		t.Fatalf("linked DBFight ask options=%+v, want both distinct opponent creatures", d.Options)
	}
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("precondition: mana paid before the linked target ask: pool=%d want 1", got)
	}
	if e.G.Obj(paymentA).Tapped || e.G.Obj(paymentB).Tapped {
		t.Fatalf("precondition: Teamwork paid before linked target ask: payment creature=%+v root=%+v",
			e.G.Obj(paymentA), e.G.Obj(paymentB))
	}
	answerCastSubObj(t, e, linkedTarget)
	so := e.G.Obj(spell)
	if so == nil || so.Zone != state.ZStack || so.CastFlags&state.FlagTeamworkPaid == 0 ||
		!e.G.Obj(paymentA).Tapped || !e.G.Obj(paymentB).Tapped {
		t.Fatalf("paid cast not completed after target announcement: spell=%+v payment=%+v root=%+v",
			so, e.G.Obj(paymentA), e.G.Obj(paymentB))
	}
	if len(so.SubTargets) != 1 || so.SubTargets[0].Obj != linkedTarget {
		t.Fatalf("recorded SubTargets=%+v, want selected opponent %d", so.SubTargets, linkedTarget)
	}
	if d = e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after cast pending=%+v, want priority", d)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("mana payment after target announcement left pool=%d, want 0", got)
	}
	cr601ResolveQuietly(t, e)
	if got := e.G.Obj(linkedTarget); got == nil || got.Zone != state.ZGraveyard {
		t.Fatalf("selected linked target %d=%+v, want it to have fought and died", linkedTarget, got)
	}
	if got := e.G.Obj(otherOpponent); got == nil || got.Zone != state.ZBattlefield {
		t.Fatalf("unselected opponent %d=%+v, want untouched on battlefield", otherOpponent, got)
	}
	replayCheck(t, e, cfg)
}

func TestGoNutsDeclinedTeamworkSingleCharmModeAnnouncesFightTargetBeforePayment(t *testing.T) {
	t.Parallel()
	e, cfg, mine, theirs := cr601Board(t, 61036,
		map[string]state.Zone{"Go Nuts!": state.ZHand, "Goblin Piker": state.ZBattlefield, "Hill Giant": state.ZBattlefield},
		map[string]state.Zone{"Grizzly Bears": state.ZBattlefield, "Craw Wurm": state.ZBattlefield})
	spell, paymentCreature, modeRoot := mine["Go Nuts!"], mine["Goblin Piker"], mine["Hill Giant"]
	linkedTarget, otherOpponent := theirs["Grizzly Bears"], theirs["Craw Wurm"]
	for _, check := range []struct {
		name string
		id   state.ObjID
		zone state.Zone
	}{{"spell", spell, state.ZHand}, {"Teamwork creature", paymentCreature, state.ZBattlefield},
		{"fight mode root", modeRoot, state.ZBattlefield}, {"linked target", linkedTarget, state.ZBattlefield},
		{"other opponent creature", otherOpponent, state.ZBattlefield}} {
		if o := e.G.Obj(check.id); o == nil || o.Zone != check.zone {
			t.Fatalf("precondition: %s %d zone=%v, want %v", check.name, check.id, o, check.zone)
		}
	}
	if e.G.Obj(modeRoot).Controller != 0 || e.G.Obj(linkedTarget).Controller != 1 || e.G.Obj(otherOpponent).Controller != 1 ||
		modeRoot == linkedTarget || linkedTarget == otherOpponent ||
		e.G.Obj(modeRoot).Face().Power() == e.G.Obj(linkedTarget).Face().Power() {
		t.Fatalf("precondition: legal distinct own fight-root and opponent targets with differing power required: root=%+v linked=%+v other=%+v",
			e.G.Obj(modeRoot), e.G.Obj(linkedTarget), e.G.Obj(otherOpponent))
	}
	addMana(t, e, 0, "G")
	priority := e.Pending()
	plainCast, teamworkCast := false, false
	if priority == nil || priority.Kind != decision.KPriority {
		t.Fatalf("precondition: cast priority pending, got %+v", priority)
	}
	for _, option := range priority.Options {
		if option.Kind == "cast" && option.Obj == spell && option.Mode == "" {
			plainCast = true
		}
		if option.Kind == "cast" && option.Obj == spell && option.Mode == "teamworked" {
			teamworkCast = true
		}
	}
	if !plainCast || !teamworkCast {
		t.Fatalf("precondition: normal and Teamwork cast choices should both be available: %+v", priority.Options)
	}
	cr601Cast(t, e, spell, "") // Decline Teamwork by choosing the normal cast.
	modes := e.Pending()
	if modes == nil || modes.Kind != decision.KModes || modes.ResumeKind != "cast_modes" || modes.Min != 1 || modes.Max != 1 {
		t.Fatalf("pending=%+v, want exactly one Charm mode after declining Teamwork", modes)
	}
	fightMode := -1
	for _, option := range modes.Options {
		if strings.Contains(option.Label, "fights") {
			fightMode = option.Index
		}
	}
	if fightMode < 0 {
		t.Fatalf("precondition: Go Nuts! fight mode not offered: %+v", modes.Options)
	}
	submitChoices(t, e, fightMode)
	roots := e.Pending()
	if roots == nil || roots.Kind != decision.KTarget || roots.ResumeKind == "cast_sub" || roots.Min != 1 || roots.Max != 1 {
		t.Fatalf("pending=%+v, want single fight-mode root target before payment", roots)
	}
	rootIndex := -1
	for _, option := range roots.Options {
		if option.Kind != "player" && option.Obj == modeRoot {
			rootIndex = option.Index
		}
	}
	if rootIndex < 0 {
		t.Fatalf("legal own fight-mode root %d not offered: %+v", modeRoot, roots.Options)
	}
	submitChoices(t, e, rootIndex)
	linkedAsk := e.Pending()
	if linkedAsk == nil || linkedAsk.Kind != decision.KTarget || linkedAsk.ResumeKind != "cast_sub" {
		kind, resume := decision.Kind("<nil>"), ""
		if linkedAsk != nil {
			kind, resume = linkedAsk.Kind, linkedAsk.ResumeKind
		}
		t.Fatalf("after root target, pending kind=%s resume=%q, want cast_sub KTarget before payment", kind, resume)
	}
	foundLinked, foundOther := false, false
	for _, option := range linkedAsk.Options {
		foundLinked = foundLinked || option.Obj == linkedTarget
		foundOther = foundOther || option.Obj == otherOpponent
	}
	if !foundLinked || !foundOther {
		t.Fatalf("linked DBFight ask options=%+v, want both distinct opponent creatures", linkedAsk.Options)
	}
	if got := e.G.Players[0].Pool.Total(); got != 1 || e.G.Obj(paymentCreature).Tapped {
		t.Fatalf("precondition: declined Teamwork and mana must remain unpaid at linked ask: pool=%d creature=%+v", got, e.G.Obj(paymentCreature))
	}
	answerCastSubObj(t, e, linkedTarget)
	so := e.G.Obj(spell)
	if so == nil || so.Zone != state.ZStack || so.CastFlags&state.FlagTeamworkPaid != 0 {
		t.Fatalf("declined-Teamwork spell not completed with declined provenance: %+v", so)
	}
	if len(so.SubTargets) != 1 || so.SubTargets[0].Obj != linkedTarget {
		t.Fatalf("recorded SubTargets=%+v, want selected opponent %d", so.SubTargets, linkedTarget)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("mana payment after target announcement left pool=%d, want 0", got)
	}
	cr601ResolveQuietly(t, e)
	if got := e.G.Obj(linkedTarget); got == nil || got.Zone != state.ZGraveyard {
		t.Fatalf("selected linked target %d=%+v, want it to have fought and died", linkedTarget, got)
	}
	if got := e.G.Obj(otherOpponent); got == nil || got.Zone != state.ZBattlefield {
		t.Fatalf("unselected opponent %d=%+v, want untouched on battlefield", otherOpponent, got)
	}
	replayCheck(t, e, cfg)
}
