package manabrew

import (
	"strconv"
	"strings"

	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// gameView reshapes a seat-redacted view without widening its information.
func (t *Translator) gameView(v view.View) mb.GameViewDto {
	gv := mb.GameViewDto{
		GameID: t.GameID(), Turn: int(v.Turn), Step: stepKind(v.Step),
		CombatAssignments: combatAssignments(v), ActivePlayerID: playerID(v.Active),
		PriorityPlayerID: playerID(v.Priority), Players: make([]mb.PlayerDto, 0, len(v.Players)),
		Zones: make([]mb.ZoneDto, 0, len(v.Players)*5), Stack: make([]mb.StackObjectDto, 0, len(v.Stack)),
		GameOver: v.Over, MonarchID: nil, InitiativeHolderID: nil,
	}
	if v.Winner != nil {
		id := playerID(*v.Winner)
		gv.WinnerID = &id
	}
	for _, p := range v.Players {
		gv.Players = append(gv.Players, player(p))
		if p.HasInitiative {
			id := playerID(p.ID)
			gv.InitiativeHolderID = &id
		}
		gv.Zones = append(gv.Zones, playerZones(v.Viewer, p)...)
	}
	gv.Zones = append(gv.Zones, battlefieldZones(v)...)
	for _, s := range v.Stack {
		gv.Stack = append(gv.Stack, stackObject(s))
	}
	return gv
}

func stepKind(s string) mb.StepKind {
	switch s {
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
	default:
		return mb.StepKind(s)
	}
}

func player(p view.PlayerView) mb.PlayerDto {
	status := mb.PlayerStatus("active")
	if p.Lost {
		status = "lost"
	}
	casts := make(map[string]int, len(p.Commanders))
	for i, c := range p.Commanders {
		n := 0
		if i < len(p.CommanderCasts) {
			n = int(p.CommanderCasts[i])
		}
		casts[cardID(c.ID)] = n
	}
	damage := make(map[string]int, len(p.CmdDamage))
	for id, n := range p.CmdDamage {
		damage[cardID(id)] = int(n)
	}
	pool := make(map[mb.ManaColor]int, len(p.Pool))
	for color, n := range p.Pool {
		pool[mb.ManaColor(color)] = int(n)
	}
	return mb.PlayerDto{ID: playerID(p.ID), Name: p.Name, Status: status, IsHuman: true, Life: int(p.Life), MaxHandSize: 7,
		PlayerKeywords: []string{}, CommanderCasts: casts, Counters: map[mb.PlayerCounterKind]int{}, ManaPool: pool,
		CommanderDamage: damage, HasCityBlessing: false, HasEnduringStory: false}
}

func playerZones(viewer state.PlayerID, p view.PlayerView) []mb.ZoneDto {
	owner := playerID(p.ID)
	out := make([]mb.ZoneDto, 0, 6)
	if p.Hand == nil {
		out = append(out, mb.ZoneDto{Zone: mb.ZoneHand, OwnerID: owner, Cards: []mb.CardView{}, Count: p.HandSize})
	} else {
		out = append(out, zone(mb.ZoneHand, owner, p.Hand, len(p.Hand)))
	}
	library := make([]mb.CardView, 0, 1)
	if p.ID == viewer && p.LibraryTop != nil {
		library = append(library, visibleCard(*p.LibraryTop))
	}
	out = append(out, mb.ZoneDto{Zone: mb.ZoneLibrary, OwnerID: owner, Cards: library, Count: p.LibrarySize})
	out = append(out, zone(mb.ZoneGraveyard, owner, p.Graveyard, len(p.Graveyard)))
	exile := make([]mb.CardView, 0, len(p.Exile))
	for i, c := range p.Exile {
		if c.FaceDown {
			exile = append(exile, hiddenCard(mb.ZoneExile, p.ID, i))
		} else {
			exile = append(exile, visibleCard(c))
		}
	}
	out = append(out, mb.ZoneDto{Zone: mb.ZoneExile, OwnerID: owner, Cards: exile, Count: len(exile)})
	out = append(out, zone(mb.ZoneCommand, owner, p.Command, len(p.Command)))
	return out
}

func battlefieldZones(v view.View) []mb.ZoneDto {
	out := make([]mb.ZoneDto, 0, len(v.Players))
	for _, p := range v.Players {
		cards := make([]mb.CardView, 0)
		for _, c := range p.Battlefield {
			if c.Controller == p.ID {
				cards = append(cards, visibleCard(c))
			}
		}
		out = append(out, mb.ZoneDto{Zone: mb.ZoneBattlefield, OwnerID: playerID(p.ID), Cards: cards, Count: len(cards)})
	}
	return out
}
func zone(kind mb.ZoneKind, owner string, cards []view.CardView, n int) mb.ZoneDto {
	out := mb.ZoneDto{Zone: kind, OwnerID: owner, Cards: make([]mb.CardView, 0, len(cards)), Count: n}
	for _, c := range cards {
		if c.FaceDown {
			out.Cards = append(out.Cards, hiddenCard(kind, c.Owner, len(out.Cards)))
		} else {
			out.Cards = append(out.Cards, visibleCard(c))
		}
	}
	return out
}
func hiddenCard(zone mb.ZoneKind, owner state.PlayerID, i int) mb.CardView {
	return mb.CardView{Value: mb.HiddenCard{Visibility: "hidden", ID: hiddenCardID(string(zone), owner, i)}}
}
func visibleCard(c view.CardView) mb.CardView {
	name := c.Printing.Name
	identity := mb.CardIdentity{Name: name, SetCode: c.Printing.Set, CardNumber: c.Printing.Number}
	if c.FaceDown {
		identity = mb.CardIdentity{}
	}
	identity.IsToken = c.Token != ""
	power, toughness := strconv.Itoa(int(c.Power)), strconv.Itoa(int(c.Toughness))
	card := mb.CardDto{ID: cardID(c.ID), Identity: identity, Color: []string{}, ManaCost: c.ManaCost, Types: []string{}, Subtypes: []string{}, Supertypes: []string{}, Power: &power, Toughness: &toughness,
		ClassLevels: []mb.ClassLevelDto{}, SagaChapters: []mb.SagaChapterDto{}, Text: "", Choices: []mb.CardChoiceDto{}, ControllerID: playerID(c.Controller), OwnerID: playerID(c.Owner), Tapped: c.Tapped,
		IsAttacking: c.Attacking, Keywords: append([]string{}, c.Keywords...), Counters: make(map[string]int), Damage: int(c.Damage), SummoningSick: c.SummonSick, IsCopy: false, IsDoubleFaced: false, IsTransformed: false, IsFaceDown: c.FaceDown, IsBestowed: false, PhasedOut: false, Exerted: false, AttachmentIDs: []string{}, MergedCardIDs: []string{}}
	for k, v := range c.Counters {
		card.Counters[k] = int(v)
	}
	typeLine := strings.SplitN(c.Types, "—", 2)
	for _, typ := range strings.Fields(typeLine[0]) {
		switch typ {
		case "Basic", "Legendary", "Snow", "World":
			card.Supertypes = append(card.Supertypes, typ)
		default:
			card.Types = append(card.Types, typ)
		}
	}
	if len(typeLine) == 2 {
		card.Subtypes = strings.Fields(typeLine[1])
	}
	if c.AttackingPlayer != nil {
		card.AttackingPlayerID = playerID(*c.AttackingPlayer)
	}
	if c.AttachedTo != 0 {
		card.AttachedTo = cardID(c.AttachedTo)
	}
	return mb.CardView{Value: mb.VisibleCard{Visibility: "visible", CardDto: card}}
}
func combatAssignments(v view.View) []mb.CombatAssignmentDto {
	out := make([]mb.CombatAssignmentDto, 0)
	for _, p := range v.Players {
		for _, attacker := range p.Battlefield {
			for _, blocker := range attacker.BlockedBy {
				out = append(out, mb.CombatAssignmentDto{BlockerID: cardID(blocker), AttackerID: cardID(attacker.ID)})
			}
		}
	}
	return out
}
func stackObject(s view.StackView) mb.StackObjectDto {
	name := s.Name
	if s.Card != nil {
		name = s.Card.Printing.Name
	}
	return mb.StackObjectDto{ID: stackID(s.ID), SourceID: cardID(s.Source), ControllerID: playerID(s.Controller), OwnerID: playerID(s.Controller), Identity: mb.CardIdentity{Name: name}, Text: s.Text, IsPermanentSpell: s.Kind == "spell", IsCasting: s.Kind == "spell", FaceIndex: 0, Targets: []mb.TargetRef{}}
}
