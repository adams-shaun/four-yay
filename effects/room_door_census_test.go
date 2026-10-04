package effects

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// This is the measured whole-corpus carrier census, not a repo-deck sample.
// A newly printed carrier changes this map and must receive a reviewed
// behavior test before the primitive's coverage is considered complete.
func TestRoomDoorPrimitiveCorpusCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	got := map[string][]string{
		"api:UnlockDoor":              {},
		"count:UnlockedDoors":         {},
		"count:DistinctUnlockedDoors": {},
	}
	for _, card := range reg.Cards {
		for _, primitive := range card.Primitives() {
			if primitive == "api:UnlockDoor" {
				got[primitive] = append(got[primitive], card.Faces[0].Name)
			}
		}
		for _, face := range card.Faces {
			for _, body := range face.SVars {
				body = strings.TrimSpace(body)
				for _, head := range []string{"UnlockedDoors", "DistinctUnlockedDoors"} {
					if body == "Count$"+head || strings.HasPrefix(body, "Count$"+head+" ") || strings.HasPrefix(body, "Count$"+head+"/") {
						got["count:"+head] = append(got["count:"+head], face.Name)
					}
				}
			}
		}
	}
	want := map[string][]string{
		"api:UnlockDoor":              {"Ghostly Dancers", "Ghostly Keybearer", "Keys to the House", "Marina Vendrell"},
		"count:UnlockedDoors":         {"Misty Salon", "Rampaging Soulrager"},
		"count:DistinctUnlockedDoors": {"Promising Stairs"},
	}
	for primitive := range want {
		slices.Sort(got[primitive])
		if !slices.Equal(got[primitive], want[primitive]) {
			t.Errorf("corpus carriers of %s = %v, want %v", primitive, got[primitive], want[primitive])
		}
	}
}
