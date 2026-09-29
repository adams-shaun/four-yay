# Flow Profiles, Breakpoints and Keymap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build sub-project 1 of the table UI rework:

- **Breakpoints** in flow profiles, which pause auto-pass on events.
- **A rebindable global keymap**, with an editor and a cheat sheet.
- **File export and import** for flow profiles and the keymap.

**Architecture:**

- Breakpoints are a new `breakpoints` block on `PlaySettings`, bringing the settings blob to v3.
  - Flow profiles already store full `PlaySettings`, so profiles gain breakpoints for free.
- A new pure module, `lib/breakpoints.ts`, checks the view for a breakpoint hit.
  - `decide()` consults it.
  - `SeatPanelState` remembers which decision seq each hit fired at, so a breakpoint stops once, not forever.
- A new pure module, `lib/keymap.ts`, owns the bindings. `hotkeys.ts` keeps its guards and matches through the keymap.
- UI goes in new, small components mounted from the existing panel and strip. The 985-line `PlaySettingsPanel.svelte` is not extended.

**Tech Stack:** Svelte 5 (runes), TypeScript 6, Vitest 5 (Node 22+), with Playwright-backed browser fixtures for components.

**Spec:** `docs/superpowers/specs/2026-09-28-client-table-ui-rework-design.md`, section "1. Flow profiles and keymap".

## Global Constraints

- **Setup.** Work in a task worktree made with `scripts/agent-worktree.sh flow-profiles --web`. Never use a bare `git worktree add`.
- **Never run `npm ci` or `npm install`.** `web/node_modules` is a shared symlink, and a reinstall wipes it for every checkout.
- **Commands.** Run them from `<worktree>/web`:
  - `npx vitest run <file>` for tests;
  - `npx svelte-check --tsconfig ./tsconfig.json` for types;
  - `npx eslint .` for lint.
- **Search with `/usr/bin/grep`**, never bare `grep`. Bare `grep` is ugrep here and honours `.gitignore`.
- **Everything is client-side.** Nothing is sent to the server.
- **Storage keys:**
  - The existing keys stay: `gorge.playsettings.v1` for settings and `gorge.playsettings.profiles.v1` for profiles. Their blobs move to settings version 3 in place.
  - The one new key is `gorge.keymap.v1`.
- **Migration.** A v1 or v2 settings or profile blob must load with its fields unchanged and `breakpoints` set to the defaults: all off, empty watchlist.
- **Defaults must not change behaviour.** Every preset ships breakpoints all off. With breakpoints off, `decide()` must return exactly what it returns today.
- **Applying a preset or cycling profiles never clears the player's breakpoints.** They are the player's, not the preset's.
- **Today's hotkeys stay as the default bindings.** `src/lib/hotkeys.test.ts` must pass unchanged.
- **Hotkey guards stay:**
  - Escape always cancels a run.
  - Meta is never a hotkey.
  - Keys are ignored while an input is focused or a modal picker is open.
  - Ctrl without Shift is reserved as the hold-priority modifier and is never a binding.
- **Import rules.** Files are validated through the stores' own `validate` functions. A name clash gets the suffix ` (2)`, ` (3)` and so on.
- **Commit hygiene.** No `Co-Authored-By` or other attribution trailer in commits (repo hooks reject them). Stage explicit paths; never `git add -A`.

## Review Focus

1. **A breakpoint firing twice or never.** The seat state re-derives the same window many times: considerAuto, pacing timers, re-renders.
   - Expected: a hit stops every re-check of that decision seq, then passes later windows for the same object.
   - Test: Task 4, "re-deriving the same window keeps it stopped".
2. **Upgrading with saved v2 profiles.**
   - Expected: every saved profile survives with its rules and gains default breakpoints. A v2 blob is never treated as corrupt.
   - Tests: Task 1, "a v2 blob migrates", and "a v2 profile store migrates".
3. **Picking a preset after setting a watchlist.**
   - Expected: the watchlist survives, and the preset label still reads that preset, not Custom.
   - Test: Task 1, "preset relabel ignores breakpoints".
4. **Rebinding onto a reserved chord.** Ctrl+X alone, a bare modifier, or Meta.
   - Expected: refused, with the keymap unchanged.
   - Test: Task 6, "bindingFromEvent refuses reserved chords".
5. **Importing a file that is not ours** (garbage, the wrong kind, a partly valid file).
   - Expected: a clear error, or only the valid entries imported. Existing profiles are never damaged.
   - Tests: Task 9, "wrong kind is an error", and "bad entries are skipped".

---

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `web/src/lib/playsettings.ts` | modify | `Breakpoints` type; `PlaySettings` v3; validate and migrate; watchlist helpers |
| `web/src/lib/profiles.ts` | modify | clone the new nested field |
| `web/src/lib/breakpoints.ts` | create | pure breakpoint check over (view, seat, settings) |
| `web/src/lib/autopilot.ts` | modify | call `checkBreakpoints`; `StopReason` gains `'breakpoint'`; `targetsMe` moves to `breakpoints.ts` |
| `web/src/lib/seatpanel.svelte.ts` | modify | fired-at map; stop note text; `applyProfileAt`; `pickHotkey`; `importProfilesText` |
| `web/src/components/BreakpointsSection.svelte` | create | "Pause when…" editor |
| `web/src/lib/keymap.ts` | create | actions, bindings, defaults, validate, load/save, labels, conflicts |
| `web/src/lib/keymap.svelte.ts` | create | reactive keymap store (capture flag) |
| `web/src/lib/hotkeys.ts` | modify | match through the keymap |
| `web/src/components/HotButtonStrip.svelte` | modify | dispatch the new actions; mount the cheat sheet |
| `web/src/components/KeymapEditor.svelte` | create | "Keys" editor section |
| `web/src/components/KeyCheatSheet.svelte` | create | `?` overlay |
| `web/src/lib/transfer.ts` | create | export and import envelopes |
| `web/src/lib/download.ts` | create | save text as a file (DOM) |
| `web/src/components/PlaySettingsPanel.svelte` | modify | mount the two new sections; add export and import buttons |
| `docs/superpowers/specs/2026-09-28-client-table-ui-rework-design.md` | modify | record the storage and scope decisions made here |

---

### Task 1: Settings v3 with breakpoints

**Files:**
- Modify: `web/src/lib/playsettings.ts`
- Modify: `web/src/lib/profiles.ts` (`deepClone`)
- Test: `web/src/lib/playsettings.test.ts`, `web/src/lib/profiles.test.ts`

**Interfaces:**
- Produces:
  - `interface Breakpoints { targetsMe: boolean; watchlist: string[]; attacked: boolean; stackDepth: number }`
  - `PlaySettings.version: 3`
  - `PlaySettings.breakpoints: Breakpoints`
  - `noBreakpoints(): Breakpoints`
  - `normaliseWatchName(raw: string): string | null`
  - `withWatch(list: readonly string[], raw: string): string[]`
  - `withoutWatch(list: readonly string[], name: string): string[]`
  - `STACK_DEPTHS: readonly number[]` (`[0, 2, 3, 4, 5]`)
  - `MAX_WATCHLIST = 50`
  - `MAX_WATCH_NAME = 60`

- [ ] **Step 1: Write the failing tests** (append to `playsettings.test.ts`)

```ts
import { noBreakpoints, normaliseWatchName, withWatch, withoutWatch, validate } from './playsettings';

describe('breakpoints (settings v3)', () => {
  it('every preset ships breakpoints all off', () => {
    for (const p of Object.values(PRESETS)) expect(p.breakpoints).toEqual(noBreakpoints());
    expect(noBreakpoints()).toEqual({ targetsMe: false, watchlist: [], attacked: false, stackDepth: 0 });
  });

  it('a v2 blob migrates: every field kept, breakpoints defaulted', () => {
    const v2 = { ...PRESETS['no-tells'], version: 2 } as Record<string, unknown>;
    delete v2.breakpoints;
    const st = memStorage();
    st.setItem('gorge.playsettings.v1', JSON.stringify(v2));
    const got = loadSettings(st);
    expect(got.version).toBe(3);
    expect(got.opponentSpell).toBe(PRESETS['no-tells'].opponentSpell);
    expect(got.breakpoints).toEqual(noBreakpoints());
  });

  it('a v3 blob round-trips its breakpoints', () => {
    const s = withChange(defaultSettings(), { breakpoints: { targetsMe: true, watchlist: ["Thassa's Oracle"], attacked: true, stackDepth: 3 } });
    const st = memStorage();
    saveSettings(st, s);
    expect(loadSettings(st).breakpoints).toEqual({ targetsMe: true, watchlist: ["Thassa's Oracle"], attacked: true, stackDepth: 3 });
  });

  it('a v3 blob with a bad breakpoints block is corrupt (defaults)', () => {
    for (const bad of [
      { targetsMe: 'yes', watchlist: [], attacked: false, stackDepth: 0 },
      { targetsMe: false, watchlist: 'x', attacked: false, stackDepth: 0 },
      { targetsMe: false, watchlist: [], attacked: false, stackDepth: 1 },
      { targetsMe: false, watchlist: [], attacked: false, stackDepth: 99 },
    ]) {
      expect(validate({ ...PRESETS.casual, breakpoints: bad })).toBeNull();
    }
  });

  it('validate normalises the watchlist: trims, drops empties, dedupes case-insensitively', () => {
    const got = validate({ ...PRESETS.casual, breakpoints: { ...noBreakpoints(), watchlist: ['  Oracle ', 'oracle', '', 'Rift'] } });
    expect(got?.breakpoints.watchlist).toEqual(['Oracle', 'Rift']);
  });

  it('preset relabel ignores breakpoints: a casual config with a watchlist still reads casual', () => {
    const s = withChange(defaultSettings(), { breakpoints: { ...noBreakpoints(), watchlist: ['Oracle'] } });
    expect(s.preset).toBe('casual');
  });

  it('applying a preset keeps the player’s breakpoints', () => {
    const mine = withChange(defaultSettings(), { breakpoints: { ...noBreakpoints(), attacked: true } });
    const next = withChange(mine, presetPatch('full-control'));
    expect(next.preset).toBe('full-control');
    expect(next.breakpoints.attacked).toBe(true);
  });

  it('watch helpers: add normalises and dedupes, remove is case-insensitive, cap is enforced', () => {
    expect(normaliseWatchName('  Rhystic Study ')).toBe('Rhystic Study');
    expect(normaliseWatchName('   ')).toBeNull();
    expect(normaliseWatchName('x'.repeat(61))).toBeNull();
    expect(withWatch(['Oracle'], 'oracle')).toEqual(['Oracle']);
    expect(withWatch(['Oracle'], 'Rift')).toEqual(['Oracle', 'Rift']);
    expect(withoutWatch(['Oracle', 'Rift'], 'ORACLE')).toEqual(['Rift']);
    const full = Array.from({ length: 50 }, (_, i) => `c${i}`);
    expect(withWatch(full, 'one more')).toEqual(full);
  });
});
```

Also change two existing expectations in the same file:
- in "casual matches the settings table exactly", `expect(s.version).toBe(2)` becomes `expect(s.version).toBe(3)`;
- the wrong-version test becomes "3 is now the live version; a 4 is corrupt", writing `version: 4`.

Add `presetPatch` to the file's import list.

Append to `profiles.test.ts`:

```ts
it('a v2 profile store migrates: each saved profile gains default breakpoints', () => {
  const v2 = { ...PRESETS.casual, version: 2 } as Record<string, unknown>;
  delete v2.breakpoints;
  const store = validateStore({ version: 1, profiles: { Mine: v2 }, order: ['Mine'], lastActive: 'Mine' });
  expect(store.order).toEqual(['Mine']);
  expect(store.profiles.Mine.breakpoints).toEqual(noBreakpoints());
  expect(store.profiles.Mine.version).toBe(3);
});

it('a stored profile does not share its watchlist array with the caller', () => {
  const s = withChange(defaultSettings(), { breakpoints: { ...noBreakpoints(), watchlist: ['Oracle'] } });
  const store = storeSave(emptyStore(), 'W', s);
  s.breakpoints.watchlist.push('Mutated');
  expect(store.profiles.W.breakpoints.watchlist).toEqual(['Oracle']);
});
```

(Import `PRESETS`, `defaultSettings`, `withChange`, `noBreakpoints` from `./playsettings`, plus `validateStore`, `storeSave` and `emptyStore` from `./profiles`, if they are not already imported.)

- [ ] **Step 2: Run to verify failure**

Run: `npx vitest run src/lib/playsettings.test.ts src/lib/profiles.test.ts`
Expected: FAIL. `noBreakpoints` is not exported, and version expectations are off.

- [ ] **Step 3: Implement in `playsettings.ts`**

Add after the `StoppableStep` type:

