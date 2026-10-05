package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// copyOnStack returns the stack object that is a CR 707.10 copy, or 0.
func copyOnStack(e *Engine) state.ObjID {
	for _, id := range e.G.Stack {
		if o := e.G.Obj(id); o != nil && o.IsCopy {
			return id
		}
	}
	return 0
}

// countTargetsChosenOn counts the TargetsChosen events recorded against obj:
// the copy-target election's answer emits exactly one, which is how the test
// proves the election is one-shot (a re-ask would record a second).
func countTargetsChosenOn(e *Engine, obj state.ObjID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TargetsChosen && ev.Obj == obj {
			n++
		}
	}
	return n
}

// TestChainLightningMayChooseTargetUsesTheRealCorpus pins CR 707.10c on the
// real Chain Lightning card (ur-delver, 4 slots). Its paid copy must offer
// its controller a new-target choice -- the inherited target first (the
// keep-current default), plus the battlefield creature -- and, once answered,
// the copy must resolve its damage and leave the stack, with the whole log
// replaying byte-for-byte. This is the end-to-end peer of the corpus card,
// not a synthetic fixture.
func TestChainLightningMayChooseTargetUsesTheRealCorpus(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	sf := searchCorpusCard(t, reg, "Chain Lightning")
	// PRECONDITION: this is the real corpus card and it really carries the
	// pay-to-copy clause the test leans on. A synthetic look-alike would
	// silently pass the rest of the assertions.
	if len(sf.Faces) == 0 || sf.Faces[0].Name != "Chain Lightning" {
		t.Fatalf("corpus card is not Chain Lightning: %+v", sf.Faces)
	}
	mayChoose := false
	for _, face := range sf.Faces {
		for _, ab := range face.Abilities {
			for _, sub := range spellAbilityChain(ab) {
				if sub.API == "CopySpellAbility" && sub.Params["MayChooseTarget"] == "True" {
					mayChoose = true
				}
			}
		}
	}
	if !mayChoose {
		t.Fatal("Chain Lightning corpus card has no MayChooseTarget$ CopySpellAbility")
	}

	eng, cfg := miscHandsEngine(t, reg,
		[]string{"Chain Lightning"}, nil, nil, []string{"Grizzly Bears"})
	addMana(t, eng, 0, "R")
	addMana(t, eng, 1, "RR")
	bear := miscBoardObj(t, eng, 1, "Grizzly Bears")
	life := eng.G.Players[1].Life
	boltObj := miscHandObj(t, eng, 0, "Chain Lightning")
	submitChoices(t, eng, miscCastOption(t, eng, boltObj))
	d := eng.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Chain Lightning target ask, got %+v", d)
	}
	player := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			player = o.Index
		}
	}
	if player < 0 {
		t.Fatal("seat 1 was not offered as Chain Lightning's target")
	}
	submitChoices(t, eng, player)

	// The target's controller is asked the unless-pay mid-resolution.
	d = passUntilNonPriority(t, eng, 30)
	if d == nil || d.Kind != decision.KModes || d.Player != 1 {
		t.Fatalf("expected Chain Lightning's unless-pay ask, got %+v", d)
	}
	submitChoices(t, eng, d.Options[0].Index)

	// CR 707.10c: the paid copy asks its controller for a new target.
	d = passUntilNonPriority(t, eng, 30)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected MayChooseTarget$ ask for the copy, got %+v", d)
	}
	if d.ResumeKind != "copy_targets" {
		t.Fatalf("copy ask transport = kind %q, want copy_targets", d.ResumeKind)
	}
	// PRECONDITION: the ask names a real copy on the stack that still holds
	// the election; without this the option assertions below prove nothing.
	copyID := d.Source
	co := eng.G.Obj(copyID)
	if co == nil || !co.IsCopy || co.Zone != state.ZStack {
		t.Fatalf("copy ask source %d is not a stack copy: %+v", copyID, co)
	}
	if !co.CopyMayChooseTarget {
		t.Fatalf("copy %d does not hold the MayChooseTarget$ election", copyID)
	}
	if len(d.Options) < 2 || d.Options[0].Kind != "player" || d.Options[0].Player != 1 {
		t.Fatalf("copy target ask does not keep the current target first: %+v", d.Options)
	}
	bearOpt := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			bearOpt = o.Index
		}
	}
	if bearOpt < 0 {
		t.Fatalf("copy target ask did not offer the real battlefield Grizzly Bears: %+v", d.Options)
	}
	submitChoices(t, eng, bearOpt)

	// The answer is recorded on the copy and consumes the one-shot election.
	if got := eng.G.Obj(copyID).Targets; len(got) != 1 || got[0].Obj != bear {
		t.Fatalf("copy targets after answering = %+v, want the Grizzly Bears", got)
	}
	if eng.G.Obj(copyID).CopyMayChooseTarget {
		t.Fatal("election was not consumed by the answered target")
	}

	// Drive the copy through its resolution: it deals 3 to the 2/2 bear, so
	// the bear dies and the copy leaves the stack; the original still hits
	// seat 1 for 3. A livelock (the re-ask defect) would fail here.
	drainToEnd(t, eng, 40)
	if z := eng.G.Obj(copyID).Zone; z != state.ZCeased {
		t.Fatalf("resolved copy sits in %s, want ceased (CR 707.10a)", z)
	}
	if z := eng.G.Obj(bear).Zone; z != state.ZGraveyard {
		t.Fatalf("Grizzly Bears (3 damage on a 2/2) sits in %s, want Graveyard", z)
	}
	if got := eng.G.Players[1].Life; got != life-3 {
		t.Fatalf("seat 1 life = %d, want %d (only the original hits the player)", got, life-3)
	}
	if n := countTargetsChosenOn(eng, copyID); n != 1 {
		t.Fatalf("copy recorded %d TargetsChosen events, want exactly 1 (one-shot election)", n)
	}
	if eng.Pending() == nil || eng.Pending().Kind != decision.KPriority {
		t.Fatalf("resolution did not settle back to priority: %+v", eng.Pending())
	}
	replayCheck(t, eng, cfg)
}

