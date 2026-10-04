package rules

import "testing"

// TestEffectReplacementResumeEndsSelfExile is the path-1 pin: the
// registration exists before the suspended ask is answered and is gone after
// the resumed self-exile runs, and the body's other work (the +1/+1 counter)
// proves the resume ran the body rather than short-circuiting.
func TestEffectReplacementResumeEndsSelfExile(t *testing.T) {
	t.Parallel()
	e, cfg, find := etbConfig(t, seedTossSeat0(307), []string{effectSelfExileResumeSrc, wgBearSrc}, nil)

	// Cast the Effect sorcery: it registers a live Moved replacement.
	sfx := find("Fixture Self-Exile Effect", 0)
	addMana(t, e, 0, "UU")
	castObj(t, e, sfx)
	if got := len(activeMovedReplacements(e)); got != 1 {
		t.Fatalf("precondition: %d live Moved Effect replacements, want 1 (the Effect did not register)", got)
	}
	if hasNote(e, "continuous replacement unimplemented") {
		t.Fatal("precondition: the ETBCreat replacement was not registered (unimplemented Note in the log)")
	}

	// Cast the creature; its entry fires the replacement, whose body asks.
	bear := find("Fixture Bear", 0)
	addMana(t, e, 0, "GG")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bear {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for the bear: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	ask := seekUnlessPay(t, e, 60)
	if ask == nil {
		t.Fatal("the replacement body's unless ask was never posed (the body chain did not run)")
	}
	// The registration must still be live before the answer lands: the
	// suspension happened mid-body, not after the self-exile.
	if got := len(activeMovedReplacements(e)); got != 1 {
		t.Fatalf("registration count before the answer = %d, want 1 (the body suspended before self-exiling)", got)
	}

	// Decline: the resumed body runs the self-exile, which ends the effect.
	// The pool is empty after the bear's GG and this fixture gives the payer
	// no mana source the CR 601.2g payment window could tap, so the
	// reachability gate offers the decline alone (see the control test below,
	// where a spare G makes the pay option reachable).
	decline := ask.Options[len(ask.Options)-1]
	if decline.Label != "Don't pay" {
		t.Fatalf("the unless ask's trailing option is not the decline: %+v", ask.Options)
	}
	submitChoices(t, e, decline.Index)
	passUntilStackEmpty(t, e, 60)

	if o := e.G.Obj(bear); o == nil || o.Counter("P1P1") != 1 {
		t.Fatalf("the replacement body did not run: bear = %+v", o)
	}
	if got := len(activeMovedReplacements(e)); got != 0 {
		t.Fatalf("the resumed self-exile did not end the registration: %d live replacement(s) left", got)
	}
	replayCheck(t, e, cfg)
}

// TestEffectReplacementResumePaymentKeepsRegistration is the control: when
// the payer PAYS the unless cost on the unswitched shape the ChangeZone body
// is skipped, so the self-exile never runs and the registration must
// survive. It proves the previous test's disappearance is caused by the
// resumed self-exile, not by some incidental cleanup.
func TestEffectReplacementResumePaymentKeepsRegistration(t *testing.T) {
	t.Parallel()
	e, cfg, find := etbConfig(t, seedTossSeat0(311), []string{effectSelfExileResumeSrc, wgBearSrc}, nil)

	sfx := find("Fixture Self-Exile Effect", 0)
	addMana(t, e, 0, "UU")
	castObj(t, e, sfx)
	if got := len(activeMovedReplacements(e)); got != 1 {
		t.Fatalf("precondition: %d live Moved Effect replacements, want 1", got)
	}

	bear := find("Fixture Bear", 0)
	addMana(t, e, 0, "GGG") // GG for the cast, G spare for the unless cost
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bear {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for the bear: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	ask := seekUnlessPay(t, e, 60)
	if ask == nil {
		t.Fatal("the replacement body's unless ask was never posed")
	}
	// Pay: the body is skipped on the unswitched orientation.
	submitChoices(t, e, ask.Options[0].Index)
	passUntilStackEmpty(t, e, 60)

	if got := len(activeMovedReplacements(e)); got != 1 {
		t.Fatalf("paid unless cost still ended the registration: %d live replacement(s)", got)
	}
	replayCheck(t, e, cfg)
}