```ts
/**
 * Breakpoints are the player's "pause on X" rules (UI rework spec §1). They
 * stop auto-pass even where the stack and step rules would pass. They belong
 * to the PLAYER, not to a preset: every preset ships them off, applying a
 * preset never touches them, and the preset label ignores them.
 */
export interface Breakpoints {
  /** an opponent's object on top of the stack targets you or a permanent you control */
  targetsMe: boolean;
  /** an opponent's spell or ability from one of these card names is on top of the stack (case-insensitive exact name) */
  watchlist: string[];
  /** creatures are attacking you (first priority window of that combat) */
  attacked: boolean;
  /** the stack holds at least this many objects; 0 = off */
  stackDepth: number;
}

export const MAX_WATCHLIST = 50;
export const MAX_WATCH_NAME = 60;
/** STACK_DEPTHS is every legal stackDepth: 0 (off) or 2..5. A depth of 1 would stop on every object, which is the 'always' stack rule's job. */
export const STACK_DEPTHS: readonly number[] = [0, 2, 3, 4, 5];

export function noBreakpoints(): Breakpoints {
  return { targetsMe: false, watchlist: [], attacked: false, stackDepth: 0 };
}

/** normaliseWatchName trims a card name; empty or longer than MAX_WATCH_NAME is rejected. */
export function normaliseWatchName(raw: string): string | null {
  if (typeof raw !== 'string') return null;
  const name = raw.trim();
  return name.length === 0 || name.length > MAX_WATCH_NAME ? null : name;
}

/** withWatch returns the list with one more name: normalised, deduped case-insensitively, capped at MAX_WATCHLIST. */
export function withWatch(list: readonly string[], raw: string): string[] {
  const name = normaliseWatchName(raw);
  if (name === null || list.length >= MAX_WATCHLIST) return [...list];
  if (list.some((n) => n.toLowerCase() === name.toLowerCase())) return [...list];
  return [...list, name];
}

/** withoutWatch drops one name, case-insensitively. */
export function withoutWatch(list: readonly string[], name: string): string[] {
  return list.filter((n) => n.toLowerCase() !== name.toLowerCase());
}

function cloneBreakpoints(b: Breakpoints): Breakpoints {
  return { ...b, watchlist: [...b.watchlist] };
}
```

Change `PlaySettings`: `version: 3`, and add the field after `logAutoPasses`:

```ts
  /** the player's pause-on-X rules; never part of a preset (see Breakpoints) */
  breakpoints: Breakpoints;
```

Change `mkSettings` so presets carry default breakpoints:

```ts
function mkSettings(p: Exclude<Preset, 'custom'>, fields: Omit<PlaySettings, 'version' | 'preset' | 'breakpoints'>): PlaySettings {
  return { version: 3, preset: p, ...fields, breakpoints: noBreakpoints() };
}
```

Change `cloneSettings`:

```ts
function cloneSettings(s: PlaySettings): PlaySettings {
  return {
    ...s,
    steps: { yours: { ...s.steps.yours }, opponents: { ...s.steps.opponents } },
    pacing: { ...s.pacing },
    breakpoints: cloneBreakpoints(s.breakpoints),
  };
}
```

`presetPatch` already lists its fields explicitly and does not include `breakpoints`. Leave it alone; that omission is what keeps breakpoints across preset changes.

In `withChange`, make the relabel comparison ignore breakpoints. Replace the `if (deepEqual(...))` line with:

```ts
    if (deepEqual({ ...next, preset: name, breakpoints: PRESETS[name].breakpoints }, PRESETS[name])) {
```

In `validate`:
- change the version gate to `if (v.version !== 1 && v.version !== 2 && v.version !== 3) return null;`;
- change the v2 field requirement to `if (v.version >= 2 && typeof v.autoOrderAllTriggers !== 'boolean') return null;`;
- before the `return`, add:

```ts
  let breakpoints: Breakpoints;
  if (v.version === 3) {
    const b = readBreakpoints(v.breakpoints);
    if (b === null) return null;
    breakpoints = b;
  } else {
    // v1/v2 predate breakpoints: migrate with them off, keep everything else.
    breakpoints = noBreakpoints();
  }
```

- make the returned object `version: 3`, with `breakpoints,` as its last field.

Add the reader below `isRule`:

```ts
function readBreakpoints(v: unknown): Breakpoints | null {
  if (!isPlainObject(v)) return null;
  if (typeof v.targetsMe !== 'boolean' || typeof v.attacked !== 'boolean') return null;
  if (typeof v.stackDepth !== 'number' || !STACK_DEPTHS.includes(v.stackDepth)) return null;
  if (!Array.isArray(v.watchlist)) return null;
  let watchlist: string[] = [];
  for (const raw of v.watchlist) {
    if (typeof raw !== 'string') return null;
    watchlist = withWatch(watchlist, raw);
  }
  return { targetsMe: v.targetsMe, attacked: v.attacked, stackDepth: v.stackDepth, watchlist };
}
```

Update the doc comment above `validate` to say that blob versions 1 and 2 migrate to 3 with breakpoints off.

In `profiles.ts` `deepClone`, add the nested field:

```ts
    breakpoints: { ...s.breakpoints, watchlist: [...s.breakpoints.watchlist] },
```

- [ ] **Step 4: Run to verify pass, plus every suite that builds PlaySettings**

Run: `npx vitest run src/lib/playsettings.test.ts src/lib/profiles.test.ts src/lib/autopilot.test.ts src/lib/seatpanel.auto.test.ts src/lib/hotkeys-profiles.test.ts`
Expected: PASS.

Then run `npx svelte-check --tsconfig ./tsconfig.json`. Expected: 0 errors. If a test fixture builds a `PlaySettings` literal by hand and now misses `breakpoints`, add `breakpoints: noBreakpoints()` to that fixture. Do not loosen the type.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/playsettings.ts web/src/lib/profiles.ts web/src/lib/playsettings.test.ts web/src/lib/profiles.test.ts
git commit -m "feat(web): settings v3 carries the player's breakpoints; v1/v2 migrate"
```

(Include any fixture files Step 4 required, by explicit path.)

---

### Task 2: Breakpoint check

**Files:**
- Create: `web/src/lib/breakpoints.ts`
- Test: `web/src/lib/breakpoints.test.ts`

**Interfaces:**
- Consumes: `Breakpoints` from Task 1; `View`, `StackView` from `../protocol`.
- Produces:
  - `type BreakpointKind = 'targets-me' | 'watchlist' | 'attacked' | 'stack-depth'`
  - `interface BreakpointHit { kind: BreakpointKind; key: string; detail: string }`
  - `targetsSeat(view: View, seat: number, top: { targets?: { obj?: number; player: number; is_player: boolean }[] }): boolean`
  - `checkBreakpoints(a: { view: View; seat: number; bp: Breakpoints; fired: ReadonlyMap<string, number>; seq: number; skipTop: boolean }): BreakpointHit | null`

Firing rule: a hit's `key` names the thing it fired for:
- `targets:<stackId>` and `watch:<stackId>` (per stack object);
- `depth:<topId>` (per top object at that depth);
- `attacked:<turn>` (per combat turn).

The hit is returned only if `fired` lacks that key, or maps it to this same `seq`. So it stops the window it first fired on, however many times that window is re-checked, and never again after that.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from 'vitest';
import { checkBreakpoints, targetsSeat } from './breakpoints';
import { noBreakpoints, type Breakpoints } from './playsettings';
import type { View } from '../protocol';

const card = (id: number, controller: number, name: string, extra: Record<string, unknown> = {}) => ({ id, controller, owner: controller, name, ...extra });
const top = (id: number, controller: number, name: string, targets: { obj?: number; player: number; is_player: boolean }[] = []) =>
  ({ id, controller, name, kind: 'spell', text: '', targets, optional: false });

const mkView = (over: Partial<Record<string, unknown>> = {}): View =>
  ({
    turn: 7,
    active: 1,
    step: 'main1',
    stack: [],
    players: [
      { seat: 0, battlefield: [card(10, 0, 'Dark Confidant')], hand: [], graveyard: [], exile: [] },
      { seat: 1, battlefield: [card(20, 1, 'Thragtusk')], hand: [], graveyard: [], exile: [] },
    ],
    ...over,
  }) as unknown as View;

const bp = (over: Partial<Breakpoints>): Breakpoints => ({ ...noBreakpoints(), ...over });
const run = (view: View, b: Breakpoints, fired = new Map<string, number>(), seq = 5, skipTop = false) =>
  checkBreakpoints({ view, seat: 0, bp: b, fired, seq, skipTop });

describe('targetsSeat', () => {
  it('matches a player target on the seat and an object the seat controls', () => {
    const v = mkView();
    expect(targetsSeat(v, 0, top(1, 1, 'Bolt', [{ player: 0, is_player: true }]))).toBe(true);
    expect(targetsSeat(v, 0, top(1, 1, 'Bolt', [{ obj: 10, player: 0, is_player: false }]))).toBe(true);
    expect(targetsSeat(v, 0, top(1, 1, 'Bolt', [{ obj: 20, player: 1, is_player: false }]))).toBe(false);
  });
});

describe('checkBreakpoints', () => {
  it('all off never fires', () => {
    const v = mkView({ stack: [top(1, 1, 'Bolt', [{ player: 0, is_player: true }])] });
    expect(run(v, noBreakpoints())).toBeNull();
  });

  it('targets-me fires for an opponent object targeting my permanent, naming it', () => {
    const v = mkView({ stack: [top(1, 1, 'Lightning Bolt', [{ obj: 10, player: 0, is_player: false }])] });
    expect(run(v, bp({ targetsMe: true }))).toEqual({ kind: 'targets-me', key: 'targets:1', detail: 'Lightning Bolt targets Dark Confidant' });
  });

  it('targets-me says "you" for a player target and ignores my own objects', () => {
    const mine = mkView({ stack: [top(1, 0, 'Bolt', [{ player: 0, is_player: true }])] });
    expect(run(mine, bp({ targetsMe: true }))).toBeNull();
    const theirs = mkView({ stack: [top(2, 1, 'Bolt', [{ player: 0, is_player: true }])] });
    expect(run(theirs, bp({ targetsMe: true }))?.detail).toBe('Bolt targets you');
  });

  it('watchlist matches the top object name case-insensitively, opponents only', () => {
    const v = mkView({ stack: [top(3, 1, "Thassa's Oracle")] });
    expect(run(v, bp({ watchlist: ["thassa's oracle"] }))).toEqual({ kind: 'watchlist', key: 'watch:3', detail: "Thassa's Oracle is on your watchlist" });
    const mine = mkView({ stack: [top(3, 0, "Thassa's Oracle")] });
    expect(run(mine, bp({ watchlist: ["Thassa's Oracle"] }))).toBeNull();
  });

  it('watchlist also matches the stack object’s card name (an ability named differently)', () => {
    const t = { ...top(4, 1, 'Triggered ability'), card: { id: 99, name: 'Rhystic Study' } };
    expect(run(mkView({ stack: [t] }), bp({ watchlist: ['Rhystic Study'] }))?.kind).toBe('watchlist');
  });

  it('attacked fires when an opponent creature attacks me, keyed per turn', () => {
    const players = [
      { seat: 0, battlefield: [], hand: [], graveyard: [], exile: [] },
      { seat: 1, battlefield: [card(20, 1, 'Thragtusk', { attacking: true, attacking_player: 0 }), card(21, 1, 'Elf', { attacking: true, attacking_player: 2 })], hand: [], graveyard: [], exile: [] },
    ];
    const v = mkView({ players, step: 'declare-attackers' });
    expect(run(v, bp({ attacked: true }))).toEqual({ kind: 'attacked', key: 'attacked:7', detail: '1 creature is attacking you' });
  });

  it('stack-depth fires at the threshold, keyed on the top object', () => {
    const v = mkView({ stack: [top(1, 1, 'A'), top(2, 0, 'B')] });
    expect(run(v, bp({ stackDepth: 3 }))).toBeNull();
    expect(run(v, bp({ stackDepth: 2 }))).toEqual({ kind: 'stack-depth', key: 'depth:2', detail: '2 objects are on the stack' });
  });

  it('a hit already fired at an earlier seq does not fire again; the same seq does', () => {
    const v = mkView({ stack: [top(1, 1, 'Bolt', [{ player: 0, is_player: true }])] });
    const b = bp({ targetsMe: true });
    expect(run(v, b, new Map([['targets:1', 5]]), 5)?.key).toBe('targets:1');
    expect(run(v, b, new Map([['targets:1', 4]]), 5)).toBeNull();
  });

  it('skipTop suppresses the top-object rules but not attacked', () => {
    const players = [
      { seat: 0, battlefield: [], hand: [], graveyard: [], exile: [] },
      { seat: 1, battlefield: [card(20, 1, 'Thragtusk', { attacking: true, attacking_player: 0 })], hand: [], graveyard: [], exile: [] },
    ];
    const v = mkView({ players, stack: [top(1, 1, 'Bolt', [{ player: 0, is_player: true }])] });
    expect(run(v, bp({ targetsMe: true, attacked: true }), new Map(), 5, true)?.kind).toBe('attacked');
    expect(run(v, bp({ targetsMe: true }), new Map(), 5, true)).toBeNull();
  });

  it('order: targets-me, then watchlist, then attacked, then stack depth', () => {
    const v = mkView({ stack: [top(1, 1, 'Bolt'), top(2, 1, 'Oracle', [{ player: 0, is_player: true }])] });
    expect(run(v, bp({ targetsMe: true, watchlist: ['Oracle'], stackDepth: 2 }))?.kind).toBe('targets-me');
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run: `npx vitest run src/lib/breakpoints.test.ts`
Expected: FAIL. The module does not exist.

- [ ] **Step 3: Implement `web/src/lib/breakpoints.ts`**

```ts
import type { View } from '../protocol';
import type { Breakpoints } from './playsettings';

