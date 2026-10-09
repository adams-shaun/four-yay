package oraclegen

import (
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

func roomLockPair(unlockLabel, doorLabel, ref string) []rules.OracleDecision {
	return []rules.OracleDecision{
		{Step: 1, Seat: 0, Kind: "choose_n", Resume: "choice", Options: 2,
			Picks: []string{unlockLabel}, PickKinds: []string{"unlock"}, PickRefs: []string{unlockLabel},
			PickIdx: []int{0}, Min: 1, Max: 1},
		{Step: 1, Seat: 0, Kind: "choose_n", Resume: "choice", Options: 2,
			Picks: []string{doorLabel}, PickKinds: []string{"card"}, PickRefs: []string{ref},
			PickIdx: []int{0}, Min: 1, Max: 1},
	}
}

func answersOf(ds []rules.OracleDecision, steps int) [][]XAnswer {
	return xanswers(ds, steps, nil, nil)
}

// TestCollapseRoomLockAnswers pins the collapse and its guards: an
// unlock/lock ask followed by a two-option front-face door pick becomes ONE
// chooseUse boolean ("yes" = left = face 0), the lock half and every shape
// the both-locked pair does not have is left untouched.
func TestCollapseRoomLockAnswers(t *testing.T) {
	t.Run("front face collapses to yes", func(t *testing.T) {
		ds := roomLockPair("Unlock a door", "Bottomless Pool", "p0:Bottomless Pool")
		collapseRoomLock(ds)
		as := answersOf(ds, 2)
		if len(as[1]) != 1 || as[1][0].Seat != 0 || as[1][0].Kind != "choice" || as[1][0].Value != "yes" {
			t.Fatalf("answers = %+v, want one choice yes", as[1])
		}
	})
	t.Run("back face collapses to no", func(t *testing.T) {
		ds := roomLockPair("Unlock a door", "Locker Room", "p0:Bottomless Pool")
		collapseRoomLock(ds)
		as := answersOf(ds, 2)
		if len(as[1]) != 1 || as[1][0].Value != "no" {
			t.Fatalf("answers = %+v, want one choice no", as[1])
		}
	})
	t.Run("lock half is untouched", func(t *testing.T) {
		ds := roomLockPair("Lock a door", "Bottomless Pool", "p0:Bottomless Pool")
		ds[0].PickKinds = []string{"lock"}
		collapseRoomLock(ds)
		if ds[0].Kind != "choose_n" || ds[1].Kind != "choose_n" {
			t.Fatalf("lock-half pair was rewritten: %+v", ds)
		}
	})
	t.Run("a whole-name ref is untouched", func(t *testing.T) {
		ds := roomLockPair("Unlock a door", "Dazzling Theater", "p0:Dazzling Theater // Prop Room")
		collapseRoomLock(ds)
		if ds[0].Kind != "choose_n" || ds[1].Kind != "choose_n" {
			t.Fatalf("whole-name pair was rewritten: %+v", ds)
		}
	})
	t.Run("a one-candidate door ask is untouched", func(t *testing.T) {
		ds := roomLockPair("Unlock a door", "Bottomless Pool", "p0:Bottomless Pool")
		ds[1].Options = 1
		collapseRoomLock(ds)
		if ds[0].Kind != "choose_n" {
			t.Fatalf("single-candidate pair was rewritten: %+v", ds)
		}
	})
	t.Run("a token ref is untouched", func(t *testing.T) {
		ds := roomLockPair("Unlock a door", "Bottomless Pool", "p0:token:Bottomless Pool")
		collapseRoomLock(ds)
		if ds[0].Kind != "choose_n" {
			t.Fatalf("token pair was rewritten: %+v", ds)
		}
	})
	t.Run("an unrelated follow-up is untouched", func(t *testing.T) {
		ds := roomLockPair("Unlock a door", "Bottomless Pool", "p0:Bottomless Pool")
		ds[1].PickKinds = []string{"permanent"}
		collapseRoomLock(ds)
		if ds[0].Kind != "choose_n" {
			t.Fatalf("non-card follow-up pair was rewritten: %+v", ds)
		}
	})
}
