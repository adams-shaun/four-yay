package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// A Room is dealt under its catalogue name and its cast step may name the
// OTHER door (CR 709.5): the runner binds the door's name to the physical card
// and casts it through the engine's room_alt offer, so the Room enters with
// that door unlocked and shows that face -- not the front door.
const roomAltFaceScenario = `{"name":"room-alt-face","cr":["709.5"],"why":"cast the second door of a Room named by the step","setup":{"p0":{"hand":["Bottomless Pool // Locker Room"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Locker Room","mana":"UUUUU"},{"op":"resolve","seat":0}]}`

func TestOracleRoomAltFaceCastStep(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(roomAltFaceScenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	setup := res.Snapshots[0]
	if h := setup.Players[0].Hand; len(h) != 1 {
		t.Fatalf("precondition: p0 hand = %v, want the one Room", h)
	}
	final := res.Snapshots[len(res.Snapshots)-1]
	if h := final.Players[0].Hand; len(h) != 0 {
		t.Fatalf("p0 hand = %v, want empty: the Room was not cast", h)
	}
	// The snapshot ref is the physical card's catalogue name; Name is the
	// face the permanent shows.
	room, ok := snapPerm(final, "p0:Bottomless Pool // Locker Room")
	if !ok {
		t.Fatalf("the Room is not on the battlefield: %+v\n%s", final.Permanents, strings.Join(res.Transcript, "\n"))
	}
	if room.Name != "Locker Room" {
		t.Fatalf("the Room shows %q, want the named door Locker Room", room.Name)
	}
}
