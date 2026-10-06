package cards

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	// ForgeRepo is gorge's fork of Card-Forge/forge. Its `gorge` branch is
	// upstream plus script corrections that disagree with the printed card,
	// each also offered upstream (Card-Forge/forge#12153); FORGE_REF pins a
	// commit on it.
	ForgeRepo    = "https://github.com/adams-shaun/forge.git"
	UpstreamRepo = "https://github.com/Card-Forge/forge.git"
	forgeLicense = "GPL-3.0"
)

// forgeSubpaths are the sparse-checkout paths Fetch pulls from Forge: the
// card corpus itself, and the token scripts a card's TokenScript$ parameter
// (e.g. "make a 1/1 red Goblin") references by file stem. Order matters:
// index 0 is always the card corpus, matched against Lock.FetchedPath for
// old lock readers.
var forgeSubpaths = []string{"forge-gui/res/cardsfolder", "forge-gui/res/tokenscripts"}

// fullCommitSHA matches a full 40-hex-character git commit hash, as opposed
// to a branch or tag name. A full SHA needs a different clone strategy (see
// fetchRepo): a --branch clone only reaches refs a remote advertises at
// their tips, but an arbitrary pinned commit may not be one.
var fullCommitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// destFor returns where fetchRepo moves one fetched sparse-checkout path
// once it lands inside dir.
func destFor(dir, subpath string) string {
	if subpath == forgeSubpaths[1] {
		return TokensDir(dir)
	}
	return CorpusDir(dir)
}

// Lock records exactly which upstream card data a build used, so a match's
// card definitions are as reproducible as its seed.
type Lock struct {
	Repo    string `json:"repo"`
	Ref     string `json:"ref"`
	Commit  string `json:"commit"`
	License string `json:"license"`
	// FetchedPath is the card corpus path alone, kept (and still populated)
	// for readers of a lock written before FetchedPaths existed.
	FetchedPath string `json:"fetched_path,omitempty"`
	// FetchedPaths lists every sparse-checkout path this fetch pulled —
	// today the card corpus and the token scripts.
	FetchedPaths []string `json:"fetched_paths"`
	Files        int      `json:"files"`
	Digest       string   `json:"digest"`
}

// CorpusDir is where Fetch places the scripts inside dir.
func CorpusDir(dir string) string { return filepath.Join(dir, "cardsfolder") }

func lockPath(dir string) string { return filepath.Join(dir, "cards.lock") }

// Fetch sparse-clones the Forge card corpus and token scripts at ref from
// ForgeRepo into dir. The scripts are GPL-3.0 and must never be committed or
// shipped; dir is gitignored.
func Fetch(dir, ref string) (*Lock, error) {
	return fetchRepo(ForgeRepo, dir, ref)
}

// FetchFrom is Fetch from another Forge repository (UpstreamRepo, or a
// local mirror).
func FetchFrom(repo, dir, ref string) (*Lock, error) {
	return fetchRepo(repo, dir, ref)
}

// fetchRepo is the internal implementation of Fetch that accepts a repo URL,
// allowing tests to use a local repository without network access.
func fetchRepo(repo, dir, ref string) (*Lock, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	work := filepath.Join(dir, "forge.git-checkout")
	if err := os.RemoveAll(work); err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	run := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Env = GitEnv()
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out)), nil
	}

	var commit string
	if fullCommitSHA.MatchString(ref) {
		// FL-11: a pinned commit is not necessarily any branch's tip, so it
		// cannot be requested with --branch, and the earlier approach of
		// `clone --no-checkout` first (which pulls the *whole* commit/tree
		// history's metadata before narrowing) wastes a full-history
		// transfer just to throw most of it away. Instead: start an empty
		// repo with no history at all, wire up the remote and the sparse
		// paths, then do exactly one depth-1, blob:none-filtered fetch of
		// this single commit and check it out — nothing else is ever
		// transferred.
		if _, err := run("init", work); err != nil {
			return nil, err
		}
		if _, err := run("-C", work, "remote", "add", "origin", repo); err != nil {
			return nil, err
		}
		if _, err := run("-C", work, "sparse-checkout", "init", "--cone"); err != nil {
			return nil, err
		}
		if _, err := run(append([]string{"-C", work, "sparse-checkout", "set"}, forgeSubpaths...)...); err != nil {
			return nil, err
		}
		if _, err := run("-C", work, "fetch", "--depth", "1", "--filter=blob:none", "origin", ref); err != nil {
			return nil, err
		}
		if _, err := run("-C", work, "checkout", "FETCH_HEAD"); err != nil {
			return nil, err
		}
		commit = ref
	} else {
		if _, err := run("clone", "--depth", "1", "--filter=blob:none", "--sparse",
			"--branch", ref, repo, work); err != nil {
			return nil, err
		}
		if _, err := run(append([]string{"-C", work, "sparse-checkout", "set"}, forgeSubpaths...)...); err != nil {
			return nil, err
		}
		head, err := run("-C", work, "rev-parse", "HEAD")
		if err != nil {
			return nil, err
		}
		commit = head
	}

	var fetchedPaths []string
	for _, sub := range forgeSubpaths {
		dest := destFor(dir, sub)
		if err := os.RemoveAll(dest); err != nil {
			return nil, err
		}
		if err := os.Rename(filepath.Join(work, sub), dest); err != nil {
			return nil, err
		}
		fetchedPaths = append(fetchedPaths, sub)
	}

	digest, files, err := DigestDir(CorpusDir(dir))
	if err != nil {
		return nil, err
	}
	_, tokenFiles, err := DigestDir(TokensDir(dir))
	if err != nil {
		return nil, err
	}

	l := &Lock{
		Repo: repo, Ref: ref, Commit: commit, License: forgeLicense,
		FetchedPath:  forgeSubpaths[0],
		FetchedPaths: fetchedPaths,
		Files:        files, Digest: digest,
	}
	if err := WriteLock(dir, l); err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr,
		"fetched %d card scripts and %d token scripts from %s at %s (%s)\n"+
			"These scripts are licensed %s. They are a build input only:\n"+
			"never commit them, never include them in a published image.\n",
		files, tokenFiles, repo, commit[:min(8, len(commit))], ref, forgeLicense)
	return l, nil
}

// DigestDir hashes a directory's .txt contents in sorted path order, so the
// digest depends on content and layout but not on filesystem iteration order.
// Uses length-framing to prevent collision attacks where content can embed
// path-like strings and NUL bytes.
func DigestDir(dir string) (string, int, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".txt") {
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			paths = append(paths, rel)
		}
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		b, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return "", 0, err
		}
		pathBytes := []byte(filepath.ToSlash(rel))
		var pathLen [8]byte
		binary.BigEndian.PutUint64(pathLen[:], uint64(len(pathBytes)))
		h.Write(pathLen[:])
		h.Write(pathBytes)
		var contentLen [8]byte
		binary.BigEndian.PutUint64(contentLen[:], uint64(len(b)))
		h.Write(contentLen[:])
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), len(paths), nil
}

func WriteLock(dir string, l *Lock) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(lockPath(dir), append(b, '\n'), 0o644)
}

func ReadLock(dir string) (*Lock, error) {
	b, err := os.ReadFile(lockPath(dir))
	if err != nil {
		return nil, err
	}
	var l Lock
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, err
	}
	return &l, nil
}
