package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// walkerClause extracts the actual walker half of a corpus restriction body.
// The player half is deliberately excluded: it already blocks the same
// defender and would make a walker-specific assertion pass without this fix.
func walkerClause(t *testing.T, body string) string {
	t.Helper()
	for _, part := range strings.Split(body, "|") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "Target$ ") {
			continue
		}
		for _, clause := range strings.Split(strings.TrimPrefix(part, "Target$ "), ",") {
			clause = strings.TrimSpace(clause)
			if strings.HasPrefix(clause, "Planeswalker.") {
				return clause
			}
		}
	}
	t.Fatalf("precondition: corpus restriction has no planeswalker Target$: %q", body)
	return ""
}

func TestCantAttackWalkerControlledByCardOwner(t *testing.T) {
	e := layerEngine(t)
	x := corpusCard(t, "Xantcha, Sleeper Agent")
	if len(x.Faces[0].Statics) == 0 {
		t.Fatal("precondition: Xantcha has no statics")
	}
	target := x.Faces[0].Statics[1].Params["Target"]
	clause := walkerClause(t, "Target$ "+target)
	if clause != "Planeswalker.ControlledBy Player.CardOwner" {
		t.Fatalf("precondition: corpus walker clause = %q", clause)
	}
	// activeStatics scans an on-board card's OWN face statics, and Xantcha's
	// real CantAttack line carries BOTH target halves. At the corpus level the
	// player half is now enforced (see TestCantAttackPlayerCardOwner), so
	// leaving it live would make the walker half unobservable here. Place a
	// copy of Xantcha whose printed statics are stripped, so the manual
	// walker-only restriction below is the sole CantAttack source and every
	// assertion tests the walker half alone. The clause itself is still the
	// corpus's exact text (asserted above).
	isolated := *x
	sharedFaces := append([]*cards.Face(nil), x.Faces...)
	stripped := *x.Faces[0]
	stripped.Statics = nil
	sharedFaces[0] = &stripped
	isolated.Faces = sharedFaces
	id := onBoardCard(t, e, 0, &isolated)
	e.emit(events.Event{Kind: events.ControlChange, Obj: id, Player: 1})
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Owner != 0 || o.Controller != 1 {
		t.Fatalf("precondition: Xantcha must be owned by 0 and controlled by 1: %+v", o)
	}
	// Use the corpus's exact walker clause, without its redundant Player.CardOwner
	// half, to make the walker half independently observable through attackBlocked.
	e.AddContinuous(ContinuousEffect{Source: id, Controller: 1, Restriction: "CantAttack",
		RestrictParams: map[string]string{"ValidCard": "Card.Self", "Target": clause}})
	if e.attackBlocked(id, 0, 0) {
		t.Fatal("no planeswalker: owner incorrectly blocked")
	}
	walker := onBoard(t, e, 0, targetWalkerFixture)
	if o := e.G.Obj(walker); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 || !faceHasType(o, "Planeswalker") {
		t.Fatalf("precondition: owner's walker not on battlefield: %+v", o)
	}
	if !e.attackBlocked(id, 0, walker) {
		t.Fatal("Xantcha could attack the planeswalker controlled by its owner")
	}
	if e.attackBlocked(id, 1, 0) {
		t.Fatal("owner clause also blocked Xantcha's controller")
	}
}

func TestCantAttackWalkerRememberedSelectors(t *testing.T) {
	for _, tc := range []struct{ card, face, svar, want string }{
		{"Chaos Dragon", "", "STCantAttack", "Planeswalker.RememberedPlayerCtrl"},
		{"Unstable Glyphbridge", "Sandswirl Wanderglyph", "STCantAttack", "Planeswalker.ControlledBy Remembered"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			e := layerEngine(t)
			c := corpusCard(t, tc.card)
			var f *cards.Face
			for i := range c.Faces {
				if tc.face == "" || c.Faces[i].Name == tc.face {
					f = c.Faces[i]
					break
				}
			}
			if f == nil {
				t.Fatalf("precondition: missing face %q", tc.face)
			}
			body := f.SVars[tc.svar]
			clause := walkerClause(t, body)
			if clause != tc.want {
				t.Fatalf("precondition: corpus walker clause = %q, want %q", clause, tc.want)
			}
			source := onBoardCard(t, e, 0, c)
			attacker := onBoard(t, e, 0, "Name:Attacker Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
			if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: restriction source absent: %+v", o)
			}
			if o := e.G.Obj(attacker); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
				t.Fatalf("precondition: attacker absent: %+v", o)
			}
			e.AddContinuous(ContinuousEffect{Source: source, Controller: 0, Restriction: "CantAttack",
				RestrictParams: map[string]string{"ValidCard": "Creature.YouCtrl", "Target": clause}, RememberedPlayers: []state.PlayerID{1}})
			if e.attackBlocked(attacker, 1, 0) {
				t.Fatal("no walker: remembered defender incorrectly blocked")
			}
			walker := onBoard(t, e, 1, targetWalkerFixture)
			if o := e.G.Obj(walker); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 || !faceHasType(o, "Planeswalker") {
				t.Fatalf("precondition: remembered player's walker absent: %+v", o)
			}
			if !e.attackBlocked(attacker, 1, walker) {
				t.Fatal("attacker could attack remembered player's planeswalker")
			}
			if e.attackBlocked(attacker, 0, 0) {
				t.Fatal("remembered clause blocked a different defender")
			}
		})
	}
}