// TestChainLightningCopyKeepsCurrentTargetIsDeterministic drives the same real
// corpus card but answers the copy-target election with the keep-current
// option (the one drainToEnd takes). The copy must still resolve and the whole
// log must replay: the default is a legal answer, not a dead end.
func TestChainLightningCopyKeepsCurrentTargetIsDeterministic(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	eng, cfg := miscHandsEngine(t, reg,
		[]string{"Chain Lightning"}, nil, nil, []string{"Grizzly Bears"})
	addMana(t, eng, 0, "R")
	addMana(t, eng, 1, "RR")
	life := eng.G.Players[1].Life
	boltObj := miscHandObj(t, eng, 0, "Chain Lightning")
	submitChoices(t, eng, miscCastOption(t, eng, boltObj))
	d := eng.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Chain Lightning target ask, got %+v", d)
	}
	player := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			player = o.Index
		}
	}
	if player < 0 {
		t.Fatal("seat 1 was not offered as Chain Lightning's target")
	}
	submitChoices(t, eng, player)
	d = passUntilNonPriority(t, eng, 30)
	if d == nil || d.Kind != decision.KModes || d.Player != 1 {
		t.Fatalf("expected Chain Lightning's unless-pay ask, got %+v", d)
	}
	submitChoices(t, eng, d.Options[0].Index)
	d = passUntilNonPriority(t, eng, 30)
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "copy_targets" {
		t.Fatalf("expected the copy-target ask, got %+v", d)
	}
	copyID := d.Source
	if !eng.G.Obj(copyID).CopyMayChooseTarget {
		t.Fatalf("copy %d does not hold the election", copyID)
	}
	submitChoices(t, eng, d.Options[0].Index) // keep the current player target
	if got := eng.G.Obj(copyID).Targets; len(got) != 1 || !got[0].IsPlayer || got[0].Player != 1 {
		t.Fatalf("kept copy target = %+v, want the player target kept", got)
	}
	drainToEnd(t, eng, 40)
	if z := eng.G.Obj(copyID).Zone; z != state.ZCeased {
		t.Fatalf("resolved copy sits in %s, want ceased (CR 707.10a)", z)
	}
	if got := eng.G.Players[1].Life; got != life-3-3 {
		t.Fatalf("seat 1 life = %d, want %d (original + copy both keep the player)", got, life-6)
	}
	replayCheck(t, eng, cfg)
}

// spellAbilityChain flattens an SA and its SubAbility chain for the corpus
// precondition scan.
func spellAbilityChain(sa *cards.SA) []*cards.SA {
	var out []*cards.SA
	for s := sa; s != nil; s = s.Sub {
		out = append(out, s)
	}
	return out
}
