package templates

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// roomDoorDecisions returns the both-locked Room lock/unlock answer pair the
// engine recorded at the resolve step: the unlock/lock ask (the Mode$
// LockOrUnlock election, effects/unlockdoor.go askUnlockDoorHalf) and the
// which-door ask (effects/unlockdoor.go's door chooser).
func roomDoorDecisions(t *testing.T, reg *cards.Registry, it oraclegen.Item) (rules.OracleDecision, rules.OracleDecision) {
	t.Helper()
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok {
		t.Fatalf("scenario does not play through")
	}
	var unlock, door *rules.OracleDecision
	for i := range res.Decisions {
		d := &res.Decisions[i]
		if d.Step != 1 || d.Seat != 0 || d.Kind != "choose_n" {
			continue
		}
		if len(d.Picks) == 1 && len(d.PickKinds) == 1 {
			switch d.PickKinds[0] {
			case "unlock":
				unlock = d
			case "card":
				door = d
			}
		}
	}
	if unlock == nil || door == nil {
		t.Fatalf("precondition: gorge posed no Room lock/unlock pair at step 1: %+v", res.Decisions)
	}
	if unlock.Resume != "choice" || unlock.Options != 2 || unlock.Picks[0] != "Unlock a door" {
		t.Fatalf("precondition: unlock ask shape = %+v", unlock)
	}
	if door.Resume != "choice" || door.Options != 2 || len(door.PickRefs) != 1 {
		t.Fatalf("precondition: door ask shape = %+v", door)
	}
	return *unlock, *door
}

// TestRoomLockAnswersCollapseToYesNo pins the XMage answer shape for the two
// Mode$ LockOrUnlock activate rows (agent-20261009T041321Z-84a3a3ce): gorge's
// unlock/lock ask plus which-door ask collapses into ONE chooseUse boolean,
// "yes" for the left (front-face, face 0) door, and the trigger's optional
// target stays an explicit skip. XMage's LockOrUnlockRoomTargetEffect poses
// only that boolean ("the left door?", true = Left); the old label answers
// ("Unlock a door", "Bottomless Pool") trip TestPlayer's choice-usage assert.
func TestRoomLockAnswersCollapseToYesNo(t *testing.T) {
	reg := loadGenRegistry(t)
	pool, ok := reg.Lookup("Bottomless Pool")
	if !ok {
		t.Fatalf("precondition: Bottomless Pool missing from the corpus")
	}
	if !levelb.IsRoomCard(pool) {
		t.Fatalf("precondition: Bottomless Pool is not a Room card: %v", pool.Faces)
	}
	front := pool.Faces[0].Name
	if front != "Bottomless Pool" {
		t.Fatalf("precondition: Bottomless Pool's front face is %q", front)
	}
	for _, name := range []string{"Keys to the House", "Marina Vendrell"} {
		t.Run(name, func(t *testing.T) {
			key := "activate#0.0"
			if name == "Keys to the House" {
				key = "activate#0.1"
			}
			it, req := activateRequirement(t, reg, name, key)
			// Precondition: the fixture Room is on p0's battlefield, the
			// requirement is the Mode$ LockOrUnlock ability, and gorge posed
			// the two-door pair picking the front face.
			seat := it.Scenario.Setup["p0"]
			seen := false
			for _, n := range seat.Battlefield {
				if n == front {
					seen = true
				}
			}
			if !seen {
				t.Fatalf("precondition: fixture Room %q absent from p0 battlefield: %v", front, seat.Battlefield)
			}
			c, _ := reg.Lookup(name)
			idx := 0
			if n, err := strconv.Atoi(req.Slot); err == nil {
				idx = n
			}
			ab := c.Faces[req.Face].Abilities[idx]
			if !strings.Contains(ab.ParamStr(cards.PKMode), "LockOrUnlock") {
				t.Fatalf("precondition: %s ability %d is not Mode$ LockOrUnlock: %s", name, idx, ab.ParamStr(cards.PKMode))
			}
			_, door := roomDoorDecisions(t, reg, it)
			if door.Picks[0] != front {
				t.Fatalf("precondition: gorge picked door %q, want the front face %q", door.Picks[0], front)
			}
			// The collapsed stream: one boolean choice, then the declined
			// trigger target skip. Exactly two answers, nothing else.
			got := it.XAnswers[1]
			if len(got) != 2 {
				t.Fatalf("step 1 answers = %+v, want [choice yes, target skip]", got)
			}
			if got[0].Seat != 0 || got[0].Kind != "choice" || got[0].Value != "yes" {
				t.Fatalf("step 1 first answer = %+v, want choice yes (left door)", got[0])
			}
			if got[1].Seat != 0 || got[1].Kind != "target" || got[1].Value != "[target_skip]" {
				t.Fatalf("step 1 second answer = %+v, want target skip", got[1])
			}
			for si, as := range it.XAnswers {
				for _, a := range as {
					if a.Kind == "choice" && (a.Value == "Unlock a door" || a.Value == front) {
						t.Fatalf("step %d still carries the unanswerable label answer %q: %+v", si, a.Value, as)
					}
				}
			}
		})
	}
}

