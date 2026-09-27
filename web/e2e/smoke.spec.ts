import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

/**
 * Task SG1 — a browser smoke gate.
 *
 * The unit suite (45 files, 439 tests), svelte-check over 382 files and
 * eslint were ALL GREEN while the whole public demo was dead: every table
 * page stuck on "Loading match N..." because `routes/Table.svelte` spread
 * `...p.hand`, which is a literal JSON `null` for a viewer without
 * omniscience. Every fixture in every test had an array hand, so nothing in
 * the suite saw the real-server shape.
 *
 * This spec is the gate that would have caught it. It drives the REAL built
 * client against a REAL `gorged`, in the two spectator modes the server
 * serves — `public` (the mode that was broken) and `omniscient` (the mode
 * that hid it) — and fails the build on any pageerror, any console.error,
 * any failed request to the app's own origin, or a page that silently never
 * leaves its loading state.
 *
 * The servers are started by scripts/smoke.sh and handed to us as base URLs
 * in SMOKE_PUBLIC / SMOKE_OMNI. Nothing here starts or stops a server; the
 * gate's teardown lives in the script so both servers die even on failure.
 */

const PUBLIC = process.env.SMOKE_PUBLIC;
const OMNI = process.env.SMOKE_OMNI;
const SEATED = process.env.SMOKE_SEATED;
const FIXTURE = process.env.SMOKE_FIXTURE;
const WHEEL = process.env.SMOKE_WHEEL;
const TALIS = process.env.SMOKE_TALISMAN;

// ui19: the seated-view gate. The two spectator modes above drove no seated
// client, which is precisely the gap the task closes. These helpers measure
// the REAL rendered DOM with boundingBox()/getBoundingClientRect — never by
// reading CSS — because both ui19 regressions (a fan that sized itself from
// its own output, and a seated identity bar drawn on top of the seat's own
// first card) were geometry facts that the whole unit suite reported as
// correct. See the seated describe block at the bottom.

/** Rectangle helper: the overlapping test used across the seated assertions. */
type Rect = { x: number; y: number; width: number; height: number };

/** intersects reports whether two rects overlap on BOTH axes — the overlap
 *  that makes one element cover the other, not merely share an edge. */
function intersects(a: Rect | null, b: Rect | null): boolean {
  if (!a || !b) return false;
  const ax = a.x + a.width;
  const ay = a.y + a.height;
  const bx = b.x + b.width;
  const by = b.y + b.height;
  return a.x < bx && b.x < ax && a.y < by && b.y < ay;
}

/** right edge, named for the boundedness assertions below. */
function right(r: Rect): number { return r.x + r.width; }

/** seatedIdentity returns the identity bar rect for a given seat number. */
async function identityRect(page: Page, seat: number): Promise<Rect | null> {
  // 7022042e6 replaced IdentityBar (`.identity[data-seat]`) with SeatPills;
  // a seat's identity is now its `[data-player-pill]`.
  const el = page.locator(`[data-player-pill="${seat}"]`);
  if ((await el.count()) === 0) return null;
  return await el.boundingBox();
}

/** boardRect is the section.board felt box the hand must be bounded by. */
async function boardRect(page: Page): Promise<Rect | null> {
  return await page.locator('section.board').boundingBox();
}

/** ownHandRect is the seated player's own hand fan row. Gated on existence:
 *  a spectator client mounts no fan, so a null here means the assertion is
 *  about a non-seated page and must not be run. */
async function ownHandRect(page: Page): Promise<Rect | null> {
  return await page.locator('.handtrack .handfan').boundingBox();
}

/** assertOwnHandClearOfIdentity checks the ui19 identity-bar defect: the
 *  seated player's OWN identity bar must not overlap their own hand fan. The
 *  defect had the bar's bottom offset at `var(--sp-2)`, dropping it onto the
 *  fan; the measured fix sits it above by --own-hand-h. This is the exact
 *  check that would have caught the bug its own author reported (hand top
 *  561 / identity top 644) and still missed. */
async function assertOwnHandClearOfIdentity(page: Page, seat: number, label: string): Promise<void> {
  const identity = await identityRect(page, seat);
  const hand = await ownHandRect(page);
  expect(identity, `${label} seated seat ${seat} should render an identity bar`).not.toBeNull();
  expect(hand, `${label} seated seat ${seat} should render its own hand fan`).not.toBeNull();
  const overlap = intersects(identity, hand);
  expect(
    overlap,
    `${label} seat ${seat}: own identity bar ${JSON.stringify(identity)} overlaps own hand fan ${JSON.stringify(hand)} (both axes)`,
  ).toBe(false);
}

/** assertHandWithinBoard checks the ui19 hand-fan defect: the fan's right
 *  edge must not exceed the board's, and its left edge must not precede it.
 *  The defect sized maxWidth from the fan's own output, so a hand wider than
 *  the felt ran straight off it instead of tightening. */
async function assertHandWithinBoard(page: Page, label: string, tolerate = 1): Promise<void> {
  const board = await boardRect(page);
  const hand = await ownHandRect(page);
  expect(board, `${label} should render a board`).not.toBeNull();
  expect(hand, `${label} should render the seated hand fan`).not.toBeNull();
  if (!board || !hand) return;
  expect(epsLe(right(hand), right(board), tolerate), `${label}: fan right ${right(hand)} must not exceed board right ${right(board)}`).toBe(true);
  // A centred fan is strictly inside the felt, so its left edge is normally
  // well INSIDE the board's. The defect that matters is the opposite: a fan
  // that measured its own output could OVERGROW the felt, so both edges must
  // stay within the board's left/right bounds.
  expect(epsGe(hand.x, board.x, tolerate), `${label}: fan left ${hand.x} must not precede board left ${board.x}`).toBe(true);
}

/** A small uniform tolerance so the browser's sub-pixel layout rounding does
 *  not turn a perfectly-bounded element into a one-pixel failure. */
/** epsLe asserts a <= b within tolerance t. */
function epsLe(a: number, b: number, t: number): boolean {
  return a <= b + t;
}

/** epsGe asserts a >= b within tolerance t. */
function epsGe(a: number, b: number, t: number): boolean {
  return a + t >= b;
}

/**
 * driveToCardOptionsWindow drives the seeded seated game to its FIRST
 * card-options window (ui23 R-E4-1). It answers the only decision the
 * engine's auto-pass does not handle — the opening mulligan (keep) — by
 * clicking the seated panel's own keep button, then poll the pending decision
 * until the seat's first main phase offers a card from hand at a NON-ZERO
 * wire index. The empty maintenance/priority windows on the way are
 * auto-passed by the seated client's own skip-empty floor, so this function
 * never touches them; it returns the target Decision to click its menu.
 *
 * The policy is deliberately trivial and fixed (answer keep, wait), so it is
 * reproducible against the seeded game: no timing-dependent cleverness, no
 * retries — just a generous wall-clock bound.
 */
