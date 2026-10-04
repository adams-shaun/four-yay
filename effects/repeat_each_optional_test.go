package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

const testRepeatEachMessage = "Do you want to create X 1/1 red Elemental creature tokens with haste?"

// repeatEachOptionalSA builds the RepeatOptionalForEachPlayer$ unit under
// test, with the body pointed at a spy SVar so the effects-level tests can
// observe whether a subject's body ran.
func repeatEachOptionalSA() *cards.SA {
	return &cards.SA{API: "RepeatEach", Params: map[string]string{
		"RepeatSubAbility":            "Body",
		"RepeatPlayers":               "Player.Opponent",
		"RepeatOptionalForEachPlayer": "True",
		"RepeatOptionalMessage":       testRepeatEachMessage,
	}}
}

// repeatEachOptionalSPrecondition ties the unit to the real corpus carrier:
// Tempt with Vengeance must still carry the parameter pair this feature
// reads, so the shape under test is the corpus shape, not an invented one.
func repeatEachOptionalSPrecondition(t *testing.T) {
	t.Helper()
	if _, sa := corpusSA(t, "Tempt with Vengeance", "DBRepeat"); sa == nil ||
		sa.Params["RepeatOptionalForEachPlayer"] != "True" || sa.Params["RepeatOptionalMessage"] == "" {
		t.Fatal("precondition failed: Tempt with Vengeance's RepeatOptionalForEachPlayer$ shape changed")
	}
}

// TestRepeatEachOptionalForEachPlayerNoAskDeclines is the R-9 contract: a
// host with no decision channel (the effects double's Ask reports false)
// declines each subject, never parks a suspension and never runs a body.
func TestRepeatEachOptionalForEachPlayerNoAskDeclines(t *testing.T) {
	repeatEachOptionalSPrecondition(t)
	ran := 0
	Register("TestRepeatEachOptionalSpy", func(Host, *Ctx, *cards.SA) { ran++ })
	t.Cleanup(func() { unregister("TestRepeatEachOptionalSpy") })

	h := newHost(t, 3)
	c := &Ctx{Source: 1, Controller: 0, SVars: map[string]string{"Body": "DB$ TestRepeatEachOptionalSpy"}}
	effRepeatEach(h, c, repeatEachOptionalSA())

	if h.askCount != 2 {
		t.Fatalf("no-ask host asked %d times, want 2 (one election per opponent)", h.askCount)
	}
	if ran != 0 {
		t.Fatalf("no-ask host ran %d bodies, want 0 (every election declined)", ran)
	}
	if h.lastAsk == nil || h.lastAsk.ResumeKind != "repeat_each_optional" ||
		h.lastAsk.Player != 2 || h.lastAsk.Prompt != testRepeatEachMessage {
		t.Fatalf("last election = %+v, want repeat_each_optional for player 2 with the message prompt", h.lastAsk)
	}
}
