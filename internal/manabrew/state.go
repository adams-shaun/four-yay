package manabrew

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/deck"
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
		gv.Zones = append(gv.Zones, t.playerZones(v.Viewer, p)...)
	}
	gv.Zones = append(gv.Zones, t.battlefieldZones(v)...)
	for _, s := range v.Stack {
		gv.Stack = append(gv.Stack, stackObject(s))
	}
	// The x_gorge_own_deck_v1 extension (seat-deck-manifest spec, interface
	// mapping item 4) rides the seat's own state: v is a seat-redacted view,
	// so v.OwnDeck is non-nil exactly when the viewer is an in-range seat and
	// carries ONLY the viewer's manifest (view.ProjectFor fills it for
	// Visibility == Seat and nothing else). A nil manifest -- the synthetic
	// fixtures, an implementation with no deck data -- omits the optional
	// member rather than publishing an empty lie.
	if v.OwnDeck != nil {
		gv.OwnDeck = ownDeckExtension(v.OwnDeck)
	}
	return gv
}

// ownDeckExtension renders one seat's native own-deck manifest into the
// x_gorge_own_deck_v1 payload. The row list is the manifest's own ordered
// name/count rows, copied so neither the view's backing slice nor the
// engine's manifest can be retained or mutated through the DTO. main is
// always a list ([] when empty), an absent sideboard stays omitted, and
// commanders keep their declared order.
func ownDeckExtension(m *deck.Manifest) *mb.OwnDeckExtension {
	ext := &mb.OwnDeckExtension{Name: m.Name, Main: make([]mb.OwnDeckRow, 0, len(m.Main))}
	for _, row := range m.Main {
		ext.Main = append(ext.Main, mb.OwnDeckRow{Name: row.Name, Count: row.Count})
	}
	if len(m.Sideboard) > 0 {
		ext.Sideboard = make([]mb.OwnDeckRow, 0, len(m.Sideboard))
		for _, row := range m.Sideboard {
			ext.Sideboard = append(ext.Sideboard, mb.OwnDeckRow{Name: row.Name, Count: row.Count})
		}
	}
	if len(m.Commanders) > 0 {
		ext.Commanders = append([]string{}, m.Commanders...)
	}
	return ext
}

