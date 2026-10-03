// Command compliance maintains gorge's declared-set compliance data
// (docs/superpowers/specs/2026-10-02-xmage-compliance-oracle-design.md).
//
//	compliance manifest -xmage <XMage checkout> -ref <sha> [-out compliance/manifests]
//	compliance resolve  [-cards .cards] [-v] <manifest.json>...
//
// manifest writes one <CODE>.json per XMage set class, refusing a checkout
// whose HEAD is not -ref. resolve reports, per manifest, how many of its
// cards the corpus has, so a FORGE_REF bump's coverage of a set is one line.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: compliance manifest|resolve ...")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "manifest":
		fs := flag.NewFlagSet("manifest", flag.ExitOnError)
		xmage := fs.String("xmage", "", "XMage checkout (needs Mage.Sets/src/mage/sets)")
		ref := fs.String("ref", "", "XMAGE_REF the checkout must be at")
		out := fs.String("out", "compliance/manifests", "output directory")
		fs.Parse(os.Args[2:])
		err = runManifest(*xmage, *ref, *out)
	case "resolve":
		fs := flag.NewFlagSet("resolve", flag.ExitOnError)
		dir := fs.String("cards", ".cards", "corpus directory")
		verbose := fs.Bool("v", false, "list the missing names")
		fs.Parse(os.Args[2:])
		err = runResolve(*dir, *verbose, fs.Args())
	default:
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "compliance:", err)
		os.Exit(1)
	}
}

func gitHead(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	cmd.Env = cards.GitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func runManifest(xmage, ref, out string) error {
	if xmage == "" || ref == "" {
		return fmt.Errorf("manifest needs -xmage and -ref")
	}
	head, err := gitHead(xmage)
	if err != nil {
		return err
	}
	if head != ref {
		return fmt.Errorf("%s is at %s, not -ref %s: check out the pin first", xmage, head, ref)
	}
	paths, err := filepath.Glob(filepath.Join(xmage, "Mage.Sets", "src", "mage", "sets", "*.java"))
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no set classes under %s/Mage.Sets/src/mage/sets", xmage)
	}
	sort.Strings(paths)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	seen := map[string]string{}
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		m, err := compliance.ParseSetClass(src, ref)
		if err != nil {
			return fmt.Errorf("%s: %v", filepath.Base(p), err)
		}
		if prev, dup := seen[m.Code]; dup {
			return fmt.Errorf("set code %s in both %s and %s", m.Code, prev, filepath.Base(p))
		}
		seen[m.Code] = filepath.Base(p)
		if err := os.WriteFile(filepath.Join(out, m.Code+".json"), m.MarshalLines(), 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("wrote %d manifests to %s\n", len(paths), out)
	return nil
}

func runResolve(dir string, verbose bool, manifests []string) error {
	reg, err := cards.OpenCorpus(dir)
	if err != nil {
		return err
	}
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	for _, p := range manifests {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var m compliance.Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("%s: %v", p, err)
		}
		var missing []string
		for _, c := range m.Cards {
			if _, ok := compliance.CorpusName(has, c.Name); !ok {
				missing = append(missing, c.Name)
			}
		}
		fmt.Printf("%-6s cards %4d  in-corpus %4d  missing %4d\n", m.Code, len(m.Cards), len(m.Cards)-len(missing), len(missing))
		if verbose {
			for _, n := range missing {
				fmt.Printf("    %s\n", n)
			}
		}
	}
	return nil
}
