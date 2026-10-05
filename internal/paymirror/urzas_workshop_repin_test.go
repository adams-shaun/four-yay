package paymirror

import (
	"context"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

// urzasWorkshopRepinList is the hand-built 60-card list this test drives: four
// complete Urza's assemblies (so Workshop's metalcraft count is large and
// stable), free artifacts that satisfy metalcraft, and paid artifacts
// ({1} Pithing Needle, {2} Cranial Plating) to force the
// planner to tap a mana source. It is authored here rather than drawn from a
// repo deck (only ulalek-eldrazi carries a single Workshop, and a seed search
// over it is fragile): the deck and the driver seed fully determine the game.
func urzasWorkshopRepinList() []string {
	list := []string{}
	for i := 0; i < 4; i++ {
		list = append(list, "Urza's Workshop", "Urza's Mine", "Urza's Tower", "Urza's Power Plant")
	}
	for i := 0; i < 4; i++ {
		list = append(list, "Ornithopter", "Memnite", "Welding Jar")
	}
	list = append(list, "Pithing Needle", "Cranial Plating", "Pithing Needle", "Cranial Plating")
	for len(list) < 60 {
		list = append(list, "Urza's Workshop")
	}
	return list
}

// watchedCast is one planned cast the driver observed, with the source names
// resolved from the live engine at check time (a Report keeps only ObjIDs).
type watchedCast struct {
	report  *Report
	sources []string // name of every activation source, in plan order
	metalc  int      // activations on an Urza's Workshop metalcraft ability
}

// driveUrzasWorkshopGame plays the authored list to completion exactly as
// PlayConfig does, but keeps the live engine so each report's activation
// sources can be named, and returns every planned cast it mirrored.
func driveUrzasWorkshopGame(t *testing.T, d *Decks, spec GameSpec) []watchedCast {
	t.Helper()
	cfg, err := d.config(spec)
	if err != nil {
		t.Fatal(err)
	}
	e := rules.NewStartingPlayerChoice(cfg)
	bots := make([]*seat.Bot, len(cfg.Names))
	for i := range bots {
		bots[i] = botFor(spec, i)
	}
	ctx := context.Background()
	answer := func(e *rules.Engine, dec *decision.Decision) (decision.Intent, error) {
		return bots[dec.Player].DecideBoard(ctx, botpolicy.BoardFromGame(e.G, e, dec.Player), *dec)
	}
	e.AskStartingPlayer()
	e.Advance()
	var out []watchedCast
	for n := 0; !e.G.Over && e.Pending() != nil && n < 20000 && e.G.Turn <= 60; {
		dec := e.Pending()
		e.EnsurePaymentActions()
		in, err := answer(e, dec)
		if err != nil {
			t.Fatalf("bot: %v", err)
		}
		if in.Payment == nil {
			if err := e.Submit(in); err != nil {
				t.Fatalf("intent rejected: %v", err)
			}
			n++
			continue
		}
		rep := CheckLive(e, in, answer, Options{Control: true, Resolve: true})
		wc := watchedCast{report: rep}
		for _, act := range rep.Plan.Activations {
			name := ""
			if o := e.G.Obj(act.Source); o != nil && o.Face() != nil {
				name = o.Face().Name
			}
			wc.sources = append(wc.sources, name)
			if name == "Urza's Workshop" && act.Ability.Kind == decision.PaymentAbilityPrinted && act.Ability.Index == 1 {
				wc.metalc++
			}
		}
		out = append(out, wc)
		n += 1 + len(rep.FollowUps)
	}
	return out
}

// TestUrzasWorkshopMetalcraftMirrorsRePin restores the round-7 paymirror pin
// that commit e641cc0bd (ConditionZone$) moved out of seed 4038: a named cast
// paid through Urza's Workshop's metalcraft ability ("{T}: Add {C} for each
// Urza's land you control; activate only if you control three or more
// artifacts"). The wheel labels that non-literal Amount$ as the bare "Add C",
// indistinguishable from the card's plain "{T}: Add {C}", so the floating
// route cannot match it by label and proves it on a throwaway clone
// (verifiedProductions, paymirror.go). The hand-built list below guarantees
// the path: four Urza's assemblies and free artifacts turn metalcraft on, and
// the paid artifacts force the planner to tap a land, so at least one planned
// cast must activate Workshop's Index-1 ability for more than one mana and
// must mirror the planner's witness through the executor.
func TestUrzasWorkshopMetalcraftMirrorsRePin(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	d, err := LoadDecks(reg)
	if err != nil {
		t.Fatal(err)
	}
	list := urzasWorkshopRepinList()
	spec := GameSpec{Seed: 7, Policy: "bot",
		Decks: []string{"urza-workshop-a", "urza-workshop-b"},
		Lists: [][]string{list, list}}
	cases := driveUrzasWorkshopGame(t, d, spec)
	if len(cases) == 0 {
		t.Fatal("no planned cast was mirrored; the game never reached a payment (the assertion would be vacuous)")
	}

	var pinned *watchedCast
	for i := range cases {
		if cases[i].metalc == 0 {
			continue
		}
		if pinned != nil {
			t.Fatalf("two casts used Workshop metalcraft (%q and %q); pin one path", pinned.report.Card, cases[i].report.Card)
		}
		pinned = &cases[i]
	}
	if pinned == nil {
		var got []string
		for _, c := range cases {
			got = append(got, c.report.Card)
		}
		t.Fatalf("no planned cast was paid through Urza's Workshop's metalcraft ability; planned casts: %v", got)
	}

	// Precondition: the named cast is a real paid artifact, and its witness
	// names the Workshop's metalcraft ability for more than the one mana the
	// printed "{T}: Add {C}" makes. Without this the equivalence below could
	// be vacuous.
	rep := pinned.report
	if rep.Card != "Cranial Plating" {
		t.Fatalf("pinned cast = %q, want Cranial Plating (the paid artifact the list forces)", rep.Card)
	}
	if len(pinned.sources) != 1 || pinned.sources[0] != "Urza's Workshop" {
		t.Fatalf("pinned cast sources = %v, want exactly the Workshop", pinned.sources)
	}
	if got := rep.Plan.Activations[0].Produces[5]; got < 2 {
		t.Fatalf("Workshop metalcraft witness produces %d colourless, want > 1 (the plain ability's amount)", got)
	}

	if st, key := rep.Verdict(); st != Equivalent {
		t.Fatalf("cast %q seq %d: verdict %s %q, want equivalent (planner and executor disagree on the Workshop payment)", rep.Card, rep.Seq, st, key)
	}
	if rep.Control == nil || rep.Control.Status != Equivalent {
		t.Fatalf("cast %q seq %d: control %+v, want an equivalent clone replay", rep.Card, rep.Seq, rep.Control)
	}
	t.Logf("pinned cast %q seq %d: Workshop metalcraft produced %d, verdict equivalent",
		rep.Card, rep.Seq, rep.Plan.Activations[0].Produces[5])
}