async function driveToCardOptionsWindow(
  page: Page,
  request: APIRequestContext,
  b: string,
  table: string,
  seat: number,
  token: string,
): Promise<{ kind: string; options: Array<{ index: number; kind: string; obj?: number }> }> {
  const mResp = await request.get(`${b}/api/tables/${table}/matches`);
  expect(mResp.ok(), `GET /api/tables/${table}/matches on ${b} should succeed`).toBe(true);
  const matches = (await mResp.json()) as Array<{ match: number; events: number }>;
  const have = matches.filter((m) => m.events > 0);
  expect(have.length, `table ${table} should have a live match`).toBeGreaterThan(0);
  const match = have[have.length - 1].match;
  const sq = `?seat=${seat}&token=${encodeURIComponent(token)}`;
  const pendingURL = `${b}/api/tables/${table}/matches/${match}/pending${sq}`;

  const deadline = Date.now() + DRIVE_MS;
  while (Date.now() < deadline) {
    const p = await request.get(pendingURL);
    if (p.status() === 409) {
      // Nothing pending for this seat right now — the engine is elsewhere.
      await page.waitForTimeout(400);
      continue;
    }
    expect(p.ok(), `GET ${pendingURL} should succeed`).toBe(true);
    const d = (await p.json()) as { kind: string; options: Array<{ index: number; kind: string; obj?: number }> };

    // Target: a card-options window — a priority ask that offers a card from
    // hand (obj defined) at a NON-ZERO index. That is the discriminating
    // shape: clicking its menu must post the option's own index, because a
    // positional (indexOf) bug would post 0 and act on the wrong card.
    if (d.kind === 'priority' && d.options.some((o) => o.obj !== undefined && o.index > 0)) {
      return d;
    }

    if (d.kind === 'mulligan') {
      const keep = d.options.find((o) => o.kind === 'keep');
      if (keep) {
        // The pending poll can lag the client: a keep already posted still
        // reads as a pending mulligan for a moment, and its button is gone.
        // Click only a button that shows up, wait for the answer to take the
        // round down, and otherwise re-poll -- DRIVE_MS still bounds a keep
        // button that never renders at all.
        const btn = page.locator(`.seat-panel [data-option="${keep.index}"]`);
        const shown = await btn.waitFor({ state: 'visible', timeout: WAIT_MS }).then(() => true, () => false);
        if (shown) {
          await btn.click();
          await btn.waitFor({ state: 'detached', timeout: WAIT_MS }).catch(() => {});
        }
      }
      continue;
    }

    // CR 103.1's toss ask precedes the mulligan round, so the panel poses it
    // first. Answer it through the same panel-option click, or the game never
    // enters the round this driver walks (fb-20260923T050205Z smoke triage).
    if (d.kind === 'starting_player') {
      const first = d.options[0];
      if (first) {
        const btn = page.locator(`.seat-panel [data-option="${first.index}"]`);
        await btn.waitFor({ state: 'visible', timeout: WAIT_MS });
        await btn.click();
        await page.waitForTimeout(600);
      }
      continue;
    }

    // Any other shape before the target (an empty priority window the
    // skip-empty floor is about to pass) — wait for the game to advance.
    await page.waitForTimeout(400);
  }
  throw new Error('driveToCardOptionsWindow: did not reach a card-options window');
}

/** ui24's fixture has two human seats, so this tiny driver echoes only
 * server-provided option indices. It casts every offered zero-cost creature,
 * passes every other priority window, and declines early attacks. With the
 * fixed seed/deck this constructs the board instead of depending on bot policy. */
type WireDecision = {
  seq: number;
  player: number;
  kind: string;
  min: number;
  max: number;
  options: Array<{ index: number; kind: string; label: string; obj?: number }>;
};

const fixtureToken = (seat: number): string => seat === 0 ? 'ui24fixture' : 'ui24fixture-1';

async function postFixtureIntent(request: APIRequestContext, base: string, d: WireDecision, choices: number[]): Promise<void> {
  const resp = await request.post(`${base}/api/tables/t1/matches/1/intent`, {
    headers: { Authorization: `Bearer ${fixtureToken(d.player)}` },
    data: { seq: d.seq, player: d.player, choices },
  });
  expect(resp.status(), `fixture intent for ${d.kind} seat ${d.player}`).toBe(204);
}

/**
 * settleStartingPlayer answers CR 103.1's pre-game toss ask for one seat of a
 * real game, so a fixture that does not drive the engine by hand still reaches
 * turn 1. gorged poses `starting_player` (the toss winner choosing who takes
 * the first turn) BEFORE the mulligan round, and every surface that reads "the
 * mulligan round is under way" -- the rail's log toggle, the OPTIONS drop -- is
 * gated on that round having begun. A seated smoke test that navigates straight
 * into a game whose toss ask is unanswered therefore sits parked on the ask
 * instead of the round it means to assert.
 *
 * The ask may legitimately not be posed (the R-9 no-ask fallback) or belong to
 * the other seat (the bot answers its own), so this returns as soon as the
 * seat's pending decision is anything else, and waits out the 409 the seat gets
 * while the toss belongs to its opponent.
 */
async function settleStartingPlayer(
  request: APIRequestContext,
  base: string,
  table: string,
  match: number,
  seat: number,
  token: string,
): Promise<void> {
  const deadline = Date.now() + STALL_MS;
  const pendingURL = `${base}/api/tables/${table}/matches/${match}/pending?seat=${seat}&token=${encodeURIComponent(token)}`;
  while (Date.now() < deadline) {
    const p = await request.get(pendingURL);
    // 409: the toss belongs to the other seat (the bot answers its own), so
    // nothing is pending for this seat yet. 404: the match exists but its
    // first decision is not registered in the instant after POST /api/games.
    // Both are transient; anything else is a real error and is asserted.
    if (p.status() === 409 || p.status() === 404) {
      await new Promise((resolve) => setTimeout(resolve, 50));
      continue;
    }
    expect(p.ok(), `toss pending ${table} seat ${seat} (status ${p.status()})`).toBe(true);
    const d = await p.json() as WireDecision;
    if (d.kind !== 'starting_player') return;
    const first = d.options[0];
    expect(first, `toss ask ${table} seat ${seat} must offer an option`).toBeDefined();
    const resp = await request.post(`${base}/api/tables/${table}/matches/${match}/intent`, {
      headers: { Authorization: `Bearer ${token}` },
      data: { seq: d.seq, player: seat, choices: [first.index] },
    });
    expect(resp.status(), `toss intent ${table} seat ${seat}`).toBe(204);
    return;
  }
  throw new Error(`settleStartingPlayer: ${table} seat ${seat} never resolved the toss`);
}

