package cards

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxSiblingCaches bounds how many fingerprint-keyed IR caches one corpus
// directory keeps. Each distinct compiler fingerprint (see
// CompilerFingerprint) writes its own ir-<hash>.gob.gz, so a shared .cards
// directory accumulates one file per parser anyone has run; keeping only the
// newest few stops that from growing without bound while leaving the caches
// that are actually in use.
const maxSiblingCaches = 4

// cachePathFor returns the fingerprint-keyed cache file for dir. A different
// fingerprint is a different file, so no build can read or overwrite another
// build's IR.
func cachePathFor(dir, fingerprint string) string {
	return filepath.Join(dir, "ir-"+fingerprint+".gob.gz")
}

// CachePath returns dir's IR cache path for the compiler this binary was
// built from. Callers that read or write the cache directly (forgec,
// keywordbench, corpus tests) must go through this rather than a fixed
// "ir.gob.gz" name, or they reintroduce the cross-compiler sharing the
// fingerprint exists to prevent.
func CachePath(dir string) string { return cachePathFor(dir, CompilerFingerprint) }

// OpenCorpus loads dir's compiled IR cache (dir/ir-<fingerprint>.gob.gz), or
// compiles dir/cardsfolder afresh when the cache is absent, unreadable, or
// older than dir/cards.lock — a fetch since the last compile invalidates it,
// the same staleness rule forgec's own fetch-then-compile pipeline assumes.
// It returns a plain error, never a panic, when neither cache nor corpus is
// present, so a clean checkout with nothing fetched is the caller's
// decision (tests Skip; a server refuses to start).
//
// The cache file name carries the cards package's CompilerFingerprint, so the
// registry always comes from this binary's own parser: a worktree whose
// parser differs from main's neither reads main's cache nor overwrites it.
//
// A recompile writes the fresh cache back (best effort): without it a
// cache left behind by an older cacheVersion is decoded, rejected and the
// whole corpus recompiled on EVERY process start -- measured at ~0.6 s CPU
// and ~740 MB allocated per botbench/test process. A write failure (a
// read-only corpus) is ignored; the compiled registry is still returned.
func OpenCorpus(dir string) (*Registry, error) {
	return openCorpus(dir, CompilerFingerprint)
}

// openCorpus is OpenCorpus with the fingerprint supplied explicitly, so tests
// can stand in for two builds with differing parsers. Production callers use
// OpenCorpus, which passes the embedded CompilerFingerprint.
func openCorpus(dir, fingerprint string) (*Registry, error) {
	cache := cachePathFor(dir, fingerprint)
	cacheInfo, cacheErr := os.Stat(cache)
	lockInfo, lockErr := os.Stat(filepath.Join(dir, "cards.lock"))
	stale := cacheErr != nil || (lockErr == nil && lockInfo.ModTime().After(cacheInfo.ModTime()))
	if !stale {
		if r, err := LoadRegistry(cache); err == nil {
			rerootPaths(r, dir)
			return r, nil
		}
	}
	r, _, err := CompileDir(CorpusDir(dir))
	if err != nil {
		return nil, err
	}
	_ = r.Save(cache)
	PruneCaches(dir, cache)
	return r, nil
}

// PruneCaches removes fingerprint-keyed sibling caches in dir, keeping the
// keepPath cache plus the newest ones up to maxSiblingCaches in total. It
// never touches the legacy ir.gob.gz name (it predates fingerprint keying and
// is not a sibling this scheme created) and ignores every error: pruning is a
// best-effort housekeeping step on a directory shared by many processes, and
// losing a race to another process's write/remove must never fail an open.
func PruneCaches(dir, keepPath string) {
	paths, err := filepath.Glob(filepath.Join(dir, "ir-*.gob.gz"))
	if err != nil || len(paths) <= maxSiblingCaches {
		return
	}
	type entry struct {
		path string
		mod  int64
	}
	entries := make([]entry, 0, len(paths))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		entries = append(entries, entry{p, info.ModTime().UnixNano()})
	}
	// The just-written cache sorts first regardless of timestamp ties, so a
	// prune racing the save can never delete the file the caller is about to
	// read. Everything else is newest first, name-descending as a
	// deterministic tie-break.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].path == keepPath {
			return true
		}
		if entries[j].path == keepPath {
			return false
		}
		if entries[i].mod != entries[j].mod {
			return entries[i].mod > entries[j].mod
		}
		return entries[i].path > entries[j].path
	})
	for _, e := range entries[maxSiblingCaches:] {
		_ = os.Remove(e.path)
	}
}

// rerootPaths points every card's and token's script Path at dir, the corpus
// directory this process actually opened. A cache records the absolute paths
// of whichever process compiled it, and .cards is shared into every seat
// worktree by symlink, so a cache compiled from .worktrees/<id>/.cards names
// files under that worktree. Once the worktree is removed after its merge,
// host's feedback capture cannot read any token script back (all of them land
// in tokens_unread), which failed cmd/repro on main and in every worktree
// (measured 2026-09-25). The part from the corpus subdirectory on is the
// same in every copy, so only the prefix is replaced.
func rerootPaths(r *Registry, dir string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	fix := func(c *Card) {
		if c == nil {
			return
		}
		for _, sub := range []string{"cardsfolder", "tokenscripts"} {
			seg := string(filepath.Separator) + sub + string(filepath.Separator)
			if i := strings.LastIndex(c.Path, seg); i >= 0 {
				c.Path = filepath.Join(abs, c.Path[i+1:])
				return
			}
		}
	}
	for _, c := range r.Cards {
		fix(c)
	}
	for _, c := range r.Tokens {
		fix(c)
	}
}