// stepKind maps an engine step name (view.View.Step, i.e. state.Step.String())
// onto the ManaBrew StepKind. The name is first parsed back to the engine's
// step enum with state.ParseStep -- the ONE home of the name<->Step mapping --
// so the switch below is over state.Step and a step the engine adds is a
// missing case, not a string that silently passes through: an unknown name
// yields the empty StepKind. TestStepMapTotal iterates every valid state.Step,
// so a new engine step fails that test rather than shipping unmapped.
func stepKind(s string) mb.StepKind {
	step, ok := state.ParseStep(s)
	if !ok {
		return mb.StepKind("")
	}
	switch step {
	case state.StepUntap:
		return mb.StepUntap
	case state.StepUpkeep:
		return mb.StepUpkeep
	case state.StepDraw:
		return mb.StepDraw
	case state.StepMain1:
		return mb.StepMain1
	case state.StepBeginCombat:
		return mb.StepCombatBegin
	case state.StepDeclareAttackers:
		return mb.StepCombatDeclareAttackers
	case state.StepDeclareBlockers:
		return mb.StepCombatDeclareBlockers
	case state.StepCombatDamage:
		return mb.StepCombatDamage
	case state.StepEndCombat:
		return mb.StepCombatEnd
	case state.StepMain2:
		return mb.StepMain2
	case state.StepEnd:
		return mb.StepEndOfTurn
	case state.StepCleanup:
		return mb.StepCleanup
	default:
		// A state.Step the table above does not name: leave it empty rather
		// than emit a bogus kind.
		return mb.StepKind("")
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

func (t *Translator) playerZones(viewer state.PlayerID, p view.PlayerView) []mb.ZoneDto {
	owner := playerID(p.ID)
	out := make([]mb.ZoneDto, 0, 6)
	if p.Hand == nil {
		out = append(out, mb.ZoneDto{Zone: mb.ZoneHand, OwnerID: owner, Cards: []mb.CardView{}, Count: p.HandSize})
	} else {
		out = append(out, t.zone(mb.ZoneHand, owner, p.Hand, len(p.Hand)))
	}
	library := make([]mb.CardView, 0, 1)
	if p.ID == viewer && p.LibraryTop != nil {
		library = append(library, t.visibleCard(*p.LibraryTop))
	}
	out = append(out, mb.ZoneDto{Zone: mb.ZoneLibrary, OwnerID: owner, Cards: library, Count: p.LibrarySize})
	out = append(out, t.zone(mb.ZoneGraveyard, owner, p.Graveyard, len(p.Graveyard)))
	exile := make([]mb.CardView, 0, len(p.Exile))
	for i, c := range p.Exile {
		if c.FaceDown {
			exile = append(exile, hiddenCard(mb.ZoneExile, p.ID, i))
		} else {
			exile = append(exile, t.visibleCard(c))
		}
	}
	out = append(out, mb.ZoneDto{Zone: mb.ZoneExile, OwnerID: owner, Cards: exile, Count: len(exile)})
	out = append(out, t.zone(mb.ZoneCommand, owner, p.Command, len(p.Command)))
	return out
}

func (t *Translator) battlefieldZones(v view.View) []mb.ZoneDto {
	out := make([]mb.ZoneDto, 0, len(v.Players))
	for _, p := range v.Players {
		cards := make([]mb.CardView, 0)
		for _, c := range p.Battlefield {
			if c.Controller == p.ID {
				cards = append(cards, t.visibleCard(c))
			}
		}
		out = append(out, mb.ZoneDto{Zone: mb.ZoneBattlefield, OwnerID: playerID(p.ID), Cards: cards, Count: len(cards)})
	}
	return out
}
func (t *Translator) zone(kind mb.ZoneKind, owner string, cards []view.CardView, n int) mb.ZoneDto {
	out := mb.ZoneDto{Zone: kind, OwnerID: owner, Cards: make([]mb.CardView, 0, len(cards)), Count: n}
	for _, c := range cards {
		if c.FaceDown {
			out.Cards = append(out.Cards, hiddenCard(kind, c.Owner, len(out.Cards)))
		} else {
			out.Cards = append(out.Cards, t.visibleCard(c))
		}
	}
	return out
}
func hiddenCard(zone mb.ZoneKind, owner state.PlayerID, i int) mb.CardView {
	return mb.CardView{Value: mb.HiddenCard{Visibility: "hidden", ID: hiddenCardID(string(zone), owner, i)}}
}

// visibleCard projects one seat-visible view.CardView. The published
// ManaBrew CardDto.text is filled from the operator-approved CardText seam
// (scoping spec §10.1 Q9): the lookup is keyed on the SAME name the redacted
// view already shows, so it can leak nothing, and a nil seam or a miss is an
// empty string (gap G-7's v1 default). A FaceDown card has no name in the
// view and is never looked up.
func (t *Translator) visibleCard(c view.CardView) mb.CardView {
	name := c.Printing.Name
	identity := mb.CardIdentity{Name: name, SetCode: c.Printing.Set, CardNumber: c.Printing.Number}
	text := ""
	if c.FaceDown {
		identity = mb.CardIdentity{}
	} else if t != nil && t.text != nil && name != "" {
		if s, ok := t.text.Text(name); ok {
			text = s
		}
	}
	identity.IsToken = c.Token != ""
	power, toughness := strconv.Itoa(int(c.Power)), strconv.Itoa(int(c.Toughness))
	card := mb.CardDto{ID: cardID(c.ID), Identity: identity, Color: []string{}, ManaCost: c.ManaCost, EffectiveManaCost: c.EffectiveManaCost, Types: []string{}, Subtypes: []string{}, Supertypes: []string{}, Power: &power, Toughness: &toughness,
		ClassLevels: []mb.ClassLevelDto{}, SagaChapters: []mb.SagaChapterDto{}, Text: text, Choices: []mb.CardChoiceDto{}, ControllerID: playerID(c.Controller), OwnerID: playerID(c.Owner), Tapped: c.Tapped,
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
	// OwnerID is the controller: view.StackView (view/view.go) carries no
	// owner, and for a spell whose controller differs from its owner the
	// stack's owner is unknowable from the redacted view. This is the v1
	// approximation -- the field is emitted as authoritative and would be
	// wrong only for a stack object cast/created for another player.
	return mb.StackObjectDto{ID: stackID(s.ID), SourceID: cardID(s.Source), ControllerID: playerID(s.Controller), OwnerID: playerID(s.Controller), Identity: mb.CardIdentity{Name: name}, Text: s.Text, IsPermanentSpell: s.Kind == "spell", IsCasting: s.Kind == "spell", FaceIndex: 0, Targets: []mb.TargetRef{}}
}
