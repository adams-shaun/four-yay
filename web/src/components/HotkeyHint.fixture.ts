import { mount } from 'svelte';
import type { Decision, Option, PlayerView, SeatInfo, View } from '../protocol';
import { SeatPanelState } from '../lib/seatpanel.svelte';
import { clientBreadcrumbs } from '../lib/breadcrumbs';
import '../app.css';
import HotButtonStrip from './HotButtonStrip.svelte';

/**
 * HotkeyHint fixture mounts the REAL HotButtonStrip (whose onMount owns the
 * document-level hotkeys and whose markup now carries the suppression cue),
 * with a real SeatPanelState, so a mounted test can drive a real keyboard
 * press at a focused control and observe the cue the player sees:
 *
 *  - window.__posts: every intent POST body the seat panel sent (a suppressed
 *    key must post NOTHING; a key the felt owns posts the pass);
 *  - window.__breadcrumbs(): the client diagnostic snapshot, so the test can
 *    assert the 'hotkey-suppressed' breadcrumb — the half that survives into
 *    a feedback report, which is how this family of reports was filed.
 */

interface FixtureWindow {
  __posts: unknown[];
  __state: SeatPanelState;
  __view: View;
  __breadcrumbs: () => Record<string, unknown>;
}

const win = window as unknown as FixtureWindow;

const decision: Decision = {
  seq: 7, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1,
  options: [
    { index: 5, kind: 'cast', label: 'Cast Bolt', obj: 11, player: 0 } as Option,
    { index: 9, kind: 'pass', label: 'Pass priority', player: 0 } as Option,
  ],
};

const posts: unknown[] = [];
win.__posts = posts;
win.__breadcrumbs = () => clientBreadcrumbs.snapshot();
window.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
  const u = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
  const method = init?.method ?? 'GET';
  if (method === 'POST' && u.includes('/intent')) {
    posts.push(init?.body === undefined ? null : JSON.parse(String(init.body)));
    return new Response(null, { status: 200 });
  }
  if (u.includes('/pending')) {
    return new Response(JSON.stringify(decision), { status: 200, headers: { 'Content-Type': 'application/json' } });
  }
  return new Response(JSON.stringify({ code: 'http', message: 'fixture stub' }), { status: 404, headers: { 'Content-Type': 'application/json' } });
}) as typeof window.fetch;

const player: PlayerView = {
  seat: 0, name: 'Ari', life: 20, lost: false, library_size: 53, hand_size: 0,
  graveyard_size: 0, hand: [], battlefield: [], graveyard: [], exile: [], pool: {},
  completed_dungeons: 0,
  command: [], commanders: [], commander_casts: [],
};
const seats: SeatInfo[] = [{ name: 'Ari', deck: 'deck', colour: '#e5484d', human: true }];
const view: View = {
  viewer: 0, visibility: 'seat', turn: 1, round: 1, step: 'main1', phase: 'main1',
  active: 0, priority: 0, over: false, draw: false, winner: null,
  players: [player], stack: [], pending: [], decision,
};
win.__view = view;

// Manual seat: auto off, pass-after-acting off, zero pacing — every POST the
// fixture observes was caused by the key under test, never by a machine path.
const state = new SeatPanelState('fx', 1, { seat: 0, token: 'tok' }, null);
state.skipEmpty = false;
state.setAuto(false);
state.setActPass(false);
state.settings = { ...state.settings, pacing: { stepMs: 0, resolveMs: 0 } };
state.adoptView(decision);
win.__state = state;

mount(HotButtonStrip, {
  target: document.querySelector('#fixture')!,
  props: { view, seats, state, ctx: { seat: 0, token: 'tok' }, table: 'fx', match: 1 },
});
