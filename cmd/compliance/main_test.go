package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
)

// fakeXMage builds a git repo shaped like an XMage checkout with one set
// class, and returns its root and HEAD.
func fakeXMage(t *testing.T) (root, head string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, "Mage.Sets", "src", "mage", "sets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `super("Fixture", "FXT", ExpansionSet.buildDate(2026, 10, 2), SetType.EXPANSION);
        cards.add(new SetCardInfo("Zap", 2, Rarity.COMMON, mage.cards.z.Zap.class));`
	if err := os.WriteFile(filepath.Join(dir, "Fixture.java"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"}, {"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--no-verify", "-m", "fixture"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = cards.GitEnv() // strips inherited GIT_* (a hook's GIT_DIR would redirect this)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	out, err := gitHead(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, out
}

func TestManifestCommandWritesOneFilePerSet(t *testing.T) {
	root, head := fakeXMage(t)
	out := t.TempDir()
	if err := runManifest(root, head, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "FXT.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m compliance.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.XMageRef != head || len(m.Cards) != 1 || m.Cards[0].Name != "Zap" {
		t.Fatalf("got %+v", m)
	}
}

func TestManifestCommandRefusesRefMismatch(t *testing.T) {
	root, _ := fakeXMage(t)
	err := runManifest(root, "0000000000000000000000000000000000000000", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "is at") {
		t.Fatalf("err = %v, want a ref mismatch refusal", err)
	}
}
