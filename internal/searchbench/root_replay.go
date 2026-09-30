package searchbench

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

var errRootReached = errors.New("searchbench: requested root reached")

// ReplayedRoot is a fully native, independently reconstructed search root.
// Engine and Decision are owned by the caller and may be searched or mutated.
type ReplayedRoot struct {
	Engine   *rules.Engine
	Decision *decision.Decision
}

// ReplayRoot rebuilds a recorded root from the public 17lands row and the
// explicit observed-opponent belief world. It refuses a root unless every
// recorded identity digest agrees, so a changed corpus or staging path cannot
// silently run a search from a different decision.
func ReplayRoot(reg *cards.Registry, names map[string]string, game ReplayGame, want RootRecord) (ReplayedRoot, error) {
	if reg == nil {
		return ReplayedRoot{}, fmt.Errorf("searchbench: nil corpus registry")
	}
	if game.ID != want.GameID {
		return ReplayedRoot{}, fmt.Errorf("searchbench: root game %q does not match source game %q", want.GameID, game.ID)
	}
	if want.Ordinal < 0 || want.GenesisSeed == 0 {
		return ReplayedRoot{}, fmt.Errorf("searchbench: invalid root locator for %q", game.ID)
	}
	base, err := ResolveDeck(reg, game.Deck)
	if err != nil {
		return ReplayedRoot{}, err
	}
	base, err = ObservedOpponentDeck(reg, base, game, names)
	if err != nil {
		return ReplayedRoot{}, err
	}
	got, observed, err := rebuildRoot(reg, names, game, base, want, want.GenesisSeed)
	if err != nil {
		return ReplayedRoot{}, err
	}
	gotRecord, err := NewRootRecord(game.ID, want.GenesisSeed, want.Ordinal, got.Engine, got.Decision, observed)
	if err != nil {
		return ReplayedRoot{}, err
	}
	if gotRecord.Seat != want.Seat || gotRecord.Turn != want.Turn || gotRecord.Sequence != want.Sequence || gotRecord.PrefixDigest != want.PrefixDigest || gotRecord.PublicStateDigest != want.PublicStateDigest || gotRecord.DecisionDigest != want.DecisionDigest {
		return ReplayedRoot{}, fmt.Errorf("searchbench: root identity diverged for %s/%d", game.ID, want.Ordinal)
	}
	if !reflect.DeepEqual(gotRecord.Recorded, want.Recorded) {
		return ReplayedRoot{}, fmt.Errorf("searchbench: root action diverged for %s/%d", game.ID, want.Ordinal)
	}
	return got, nil
}

// ReplayRootWorld rebuilds the same recorded public action prefix under a
// caller-selected hidden-world seed. The caller must compare its public root
// to the verified real root before using it as a fair determinization; this
// function intentionally does not require the event-chain digest to match,
// because a different shuffle is the point of a sampled world.
func ReplayRootWorld(reg *cards.Registry, names map[string]string, game ReplayGame, want RootRecord, worldSeed uint64) (ReplayedRoot, error) {
	if worldSeed == 0 {
		return ReplayedRoot{}, fmt.Errorf("searchbench: zero world seed")
	}
	if game.ID != want.GameID {
		return ReplayedRoot{}, fmt.Errorf("searchbench: root game %q does not match source game %q", want.GameID, game.ID)
	}
	base, err := ResolveDeck(reg, game.Deck)
	if err != nil {
		return ReplayedRoot{}, err
	}
	base, err = ObservedOpponentDeck(reg, base, game, names)
	if err != nil {
		return ReplayedRoot{}, err
	}
	got, _, err := rebuildRoot(reg, names, game, base, want, worldSeed)
	if err != nil {
		return ReplayedRoot{}, err
	}
	return got, nil
}

// ValidateRootWorld proves that a sampled hidden world exposes exactly the
// same root decision to the searching seat. PrefixDigest is deliberately not
// compared: it binds the original event history, including its chance stream.
func ValidateRootWorld(want RootRecord, got ReplayedRoot) error {
	if got.Engine == nil || got.Decision == nil {
		return fmt.Errorf("searchbench: nil sampled root")
	}
	record, err := NewRootRecord(want.GameID, want.GenesisSeed, want.Ordinal, got.Engine, got.Decision, decision.Intent{})
	if err != nil {
		return err
	}
	if record.Seat != want.Seat || record.Turn != want.Turn || record.Sequence != want.Sequence || record.PublicStateDigest != want.PublicStateDigest || record.DecisionDigest != want.DecisionDigest {
		return fmt.Errorf("searchbench: sampled world changes public root %s/%d", want.GameID, want.Ordinal)
	}
	return nil
}

func rebuildRoot(reg *cards.Registry, names map[string]string, game ReplayGame, opponent []*cards.Card, want RootRecord, worldSeed uint64) (ReplayedRoot, decision.Intent, error) {
	e, err := NewGenesis(reg, game, names, opponent, worldSeed)
	if err != nil {
		return ReplayedRoot{}, decision.Intent{}, err
	}
	resolved, err := ResolveReplayGame(game, names)
	if err != nil {
		return ReplayedRoot{}, decision.Intent{}, err
	}
	ordinal := 0
	var got ReplayedRoot
	var observed decision.Intent
	// Bridge policies replay the source prefix and must not vary across worlds:
	// only the engine's hidden chance stream is a world dimension.
	_, err = StageGameObserve(e, [2]*seat.Bot{seat.NewBot(want.GenesisSeed + 10), seat.NewBot(want.GenesisSeed + 11)}, resolved, func(root *rules.Engine, d *decision.Decision, recorded decision.Intent) error {
		if ordinal != want.Ordinal {
			ordinal++
			return nil
		}
		got = ReplayedRoot{Engine: root, Decision: d}
		observed = decision.CloneIntent(recorded)
		return errRootReached
	})
	if !errors.Is(err, errRootReached) {
		if err != nil {
			return ReplayedRoot{}, decision.Intent{}, fmt.Errorf("searchbench: replaying root %s/%d: %w", game.ID, want.Ordinal, err)
		}
		return ReplayedRoot{}, decision.Intent{}, fmt.Errorf("searchbench: replay did not reach root %s/%d", game.ID, want.Ordinal)
	}
	return got, observed, nil
}
