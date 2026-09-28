package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Known Backup exceptions are keyed by card name and exact missing primitive;
// each entry must explain why the granted ability is not representable.
var backupExceptions = map[string]map[string]string{}

func TestBackupCorpusRegistration(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	var carriers int
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if !slices.ContainsFunc(f.Keywords, func(k string) bool { return cards.KeywordHead(k) == "Backup" }) {
				continue
			}
			carriers++
			missing := reg.Unsupported(c, supported)
			allowed := backupExceptions[f.Name]
			for _, primitive := range missing {
				if reason, ok := allowed[primitive]; ok && reason != "" {
					continue
				}
				t.Errorf("%s Backup carrier missing %s (no documented exception): all missing=%v", f.Name, primitive, missing)
			}
			for primitive, reason := range allowed {
				if reason == "" || !slices.Contains(missing, primitive) {
					t.Errorf("stale/invalid Backup exception %s: %s (%q), missing=%v", f.Name, primitive, reason, missing)
				}
			}
		}
	}
	if carriers != 26 {
		t.Fatalf("corpus K:Backup carrier count = %d, want 26", carriers)
	}
}

func backupSaibaConfig(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID, state.PlayerID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	saiba := mustCorpusCard(t, reg, "Saiba Cryptomancer")
	memnite := mustCorpusCard(t, reg, "Memnite")
	if len(saiba.Faces) == 0 || !saiba.Faces[0].HasKeyword("Backup") || !saiba.Faces[0].HasKeyword("Hexproof") {
		t.Fatal("precondition failed: corpus Saiba Cryptomancer must print Backup and Hexproof")
	}
	if len(memnite.Faces) == 0 || !memnite.Faces[0].IsCreature() {
		t.Fatal("precondition failed: Memnite must be a creature")
	}
	deck := func() []*cards.Card { return append([]*cards.Card{saiba, memnite}, mountainDeck(t, 38)...) }
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{deck(), deck()}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	caster := e.G.Active
	sid := findByName(e, "Saiba Cryptomancer", caster)
	mid := findByName(e, "Memnite", caster)
	if sid == 0 || mid == 0 {
		t.Fatalf("precondition failed: Saiba/Memnite not found (Saiba=%d, Memnite=%d)", sid, mid)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: sid, From: e.G.Obj(sid).Zone, To: state.ZHand})
	e.emit(events.Event{Kind: events.MoveZone, Obj: mid, From: e.G.Obj(mid).Zone, To: state.ZBattlefield})
	e.pending = nil
	e.priorityRound()
	return e, cfg, sid, mid, caster
}

func resolveSaibaBackup(t *testing.T, e *Engine, sid, target state.ObjID, caster state.PlayerID) {
	t.Helper()
	addMana(t, e, caster, "1U")
	submitChoices(t, e, castOptionFor(t, e, sid).Index)
	backupAnswerTarget(t, e, target, 30)
	passUntilStackEmpty(t, e, 30)
	if e.G.Obj(sid).Zone != state.ZBattlefield || e.G.Obj(target).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: Backup source/target zones are %s/%s", e.G.Obj(sid).Zone, e.G.Obj(target).Zone)
	}
}

func TestSaibaCryptomancerBackup(t *testing.T) {
	t.Run("self", func(t *testing.T) {
		e, cfg, sid, _, caster := backupSaibaConfig(t, 511)
		resolveSaibaBackup(t, e, sid, sid, caster)
		if got := e.G.Obj(sid).Counter("P1P1"); got != 1 {
			t.Fatalf("self-target Backup placed %d counters, want 1", got)
		}
		if !e.HasKeyword(sid, "Hexproof") {
			t.Fatal("precondition/result failed: Saiba's printed Hexproof must remain present")
		}
		e.EndOfTurnCleanup()
		if !e.HasKeyword(sid, "Hexproof") {
			t.Fatal("self-target Backup removed Saiba's printed Hexproof at cleanup")
		}
		replayCheck(t, e, cfg)
	})

	t.Run("other creature and cleanup", func(t *testing.T) {
		e, cfg, sid, mid, caster := backupSaibaConfig(t, 512)
		resolveSaibaBackup(t, e, sid, mid, caster)
		if got := e.G.Obj(mid).Counter("P1P1"); got != 1 {
			t.Fatalf("Backup placed %d counters on target, want 1", got)
		}
		if !e.HasKeyword(mid, "Hexproof") {
			t.Fatal("Backup did not grant Hexproof to the other creature")
		}
		e.EndOfTurnCleanup()
		if e.HasKeyword(mid, "Hexproof") {
			t.Fatal("granted Hexproof remained after end-of-turn cleanup")
		}
		if got := e.G.Obj(mid).Counter("P1P1"); got != 1 {
			t.Fatalf("cleanup changed the Backup counter count to %d, want 1", got)
		}
		replayCheck(t, e, cfg)
	})
}