/**
 * breakpoints is the pure "pause on X" check (UI rework spec §1). It answers
 * one question for decide(): does a rule the player set want this priority
 * window shown to them? It never decides a pass. Firing is keyed: each hit
 * names the object or turn it fired for, and the caller remembers the
 * decision seq it fired at, so a hit stops that window on every re-check and
 * never re-stops later windows for the same thing.
 */

export type BreakpointKind = 'targets-me' | 'watchlist' | 'attacked' | 'stack-depth';

export interface BreakpointHit {
  kind: BreakpointKind;
  /** what it fired for: targets:<id> | watch:<id> | attacked:<turn> | depth:<id> */
  key: string;
  /** plain words for the auto note, no identifiers */
  detail: string;
}

interface Known {
  controller: number;
  name: string;
}

/** index maps every visible object id to its current controller and name (battlefield, hand, graveyard, exile, stack). */
function index(view: View): Map<number, Known> {
  const out = new Map<number, Known>();
  for (const p of view.players ?? []) {
    for (const zone of Object.values(p)) {
      if (!Array.isArray(zone)) continue;
      for (const c of zone) {
        if (c !== null && typeof c === 'object' && typeof c.id === 'number' && typeof c.controller === 'number') {
          out.set(c.id, { controller: c.controller, name: typeof c.name === 'string' ? c.name : '' });
        }
      }
    }
  }
  for (const s of view.stack ?? []) out.set(s.id, { controller: s.controller, name: s.name });
  return out;
}

/**
 * targetsSeat reports whether any target is the seat itself or an object the
 * seat currently controls. Every entry is read at its CURRENT controller, so
 * an object that changed hands resolves to whoever holds it now.
 */
export function targetsSeat(
  view: View,
  seat: number,
  top: { targets?: { obj?: number; player: number; is_player: boolean }[] },
): boolean {
  const known = index(view);
  return (top.targets ?? []).some((t) =>
    t.is_player ? t.player === seat : t.obj !== undefined && known.get(t.obj)?.controller === seat,
  );
}

/** attackersOn counts battlefield creatures, controlled by someone else, attacking the seat. */
function attackersOn(view: View, seat: number): number {
  let n = 0;
  for (const p of view.players ?? []) {
    for (const c of p.battlefield ?? []) {
      if (c.attacking && c.attacking_player === seat && c.controller !== seat) n++;
    }
  }
  return n;
}

export function checkBreakpoints(a: {
  view: View;
  seat: number;
  bp: Breakpoints;
  fired: ReadonlyMap<string, number>;
  seq: number;
  /** the caller's yield/Resolve-All baseline says the top object is already consented to: skip the top-object rules */
  skipTop: boolean;
}): BreakpointHit | null {
  const { view, seat, bp, fired, seq, skipTop } = a;
  const live = (hit: BreakpointHit): BreakpointHit | null => {
    const at = fired.get(hit.key);
    return at === undefined || at === seq ? hit : null;
  };
  const stack = view.stack ?? [];
  const top = stack.length > 0 ? stack[stack.length - 1] : null;
  const theirs = top !== null && top.controller !== seat && !skipTop;

  if (bp.targetsMe && theirs && top !== null) {
    const player = (top.targets ?? []).find((t) => t.is_player && t.player === seat);
    const known = index(view);
    const obj = (top.targets ?? []).find((t) => !t.is_player && t.obj !== undefined && known.get(t.obj)?.controller === seat);
    if (player !== undefined || obj !== undefined) {
      const what = player !== undefined ? 'you' : known.get(obj!.obj!)?.name || 'your permanent';
      const hit = live({ kind: 'targets-me', key: `targets:${top.id}`, detail: `${top.name} targets ${what}` });
      if (hit) return hit;
    }
  }

  if (bp.watchlist.length > 0 && theirs && top !== null) {
    const names = [top.name, top.card?.name].filter((n): n is string => typeof n === 'string').map((n) => n.toLowerCase());
    const match = bp.watchlist.find((w) => names.includes(w.toLowerCase()));
    if (match !== undefined) {
      const shown = top.card?.name && top.card.name.toLowerCase() === match.toLowerCase() ? top.card.name : top.name;
      const hit = live({ kind: 'watchlist', key: `watch:${top.id}`, detail: `${shown} is on your watchlist` });
      if (hit) return hit;
    }
  }

  if (bp.attacked) {
    const n = attackersOn(view, seat);
    if (n > 0) {
      const hit = live({ kind: 'attacked', key: `attacked:${view.turn}`, detail: `${n} ${n === 1 ? 'creature is' : 'creatures are'} attacking you` });
      if (hit) return hit;
    }
  }

  if (bp.stackDepth > 0 && stack.length >= bp.stackDepth && top !== null && !skipTop) {
    const hit = live({ kind: 'stack-depth', key: `depth:${top.id}`, detail: `${stack.length} objects are on the stack` });
    if (hit) return hit;
  }
  return null;
}
```

- [ ] **Step 4: Run to verify pass**

Run: `npx vitest run src/lib/breakpoints.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/breakpoints.ts web/src/lib/breakpoints.test.ts
git commit -m "feat(web): pure breakpoint check (targets me, watchlist, attacked, stack depth)"
```

---

### Task 3: decide() consults breakpoints

**Files:**
- Modify: `web/src/lib/autopilot.ts`
- Test: `web/src/lib/autopilot.test.ts`

**Interfaces:**
- Consumes: `checkBreakpoints`, `targetsSeat`, `BreakpointHit` (Task 2).
- Produces:
  - `StopReason` gains `'breakpoint'`.
  - `AutoVerdict`'s stop arm becomes `{ act: 'stop'; reason: StopReason; hit?: BreakpointHit }`.
  - `decide()` accepts `breakpointsFired?: ReadonlyMap<string, number>`.

**Placement rule:**
- Breakpoints are checked right after the master switch (step 1), before the stack rules.
- They apply to persistent Auto, to the one-shot runs and to ffwd, because a breakpoint is a stop the player set, like a step stop.
- `skipTop` is true when the top object is in the Resolve All baseline or its yield key is in `yields`.

- [ ] **Step 1: Write the failing test** (append to `autopilot.test.ts`)

```ts
import { noBreakpoints } from './playsettings';

describe('decide: breakpoints', () => {
  const quietD = (seq = 9): Decision => ({ seq, player: 0, kind: 'priority', prompt: 'p', min: 1, max: 1, options: [opt('pass', 0), opt('concede', 1)] } as Decision);
  const bolt = { id: 1, controller: 1, kind: 'spell', name: 'Bolt', text: '', optional: false, targets: [{ player: 0, is_player: true }] };
  const v = { active: 1, step: 'main1', turn: 3, stack: [bolt], players: [] } as unknown as View;
  const passing = (): PlaySettings => ({ ...defaultSettings(), opponentSpell: 'never' });

  it('breakpoints off: exactly today’s verdict', () => {
    expect(decide({ decision: quietD(), view: v, seat: 0, settings: passing() })).toEqual({ act: 'pass', index: 0 });
  });

  it('targets-me on: stops with the hit, even where the stack rule would pass', () => {
    const settings = { ...passing(), breakpoints: { ...noBreakpoints(), targetsMe: true } };
    const got = decide({ decision: quietD(), view: v, seat: 0, settings });
    expect(got).toEqual({ act: 'stop', reason: 'breakpoint', hit: { kind: 'targets-me', key: 'targets:1', detail: 'Bolt targets you' } });
  });

  it('a hit fired at an earlier seq passes', () => {
    const settings = { ...passing(), breakpoints: { ...noBreakpoints(), targetsMe: true } };
    const got = decide({ decision: quietD(9), view: v, seat: 0, settings, breakpointsFired: new Map([['targets:1', 8]]) });
    expect(got).toEqual({ act: 'pass', index: 0 });
  });

  it('a yielded top object skips the top-object breakpoints', () => {
    const settings = { ...passing(), breakpoints: { ...noBreakpoints(), targetsMe: true } };
    const yields = new Set([stackYieldKey(bolt)]);
    expect(decide({ decision: quietD(), view: v, seat: 0, settings, yields }).act).toBe('pass');
  });

  it('breakpoints stop an ffwd run too', () => {
    const settings = { ...passing(), breakpoints: { ...noBreakpoints(), targetsMe: true } };
    expect(decide({ decision: quietD(), view: v, seat: 0, settings, ffwd: true }).act).toBe('stop');
  });

  it('auto off still reports disabled, not breakpoint', () => {
    const settings = { ...passing(), autoPass: false, breakpoints: { ...noBreakpoints(), targetsMe: true } };
    expect(decide({ decision: quietD(), view: v, seat: 0, settings })).toEqual({ act: 'stop', reason: 'disabled' });
  });
});
```

(Add `import { stackYieldKey } from './yields';` to the file's imports.)

- [ ] **Step 2: Run to verify failure**

Run: `npx vitest run src/lib/autopilot.test.ts -t breakpoints`
Expected: FAIL. `decide` returns a pass where the test expects a breakpoint stop.

- [ ] **Step 3: Implement**

In `autopilot.ts`:
- Add the import: `import { checkBreakpoints, targetsSeat, type BreakpointHit } from './breakpoints';`
- Add `'breakpoint'` to `StopReason`.
- Change the stop arm of `AutoVerdict` to `{ act: 'stop'; reason: StopReason; hit?: BreakpointHit }`.
- Delete the local `targetsMe` function and its doc comment. At its one call site, use `targetsSeat(view, seat, top)`.
- Add the argument with a doc comment:

```ts
  /**
   * breakpointsFired maps a breakpoint key to the decision seq it first
   * stopped at (SeatPanelState owns it). A key fired at an earlier seq does
   * not stop again; the same seq does, so re-deriving one window is stable.
   */
  breakpointsFired?: ReadonlyMap<string, number>;
```

- Destructure `breakpointsFired = new Map<string, number>()`.
- Insert directly after the master-switch line (`if (!settings.autoPass && !ffwd) ...`):

```ts
  // 1b. Breakpoints: the player's pause-on-X rules (UI rework spec §1). They
  // stop ffwd and the one-shot runs too — like a step stop, they are the
  // player's own ask. The top-object rules respect a yield or a Resolve All
  // baseline: those are explicit consent for that object.
  const bpTop = view.stack.length > 0 ? view.stack[view.stack.length - 1] : null;
  const skipTop = bpTop !== null && ((baselineStack?.has(bpTop.id) ?? false) || (yields?.has(stackYieldKey(bpTop)) ?? false));
  const hit = checkBreakpoints({ view, seat, bp: settings.breakpoints, fired: breakpointsFired, seq: decision.seq, skipTop });
  if (hit !== null) return { act: 'stop', reason: 'breakpoint', hit };
```

- [ ] **Step 4: Run to verify pass**

Run: `npx vitest run src/lib/autopilot.test.ts src/lib/breakpoints.test.ts`
Expected: PASS, including every pre-existing autopilot test.

Then run `npx svelte-check --tsconfig ./tsconfig.json`. It will now report `WAITING_TEXT`/`RUN_WAITING_TEXT` in `seatpanel.svelte.ts` as missing the `'breakpoint'` key. That is expected, and Task 4 fixes it. No other errors are expected.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/autopilot.ts web/src/lib/autopilot.test.ts
git commit -m "feat(web): decide() stops on a breakpoint hit, once per fired window"
```

---

### Task 4: Seat state remembers hits and says why it paused

**Files:**
- Modify: `web/src/lib/seatpanel.svelte.ts`
- Test: `web/src/lib/seatpanel.breakpoints.test.ts` (create)

**Interfaces:**
- Consumes: `decide()`'s `breakpointsFired` argument and `hit` (Task 3).
- Produces:
  - `SeatPanelState` keeps a private `bpFired: Map<string, number>`, cleared in `begin()`.
  - The note `{ kind: 'waiting', reason: 'breakpoint', detail }` renders as `Auto paused here: <detail>.`
  - A stopped one-shot run reads `… a pause you set fired.`