// TestRoomSacCostPickUsesTheAliasForm pins the Intruding Soulrager fix: the
// Sac<1/Room> cost's observed pick names the fixture by its exact scenario
// ref in alias form ("@p0:Bottomless Pool"), because XMage names the Room
// object by its whole split name and the plain name can never match. A
// non-Room Sac row keeps the plain-name choice.
func TestRoomSacCostPickUsesTheAliasForm(t *testing.T) {
	reg := loadGenRegistry(t)
	t.Run("Intruding Soulrager", func(t *testing.T) {
		it, _ := activateRequirement(t, reg, "Intruding Soulrager", "activate#0.0")
		found := false
		for _, as := range it.XAnswers {
			for _, a := range as {
				if a.Kind != "choice" {
					continue
				}
				if a.Value == "@p0:Bottomless Pool" {
					found = true
					continue
				}
				if a.Value == "Bottomless Pool" || a.Value == "Bottomless Pool // Locker Room" {
					t.Fatalf("Sac pick answers the whole/half name %q, which XMage's exact name match rejects: %+v", a.Value, as)
				}
			}
		}
		if !found {
			t.Fatalf("no @p0:Bottomless Pool alias answer for the Sac<1/Room> cost: %+v", it.XAnswers)
		}
	})
	t.Run("Nita, Forum Conciliator stays a plain name", func(t *testing.T) {
		it, _ := activateRequirement(t, reg, "Nita, Forum Conciliator", "activate#0.0")
		found := false
		for _, as := range it.XAnswers {
			for _, a := range as {
				if a.Kind == "choice" && a.Value == "Llanowar Elves" {
					found = true
				}
				if a.Kind == "choice" && strings.HasPrefix(a.Value, "@") {
					t.Fatalf("non-Room Sac pick carries an alias answer %q: %+v", a.Value, as)
				}
			}
		}
		if !found {
			t.Fatalf("no plain-name Sac pick for Nita, Forum Conciliator: %+v", it.XAnswers)
		}
	})
}

// TestSacCostNamesRoomAgreesWithSacFilterFixtures pins the keying: a cost the
// Room-fixture table serves (Sac<1/Room>) is exactly a cost sacCostNamesRoom
// reports, so the fixture placement and the alias pick never disagree.
func TestSacCostNamesRoomAgreesWithSacFilterFixtures(t *testing.T) {
	for _, tok := range []string{"Sac<1/Room>", "Sac<1/Room.Other>"} {
		got, ok := sacFilterFixtures(tok, 0)
		if !ok || len(got) != 1 || got[0] != "Bottomless Pool" {
			t.Fatalf("sacFilterFixtures(%q) = (%v, %v), want [Bottomless Pool]", tok, got, ok)
		}
		if !sacCostNamesRoom(tok) {
			t.Fatalf("sacCostNamesRoom(%q) = false, want true", tok)
		}
	}
	for _, tok := range []string{"Sac<1/Creature.Other>", "Sac<1/Artifact>"} {
		if sacCostNamesRoom(tok) {
			t.Fatalf("sacCostNamesRoom(%q) = true, want false", tok)
		}
	}
}
