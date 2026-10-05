package oraclediff

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// TestPermKeysAttachedToUsesHostCurrentName pins that an attached_to line
// names its host the way the driver does: the host permanent's current name,
// which is its layer-3 SetName$ name (Honest Work's Humble Merchant), not the
// printed name the gorge ref encodes.
func TestPermKeysAttachedToUsesHostCurrentName(t *testing.T) {
	aura := rules.OracleSnapPerm{
		Ref: "p1:Honest Work", Name: "Honest Work", Controller: 0, Owner: 0,
		Types: []string{"Aura", "Enchantment"}, Colors: "U", AttachedTo: "p1:Humble Merchant",
	}
	host := rules.OracleSnapPerm{
		Ref: "p1:Humble Merchant", Name: "Humble Merchant", Controller: 1, Owner: 1,
		Types: []string{"Citizen", "Creature"}, Colors: "G", PT: "1/1",
	}
	keys := strings.Join(permKeys([]rules.OracleSnapPerm{aura, host}), "\n")
	if !strings.Contains(keys, "on=Humble Merchant") {
		t.Fatalf("permKeys did not use the host's layer-3 name:\n%s", keys)
	}
	if strings.Contains(keys, "on=Grizzly Bears") {
		t.Fatalf("permKeys used the printed name:\n%s", keys)
	}

	// The driver emits the same current name, so the two sides agree.
	xAura := rules.OracleSnapPerm{
		Name: "Honest Work", Controller: 0, Owner: 0,
		Types: []string{"Aura", "Enchantment"}, Colors: "U", AttachedTo: "Humble Merchant",
	}
	xHost := rules.OracleSnapPerm{
		Name: "Humble Merchant", Controller: 1, Owner: 1,
		Types: []string{"Citizen", "Creature"}, Colors: "G", PT: "1/1",
	}
	if a, b := permKeys([]rules.OracleSnapPerm{aura, host}), permKeys([]rules.OracleSnapPerm{xAura, xHost}); strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Fatalf("gorge/xmage perm lines differ:\ngorge %v\nxmage %v", a, b)
	}
}

// TestPermKeysFaceDownHostHasNoAttachedName pins that an attachment to a
// face-down host omits the name: the driver's attached_to is the host's
// getName(), which is empty while face down (CR 708.2a), and an empty value
// is dropped from the canonical key.
func TestPermKeysFaceDownHostHasNoAttachedName(t *testing.T) {
	equipment := rules.OracleSnapPerm{
		Ref: "p0:Cryptic Coat", Name: "Cryptic Coat", Controller: 0, Owner: 0,
		Types: []string{"Artifact", "Equipment"}, Colors: "U", AttachedTo: "p0:Wastes",
	}
	host := rules.OracleSnapPerm{
		Ref: "p0:Wastes", Name: "", Controller: 0, Owner: 0, FaceDown: true,
		Types: []string{"Creature"}, PT: "3/2",
	}
	keys := strings.Join(permKeys([]rules.OracleSnapPerm{equipment, host}), "\n")
	if strings.Contains(keys, "on=") {
		t.Fatalf("a face-down host still produced an attached name:\n%s", keys)
	}
	if !strings.Contains(keys, "c0 o0  [creature]") {
		t.Fatalf("the face-down permanent's empty name is not rendered:\n%s", keys)
	}

	xEquipment := rules.OracleSnapPerm{
		Name: "Cryptic Coat", Controller: 0, Owner: 0,
		Types: []string{"Artifact", "Equipment"}, Colors: "U",
	}
	xHost := rules.OracleSnapPerm{
		Name: "", Controller: 0, Owner: 0, FaceDown: true, Types: []string{"Creature"}, PT: "3/2",
	}
	if a, b := permKeys([]rules.OracleSnapPerm{equipment, host}), permKeys([]rules.OracleSnapPerm{xEquipment, xHost}); strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Fatalf("gorge/xmage perm lines differ:\ngorge %v\nxmage %v", a, b)
	}
}