- [ ] **Step 1: Write the failing test** (`seatpanel.breakpoints.test.ts`)

```ts
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Decision, Option, View } from '../protocol';
import { SeatPanelState, autoNoteText } from './seatpanel.svelte';
import { noBreakpoints } from './playsettings';

const { postIntentMock, fetchPendingMock } = vi.hoisted(() => ({ postIntentMock: vi.fn(), fetchPendingMock: vi.fn() }));
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  postIntent: postIntentMock,
  fetchPending: fetchPendingMock,
}));

const ctx = { seat: 0, token: 'tok' };
const pass = (i: number): Option => ({ index: i, kind: 'pass', label: 'Pass priority', player: 0 });
const concede = (i: number): Option => ({ index: i, kind: 'concede', label: 'Concede', player: 0 });
const quiet = (seq: number): Decision =>
  ({ seq, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1, options: [pass(0), concede(1)] });

/** a view with two opponent objects on the stack; the stack rule is set to 'never' so only the breakpoint can stop */
const deep = (): View =>
  ({
    active: 1, step: 'main1', turn: 4, players: [],
    stack: [
      { id: 1, controller: 1, kind: 'spell', name: 'A', text: '', optional: false, targets: [] },
      { id: 2, controller: 1, kind: 'spell', name: 'B', text: '', optional: false, targets: [] },
    ],
  }) as unknown as View;

async function settle(predicate: () => boolean, maxTicks = 200): Promise<void> {
  for (let i = 0; i < maxTicks; i++) {
    if (predicate()) return;
    await Promise.resolve();
  }
  throw new Error('settle: condition still false');
}

function seat(): SeatPanelState {
  const p = new SeatPanelState('t1', 1, ctx, null);
  p.stops = { yours: new Set(), opponents: new Set() };
  p.setAuto(true);
  p.setActPass(false);
  p.settings = {
    ...p.settings,
    opponentSpell: 'never',
    pacing: { stepMs: 0, resolveMs: 0 },
    breakpoints: { ...noBreakpoints(), stackDepth: 2 },
  };
  return p;
}

describe('SeatPanelState breakpoints', () => {
  beforeEach(() => {
    postIntentMock.mockReset();
    fetchPendingMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
  });

  it('stops the window, and says why in plain words', () => {
    const p = seat();
    p.adoptView(quiet(1));
    p.considerAuto(deep());
    expect(postIntentMock).not.toHaveBeenCalled();
    expect(autoNoteText(p.note)).toBe('Auto paused here: 2 objects are on the stack.');
  });

  it('re-deriving the same window keeps it stopped', () => {
    const p = seat();
    p.adoptView(quiet(1));
    p.considerAuto(deep());
    p.considerAuto(deep());
    p.considerAuto(deep());
    expect(postIntentMock).not.toHaveBeenCalled();
  });

  it('a later window for the same stack passes: the pause fired once', async () => {
    const p = seat();
    p.adoptView(quiet(1));
    p.considerAuto(deep());
    p.adoptView(quiet(2));
    p.considerAuto(deep());
    await settle(() => p.postedSeq === 2);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
  });

  it('begin() forgets fired pauses (a new match)', () => {
    const p = seat();
    p.adoptView(quiet(1));
    p.considerAuto(deep());
    p.begin();
    p.adoptView(quiet(2));
    p.considerAuto(deep());
    expect(postIntentMock).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run: `npx vitest run src/lib/seatpanel.breakpoints.test.ts`
Expected: FAIL. The note text is missing, and the second window stops again because nothing is recorded.

- [ ] **Step 3: Implement in `seatpanel.svelte.ts`**

1. Add `'breakpoint': 'Auto paused here: a pause you set fired.',` to `WAITING_TEXT`.
2. Add `'breakpoint': 'a pause you set fired.',` to `RUN_WAITING_TEXT`.
3. In `autoNoteText`'s `case 'waiting':`, before the `stop-set` branch:

```ts
      if (note.reason === 'breakpoint' && note.detail) return `Auto paused here: ${note.detail}.`;
```

4. Add the field next to the other session counters:

```ts
  /**
   * bpFired maps a breakpoint key (lib/breakpoints) to the decision seq it
   * first stopped at. decide() re-stops that same seq on every re-derive and
   * passes later windows for the same key. Session-scoped: begin() clears it.
   */
  private bpFired = new Map<string, number>();
```

5. In `begin()`, add `this.bpFired.clear();` beside the other counter resets.
6. In `derivePass`, widen the declared stop arm to `| { act: 'stop'; reason: StopReason; hit?: BreakpointHit }`, and pass `breakpointsFired: this.bpFired,` into its `decide({...})` call. Import the type: `import type { BreakpointHit } from './breakpoints';`
7. Also pass `breakpointsFired: this.bpFired` to the second `decide({...})` call, the one with `settings: { ...this.settings, autoPass: true }`.
8. In `considerAuto`'s stop branch, the block that begins `this.autoRun = 0;` and then sets `this.note`, record the hit first:

```ts
      if (verdict.reason === 'breakpoint' && verdict.hit) this.bpFired.set(verdict.hit.key, d.seq);
```

Then, in the non-run `else` branch, give breakpoints their detail. Replace the `this.note = labels.length > 0 ? ... : ...` assignment with:

```ts
        this.note = verdict.reason === 'breakpoint' && verdict.hit
          ? { kind: 'waiting', reason: 'breakpoint', detail: verdict.hit.detail }
          : labels.length > 0
          ? { kind: 'waiting', reason: verdict.reason, detail: labels.join(', ') }
          : { kind: 'waiting', reason: verdict.reason };
```

- [ ] **Step 4: Run to verify pass, plus the loop suites**

Run: `npx vitest run src/lib/seatpanel.breakpoints.test.ts src/lib/seatpanel.auto.test.ts src/lib/seatpanel.pacing.test.ts src/lib/seatpanel.prio6.test.ts`
Expected: PASS.

Then run `npx svelte-check --tsconfig ./tsconfig.json`. Expected: 0 errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/seatpanel.svelte.ts web/src/lib/seatpanel.breakpoints.test.ts
git commit -m "feat(web): seat state records breakpoint hits and names them in the auto note"
```

---

### Task 5: "Pause when…" editor

**Files:**
- Create: `web/src/components/BreakpointsSection.svelte`
- Create: `web/src/components/BreakpointsSection.svelte.test.ts`
- Modify: `web/src/components/PlaySettingsPanel.svelte` (mount it)

**Interfaces:**
- Consumes: `SeatPanelState.settings`, `SeatPanelState.editSettings(patch)`, `withWatch`, `withoutWatch`, `STACK_DEPTHS`, `MAX_WATCHLIST` (Task 1).
- Produces: `<BreakpointsSection state={SeatPanelState} />`.

- [ ] **Step 1: Write the failing test** (SSR, following `PlaySettingsPanel.svelte.test.ts`)

```ts
import { describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import { SeatPanelState } from '../lib/seatpanel.svelte';
import { noBreakpoints } from '../lib/playsettings';
import BreakpointsSection from './BreakpointsSection.svelte';

const ctx = { seat: 0, token: 'tok' };
const html = (s: SeatPanelState) => render(BreakpointsSection, { props: { state: s } }).html;

describe('BreakpointsSection', () => {
  it('renders all off by default: no checks, depth Off, empty watchlist message', () => {
    const h = html(new SeatPanelState('t', 1, ctx, null));
    expect(h).toContain('Pause when');
    expect(h).toMatch(/<input[^>]*data-bp="targets-me"(?![^>]*checked)[^>]*>/);
    expect(h).toMatch(/<input[^>]*data-bp="attacked"(?![^>]*checked)[^>]*>/);
    expect(h).toMatch(/<option[^>]*value="0"[^>]*selected[^>]*>Off<\/option>/);
    expect(h).toContain('No cards on your watchlist.');
  });

  it('reflects set breakpoints and lists watchlist names with a remove button each', () => {
    const s = new SeatPanelState('t', 1, ctx, null);
    s.settings = { ...s.settings, breakpoints: { ...noBreakpoints(), targetsMe: true, stackDepth: 3, watchlist: ["Thassa's Oracle", 'Cyclonic Rift'] } };
    const h = html(s);
    expect(h).toMatch(/<input[^>]*data-bp="targets-me"[^>]*checked[^>]*>/);
    expect(h).toMatch(/<option[^>]*value="3"[^>]*selected[^>]*>3 or more<\/option>/);
    expect((h.match(/data-watch-item/g) ?? []).length).toBe(2);
    expect(h).toContain('aria-label="Remove Cyclonic Rift from watchlist"');
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run: `npx vitest run src/components/BreakpointsSection.svelte.test.ts`
Expected: FAIL. The component does not exist.

- [ ] **Step 3: Implement the component**

```svelte
<script lang="ts">
  import type { SeatPanelState } from '../lib/seatpanel.svelte';
  import { MAX_WATCHLIST, STACK_DEPTHS, withWatch, withoutWatch, type Breakpoints } from '../lib/playsettings';

  let { state }: { state: SeatPanelState } = $props();
  const bp = $derived(state.settings.breakpoints);
  let draft = $state('');

  function set(patch: Partial<Breakpoints>): void {
    state.editSettings({ breakpoints: { ...bp, ...patch } });
  }
  function add(): void {
    const next = withWatch(bp.watchlist, draft);
    if (next.length !== bp.watchlist.length) set({ watchlist: next });
    draft = '';
  }
</script>