async function driveFixtureUntil(
  request: APIRequestContext,
  base: string,
  stop: (d: WireDecision) => boolean,
): Promise<WireDecision> {
  const deadline = Date.now() + 20_000;
  while (Date.now() < deadline) {
    for (const seat of [0, 1]) {
      const token = fixtureToken(seat);
      const p = await request.get(`${base}/api/tables/t1/matches/1/pending?seat=${seat}&token=${token}`);
      if (p.status() === 409) continue;
      expect(p.ok(), `fixture pending seat ${seat}`).toBe(true);
      const d = await p.json() as WireDecision;
      if (stop(d)) return d;

      let choices: number[] = [];
      if (d.kind === 'priority') {
        const cast = d.options.find((o) => o.kind === 'cast');
        const pass = d.options.find((o) => o.kind === 'pass');
        if (cast) choices = [cast.index];
        else if (pass) choices = [pass.index];
      } else if (d.kind === 'starting_player') {
        // CR 103.1's toss ask: echo its first option so the shared fixture
        // reaches turn 1. Every OTHER non-priority ask keeps its original
        // empty answer -- an early attackers window's empty answer, for
        // instance, is the legal "declare no attackers", and echoing an
        // option there would attack with a creature instead of declining.
        choices = d.options.length > 0 ? [d.options[0].index] : [];
      }
      await postFixtureIntent(request, base, d, choices);
    }
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
  throw new Error('ui24 fixture did not reach the requested decision');
}

// How long a page may sit in its loading state before we call it a hang.
// The live server paces decisions at 1.5s and pushes a snapshot on first
// subscribe, so a healthy page mounts in well under a second; 20s is a
// generous ceiling that still turns a never-mounting page into a failure.
const WAIT_MS = 20_000;

/**
 * STALL_MS is the de-flake budget for the one seated test that walks the
 * seeded game's paced engine while a real client rides it (fb-20260917T004341Z
 * round-3 diagnosis, measured in worktree traces): the seated client's fetch
 * pool can stall tens of seconds — its own intent POSTs abort (net::ERR_ABORTED
 * within ms, server-side applied anyway) and its /pending and /view GETs then
 * queue behind a stuck socket for 5-41s while the SSE stream itself keeps
 * delivering frames and the same endpoints answer curl in 0-1ms. The panel's
 * only recovery paths (the SSE-driven view refetch and the 1s pending poll)
 * both ride that stalled pool, so the game — paced 1.5s per decision — sits
 * unanswered for the whole stall and the drive/crossing budgets must absorb
 * one full stall each. Measured stalls: 5.3s, 11s, 11.2s (single-worktree
 * probes) and 41s (the smoke gate's own failing run). WAIT_MS (20s) is fine
 * for every assertion that does not straddle a stall; the R-E4-1 drive and
 * its crossing assertion get STALL_MS instead, and the test's own timeout is
 * raised to hold drive + crossing + one stall each with margin.
 */
const STALL_MS = 90_000;
// The drive loop's wall-clock bound: its HTTP polls are Node-side (they do
// not ride the browser's socket pool), so the bound is the client's own
// stall time plus the paced walk. 90s was met once at 47.4s under load.
const DRIVE_MS = 150_000;

/** sameOrigin reports whether url is on the same origin as base — the only
 *  requests the gate forbids from failing (fonts and other third-party
 *  resources may legitimately fail off-network and must not poison the run). */
function sameOrigin(base: string, url: string): boolean {
  try {
    return new URL(url).origin === new URL(base).origin;
  } catch {
    return false;
  }
}

/** externalResourceError reports whether a console.error is a THIRD-PARTY
 *  resource failure — a cross-origin fetch (the card art / oracle fetch to
 *  Scryfall) that the browser CORS-blocks, or a blocked subresource load.
 *  Such an error carries the external URL in its message, and it is exactly
 *  the class of failure the requestfailed collector below already tolerates
 *  ("fonts and other third-party resources may legitimately fail off-network
 *  and must not poison the run"); without this filter a CORS-blocked card
 *  art fetch poisons the gate even though the app itself is healthy. Named
 *  sites are fine — the collector still rejects an error that is on the
 *  app's OWN origin, or that blames a resource the app controls. */
function externalResourceError(text: string, base: string): boolean {
  const urls = text.match(/https?:\/\/[^\s'"]+/g) ?? [];
  return urls.some((u) => {
    try {
      return new URL(u).origin !== new URL(base).origin;
    } catch {
      return false;
    }
  });
}

interface Issues {
  pageErrors: string[];
  consoleErrors: string[];
  failed: string[];
}

/** watch arms the three failure collectors on the page, scoped to the server's
 *  own origin, and returns the collector to assert against after the page has
 *  settled. */
function watch(page: Page, base: string): Issues {
  const c: Issues = { pageErrors: [], consoleErrors: [], failed: [] };
  page.on('pageerror', (e) => c.pageErrors.push(String(e)));
  page.on('console', (m) => {
    if (m.type() !== 'error') return;
    // A third-party resource failure (a cross-origin card art / oracle fetch
    // CORS-blocked or failed off-network by the browser) is environment
    // noise, not a product bug — the same tolerance the requestfailed
    // collector applies. The browser also logs a generic "Failed to load
    // resource" console error for such a fetch AND for the 409 /pending
    // answer (the server's normal "nothing pending" reply); those are
    // covered by the URL-bearing handlers below (requestfailed / response),
    // which still catch a genuine SAME-ORIGIN failure and would report it
    // with its URL. Filtering this generic message therefore cannot hide a
    // product failure the URL-bearing handlers would not already report.
    const text = m.text();
    if (externalResourceError(text, base)) return;
    if (/Failed to load resource/.test(text)) return;
    c.consoleErrors.push(text);
  });
  page.on('requestfailed', (r) => {
    // ERR_ABORTED is the browser cancelling an in-flight request because
    // the page navigated away or closed — navigation noise, not a product
    // failure. A page that genuinely fails to reach the server reports a
    // connection/DNS/timeout error instead, and we still catch those.
    if (r.failure()?.errorText?.includes('ERR_ABORTED')) return;
    if (sameOrigin(base, r.url())) {
      c.failed.push(`requestfailed ${r.method()} ${r.url()} :: ${r.failure()?.errorText ?? ''}`);
    }
  });
  page.on('response', (r) => {
    // 409 is the server's benign "conflict" — /pending answers 409 when
    // nothing is pending for this seat, and /intent answers 409 on a stale
    // seq; the client recovers from both by design (see seatpanel.ts
    // refreshPending / postIntent). It is not a product failure.
    // 502 from the Scryfall-backed proxies (/art/... images, /cards/named
    // printed facts) is gorged reporting that the UPSTREAM failed
    // (cmd/gorged/art.go answers StatusBadGateway only on an upstream error); 503
    // on the same routes is the wait budget's "the upstream fetch had not
    // settled within artWaitBudget — the fetch is still running detached,
    // retry shortly" (art.go's boundedWait, added after the fb-20260917T004304Z
    // run showed a degraded upstream wedging the page). A Scryfall outage (observed 2026-09-14: 503
    // upstream) is not a gorge product failure and must not block every web
    // merge; any other status on those paths, and every other path's failure, still counts.
    if ((r.status() === 502 || r.status() === 503) && sameOrigin(base, r.url()) && /^\/(art\/|cards\/named$)/.test(new URL(r.url()).pathname)) return;
    if (sameOrigin(base, r.url()) && r.status() >= 400 && r.status() !== 409) {
      c.failed.push(`HTTP ${r.status()} ${r.request().method()} ${r.url()}`);
    }
  });
  return c;
}

function formatIssues(c: Issues): string[] {
  return [
    ...c.pageErrors.map((e) => `pageerror: ${e}`),
    ...c.consoleErrors.map((e) => `console.error: ${e}`),
    ...c.failed,
  ];
}

function expectClean(c: Issues, context: string): void {
  const bad = formatIssues(c);
  expect(bad, `${context} produced ${bad.length} browser failure(s)`).toEqual([]);
}

/** liveTableAndMatch reads the real ids from the server rather than
 *  hardcoding: the first table, and the latest match on it that has any
 *  events (the live one is the last entry of /api/tables/{t}/matches). */
async function liveTableAndMatch(request: APIRequestContext, base: string): Promise<{ table: string; match: number }> {
  const tResp = await request.get(`${base}/api/tables`);
  expect(tResp.ok(), `GET /api/tables on ${base} should succeed`).toBe(true);
  const tables = (await tResp.json()) as Array<{ id: string; state: string }>;
  expect(tables.length, `${base} should have at least one table`).toBeGreaterThan(0);
  const table = tables[0].id;

  const mResp = await request.get(`${base}/api/tables/${table}/matches`);
  expect(mResp.ok(), `GET /api/tables/${table}/matches on ${base} should succeed`).toBe(true);
  const matches = (await mResp.json()) as Array<{ match: number; events: number }>;
  const haveEvents = matches.filter((m) => m.events > 0);
  expect(haveEvents.length, `${base} table ${table} should have a match with events`).toBeGreaterThan(0);
  return { table, match: haveEvents[haveEvents.length - 1].match };
}

/** assertLobby checks the overview route mounts and stays clean. */
async function assertLobby(page: Page, base: string, label: string): Promise<void> {
  const c = watch(page, base);
  const res = await page.goto(`${base}/`, { waitUntil: 'domcontentloaded' });
  expect(res?.status() ?? 0, `${label} GET / should return 200`).toBe(200);
  await page.locator('main.overview').waitFor({ state: 'visible', timeout: WAIT_MS });
  await page.locator('main.overview h1').waitFor({ state: 'visible', timeout: WAIT_MS });
  // A beat to let the overview's own /api/tables fetch and any SSE window
  // settle before we judge its console.
  await page.waitForTimeout(1000);
  expectClean(c, `${label} lobby /`);
}

/** assertTable checks one table route (live or finished) mounts its board,
 *  renders at least one transcript line, and leaves no loading state. */
async function assertTable(page: Page, base: string, path: string, label: string): Promise<void> {
  const c = watch(page, base);
  const url = `${base}${path}`;
  const res = await page.goto(url, { waitUntil: 'domcontentloaded' });
  expect(res?.status() ?? 0, `${label} GET ${path} should return 200`).toBe(200);

  // The page must actually mount the board within the bound. A page that
  // silently never leaves "Loading match N..." / "Waiting for tX..." fails
  // here — this is exactly the bug the gate exists for.
  let mounted = true;
  try {
    await page.locator('.quadrant').first().waitFor({ state: 'visible', timeout: WAIT_MS });
  } catch {
    mounted = false;
  }
  if (!mounted) {
    // Report what the page was actually showing so the failure names the
    // loading/blank state rather than just "no board".
    let shown: string;
    try {
      shown = (await page.locator('p.waiting').first().textContent()) ?? '(no .waiting element — blank page)';
    } catch {
      shown = '(no .waiting element — blank page)';
    }
    expect(mounted, `${label} GET ${path} never mounted (showing: "${shown}")`).toBe(true);
  }

  // The page must render something real, not merely stay error-free: seat
  // boxes for every player, and at least one transcript line.
  const seats = await page.locator('.quadrant').count();
  expect(seats, `${label} GET ${path} should render seat boxes`).toBeGreaterThan(0);

  // The transcript's live lines: give the SSE/DVR a moment to land the first
  // event line for the live route.
  await page.waitForFunction(
    () => document.querySelectorAll('.transcript .line').length >= 1,
    undefined,
    { timeout: WAIT_MS },
  );
  expectClean(c, `${label} GET ${path}`);
}

for (const [mode, base] of [
  ['public', PUBLIC],
  ['omniscient', OMNI],
] as const) {
  const skip = base ? false : true;
  test.describe(`gorged [${mode}] spectator smoke`, () => {
    test.skip(skip, 'SMOKE_PUBLIC/SMOKE_OMNI unset — run via `make smoke` (scripts/smoke.sh)');

    test.describe.configure({ mode: 'serial' });

    test('lobby / and live table and specific match mount cleanly', async ({ browser, request }) => {
      const b = base as string;
      const label = `[${mode}]`;
      const ctx = await browser.newContext();
      try {
        // Each route gets its own page so a navigation from one route to
        // the next cannot abort the previous route's in-flight requests
        // (which would read as a spurious requestfailure).
        const lobby = await ctx.newPage();
        await assertLobby(lobby, b, label);
        await lobby.close();

        // Resolve the real ids from the server.
        const { table, match } = await liveTableAndMatch(request, b);

        const live = await ctx.newPage();
        await assertTable(live, b, `/t/${table}`, `${label} live table /t/${table}`);
        await live.close();

        const fin = await ctx.newPage();
        await assertTable(fin, b, `/t/${table}/m/${match}`, `${label} match /t/${table}/m/${match}`);
        await fin.close();
      } finally {
        await ctx.close();
      }
    });
  });
}

// ---------------------------------------------------------------------------
// ui19 — SEATED 1v1 smoke gate.
//
// The two spectator modes above drove no seated client, so the two regressions
// this task exists to close — a hand fan that sized itself from its own
// output, and a seated player's identity bar drawn on top of their own hand
// — were invisible to the whole gate. This block drives a REAL seated 1v1
// client (against a REAL bot) and asserts the layout invariants those bugs
// violated, on the real rendered DOM.
//
// Join paths: the `-vsbot` flow (POST /api/games) always seats the human at
// seat 0 of a fresh table; the `-humans 1` startup table (t1) seats the
// human at seat 1. Both come from the ONE SMOKE_SEATED server (see
// scripts/smoke.sh, which seeds the seat-1 token to `ui19seat1`).
//
// Assertions (all measured, never read off CSS):
//  1. the seated player's own identity bar does not overlap their own hand
//     fan on both axes (the exact check that would have caught ui19's
//     identity-bar defect);
//  2. the hand fan is bounded by the board — right edge <= board right, and
//     its left edge does not precede the board's (the exact check that would
//     have caught ui19's self-sizing-fan defect);
//  3. 1v1 is top vs bottom, relative to the viewer: the seated player's own
//     quadrant/identity is BELOW their opponent's on screen, verified from
//     EACH seat (seat 0 via -vsbot, seat 1 via -humans 1);
//  4. the usual failure collectors on the seated page: no pageerror, no
//     console.error, no failed same-origin request, and the page leaves its
//     loading state.
//
// How the large hand is reached: assertion 2 wants the overlap-tightening
// path (a hand wide enough to need it), not just a fitting 7-card opening
// hand. Reaching a 9+ card hand in a live game within this gate's budget is
// not reliably achievable without driving the whole match through its
// decisions (which would make the gate slow and order-dependent), so the
// hand-bounded-by-board assertion is made on the real opening hand AND, where
// a large hand is genuinely reached in the live flow, on that too. See the
// ui19 report for the measured geometry and the honest statement of what a
// large hand would add.
for (const [mode, base] of [['seated', SEATED]] as const) {
  const skip = base ? false : true;
  test.describe(`gorged [${mode}] seated 1v1 smoke`, () => {
    test.skip(skip, 'SMOKE_SEATED unset — run via `make smoke` (scripts/smoke.sh)');

    test.describe.configure({ mode: 'serial' });

/** createVsBotJoin creates a real play-vs-bot game and returns the seated
 *  join path (seat 0 of a fresh table) — POST /api/games, exactly what the
 *  landing page does. */
    async function createVsBotJoin(request: APIRequestContext, b: string): Promise<{ join: string; seat: number }> {
      const resp = await request.post(`${b}/api/games`, { data: { format: 'constructed' } });
      expect(resp.ok(), `POST /api/games on ${b} should succeed`).toBe(true);
      const g = (await resp.json()) as { join: string; seat: number; table: string; match: number; token: string };
      expect(g.join, `POST /api/games on ${b} should return a join path`).toBeTruthy();
      // Answer the pre-game toss before the test mounts a client, so the game
      // is in the mulligan round (controls null) and not still parked on the
      // CR 103.1 ask -- the round the rail's log toggle is drawn in.
      await settleStartingPlayer(request, b, g.table, g.match, g.seat, g.token);
      return { join: g.join, seat: g.seat };
    }

    /** assertSeatedJoins navigates to a seated join URL, waits for the real
     *  board + own hand to mount, then runs all four invariant groups for the
     *  given seat. */
    async function assertSeatedJoin(b: string, page: Page, join: string, seat: number, label: string): Promise<void> {
      const c = watch(page, b);
      const url = `${b}${join}`;
      const res = await page.goto(url, { waitUntil: 'domcontentloaded' });
      expect(res?.status() ?? 0, `${label} GET ${join} should return 200`).toBe(200);

      // The seated page must mount its board AND its own hand fan (a seated
      // client that hangs on the seat-scoped view never gets here — this is
      // the loading-state gate for the seated page).
      await page.locator('.handtrack .handfan').waitFor({ state: 'visible', timeout: WAIT_MS });
      await page.locator('.quadrant').first().waitFor({ state: 'visible', timeout: WAIT_MS });
      // A beat for the SSE/snapshot to settle so the geometry and the console
      // judgement are against a steady page.
      await page.waitForTimeout(500);

      // 1. own identity bar clear of own hand fan (ui19 identity-bar defect).
      await assertOwnHandClearOfIdentity(page, seat, label);

      // 2. hand bounded by the board (ui19 self-sizing-fan defect).
      await assertHandWithinBoard(page, label);

      // 3. 1v1 top-vs-bottom, relative to viewer: the seated player's own
      //    quadrant and identity sit BELOW the opponent's on screen. The seat
      //    numbering is fixed by the viewer, so this is exactly the check
      //    that would regress if the mapping stopped being viewer-relative.
      const opp = seat === 0 ? 1 : 0;
      const ownQ = await page.locator(`.quadrant[data-seat="${seat}"]`).boundingBox();
      const oppQ = await page.locator(`.quadrant[data-seat="${opp}"]`).boundingBox();
      expect(ownQ, `${label} seat ${seat} should render its own quadrant`).not.toBeNull();
      expect(oppQ, `${label} seat ${opp} should render the opponent quadrant`).not.toBeNull();
      if (ownQ && oppQ) {
        // "Below on screen" = greater y. Boundaries are open (one quadrant
        // occupies the lower half, the other the upper), so equality cannot
        // occur, but keep a strict > so a same-line settlement fails loudly.
        expect(ownQ.y, `${label}: own seat ${seat} quadrant (y=${ownQ.y}) must be BELOW opponent seat ${opp} (y=${oppQ.y}) on screen`).toBeGreaterThan(oppQ.y);
      }
      const ownI = await identityRect(page, seat);
      const oppI = await identityRect(page, opp);
      if (ownI && oppI) {
        expect(ownI.y, `${label}: own seat ${seat} identity (y=${ownI.y}) must be BELOW opponent seat ${opp} (y=${oppI.y})`).toBeGreaterThan(oppI.y);
      }

      // 4. No browser failure on the settled page. The loading-state check is
      //    the `.handtrack .handfan` + `.quadrant` waitFor above: a seated page
      //    that hangs never mounts either. A `p.waiting` placeholder is NOT a
      //    loading state here -- a fully mounted board still shows "Waiting for
      //    X..." whenever it is the opponent's turn -- so counting it would
      //    assert nothing.
      expectClean(c, `${label} GET ${join}`);
    }

    test('seats a human vs a bot at seat 0 and asserts the seated 1v1 layout', async ({ browser, request }) => {
      const b = base as string;
      const label = `[seated]`;
      const { join, seat } = await createVsBotJoin(request, b);
      const ctx = await browser.newContext();
      try {
        const page = await ctx.newPage();
        await assertSeatedJoin(b, page, join, seat, label);
        await page.close();
      } finally {
        await ctx.close();
      }
    });

    test('chooses both decks on the lobby and seats that exact matchup', async ({ browser, request }) => {
      const b = base as string;
      const ctx = await browser.newContext();
      try {
        const page = await ctx.newPage();
        const c = watch(page, b);
        await page.goto(`${b}/`, { waitUntil: 'domcontentloaded' });
        const human = page.getByLabel('Your deck');
        const bot = page.getByLabel('Bot deck');
        await human.locator('option[value="the-epic-storm"]').waitFor({ state: 'attached', timeout: WAIT_MS });
        await page.locator('input[value="commander"]').check();
        await expect(human.locator('option[value="foundations-calling-all-angels"]')).toContainText('Giada, Font of Hope');
        await expect(human.locator('option[value="the-epic-storm"]')).toHaveCount(0);
        await page.locator('input[value="constructed"]').check();
        await expect(human).toHaveValue('');
        await human.selectOption('the-epic-storm');
        await bot.selectOption('uw-control');
        await page.getByRole('button', { name: 'Start game' }).click();
        await page.waitForURL(/\/t\/g\d+\?seat=0&token=/, { timeout: WAIT_MS });
        await page.locator('.handtrack .handfan').waitFor({ state: 'visible', timeout: WAIT_MS });

        // The resulting real game exposes the selected deck identities both
        // in its match record and in the rendered rail's accessible labels.
        const table = new URL(page.url()).pathname.split('/')[2];
        const mResp = await request.get(`${b}/api/tables/${table}/matches`);
        expect(mResp.ok()).toBe(true);
        const matches = await mResp.json() as Array<{ seats: Array<{ deck: string }> }>;
        expect(matches[0].seats.map((s) => s.deck)).toEqual(['the-epic-storm', 'uw-control']);
        await expect(page.locator('[data-seat-row="0"] .pick')).toHaveAttribute('aria-label', /the-epic-storm/);
        await expect(page.locator('[data-seat-row="1"] .pick')).toHaveAttribute('aria-label', /uw-control/);
        expectClean(c, '[seated] selected-deck matchup');
      } finally {
        await ctx.close();
      }
    });

    test('seats a human at seat 1 (startup table) and asserts the mirrored 1v1 layout', async ({ browser }) => {
      const b = base as string;
      const label = `[seated]`;
      const ctx = await browser.newContext();
      try {
        const page = await ctx.newPage();
        // Seat 1 of the -humans 1 startup table t1; its token is fixed by
        // smoke.sh's -seat-token ui19seat1.
        await assertSeatedJoin(b, page, '/t/t1?seat=1&token=ui19seat1', 1, label);
        await page.close();
      } finally {
        await ctx.close();
      }
    });

    // The overlap-tightening path: the two tests above mount a 1440px-wide
    // board whose opening hand (7 cards) fits before any tightening is needed,
    // so a fan that sized itself from its own output would look fine there.
    // ui19's second defect was exactly that — maxWidth came from measuring the
    // fan itself, so the width constraint never bound and a hand too wide for
    // the felt ran straight off it. This test narrows the viewport so the SAME
    // real 7-card hand genuinely exceeds the board and MUST tighten; the
    // overflow is then asserted against the board bounds. (A genuinely large
    // 9+ card hand is not reachable inside a live game's budget in this build:
    // the normal hand-size rule caps it at 7 without an "no maximum hand size"
    // permanent, so we can only reach the tightening path by narrowing the
    // felt, not by growing the hand. See the ui19 report.)
    test('bounds a hand that exceeds a narrower board by tightening, not overflowing', async ({ browser, request }) => {
      const b = base as string;
      const label = `[seated]`;
      const { join, seat } = await createVsBotJoin(request, b);
      const ctx = await browser.newContext({ viewport: { width: 1000, height: 900 } });
      try {
        const page = await ctx.newPage();
        await assertSeatedJoin(b, page, join, seat, label);
        await page.close();
      } finally {
        await ctx.close();
      }
    });

    // ui21 — the log starts hidden for a SEATED player, with a toggle in the
    // rail. The contracted rail-toggle window is specifically the mulligan
    // round (fb-20260917T231628Z); fd4e6a0f0 settles the CR 103.1 toss before
    // mounting so this test can assert that window rather than assume it.
    // It drives a REAL seated client and measures the transcript visibility.
    // The other half — the per-card options affordance — is exercised below.
    test('a seated player sees the log hidden by default and can toggle it in the rail', async ({ browser, request }) => {
      const b = base as string;
      const label = `[seated]`;
      const { join, seat } = await createVsBotJoin(request, b);
      const ctx = await browser.newContext();
      try {
        const page = await ctx.newPage();
        await assertSeatedJoin(b, page, join, seat, label);

        // Watch the post-mount interactions (the toggle itself) so a console
        // error surfacing from flipping the log is caught here.
        const c = watch(page, b);
        // The toss settle must leave the mounted seat in its mulligan round.
        // Assert the actual keep option is mounted before relying on that state.
        const keep = page.locator('.seat-panel button.keep[data-option]');
        await keep.waitFor({ state: 'visible', timeout: WAIT_MS });

        // The rail carries the log switch, and the seated default is hidden.
        const toggle = page.locator('[data-log-toggle]');
        await toggle.waitFor({ state: 'visible', timeout: WAIT_MS });
        expect(await toggle.getAttribute('aria-checked')).toBe('false');
        // The two controls are mutually exclusive. The reverse exclusion is
        // Table.svelte's optionsReachable ? null : toggleLog at line 420.
        expect(await page.locator('[data-toggle="show-game-log"]').count()).toBe(0);
        // The transcript footer is hidden (its grid row collapsed, so it has
        // no display box).
        await page.waitForFunction(() => {
          const el = document.querySelector('footer.transcript');
          if (!el) return false;
          const r = el.getBoundingClientRect();
          return r.height === 0 || getComputedStyle(el).display === 'none';
        }, undefined, { timeout: WAIT_MS });

        // Clicking the rail switch shows it, measured by the footer gaining a
        // nonzero box.
        await toggle.click();
        await page.waitForFunction(() => {
          const el = document.querySelector('footer.transcript');
          if (!el) return false;
          const r = el.getBoundingClientRect();
          return r.height > 0 && getComputedStyle(el).display !== 'none';
        }, undefined, { timeout: WAIT_MS });
        expect(await toggle.getAttribute('aria-checked')).toBe('true');

        expectClean(c, `${label} log toggle`);
        await page.close();
      } finally {
        await ctx.close();
      }
    });

    // ui23 — the R-E4-1 guard, in a real browser. The rule this whole options
    // feature rests on is: never resolve an option by position. The unit
    // suite cannot express the discriminating test — vitest runs
    // `environment: 'node'` with no DOM, and the existing R-E4-1 test calls
    // the mock itself and asserts it was called, never clicking the rendered
    // menu. A positional bug (clicking menu item N posts index-of-position
    // instead of the option's own index) is type-clean, passes svelte-check,
    // and passes every unit test — only a REAL browser click on the
    // RENDERED menu can catch it.
    //
    // This drives the seeded vs-bot game to its FIRST card-options window —
    // the seat's first main phase offers several cards from hand, each a
    // distinct object with its own option at a NON-ZERO wire index (e.g.
    // `play_land,1,Ancient Tomb` while `cast,0,Chalice` is first) exactly the
    // discriminating shape: a tile whose menu item must post its object's
    // OWN index, because `list.indexOf(opt)` would post 0 (the wrong card).
    // We open the card's menu in the rendered fan, click an item, and assert
    // the option that was actually POSTED is that option's own index — most
    // robustly by which card it is that gets cast/played, since a positional
    // bug plays the wrong one.
    test('a card menu posts its own index, not a position in a rebuilt list (R-E4-1)', async ({ browser, request }) => {
      // The paced walk + the seated client's measured socket-pool stalls
      // (see STALL_MS) put this test's honest worst case far above the
      // config's 120s default; give it room explicitly.
      test.setTimeout(300_000);
      const b = base as string;
      const label = `[seated]`;
      // The deterministic seeded game (seed 1, smoke.sh's -seed 1): seat 1 of
      // the startup t1 table with smoke.sh's fixed seat token. A fixed answer
      // policy against a fixed seed is reproducible, which is why the guard
      // uses the seeded game rather than a random POST /api/games table.
      const join = '/t/t1?seat=1&token=ui19seat1';
      const seat = 1;
      const ctx = await browser.newContext();
      try {
        const page = await ctx.newPage();
        // Mount the seated client and keep it live while we drive the game
        // (see `driveToCardOptionsWindow`: answer the mulligan, let the
        // engine's auto-pass handle the empty windows, stop at the FIRST
        // card-options window).
        await page.goto(`${b}${join}`, { waitUntil: 'domcontentloaded' });
        await page.locator('.handtrack .handfan').waitFor({ state: 'visible', timeout: WAIT_MS });
        const c = watch(page, b);

        const target = await driveToCardOptionsWindow(page, request, b, 't1', seat, 'ui19seat1');

        // The target is the seat's first card-options window. Pick a CARD
        // option at a NON-ZERO index (zero would be the first wire entry,and
        // a positional bug would coincidentally be right on it) — the
        // discriminating shape. Take the LAST such card option.
        const cards = target.options.filter((o) => o.obj !== undefined && o.index > 0);
        expect(cards.length).toBeGreaterThan(0);
        const pick = cards[cards.length - 1];

        // The hand fan renders THAT card. Open its badge menu and click the
        // option item — the real click path the unit suite cannot reach.
        const cardLoc = page.locator(`.handfan [data-obj="${pick.obj}"]`);
        await cardLoc.waitFor({ state: 'visible', timeout: WAIT_MS });
        // A tile renders one of two affordances, and this guard has to drive
        // whichever it gets. A card carrying exactly ONE option collapses to a
        // direct-action button (`[data-single-action]`) with no menu at all —
        // the seeded game's target is a land whose only option is "play" — so
        // waiting for a menu badge on it hangs until the test's own timeout.
        // With two or more options the badge opens a picker that is PORTALLED
        // to <body>, so it is not a descendant of the card either: a
        // card-scoped locator matches nothing. Two-to-six options render the
        // radial wheel, whose buttons carry their own wire index; a longer
        // list renders the same `body > .menu-pop` the ui24 guard locates.
        //
        // R-E4-1 holds on both paths — the direct action posts the option's
        // own wire index exactly as a menu item does — and the assertion
        // below (this card, and only this card, crosses to the board) is what
        // actually discriminates a positional bug either way.
        // Since 7022042e6 a land's play option renders as its own PLAY
        // shortcut carrying the option's wire index (`[data-play-land]`),
        // ahead of the generic single-action and badge affordances.
        const playLand = cardLoc.locator(`[data-play-land="${pick.index}"]`);
        const single = cardLoc.locator('[data-single-action]');
        const badge = cardLoc.locator('button[aria-haspopup="menu"]');
        if (await playLand.count() > 0) {
          await playLand.click({ timeout: WAIT_MS });
        } else if (await single.count() > 0) {
          await single.waitFor({ state: 'visible', timeout: WAIT_MS });
          await single.click();
        } else {
          await badge.click({ timeout: WAIT_MS });
          const wheelItem = page.locator(`body > [data-radial-picker] button[data-wire-index="${pick.index}"]`);
          const listItem = page.locator('body > .menu-pop button[role="menuitem"]', { hasText: pick.label as string });
          const item = (await wheelItem.count()) > 0 ? wheelItem : listItem;
          await item.waitFor({ state: 'visible', timeout: WAIT_MS });
          await item.click();
        }

        // Discriminating assertion, on the OBSERVABLE: the option's OWN card
        // crosses to the board (a land played). Under a positional bug the
        // click posts index 0 (the first wire option — casting the first
        // card), so this card stays in hand and the assertion times out.
        await page.waitForFunction((obj) => {
          return document.querySelector(`.quadrant [data-obj="${obj}"]`) !== null;
        }, pick.obj as number, { timeout: STALL_MS });

        // The Options drop, now that the seat's controls are live. Two
        // regressions shipped unseen because no gate clicked it: the opening
        // click bubbled to the window as an "outside" click and closed it,
        // and the popover opened upward past the top of the viewport.
        const optionsButton = page.locator('[data-rail-options] button[aria-haspopup="dialog"]');
        await optionsButton.click({ timeout: WAIT_MS });
        const popover = page.locator('#play-options-popover');
        await popover.waitFor({ state: 'visible', timeout: WAIT_MS });
        await page.waitForTimeout(300);
        expect(await optionsButton.getAttribute('aria-expanded'), `${label} Options stays open after its own click`).toBe('true');
        const pop = await popover.boundingBox();
        const vp = page.viewportSize();
        expect(pop, `${label} Options popover has a box`).not.toBeNull();
        if (pop && vp) {
          expect(pop.y, `${label} Options popover top ${pop.y} must be on screen`).toBeGreaterThanOrEqual(0);
          expect(pop.y + pop.height, `${label} Options popover bottom must be on screen`).toBeLessThanOrEqual(vp.height + 1);
        }
        await page.keyboard.press('Escape');
        await popover.waitFor({ state: 'detached', timeout: WAIT_MS });

        expectClean(c, `${label} R-E4-1 card menu`);
        await page.close();
      } finally {
        await ctx.close();
      }
    });
  });
}

// wheel1 — reproduce the reported two-click path against a real game. The
// fixture's Underground Sea has two distinct intrinsic mana abilities. The
// priority decision first offers one source-level activate action; answering
// it makes the server pose Add U / Add B on that same object. One click on the
// card's direct action must carry the user through that continuation and open
// its radial choice without requiring a second click.
test.describe('gorged [wheel1] Underground Sea fixture', () => {
  test.skip(!WHEEL, 'SMOKE_WHEEL unset — run via scripts/smoke.sh');

  test('one click opens a live two-ability mana wheel', async ({ browser, request }) => {
    const b = WHEEL as string;
    const tokens = [fixtureToken(0), fixtureToken(1)];
    const endpoint = `${b}/api/tables/t1/matches/1`;
    let source: number | undefined;
    let played = false;
    const deadline = Date.now() + 20_000;

    // Drive the real engine to seat 0's first post-land priority window before
    // mounting the client, so its skip-empty policy cannot race this setup.
    while (Date.now() < deadline && source === undefined) {
      for (const seat of [0, 1]) {
        const pending = await request.get(`${endpoint}/pending?seat=${seat}&token=${tokens[seat]}`);
        if (pending.status() === 409) continue;
        expect(pending.ok(), `wheel pending seat ${seat}`).toBe(true);
        const d = await pending.json() as WireDecision;
        const activation = seat === 0 && played ? d.options.find((o) => o.kind === 'activate') : undefined;
        if (activation?.obj !== undefined) {
          source = activation.obj;
          break;
        }
        const land = seat === 0 && !played ? d.options.find((o) => o.kind === 'play_land') : undefined;
        const pass = d.options.find((o) => o.kind === 'pass');
        // Cleanup discard and any other forced decision encountered before
        // the seeded deck draws its first Sea take deterministic option zero.
        const choice = land ?? pass ?? d.options[0];
        expect(choice, `wheel setup ${d.kind} seat ${seat} needs an option`).toBeDefined();
        if (!choice) break;
        const intent = await request.post(`${endpoint}/intent`, {
          headers: { Authorization: `Bearer ${tokens[seat]}` },
          data: { seq: d.seq, player: seat, choices: [choice.index] },
        });
        expect(intent.status(), `wheel setup intent seat ${seat}`).toBe(204);
        if (land) played = true;
        break;
      }
      if (source === undefined) await new Promise((resolve) => setTimeout(resolve, 20));
    }
    expect(source, 'wheel fixture should reach an Underground Sea activation').toBeDefined();
    if (source === undefined) return;

    const ctx = await browser.newContext();
    try {
      const page = await ctx.newPage();
      await page.goto(`${b}/t/t1?seat=0&token=${tokens[0]}`, { waitUntil: 'domcontentloaded' });
      const tile = page.locator(`.quadrant[data-seat="0"] [data-obj="${source}"]`);
      await tile.waitFor({ state: 'visible', timeout: WAIT_MS });
      const action = tile.locator('xpath=..').locator('[data-single-action]');
      await action.waitFor({ state: 'visible', timeout: WAIT_MS });

      // This is the single user click under test. It posts the source-level
      // activation; the real server responds with the two object-bound mana
      // choices, which must appear already open rather than as a new badge
      // requiring click number two.
      await action.click();
      const wheel = page.locator('body > [data-radial-picker]');
      await expect(wheel).toBeVisible({ timeout: WAIT_MS });
      const blue = wheel.locator('[data-mana-option="U"]');
      await expect(blue).toBeVisible();
      await expect(wheel.locator('[data-mana-option="B"]')).toBeVisible();
      // Complete the choice so the serial ui24 test can continue driving this
      // shared real game from the next priority window.
      await blue.click();
    } finally {
      await ctx.close();
    }
  });
});

// fb-e079def5 / fb-20260917T232800Z — the Talisman continuation. A Talisman
// of Indulgence carries TWO mana abilities ({T}: Add {C}; {T}: Add {B} or {R}
// plus its 1 damage), so activating it poses a stage-1 ability choose that
// the player answers THROUGH the radial wheel. fb-20260917T232800Z
// FLATTENED the explicit Combo ability into per-colour options (Add C / Add
// B / Add R in one wheel), so the stage-2 colour ask no longer exists: one
// click on Add B pays the tap and lands the mana, and the wheel closes
// without a second ask re-opening. (The pre-flattening pin asserted the
// two-ask flow; that supersession is deliberate.)
test.describe('gorged [talisman] two-stage mana continuation fixture', () => {
  test.skip(!TALIS, 'SMOKE_TALISMAN unset — run via scripts/smoke.sh');

  const talismanToken = (seat: number): string => seat === 0 ? 'talismanwheel' : 'talismanwheel-1';

  async function postTalismanIntent(request: APIRequestContext, base: string, d: WireDecision, choices: number[]): Promise<number> {
    const resp = await request.post(`${base}/api/tables/t1/matches/1/intent`, {
      headers: { Authorization: `Bearer ${talismanToken(d.player)}` },
      data: { seq: d.seq, player: d.player, choices },
    });
    return resp.status();
  }

  /** Drive the real engine to seat 0's priority window whose options offer the
   *  Talisman's mana activation (the Talisman cast and untapped on the
   *  battlefield). Every earlier decision is answered by echoing one
   *  server-provided option: in a main phase play a land, cast the Talisman
   *  once the pool covers its {2}, or tap a Mountain (its single ability
   *  resolves without a wheel); any other step passes, because a Mountain
   *  tapped outside a main phase wastes its mana to the step boundary — the
   *  Talisman's {2} would then never pool. The Talisman's own activation
   *  (labelled "Activate Talisman …") is the STOP shape the test drives to. */
  async function driveTalismanUntil(
    request: APIRequestContext,
    base: string,
    stop: (d: WireDecision) => boolean,
  ): Promise<WireDecision> {
    const deadline = Date.now() + 30_000;
    while (Date.now() < deadline) {
      for (const seat of [0, 1]) {
        const p = await request.get(`${base}/api/tables/t1/matches/1/pending?seat=${seat}&token=${talismanToken(seat)}`);
        if (p.status() === 409) continue;
        expect(p.ok(), `talisman pending seat ${seat}`).toBe(true);
        const d = await p.json() as WireDecision;
        if (stop(d)) return d;

        let choices: number[] = [];
        if (d.kind === 'priority') {
          const v = await request.get(`${base}/api/tables/t1/matches/1/view?seat=${seat}&token=${talismanToken(seat)}`);
          expect(v.ok(), `talisman view seat ${seat}`).toBe(true);
          const step = (await v.json() as { step?: string }).step;
          if (step === 'main1' || step === 'main2') {
            const land = d.options.find((o) => o.kind === 'play_land');
            const cast = d.options.find((o) => o.kind === 'cast' && o.label.includes('Talisman'));
            const activate = d.options.find((o) => o.kind === 'activate' && !o.label.includes('Talisman'));
            const pass = d.options.find((o) => o.kind === 'pass');
            const choice = land ?? cast ?? activate ?? pass;
            expect(choice, `talisman setup ${d.kind} seat ${seat} needs an option`).toBeDefined();
            choices = choice ? [choice.index] : [];
          } else {
            const pass = d.options.find((o) => o.kind === 'pass');
            choices = pass ? [pass.index] : [];
          }
        } else if (d.kind === 'starting_player') {
          // CR 103.1's toss ask: echo its first option so the setup reaches
          // turn 1. Other non-priority asks keep the original empty answer.
          choices = d.options.length > 0 ? [d.options[0].index] : [];
        }
        const status = await postTalismanIntent(request, base, d, choices);
        // A 409 is a lost race against a decision that changed between the
        // pending GET and this POST (the drive loop runs fast); re-GET on the
        // next pass rather than failing the setup.
        if (status !== 204 && status !== 409) {
          expect(status, `talisman intent for ${d.kind} seat ${d.player}`).toBe(204);
        }
      }
      await new Promise((resolve) => setTimeout(resolve, 20));
    }
    throw new Error('talisman fixture did not reach the Talisman activation window');
  }

  test('a wheel-answered stage-1 re-opens the stage-2 colour wheel at the card', async ({ browser, request }) => {
    const b = TALIS as string;
    const pending = await driveTalismanUntil(request, b,
      (d) => d.kind === 'priority' && d.player === 0 &&
        d.options.some((o) => o.kind === 'activate' && o.label.includes('Talisman')));
    const activation = pending.options.find((o) => o.kind === 'activate' && o.label.includes('Talisman'))!;
    expect(activation.obj, 'the Talisman activation is card-anchored').toBeDefined();

    const ctx = await browser.newContext();
    try {
      const page = await ctx.newPage();
      await page.goto(`${b}/t/t1?seat=0&token=${talismanToken(0)}`, { waitUntil: 'domcontentloaded' });
      const tile = page.locator(`.quadrant[data-seat="0"] [data-obj="${activation.obj}"]`);
      await tile.waitFor({ state: 'visible', timeout: WAIT_MS });
      const action = tile.locator('xpath=..').locator('[data-single-action]');
      await action.waitFor({ state: 'visible', timeout: WAIT_MS });

      // Click one: the single-action badge posts the source-level activation
      // and the server answers with the stage-1 ability choose, FLATTENED
      // (fb-20260917T232800Z) into Add C / Add B / Add R — pip-tinted, one
      // wheel, no prose "Add B or R" option.
      await action.click();
      const stageOne = page.locator('body > [data-radial-picker]');
      await expect(stageOne).toBeVisible({ timeout: WAIT_MS });
      const black = stageOne.locator('button[aria-label="Add B"]');
      await expect(black).toBeVisible();
      await expect(stageOne.locator('button[aria-label="Add C"]')).toBeVisible();
      await expect(stageOne.locator('button[aria-label="Add R"]')).toBeVisible();

      // The one click under test: answering the flattened Add B pays the tap
      // and lands the mana with NO second decision — the wheel closes and no
      // stage-2 colour ask re-opens at the card.
      await black.click();
      await expect(page.locator('body > [data-radial-picker]')).toHaveCount(0, { timeout: WAIT_MS });
    } finally {
      await ctx.close();
    }
  });
});

// ui24 — construct the board the seed-1 game cannot produce. This shared
// two-human fixture casts the zero-cost Memnites and otherwise echoes wire
// options. Do not assume a particular opening-hand composition: the engine's
// seeded shuffle is intentionally free to change as start-of-game draws evolve.
test.describe('gorged [ui24] constructed board fixture', () => {
  test.skip(!FIXTURE, 'SMOKE_FIXTURE unset — run via scripts/smoke.sh');
  test.describe.configure({ mode: 'serial' });

  test('a board tile posts its non-zero wire index and a long menu escapes the quadrant (R-E4-1)', async ({ browser, request }) => {
    const b = FIXTURE as string;

    // Cast every zero-cost creature for both seats, declining early attack
    // declarations until seat 0 has the seven attackers this menu test needs.
    // The fixture deck is shuffled, so the first declaration need not have
    // seven Memnites even with a fixed seed.
    const attack = await driveFixtureUntil(request, b,
      (d) => d.kind === 'attackers' && d.player === 0 && d.options.length >= 7);
    expect(attack.options.length, 'fixture must offer at least seven attackers').toBeGreaterThanOrEqual(7);

    const ctx = await browser.newContext({ viewport: { width: 1000, height: 700 } });
    const page = await ctx.newPage();
    try {
      await page.goto(`${b}/t/t1?seat=0&token=${fixtureToken(0)}`, { waitUntil: 'domcontentloaded' });
      await page.locator('.quadrant[data-seat="0"] .card-tile[data-options]').first().waitFor({ state: 'visible', timeout: WAIT_MS });

      // The collapsed pile aggregates one attacker option per member. Pick a
      // NON-ZERO wire option from its seven-row menu; using the menu position
      // as the answer would select a different creature.
      const stack = page.locator('.quadrant[data-seat="0"] button.stacked[data-obj-group]').first();
      const pickAt = attack.options.findIndex((o) => o.index > 0 && o.obj !== undefined);
      const pick = attack.options[pickAt];
      expect(pick, 'the watched board option must have a non-zero wire index').toBeDefined();
      if (!pick || pick.obj === undefined) return;
      const tile = stack.locator('.card-tile[data-options]');
      await tile.waitFor({ state: 'visible', timeout: WAIT_MS });

      await stack.locator('button[aria-haspopup="menu"]').click();
      const menuItem = page.locator('body > .menu-pop button[role="menuitem"]').nth(pickAt);
      await menuItem.waitFor({ state: 'visible', timeout: WAIT_MS });
      await menuItem.click();
      await expect(tile).toHaveAttribute('data-selected', '1');

      // Commit every attacker through the API. The browser click above only
      // changes local selection because attackers is a Min=0 multi-pick ask.
      await postFixtureIntent(request, b, attack, attack.options.map((o) => o.index));

      // Seat 1 owns seven blockers. Every blocker gets one wire option per
      // attacker, so after expanding the collapsed stack its card menu has a
      // genuine seven-row list rather than the seed game's one-row menu.
      const blocks = await driveFixtureUntil(request, b,
        (d) => d.kind === 'blockers' && d.player === 1);
      expect(blocks.options.length, 'fixture must produce a long blocking menu').toBeGreaterThanOrEqual(7);

      await page.setViewportSize({ width: 650, height: 700 });
      await page.goto(`${b}/t/t1?seat=1&token=${fixtureToken(1)}`, { waitUntil: 'domcontentloaded' });
      const blockerStack = page.locator('.quadrant[data-seat="1"] button.stacked[data-obj-group]').first();
      await blockerStack.evaluate((el) => (el as HTMLElement).click());
      const badge = page.locator('.quadrant[data-seat="1"] button[aria-haspopup="menu"]').last();
      await badge.waitFor({ state: 'visible', timeout: WAIT_MS });
      await badge.click();
      const menu = page.locator('body > .menu-pop');
      await menu.waitFor({ state: 'visible', timeout: WAIT_MS });
      const measurement = await page.evaluate(() => {
        const menu = document.querySelector('body > .menu-pop') as HTMLElement;
        const quadrant = document.querySelector('.quadrant[data-seat="1"]') as HTMLElement;
        const m = menu.getBoundingClientRect();
        const q = quadrant.getBoundingClientRect();
        return {
          menuBottom: m.bottom,
          quadrantBottom: q.bottom,
          delta: m.bottom - q.bottom,
          rows: menu.querySelectorAll('[role="menuitem"]').length,
          parent: menu.parentElement?.tagName ?? '',
        };
      });
      console.log(`UI24_OVERFLOW ${JSON.stringify(measurement)}`);
      expect(measurement.rows).toBeGreaterThanOrEqual(7);
      expect(measurement.parent).toBe('BODY');
      expect(measurement.menuBottom).toBeLessThanOrEqual(700);
    } finally {
      await ctx.close();
    }
  });
});