<section class="sec" data-breakpoints-section>
  <h3>Pause when…</h3>
  <p class="blurb">These stop automatic passing even when your other rules would pass. Each one pauses once for the thing it caught.</p>
  <label class="row chk">
    <input type="checkbox" data-bp="targets-me" checked={bp.targetsMe} onchange={(e) => set({ targetsMe: e.currentTarget.checked })} />
    <span>an opponent's spell or ability targets me or my permanent</span>
  </label>
  <label class="row chk">
    <input type="checkbox" data-bp="attacked" checked={bp.attacked} onchange={(e) => set({ attacked: e.currentTarget.checked })} />
    <span>creatures attack me</span>
  </label>
  <label class="row sel">
    <span>the stack holds</span>
    <select data-select="stack-depth" onchange={(e) => set({ stackDepth: Number(e.currentTarget.value) })}>
      {#each STACK_DEPTHS as d (d)}
        <option value={String(d)} selected={bp.stackDepth === d}>{d === 0 ? 'Off' : `${d} or more`}</option>
      {/each}
    </select>
  </label>
  <div class="watch">
    <span class="lbl">an opponent casts or activates a card on my watchlist</span>
    {#if bp.watchlist.length === 0}
      <p class="empty">No cards on your watchlist.</p>
    {:else}
      <ul class="chips">
        {#each bp.watchlist as name (name)}
          <li data-watch-item>
            {name}
            <button type="button" aria-label={`Remove ${name} from watchlist`} data-watch-remove onclick={() => set({ watchlist: withoutWatch(bp.watchlist, name) })}>×</button>
          </li>
        {/each}
      </ul>
    {/if}
    <form class="add" onsubmit={(e) => { e.preventDefault(); add(); }}>
      <input type="text" data-watch-input placeholder="Card name" maxlength="60" bind:value={draft} disabled={bp.watchlist.length >= MAX_WATCHLIST} />
      <button type="submit" data-watch-add disabled={draft.trim() === '' || bp.watchlist.length >= MAX_WATCHLIST}>Add</button>
    </form>
  </div>
</section>

<style>
  .blurb, .empty { color: var(--ink-dim); font-size: 12px; margin: 0 0 6px; }
  .chk { display: flex; gap: 8px; align-items: center; }
  .watch { margin-top: 8px; }
  .lbl { display: block; margin-bottom: 4px; }
  .chips { list-style: none; display: flex; flex-wrap: wrap; gap: 6px; padding: 0; margin: 0 0 6px; }
  .chips li { display: flex; align-items: center; gap: 4px; padding: 2px 4px 2px 10px; border-radius: 12px; border: 1px solid var(--edge-inst); }
  .chips button { border: 0; background: none; cursor: pointer; color: var(--ink-dim); }
  .add { display: flex; gap: 6px; }
</style>
```

Mount it in `PlaySettingsPanel.svelte`:
- import: `import BreakpointsSection from './BreakpointsSection.svelte';`
- mount: `<BreakpointsSection {state} />`, directly after the closing `</section>` of the "My own spells and abilities" section.

Use the prop name the panel actually receives. It is `state`; check its `$props()` line.

- [ ] **Step 4: Run to verify pass**

Run: `npx vitest run src/components/BreakpointsSection.svelte.test.ts src/components/PlaySettingsPanel.svelte.test.ts`
Expected: PASS.

Then run `npx svelte-check --tsconfig ./tsconfig.json && npx eslint src/components/BreakpointsSection.svelte`. Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/BreakpointsSection.svelte web/src/components/BreakpointsSection.svelte.test.ts web/src/components/PlaySettingsPanel.svelte
git commit -m "feat(web): Pause when… editor for breakpoints and the watchlist"
```

---

### Task 6: Keymap model; hotkeys match through it

**Files:**
- Create: `web/src/lib/keymap.ts`
- Create: `web/src/lib/keymap.test.ts`
- Modify: `web/src/lib/hotkeys.ts`

**Interfaces:**
- Produces:
  - `type KeyAction` (the union below)
  - `KEY_ACTIONS: readonly KeyAction[]`
  - `ACTION_LABELS: Record<KeyAction, string>`
  - `ACTION_GROUPS: readonly { title: string; actions: readonly KeyAction[] }[]`
  - `interface Binding { code: string; ctrl: boolean; shift: boolean; alt: boolean }`
  - `type Keymap = Record<KeyAction, Binding[]>`
  - `defaultKeymap(): Keymap`
  - `KEYMAP_KEY = 'gorge.keymap.v1'`
  - `loadKeymap(storage: Storage | null): Keymap`
  - `saveKeymap(storage: Storage | null, k: Keymap): void`
  - `validateKeymap(v: unknown): Keymap | null`
  - `bindingFromEvent(e: { code?: string; key: string; ctrlKey: boolean; shiftKey: boolean; altKey?: boolean; metaKey: boolean }): Binding | null`
  - `bindingLabel(b: Binding): string`
  - `eventCode(e: { code?: string; key: string }): string`
  - `matchKeymap(k: Keymap, e: HotkeyEvent): KeyAction | null`
  - `conflictsFor(k: Keymap, action: KeyAction): KeyAction[]`
  - `withBinding(k: Keymap, action: KeyAction, b: Binding): Keymap`
  - `withoutBinding(k: Keymap, action: KeyAction, i: number): Keymap`
  - `MAX_BINDINGS = 2`
- `hotkeys.ts`:
  - `HotkeyAction` becomes an alias of `KeyAction`;
  - `hotkeyAction(e, pickerOpen?, keymap?: Keymap)`;
  - `HotkeyEvent` gains `altKey?: boolean`.

- [ ] **Step 1: Write the failing test** (`keymap.test.ts`)

```ts
import { describe, expect, it } from 'vitest';
import {
  bindingFromEvent, bindingLabel, conflictsFor, defaultKeymap, eventCode, KEY_ACTIONS, loadKeymap, matchKeymap,
  saveKeymap, validateKeymap, withBinding, withoutBinding, KEYMAP_KEY,
} from './keymap';

const mem = (): Storage => {
  const m = new Map<string, string>();
  return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k), clear: () => m.clear(), key: () => null, length: 0 } as unknown as Storage;
};
const ev = (key: string, mods: Partial<{ ctrlKey: boolean; shiftKey: boolean; altKey: boolean; metaKey: boolean; code: string }> = {}) =>
  ({ key, ctrlKey: false, shiftKey: false, altKey: false, metaKey: false, ...mods });

describe('keymap', () => {
  it('defaults reproduce today’s bindings', () => {
    const k = defaultKeymap();
    expect(matchKeymap(k, ev(' '))).toBe('pass');
    expect(matchKeymap(k, ev('Enter'))).toBe('end-turn');
    expect(matchKeymap(k, ev('Enter', { shiftKey: true }))).toBe('hard-skip');
    expect(matchKeymap(k, ev('F', { ctrlKey: true, shiftKey: true }))).toBe('toggle-full-control');
    expect(matchKeymap(k, ev('O', { ctrlKey: true, shiftKey: true }))).toBe('toggle-options');
    expect(matchKeymap(k, ev('}', { ctrlKey: true, shiftKey: true, code: 'BracketRight' }))).toBe('next-profile');
    expect(matchKeymap(k, ev('{', { ctrlKey: true, shiftKey: true }))).toBe('prev-profile');
    expect(matchKeymap(k, ev('3'))).toBe('pick-3');
    expect(matchKeymap(k, ev('!', { ctrlKey: true, shiftKey: true, code: 'Digit1' }))).toBe('profile-1');
    expect(matchKeymap(k, ev('?', { shiftKey: true, code: 'Slash' }))).toBe('show-keys');
  });

  it('eventCode derives a physical code for synthetic events without one', () => {
    expect(eventCode(ev(' '))).toBe('Space');
    expect(eventCode(ev('f'))).toBe('KeyF');
    expect(eventCode(ev('7'))).toBe('Digit7');
    expect(eventCode(ev('}'))).toBe('BracketRight');
    expect(eventCode(ev('?'))).toBe('Slash');
  });

  it('bindingFromEvent refuses reserved chords: bare modifiers, Meta, Ctrl without Shift', () => {
    expect(bindingFromEvent(ev('Shift', { shiftKey: true, code: 'ShiftLeft' }))).toBeNull();
    expect(bindingFromEvent(ev('k', { metaKey: true, code: 'KeyK' }))).toBeNull();
    expect(bindingFromEvent(ev('k', { ctrlKey: true, code: 'KeyK' }))).toBeNull();
    expect(bindingFromEvent(ev('k', { ctrlKey: true, shiftKey: true, code: 'KeyK' }))).toEqual({ code: 'KeyK', ctrl: true, shift: true, alt: false });
  });

  it('labels read like keys', () => {
    expect(bindingLabel({ code: 'KeyF', ctrl: true, shift: true, alt: false })).toBe('Ctrl+Shift+F');
    expect(bindingLabel({ code: 'Space', ctrl: false, shift: false, alt: false })).toBe('Space');
    expect(bindingLabel({ code: 'BracketRight', ctrl: true, shift: true, alt: false })).toBe('Ctrl+Shift+]');
    expect(bindingLabel({ code: 'Digit4', ctrl: false, shift: false, alt: false })).toBe('4');
  });

  it('withBinding caps at two per action; withoutBinding removes by position', () => {
    let k = defaultKeymap();
    k = withBinding(k, 'undo', { code: 'KeyU', ctrl: false, shift: false, alt: false });
    k = withBinding(k, 'undo', { code: 'KeyY', ctrl: false, shift: false, alt: false });
    expect(k.undo.length).toBe(2);
    // cap of two: the default Ctrl+Shift+Z dropped when Y arrived
    expect(k.undo.map(bindingLabel)).toEqual(['U', 'Y']);
    k = withoutBinding(k, 'undo', 0);
    expect(k.undo.map(bindingLabel)).toEqual(['Y']);
  });

  it('conflictsFor names other actions sharing a chord', () => {
    const k = withBinding(defaultKeymap(), 'undo', { code: 'Space', ctrl: false, shift: false, alt: false });
    expect(conflictsFor(k, 'undo')).toEqual(['pass']);
    expect(conflictsFor(defaultKeymap(), 'pass')).toEqual([]);
  });

  it('save stores only overrides; load merges them onto defaults; corrupt storage yields defaults', () => {
    const st = mem();
    const k = withBinding(defaultKeymap(), 'resolve-all', { code: 'KeyR', ctrl: false, shift: false, alt: false });
    saveKeymap(st, k);
    const raw = JSON.parse(st.getItem(KEYMAP_KEY)!);
    expect(Object.keys(raw.overrides)).toEqual(['resolve-all']);
    expect(loadKeymap(st)).toEqual(k);
    st.setItem(KEYMAP_KEY, '{bad');
    expect(loadKeymap(st)).toEqual(defaultKeymap());
  });

  it('validateKeymap drops unknown actions and invalid bindings, keeps the rest', () => {
    const got = validateKeymap({ version: 1, overrides: { undo: [{ code: 'KeyU', ctrl: false, shift: false, alt: false }], nope: [], pass: [{ code: 'KeyK', ctrl: true, shift: false, alt: false }] } });
    expect(got?.undo).toEqual([{ code: 'KeyU', ctrl: false, shift: false, alt: false }]);
    expect(got?.pass).toEqual(defaultKeymap().pass);
    expect(validateKeymap({ version: 2, overrides: {} })).toBeNull();
  });

  it('every action has a label and belongs to exactly one group', async () => {
    const { ACTION_LABELS, ACTION_GROUPS } = await import('./keymap');
    const grouped = ACTION_GROUPS.flatMap((g) => g.actions);
    expect([...grouped].sort()).toEqual([...KEY_ACTIONS].sort());
    for (const a of KEY_ACTIONS) expect(ACTION_LABELS[a].length).toBeGreaterThan(0);
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run: `npx vitest run src/lib/keymap.test.ts`
Expected: FAIL. The module does not exist.

- [ ] **Step 3: Implement `keymap.ts`**

```ts
/**
 * keymap is the pure, rebindable binding table behind the table's hotkeys
 * (UI rework spec §1). hotkeys.ts keeps the guards (Meta, modal pickers,
 * focused inputs, the Escape panic key) and asks matchKeymap which action a
 * chord means. Bindings match the PHYSICAL key (KeyboardEvent.code), so a
 * chord survives Shift changing the printable key. Ctrl without Shift is
 * reserved: Ctrl held alone is the hold-priority click modifier.
 */
import type { HotkeyEvent } from './hotkeys';

type Nine = 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9;
export type KeyAction =
  | 'pass' | 'end-turn' | 'hard-skip' | 'cancel-run' | 'undo' | 'resolve-all'
  | 'confirm' | `pick-${Nine}`
  | 'toggle-full-control' | 'next-profile' | 'prev-profile' | `profile-${Nine}`
  | 'toggle-options' | 'show-keys';

const NINE: Nine[] = [1, 2, 3, 4, 5, 6, 7, 8, 9];
const PICKS = NINE.map((n) => `pick-${n}` as const);
const PROFILES = NINE.map((n) => `profile-${n}` as const);

export const ACTION_GROUPS: readonly { title: string; actions: readonly KeyAction[] }[] = [
  { title: 'Priority', actions: ['pass', 'end-turn', 'hard-skip', 'cancel-run', 'undo', 'resolve-all'] },
  { title: 'Decisions', actions: ['confirm', ...PICKS] },
  { title: 'Profiles', actions: ['toggle-full-control', 'next-profile', 'prev-profile', ...PROFILES] },
  { title: 'View', actions: ['toggle-options', 'show-keys'] },
];
export const KEY_ACTIONS: readonly KeyAction[] = ACTION_GROUPS.flatMap((g) => g.actions);

export const ACTION_LABELS: Record<KeyAction, string> = {
  'pass': 'Pass priority once',
  'end-turn': 'End turn (pass until something needs me)',
  'hard-skip': 'Skip the rest of the turn',
  'cancel-run': 'Stop an End Turn / Skip / Resolve All run',
  'undo': 'Undo',
  'resolve-all': 'Resolve the whole stack',
  'confirm': 'Confirm the current choice (Done)',
  ...Object.fromEntries(PICKS.map((a, i) => [a, `Pick option ${i + 1}`])),
  'toggle-full-control': 'Toggle full control',
  'next-profile': 'Next flow profile',
  'prev-profile': 'Previous flow profile',
  ...Object.fromEntries(PROFILES.map((a, i) => [a, `Switch to flow profile ${i + 1}`])),
  'toggle-options': 'Open or close game options',
  'show-keys': 'Show the keyboard shortcuts',
} as Record<KeyAction, string>;

export interface Binding {
  code: string;
  ctrl: boolean;
  shift: boolean;
  alt: boolean;
}
export type Keymap = Record<KeyAction, Binding[]>;

export const MAX_BINDINGS = 2;
export const KEYMAP_KEY = 'gorge.keymap.v1';

const b = (code: string, mods: Partial<Omit<Binding, 'code'>> = {}): Binding => ({ code, ctrl: false, shift: false, alt: false, ...mods });
const CS = { ctrl: true, shift: true };

export function defaultKeymap(): Keymap {
  const k = Object.fromEntries(KEY_ACTIONS.map((a) => [a, [] as Binding[]])) as Keymap;
  k['pass'] = [b('Space')];
  k['end-turn'] = [b('Enter')];
  k['hard-skip'] = [b('Enter', { shift: true })];
  k['cancel-run'] = [b('Escape')];
  k['undo'] = [b('KeyZ', CS)];
  k['toggle-full-control'] = [b('KeyF', CS)];
  k['toggle-options'] = [b('KeyO', CS)];
  k['next-profile'] = [b('BracketRight', CS)];
  k['prev-profile'] = [b('BracketLeft', CS)];
  k['show-keys'] = [b('Slash', { shift: true })];
  NINE.forEach((n) => {
    k[`pick-${n}`] = [b(`Digit${n}`)];
    k[`profile-${n}`] = [b(`Digit${n}`, CS)];
  });
  return k;
}

const SHIFTED: Record<string, string> = { '}': 'BracketRight', ']': 'BracketRight', '{': 'BracketLeft', '[': 'BracketLeft', '?': 'Slash', '/': 'Slash' };

/** eventCode is the event's physical code, or one derived from `key` for a synthetic event that omits it. */
export function eventCode(e: { code?: string; key: string }): string {
  if (e.code) return e.code;
  if (e.key === ' ') return 'Space';
  if (e.key in SHIFTED) return SHIFTED[e.key];
  if (/^[a-z]$/i.test(e.key)) return `Key${e.key.toUpperCase()}`;
  if (/^[0-9]$/.test(e.key)) return `Digit${e.key}`;
  return e.key;
}

const same = (x: Binding, y: Binding) => x.code === y.code && x.ctrl === y.ctrl && x.shift === y.shift && x.alt === y.alt;
const MODIFIER_CODE = /^(Shift|Control|Alt|Meta|OS)(Left|Right)?$/;

function validBinding(v: unknown): v is Binding {
  if (typeof v !== 'object' || v === null) return false;
  const o = v as Record<string, unknown>;
  if (typeof o.code !== 'string' || !/^[A-Za-z0-9]{1,24}$/.test(o.code) || MODIFIER_CODE.test(o.code)) return false;
  if (typeof o.ctrl !== 'boolean' || typeof o.shift !== 'boolean' || typeof o.alt !== 'boolean') return false;
  return !(o.ctrl && !o.shift);
}

/** bindingFromEvent turns a captured keydown into a binding, or null for a reserved chord. */
export function bindingFromEvent(e: { code?: string; key: string; ctrlKey: boolean; shiftKey: boolean; altKey?: boolean; metaKey: boolean }): Binding | null {
  if (e.metaKey) return null;
  const cand = { code: eventCode(e), ctrl: e.ctrlKey, shift: e.shiftKey, alt: e.altKey ?? false };
  return validBinding(cand) ? cand : null;
}

const CODE_LABEL: Record<string, string> = { BracketRight: ']', BracketLeft: '[', Slash: '/', Space: 'Space', Enter: 'Enter', Escape: 'Esc', Backquote: '`', Minus: '-', Equal: '=', Comma: ',', Period: '.', Semicolon: ';', Quote: "'", Backslash: '\\' };

export function bindingLabel(x: Binding): string {
  const key = CODE_LABEL[x.code] ?? x.code.replace(/^Key/, '').replace(/^Digit/, '').replace(/^Numpad/, 'Num ');
  return [x.ctrl && 'Ctrl', x.alt && 'Alt', x.shift && 'Shift', key].filter(Boolean).join('+');
}

export function matchKeymap(k: Keymap, e: HotkeyEvent): KeyAction | null {
  const chord: Binding = { code: eventCode(e), ctrl: e.ctrlKey, shift: e.shiftKey, alt: e.altKey ?? false };
  for (const a of KEY_ACTIONS) if (k[a].some((x) => same(x, chord))) return a;
  return null;
}

export function conflictsFor(k: Keymap, action: KeyAction): KeyAction[] {
  return KEY_ACTIONS.filter((a) => a !== action && k[a].some((x) => k[action].some((y) => same(x, y))));
}

export function withBinding(k: Keymap, action: KeyAction, x: Binding): Keymap {
  if (!validBinding(x) || k[action].some((y) => same(x, y))) return k;
  const list = [...k[action], x].slice(-MAX_BINDINGS);
  return { ...k, [action]: list };
}

export function withoutBinding(k: Keymap, action: KeyAction, i: number): Keymap {
  return { ...k, [action]: k[action].filter((_, j) => j !== i) };
}

/** validateKeymap reads a stored {version:1, overrides} blob onto the defaults; unknown actions and bad bindings are dropped, never fatal. */
export function validateKeymap(v: unknown): Keymap | null {
  if (typeof v !== 'object' || v === null) return null;
  const o = v as Record<string, unknown>;
  if (o.version !== 1 || typeof o.overrides !== 'object' || o.overrides === null) return null;
  const k = defaultKeymap();
  for (const [a, list] of Object.entries(o.overrides as Record<string, unknown>)) {
    if (!(KEY_ACTIONS as readonly string[]).includes(a) || !Array.isArray(list)) continue;
    const good = list.filter(validBinding).slice(0, MAX_BINDINGS);
    if (good.length !== list.length) continue; // a partly-bad entry keeps the default
    k[a as KeyAction] = good.map((x) => ({ code: x.code, ctrl: x.ctrl, shift: x.shift, alt: x.alt }));
  }
  return k;
}

/** overridesOf is the difference from defaults: only changed actions are stored or exported. */
export function overridesOf(k: Keymap): Partial<Keymap> {
  const d = defaultKeymap();
  const out: Partial<Keymap> = {};
  for (const a of KEY_ACTIONS) {
    const same2 = k[a].length === d[a].length && k[a].every((x, i) => same(x, d[a][i]));
    if (!same2) out[a] = k[a];
  }
  return out;
}

export function loadKeymap(storage: Storage | null): Keymap {
  try {
    const raw = storage?.getItem(KEYMAP_KEY) ?? null;
    if (raw === null) return defaultKeymap();
    return validateKeymap(JSON.parse(raw)) ?? defaultKeymap();
  } catch {
    return defaultKeymap();
  }
}

export function saveKeymap(storage: Storage | null, k: Keymap): void {
  try {
    storage?.setItem(KEYMAP_KEY, JSON.stringify({ version: 1, overrides: overridesOf(k) }));
  } catch {
    /* private mode or quota: keep the in-memory copy */
  }
}
```

Then refactor `hotkeys.ts`:
- Update the module doc to say the grammar's chords now come from the keymap.
- Add `altKey?: boolean;` to `HotkeyEvent`.
- Replace the `HotkeyAction` union with `export type HotkeyAction = KeyAction;` and import `KeyAction`, `Keymap`, `defaultKeymap` and `matchKeymap` from `./keymap`.
- Replace `hotkeyAction`'s body after the focus guard:

```ts
export function hotkeyAction(
  e: HotkeyEvent,
  pickerOpen: () => boolean = () => false,
  keymap: Keymap = DEFAULTS,
): HotkeyAction | null {
  if (e.metaKey) return null;
  if (pickerOpen()) return null;
  if (e.key === 'Escape') return 'cancel-run';
  const t = e.target as { closest?: (sel: string) => unknown } | null | undefined;
  if (t && typeof t.closest === 'function' && t.closest('button, a, input, textarea, select, [contenteditable]')) return null;
  if (e.ctrlKey && !e.shiftKey) return null; // Ctrl alone is the hold-priority modifier, never a hotkey
  return matchKeymap(keymap, e);
}
const DEFAULTS = defaultKeymap();
```

(Declare `DEFAULTS` above the function.)

- [ ] **Step 4: Run to verify pass**

Run: `npx vitest run src/lib/keymap.test.ts src/lib/hotkeys.test.ts src/lib/hotkeys-profiles.test.ts`
Expected: PASS, with `hotkeys.test.ts` passing unchanged.

If a `hotkeys.test.ts` case asserts that a digit or `?` returns `null`, keep that behaviour: remove that default binding from `defaultKeymap`. Don't edit the test, and say so in the report.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/keymap.ts web/src/lib/keymap.test.ts web/src/lib/hotkeys.ts
git commit -m "feat(web): rebindable keymap; hotkeys match through it with today's defaults"
```

---

### Task 7: Keymap store, and dispatch of the new actions

**Files:**
- Create: `web/src/lib/keymap.svelte.ts`
- Modify: `web/src/lib/seatpanel.svelte.ts` (add `applyProfileAt`, `pickHotkey`)
- Modify: `web/src/components/HotButtonStrip.svelte`
- Test: `web/src/lib/keymap.store.test.ts` (create) and `web/src/lib/hotkeys-profiles.test.ts` (append)

**Interfaces:**
- Consumes: Task 6.
- Produces:
  - the store:
    - `class KeymapStore { current: Keymap; capturing: boolean; constructor(storage?: Storage | null) }`;
    - its methods `add(a, b)`, `remove(a, i)`, `reset()` and `replace(k)`, each of which persists;
    - `export const keymapStore = new KeymapStore()`.
  - `SeatPanelState.applyProfileAt(i: number, presetIds: readonly PresetName[]): string | null`: 0-based over the same list `cycleProfile` walks.
  - `SeatPanelState.pickHotkey(n: number): boolean`: answers option n (1-based) of a pending non-priority decision through `click()`.

- [ ] **Step 1: Write the failing tests**

`keymap.store.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { KeymapStore } from './keymap.svelte';
import { defaultKeymap, KEYMAP_KEY } from './keymap';

const mem = (): Storage => {
  const m = new Map<string, string>();
  return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: () => {}, clear: () => {}, key: () => null, length: 0 } as unknown as Storage;
};

describe('KeymapStore', () => {
  it('loads defaults, persists an added binding, resets', () => {
    const st = mem();
    const s = new KeymapStore(st);
    expect(s.current).toEqual(defaultKeymap());
    s.add('resolve-all', { code: 'KeyR', ctrl: false, shift: false, alt: false });
    expect(new KeymapStore(st).current['resolve-all']).toEqual([{ code: 'KeyR', ctrl: false, shift: false, alt: false }]);
    s.reset();
    expect(JSON.parse(st.getItem(KEYMAP_KEY)!).overrides).toEqual({});
  });
});
```

Append to `hotkeys-profiles.test.ts`, reusing that file's existing seat builder. Look at how it constructs a `SeatPanelState` and saves profiles, and follow it:

```ts
it('applyProfileAt(i) walks the same list as the cycle: presets first, then saved profiles', () => {
  const p = new SeatPanelState('t', 1, { seat: 0, token: 'tok' }, null);
  p.saveProfile('Mine');
  expect(p.applyProfileAt(0, ['casual', 'no-tells', 'full-control'])).toBe('casual');
  expect(p.applyProfileAt(3, ['casual', 'no-tells', 'full-control'])).toBe('Mine');
  expect(p.applyProfileAt(9, ['casual', 'no-tells', 'full-control'])).toBeNull();
});
```

(If `saveProfile` takes a different signature in this codebase, call it the way the existing tests in this file do.)

Append to `seatpanel.pick.test.ts`, following its existing decision builders:

```ts
it('pickHotkey answers option n of a non-priority decision and ignores priority windows', () => {
  const p = new SeatPanelState('t', 1, ctx, null);
  p.adoptView(d); // this file's blockers decision: min 0, max 4, so a pick toggles, never posts
  expect(p.pickHotkey(1)).toBe(true);
  expect(p.picked).toEqual([0]);
  expect(p.pickHotkey(9)).toBe(false); // no ninth option
  const prio: Decision = { seq: 4, player: 0, kind: 'priority', prompt: 'p', min: 1, max: 1, options: [{ index: 0, kind: 'pass', label: 'Pass', player: 0 }] };
  p.adoptView(prio);
  expect(p.pickHotkey(1)).toBe(false);
});
```

`d` and `ctx` are that file's existing fixtures. If `adoptView` needs extra arguments there, call it the way the file's other `SeatPanelState` tests do.

- [ ] **Step 2: Run to verify failure**

Run: `npx vitest run src/lib/keymap.store.test.ts src/lib/hotkeys-profiles.test.ts src/lib/seatpanel.pick.test.ts`
Expected: FAIL. `KeymapStore`, `applyProfileAt` and `pickHotkey` are missing.

- [ ] **Step 3: Implement**

`keymap.svelte.ts`:

```ts
import { defaultKeymap, loadKeymap, saveKeymap, withBinding, withoutBinding, type Binding, type KeyAction, type Keymap } from './keymap';
import { safeStorage } from './storage';

/**
 * KeymapStore is the reactive shell over lib/keymap (the pure model), the
 * same split layoutsettings.svelte.ts uses. `capturing` is true while the
 * editor waits for a chord: the table's hotkey listener must stand down then,
 * or the chord being recorded would also fire.
 */
export class KeymapStore {
  #storage: Storage | null;
  current = $state<Keymap>(defaultKeymap());
  capturing = $state(false);

  constructor(storage: Storage | null | undefined = undefined) {
    this.#storage = storage !== undefined ? storage : safeStorage();
    this.current = loadKeymap(this.#storage);
  }
  #set(k: Keymap) {
    this.current = k;
    saveKeymap(this.#storage, k);
  }
  add(a: KeyAction, b: Binding) { this.#set(withBinding(this.current, a, b)); }
  remove(a: KeyAction, i: number) { this.#set(withoutBinding(this.current, a, i)); }
  reset() { this.#set(defaultKeymap()); }
  replace(k: Keymap) { this.#set(k); }
}

export const keymapStore = new KeymapStore();
```

In `seatpanel.svelte.ts`, next to `cycleProfile`:

```ts
  /** applyProfileAt applies entry i (0-based) of the cycle's list — presets first, then saved profiles — for the profile-N hotkeys. */
  applyProfileAt(i: number, presetIds: readonly PresetName[]): string | null {
    const saved = listProfiles(this.profiles);
    if (i < 0 || i >= presetIds.length + saved.length) return null;
    if (i < presetIds.length) {
      this.applyNamedPreset(presetIds[i]);
      return presetIds[i];
    }
    const name = saved[i - presetIds.length];
    this.applyProfile(name);
    return name;
  }

  /** pickHotkey answers option n (1-based, in the order the panel lists them) of a pending NON-priority decision, exactly as clicking it would. */
  pickHotkey(n: number): boolean {
    const d = this.pending;
    if (d === null || d.kind === 'priority' || d.seq === this.postedSeq || this.busy) return false;
    const o = d.options[n - 1];
    if (o === undefined) return false;
    this.click(o.index);
    return true;
  }
```

(Check `click`'s real signature in this file and call it the way the panel's option buttons do. If the panel lists options in a different order than `d.options`, use that order and say so in a comment.)

In `HotButtonStrip.svelte`:
- Import `keymapStore` and `KeyCheatSheet` (the latter from Task 8; for this task, add the import and markup at the Task 8 step instead).
- In `onKey`:
  - first line: `if (keymapStore.capturing) return;`
  - call `hotkeyAction(e, modalPickerOpen, keymapStore.current)`.
- Add the cases:

```ts
        case 'undo':
          if (!undoAllowed) return;
          void undo();
          break;
        case 'resolve-all':
          if (!resolveAllAvailable) return;
          logic.startResolveAll(view);
          logic.considerAuto(view);
          break;
        case 'confirm':
          if (!doneAvailable) return;
          logic.submit(false);
          break;
        case 'show-keys':
          showKeys = !showKeys;
          break;
        default:
          if (action.startsWith('pick-')) {
            if (!logic.pickHotkey(Number(action.slice(5)))) return;
          } else if (action.startsWith('profile-')) {
            if (logic.applyProfileAt(Number(action.slice(8)) - 1, PRESET_CYCLE) === null) return;
          }
          break;
```

- Declare `let showKeys = $state(false);` with the other component state.
- Move `e.preventDefault()` below the switch, so a key whose action is unavailable (the `return` paths) is not swallowed. Restructure as: compute `action`; `if (action === null) return;`; run the switch, where every unavailable path `return`s; after the switch, `e.preventDefault(); clientBreadcrumbs.record(...)`. Verify this does not change today's behaviour: `hotkeys.test.ts` passes, and the existing strip tests pass.

- [ ] **Step 4: Run to verify pass**

Run: `npx vitest run src/lib/keymap.store.test.ts src/lib/hotkeys-profiles.test.ts src/lib/seatpanel.pick.test.ts src/lib/hotkeys.test.ts src/components`
Expected: PASS.

Then run `npx svelte-check --tsconfig ./tsconfig.json`. Expected: 0 errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/keymap.svelte.ts web/src/lib/keymap.store.test.ts web/src/lib/seatpanel.svelte.ts web/src/lib/hotkeys-profiles.test.ts web/src/lib/seatpanel.pick.test.ts web/src/components/HotButtonStrip.svelte
git commit -m "feat(web): keymap store; hotkeys for undo, resolve all, confirm, pick N, profile N"
```

---

### Task 8: Keys editor and the `?` cheat sheet

**Files:**
- Create: `web/src/components/KeymapEditor.svelte`
- Create: `web/src/components/KeyCheatSheet.svelte`
- Create: `web/src/components/KeymapEditor.svelte.test.ts`
- Modify: `web/src/components/PlaySettingsPanel.svelte` (mount the editor)
- Modify: `web/src/components/HotButtonStrip.svelte` (mount the sheet)

**Interfaces:**
- Consumes: `keymapStore` (Task 7) and `ACTION_GROUPS`, `ACTION_LABELS`, `bindingLabel`, `bindingFromEvent`, `conflictsFor` (Task 6).
- Produces:
  - `<KeymapEditor store={KeymapStore} />`
  - `<KeyCheatSheet open={boolean} keymap={Keymap} onClose={() => void} />`

- [ ] **Step 1: Write the failing test** (SSR)

```ts
import { describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import KeymapEditor from './KeymapEditor.svelte';
import KeyCheatSheet from './KeyCheatSheet.svelte';
import { KeymapStore } from '../lib/keymap.svelte';
import { defaultKeymap, withBinding } from '../lib/keymap';

describe('KeymapEditor', () => {
  it('lists every group and shows default chords as key caps', () => {
    const h = render(KeymapEditor, { props: { store: new KeymapStore(null) } }).html;
    for (const g of ['Priority', 'Decisions', 'Profiles', 'View']) expect(h).toContain(`>${g}<`);
    expect(h).toMatch(/data-key-action="pass"[\s\S]*?<kbd>Space<\/kbd>/);
    expect(h).toMatch(/data-key-action="toggle-full-control"[\s\S]*?<kbd>Ctrl\+Shift\+F<\/kbd>/);
    expect(h).toContain('Reset all keys');
  });

  it('warns on a conflict', () => {
    const s = new KeymapStore(null);
    s.replace(withBinding(defaultKeymap(), 'undo', { code: 'Space', ctrl: false, shift: false, alt: false }));
    const h = render(KeymapEditor, { props: { store: s } }).html;
    expect(h).toContain('Also used by: Pass priority once');
  });
});

describe('KeyCheatSheet', () => {
  it('renders nothing closed, a labelled dialog open', () => {
    expect(render(KeyCheatSheet, { props: { open: false, keymap: defaultKeymap(), onClose: () => {} } }).html).not.toContain('role="dialog"');
    const h = render(KeyCheatSheet, { props: { open: true, keymap: defaultKeymap(), onClose: () => {} } }).html;
    expect(h).toContain('role="dialog"');
    expect(h).toContain('Keyboard shortcuts');
    expect(h).toContain('<kbd>Space</kbd>');
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run: `npx vitest run src/components/KeymapEditor.svelte.test.ts`
Expected: FAIL. The components do not exist.

- [ ] **Step 3: Implement**

`KeymapEditor.svelte`:

```svelte
<script lang="ts">
  import type { KeymapStore } from '../lib/keymap.svelte';
  import { ACTION_GROUPS, ACTION_LABELS, bindingFromEvent, bindingLabel, conflictsFor, MAX_BINDINGS, type KeyAction } from '../lib/keymap';

  let { store }: { store: KeymapStore } = $props();
  let waiting = $state<KeyAction | null>(null);
  let refused = $state(false);

  function capture(a: KeyAction): void {
    waiting = a;
    refused = false;
    store.capturing = true;
    const onKey = (e: KeyboardEvent): void => {
      e.preventDefault();
      e.stopImmediatePropagation();
      if (e.key === 'Escape') return done();
      const b = bindingFromEvent(e);
      if (b === null) {
        // a bare modifier keeps waiting; a reserved chord is refused
        if (!/^(Shift|Control|Alt|Meta)/.test(e.code)) refused = true;
        return;
      }
      store.add(a, b);
      done();
    };
    const done = (): void => {
      window.removeEventListener('keydown', onKey, true);
      store.capturing = false;
      waiting = null;
    };
    window.addEventListener('keydown', onKey, true);
  }
</script>

<section class="sec" data-keymap-editor>
  <h3>Keys</h3>
  {#each ACTION_GROUPS as g (g.title)}
    <h4>{g.title}</h4>
    <ul class="rows">
      {#each g.actions as a (a)}
        {@const clash = conflictsFor(store.current, a)}
        <li data-key-action={a}>
          <span class="lbl">{ACTION_LABELS[a]}</span>
          <span class="caps">
            {#each store.current[a] as b, i (i)}
              <span class="cap"><kbd>{bindingLabel(b)}</kbd><button type="button" aria-label={`Remove ${bindingLabel(b)} from ${ACTION_LABELS[a]}`} onclick={() => store.remove(a, i)}>×</button></span>
            {/each}
            {#if waiting === a}
              <span class="wait" aria-live="polite">{refused ? 'That chord is reserved — try another, or Esc' : 'Press a key… (Esc cancels)'}</span>
            {:else if store.current[a].length < MAX_BINDINGS}
              <button type="button" class="add" onclick={() => capture(a)}>Add key</button>
            {/if}
          </span>
          {#if clash.length > 0}
            <span class="warn">Also used by: {clash.map((c) => ACTION_LABELS[c]).join(', ')}</span>
          {/if}
        </li>
      {/each}
    </ul>
  {/each}
  <button type="button" class="reset" onclick={() => store.reset()}>Reset all keys</button>
</section>

<style>
  h4 { margin: 10px 0 4px; font-size: 13px; color: var(--ink-dim); font-weight: 500; }
  .rows { list-style: none; margin: 0; padding: 0; }
  .rows li { display: grid; grid-template-columns: 1fr auto; gap: 2px 10px; padding: 3px 0; align-items: center; }
  .caps { display: flex; gap: 6px; align-items: center; }
  .cap { display: inline-flex; align-items: center; gap: 2px; }
  .cap button { border: 0; background: none; cursor: pointer; color: var(--ink-dim); }
  kbd { font-family: var(--font-data); font-size: 11.5px; padding: 1px 6px; border-radius: 4px; border: 1px solid var(--edge-inst); }
  .warn { grid-column: 1 / -1; color: var(--danger); font-size: 12px; }
  .wait { font-size: 12px; color: var(--ink-dim); }
</style>
```

`KeyCheatSheet.svelte`:

```svelte
<script lang="ts">
  import { ACTION_GROUPS, ACTION_LABELS, bindingLabel, type Keymap } from '../lib/keymap';
  let { open, keymap, onClose }: { open: boolean; keymap: Keymap; onClose: () => void } = $props();
</script>

{#if open}
  <div class="sheet" role="dialog" aria-modal="true" aria-labelledby="keys-title" tabindex="-1"
       onkeydown={(e) => { if (e.key === 'Escape' || e.key === '?') { e.preventDefault(); onClose(); } }}>
    <h2 id="keys-title">Keyboard shortcuts</h2>
    <div class="cols">
      {#each ACTION_GROUPS as g (g.title)}
        <div>
          <h3>{g.title}</h3>
          <dl>
            {#each g.actions.filter((a) => keymap[a].length > 0) as a (a)}
              <dt>{#each keymap[a] as b, i (i)}<kbd>{bindingLabel(b)}</kbd>{/each}</dt>
              <dd>{ACTION_LABELS[a]}</dd>
            {/each}
          </dl>
        </div>
      {/each}
    </div>
    <button type="button" onclick={onClose}>Close</button>
  </div>
{/if}

<style>
  .sheet { position: fixed; inset: 10% 15%; z-index: 100; overflow: auto; padding: 20px 24px; border-radius: 12px; background: var(--instrument-raised); border: 1px solid var(--edge-inst); box-shadow: 0 20px 60px rgba(0,0,0,.6); }
  .cols { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 16px; }
  dl { display: grid; grid-template-columns: auto 1fr; gap: 4px 10px; margin: 0; }
  dt { display: flex; gap: 4px; }
  dd { margin: 0; color: var(--ink-dim); }
  kbd { font-family: var(--font-data); font-size: 11.5px; padding: 1px 6px; border-radius: 4px; border: 1px solid var(--edge-inst); }
</style>
```

Mount the editor in `PlaySettingsPanel.svelte`, after `BreakpointsSection`:
- `import KeymapEditor from './KeymapEditor.svelte';`
- `import { keymapStore } from '../lib/keymap.svelte';`
- `<KeymapEditor store={keymapStore} />`

Mount the sheet in `HotButtonStrip.svelte`, at the end of the markup: `<KeyCheatSheet open={showKeys} keymap={keymapStore.current} onClose={() => (showKeys = false)} />`. Focus the dialog when it opens, e.g. with `$effect` plus `bind:this` or a `use:` action, so Escape reaches its handler.

- [ ] **Step 4: Run to verify pass**

Run: `npx vitest run src/components/KeymapEditor.svelte.test.ts src/components/PlaySettingsPanel.svelte.test.ts`
Expected: PASS.

Then run `npx svelte-check --tsconfig ./tsconfig.json && npx eslint src/components/KeymapEditor.svelte src/components/KeyCheatSheet.svelte`. Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/KeymapEditor.svelte web/src/components/KeyCheatSheet.svelte web/src/components/KeymapEditor.svelte.test.ts web/src/components/PlaySettingsPanel.svelte web/src/components/HotButtonStrip.svelte
git commit -m "feat(web): Keys editor with capture and conflict warnings; ? shortcut sheet"
```

---

### Task 9: Export and import to file

**Files:**
- Create: `web/src/lib/transfer.ts`
- Create: `web/src/lib/transfer.test.ts`
- Create: `web/src/lib/download.ts`
- Modify: `web/src/lib/seatpanel.svelte.ts` (`exportProfilesText`, `importProfilesText`)
- Modify: `web/src/components/PlaySettingsPanel.svelte` (profile Export/Import buttons)
- Modify: `web/src/components/KeymapEditor.svelte` (keymap Export/Import buttons)

**Interfaces:**
- Consumes: `ProfileStore`, `validate`, `normaliseName`, `MAX_PROFILE_NAME`, `storeSave`, `listProfiles`; `Keymap`, `validateKeymap`, `overridesOf`.
- Produces:
  - `exportFlow(store: ProfileStore): string`
  - `importFlow(text: string, store: ProfileStore): { store: ProfileStore; added: string[]; skipped: number } | { error: string }`
  - `exportKeymap(k: Keymap): string`
  - `importKeymap(text: string): { keymap: Keymap } | { error: string }`
  - `uniqueName(name: string, taken: readonly string[]): string`
  - `downloadText(filename: string, text: string): void`
  - `SeatPanelState.exportProfilesText(): string`
  - `SeatPanelState.importProfilesText(text: string): string` (returns a plain-words result line)

- [ ] **Step 1: Write the failing test** (`transfer.test.ts`)

```ts
import { describe, expect, it } from 'vitest';
import { exportFlow, exportKeymap, importFlow, importKeymap, uniqueName } from './transfer';
import { emptyStore, storeSave } from './profiles';
import { defaultSettings, PRESETS } from './playsettings';
import { defaultKeymap, withBinding } from './keymap';

describe('transfer', () => {
  it('flow round-trips in order', () => {
    let s = storeSave(emptyStore(), 'A', defaultSettings());
    s = storeSave(s, 'B', PRESETS['full-control']);
    const got = importFlow(exportFlow(s), emptyStore());
    expect('store' in got && got.store.order).toEqual(['A', 'B']);
  });

  it('a name clash gets " (2)", then " (3)"; the originals are untouched', () => {
    const mine = storeSave(emptyStore(), 'A', PRESETS['no-tells']);
    const file = exportFlow(storeSave(storeSave(emptyStore(), 'A', defaultSettings()), 'A (2)', defaultSettings()));
    const got = importFlow(file, mine);
    if (!('store' in got)) throw new Error(got.error);
    expect(got.store.order).toEqual(['A', 'A (2)', 'A (2) (2)']);
    expect(got.store.profiles.A).toEqual(mine.profiles.A);
  });

  it('uniqueName respects the 24-char name limit', () => {
    const long = 'x'.repeat(24);
    expect(uniqueName(long, [long])).toBe(`${'x'.repeat(20)} (2)`);
  });

  it('wrong kind is an error; garbage is an error', () => {
    expect(importFlow(exportKeymap(defaultKeymap()), emptyStore())).toEqual({ error: 'This file holds keyboard shortcuts, not flow profiles.' });
    expect(importFlow('not json', emptyStore())).toEqual({ error: 'This file is not a gorge settings export.' });
    expect(importKeymap(exportFlow(emptyStore()))).toEqual({ error: 'This file holds flow profiles, not keyboard shortcuts.' });
  });

  it('bad entries are skipped and counted, good ones import', () => {
    const text = JSON.stringify({ kind: 'gorge-flow', version: 1, items: [{ name: 'Good', settings: defaultSettings() }, { name: 'Bad', settings: { version: 9 } }, { name: '', settings: defaultSettings() }] });
    const got = importFlow(text, emptyStore());
    if (!('store' in got)) throw new Error(got.error);
    expect(got.added).toEqual(['Good']);
    expect(got.skipped).toBe(2);
  });

  it('keymap round-trips its overrides', () => {
    const k = withBinding(defaultKeymap(), 'resolve-all', { code: 'KeyR', ctrl: false, shift: false, alt: false });
    const got = importKeymap(exportKeymap(k));
    expect('keymap' in got && got.keymap).toEqual(k);
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run: `npx vitest run src/lib/transfer.test.ts`
Expected: FAIL. The module does not exist.

- [ ] **Step 3: Implement**

`transfer.ts`:

```ts
/**
 * transfer is export/import-to-file for the client-side libraries (UI rework
 * spec §1). A file is one JSON envelope {kind, version, items}. Import runs
 * every entry through the owning store's own validate, so a file can never
 * carry a configuration the model would reject; a bad entry is skipped and
 * counted, never fatal, and an existing profile is never overwritten.
 */
import { listProfiles, MAX_PROFILE_NAME, normaliseName, storeSave, type ProfileStore } from './profiles';
import { validate } from './playsettings';
import { overridesOf, validateKeymap, type Keymap } from './keymap';

type Kind = 'gorge-flow' | 'gorge-keymap';
const WHAT: Record<Kind, string> = { 'gorge-flow': 'flow profiles', 'gorge-keymap': 'keyboard shortcuts' };

function envelope(text: string, want: Kind): { items: unknown } | { error: string } {
  let v: unknown;
  try {
    v = JSON.parse(text);
  } catch {
    return { error: 'This file is not a gorge settings export.' };
  }
  if (typeof v !== 'object' || v === null) return { error: 'This file is not a gorge settings export.' };
  const o = v as Record<string, unknown>;
  if (o.kind !== 'gorge-flow' && o.kind !== 'gorge-keymap') return { error: 'This file is not a gorge settings export.' };
  if (o.kind !== want) return { error: `This file holds ${WHAT[o.kind]}, not ${WHAT[want]}.` };
  if (o.version !== 1) return { error: 'This export is from a newer version of gorge.' };
  return { items: o.items };
}

export function uniqueName(name: string, taken: readonly string[]): string {
  if (!taken.includes(name)) return name;
  for (let n = 2; ; n++) {
    const suffix = ` (${n})`;
    const cand = name.slice(0, MAX_PROFILE_NAME - suffix.length) + suffix;
    if (!taken.includes(cand)) return cand;
  }
}

export function exportFlow(store: ProfileStore): string {
  const items = listProfiles(store).map((name) => ({ name, settings: store.profiles[name] }));
  return JSON.stringify({ kind: 'gorge-flow', version: 1, items }, null, 2);
}

export function importFlow(text: string, store: ProfileStore): { store: ProfileStore; added: string[]; skipped: number } | { error: string } {
  const env = envelope(text, 'gorge-flow');
  if ('error' in env) return env;
  if (!Array.isArray(env.items)) return { error: 'This file is not a gorge settings export.' };
  let next = store;
  const added: string[] = [];
  let skipped = 0;
  for (const it of env.items) {
    const o = (typeof it === 'object' && it !== null ? it : {}) as Record<string, unknown>;
    const name = normaliseName(typeof o.name === 'string' ? o.name : '');
    const settings = validate(o.settings);
    if (name === null || settings === null) {
      skipped++;
      continue;
    }
    const final = uniqueName(name, listProfiles(next));
    next = storeSave(next, final, settings);
    added.push(final);
  }
  // storeSave marks each saved profile active; an import must not switch the player's active profile.
  return { store: { ...next, lastActive: store.lastActive }, added, skipped };
}

export function exportKeymap(k: Keymap): string {
  return JSON.stringify({ kind: 'gorge-keymap', version: 1, items: overridesOf(k) }, null, 2);
}

export function importKeymap(text: string): { keymap: Keymap } | { error: string } {
  const env = envelope(text, 'gorge-keymap');
  if ('error' in env) return env;
  const k = validateKeymap({ version: 1, overrides: env.items });
  return k === null ? { error: 'This file is not a gorge settings export.' } : { keymap: k };
}
```

`download.ts`:

```ts
/** downloadText saves text as a file through a temporary object URL (the browser's download flow). */
export function downloadText(filename: string, text: string): void {
  const url = URL.createObjectURL(new Blob([text], { type: 'application/json' }));
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 0);
}
```

In `seatpanel.svelte.ts`, beside the other profile methods:

```ts
  /** exportProfilesText is the Export button's payload: every saved flow profile, in order. */
  exportProfilesText(): string {
    return exportFlow(this.profiles);
  }

  /** importProfilesText merges a file's profiles in (never overwriting) and returns the result in plain words. */
  importProfilesText(text: string): string {
    const got = importFlow(text, this.profiles);
    if ('error' in got) return got.error;
    if (got.added.length > 0) this.persistProfiles(got.store);
    const skipped = got.skipped > 0 ? ` ${got.skipped} could not be read and ${got.skipped === 1 ? 'was' : 'were'} skipped.` : '';
    return got.added.length === 0 ? `Nothing imported.${skipped}` : `Imported ${got.added.join(', ')}.${skipped}`;
  }
```

(Import `exportFlow` and `importFlow` from `./transfer`.)

**UI.** In `PlaySettingsPanel.svelte`'s profiles area, the block that holds Save/Delete profile, add two buttons and a hidden file input:
- "Export profiles…" calls `downloadText('gorge-flow-profiles.json', state.exportProfilesText())`.
- "Import profiles…" clicks the hidden `<input type="file" accept="application/json" hidden>`. Its `onchange` reads `await file.text()`, then shows `state.importProfilesText(text)` in a `role="status"` line, then clears the input.

In `KeymapEditor.svelte`, next to "Reset all keys", add the same pair:
- `downloadText('gorge-keys.json', exportKeymap(store.current))`;
- on import, `const r = importKeymap(text); if ('error' in r) message = r.error; else { store.replace(r.keymap); message = 'Keyboard shortcuts imported.'; }`.

- [ ] **Step 4: Run to verify pass**

Run: `npx vitest run src/lib/transfer.test.ts src/components/PlaySettingsPanel.svelte.test.ts src/components/KeymapEditor.svelte.test.ts src/lib/profiles.test.ts`
Expected: PASS.

Then run `npx svelte-check --tsconfig ./tsconfig.json && npx eslint .`. Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/transfer.ts web/src/lib/transfer.test.ts web/src/lib/download.ts web/src/lib/seatpanel.svelte.ts web/src/components/PlaySettingsPanel.svelte web/src/components/KeymapEditor.svelte
git commit -m "feat(web): export/import flow profiles and keyboard shortcuts to a file"
```

---

### Task 10: Spec amendment and full gate

**Files:**
- Modify: `docs/superpowers/specs/2026-09-28-client-table-ui-rework-design.md`

- [ ] **Step 1: Amend the spec's §1 to match what was built**

Replace the "Storage and migration" bullet with:

```markdown
- **Storage and migration**:
  - The existing keys are kept, not renamed. `gorge.playsettings.v1` and
    `gorge.playsettings.profiles.v1` move to settings blob version 3, in
    place.
  - Blob versions 1 and 2 load with breakpoints off.
  - The keymap is new, at `gorge.keymap.v1`. It stores only the overrides of
    the defaults.
  - Renaming keys was rejected: it would orphan saved settings, which is the
    same reason the v1→v2 bump kept its key.
```

Under the keymap bullet, add:

```markdown
  Sub-project 1 wires the priority, decision (pick 1–9, confirm) and
  profile actions, plus the cheat sheet. Actions that need UI from later
  sub-projects arrive with that UI:
  - attack with all, no blocks and auto-pay come with sub-project 4
    (prompts);
  - toggle log, toggle stacking, layout 1–9, zoom card and open grave/exile
    come with sub-projects 2 and 3.
```

Under breakpoints, add:

```markdown
  - Breakpoints stop persistent Auto, the one-shot runs and fast-forward.
    Each one fires once for the thing it caught, and stays stopped on that
    same window however often it is re-checked.
  - A yield, or the Resolve All baseline, exempts the top object from the
    top-object breakpoints.
```

- [ ] **Step 2: Run the whole web gate**

From `<worktree>/web`:

```bash
npx vitest run
npx svelte-check --tsconfig ./tsconfig.json
npx eslint .
```

Expected: all green. If a test outside this plan's files fails, run it on `main` in a fresh worktree (`scripts/agent-worktree.sh flow-gate-check --web`). If it fails there too, report it as pre-existing, with its output. If it doesn't, it is this branch's to fix.

- [ ] **Step 3: Manual smoke** (the UI is new; tests don't click through it)

- Build from the worktree with `npx vite build`. Do not use `make web`.
- Run `gorged` on a port in 8090–8099, with persistence under `/tmp/gorge-flow-profiles`.
- Play a vs-bot game and check:
  - set "the stack holds 2 or more", and auto pauses with "Auto paused here: 2 objects are on the stack.";
  - press `?`, and the sheet lists Space for pass;
  - rebind Undo, and the new chord works;
  - export profiles, import the file into a fresh browser profile, and the names appear.
- Stop the server by pid found with `ss -lptn`. Never use `pkill -f`.

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/specs/2026-09-28-client-table-ui-rework-design.md
git commit -m "docs(spec): record sub-project 1's storage and keymap scope decisions"
```
