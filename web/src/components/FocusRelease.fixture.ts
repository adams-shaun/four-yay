import { mount } from 'svelte';
import type { CardView, Decision, Option, PlayerView, SeatInfo, View } from '../protocol';
import { SeatPanelState } from '../lib/seatpanel.svelte';
import { initialDvr, type DvrAction, type DvrState } from '../lib/dvr';
import { pileOpener } from '../lib/pileopener.svelte';
import '../app.css';
import HotButtonStrip from './HotButtonStrip.svelte';
import CentreStrip from './CentreStrip.svelte';
import LayoutDrawer from './LayoutDrawer.svelte';
import DvrBar from './DvrBar.svelte';
import ManaPaymentPanel from './ManaPaymentPanel.svelte';
import ListPrompt from './prompts/ListPrompt.svelte';
import PlaySettingsPanel from './PlaySettingsPanel.svelte';
import ZonePiles from './ZonePiles.svelte';
import PileHost from './PileHost.svelte';
import WireNotice from './WireNotice.svelte';

/**
 * FocusRelease.fixture.ts mounts the REAL hot wiring (HotButtonStrip +
 * SeatPanelState) beside one representative of every remaining focusable
 * table-control family (hotkey-focus2 F1–F7), so a mounted test can click a
 * control with the pointer and assert — through the live document — that the
 * next Space reaches the table hotkey instead of re-activating the control:
 *
 *  - window.__posts: every intent POST the seat panel sent;
 *  - window.__picks: the mana panel's onPick recorder;
 *  - window.__dvrActions / __flowOpened / __dismissed: the chrome recorders;
 *  - window.__newDecision: adopts a fresh answerable priority window;
 *  - window.__pile: the shared pileOpener singleton for the F5 pair.
 */

interface FixtureWindow {
  __posts: { seq: number; player: number; choices: number[] }[];
  __picks: number[];
  __dvrActions: DvrAction[];
  __flowOpened: boolean;
  __dismissed: boolean;
  __state: SeatPanelState;
  __newDecision: () => void;
}

const win = window as unknown as FixtureWindow;
const posts: { seq: number; player: number; choices: number[] }[] = [];
win.__posts = posts;
win.__picks = [];
win.__dvrActions = [];
win.__flowOpened = false;
win.__dismissed = false;

const decision: Decision = {
  seq: 7, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1,
  options: [
    { index: 5, kind: 'cast', label: 'Cast Bolt', obj: 11, player: 0 } as Option,
    { index: 9, kind: 'pass', label: 'Pass priority', player: 0 } as Option,
  ],
};

// Installed at module top, before any mount: every intent POST lands here.
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

const card = (id: number, name: string): CardView => ({
  id, name, types: 'Instant', mana_cost: '',
  printing: { name }, token: '', tapped: false, power: 0, toughness: 0,
  damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false,
});
const land = (id: number, name: string): CardView => ({ ...card(id, name), types: 'Land' });

const player: PlayerView = {
  seat: 0, name: 'Ari', life: 20, lost: false, library_size: 53, hand_size: 0,
  graveyard_size: 2, hand: [], battlefield: [land(21, 'Mountain')], graveyard: [card(31, 'Bolt'), card(32, 'Shock')], exile: [], pool: {},
  completed_dungeons: 0,
  command: [], commanders: [], commander_casts: [],
};
const seats: SeatInfo[] = [{ name: 'Ari', deck: 'deck', colour: '#e5484d', human: true }];
const view: View = {
  viewer: 0, visibility: 'seat', turn: 1, round: 1, step: 'main1', phase: 'main1',
  active: 0, priority: 0, over: false, draw: false, winner: null,
  players: [player], stack: [], pending: [], decision,
};

// Manual seat, zero pacing: every POST observed is synchronous with the key
// or click under test.
const state = new SeatPanelState('fx', 1, { seat: 0, token: 'tok' }, null);
state.skipEmpty = false;
state.setAuto(false);
state.setActPass(false);
state.settings = { ...state.settings, pacing: { stepMs: 0, resolveMs: 0 } };
state.adoptView(decision);
win.__state = state;

win.__newDecision = () => {
  state.adoptView({ ...decision, seq: (state.pending?.seq ?? 7) + 1 });
};

// F2 + F3 + the real hotkeys.
mount(HotButtonStrip, {
  target: document.querySelector('#strip')!,
  props: { view, seats, state, ctx: { seat: 0, token: 'tok' }, table: 'fx', match: 1 },
});

// F1: the chrome pills; onOpenFlow records the flow pill's effect.
mount(CentreStrip, {
  target: document.querySelector('#centre')!,
  props: {
    view, seats,
    // The controls bundle is what makes the flow pill render (its label is
    // the seat's active flow profile).
    controls: { state, ctx: { seat: 0, token: 'tok' }, table: 'fx', match: 1 },
    onOpenFlow: () => { win.__flowOpened = true; },
  },
});

// F1: the drawer the layout pill opens (the same layoutStore singleton).
mount(LayoutDrawer, { target: document.querySelector('#layout')!, props: {} });

// F4: the mana-payment panel with a stubbed window: one Mountain source
// offering R, plus the window's four control actions. onPick records.
const mpDecision: Decision & { mana_payment: { card: number; cost: { generic: number; mana: [number, number, number, number, number, number] }; owed: { generic: number; mana: [number, number, number, number, number, number] }; pool: [number, number, number, number, number, number] } } = {
  seq: 7, player: 0, kind: 'payment', prompt: 'Pay for Bolt.', min: 1, max: 1,
  mana_payment: {
    card: 11,
    cost: { generic: 1, mana: [0, 0, 1, 0, 0, 0] },
    owed: { generic: 0, mana: [0, 0, 1, 0, 0, 0] },
    pool: [0, 0, 0, 0, 0, 0],
  },
  options: [
    { index: 1, kind: 'mana', label: 'R', obj: 21, player: 0 } as Option,
    { index: 2, kind: 'autofill', label: 'Auto-fill', player: 0 } as Option,
    { index: 3, kind: 'undo_tap', label: 'Undo last tap', player: 0 } as Option,
    { index: 4, kind: 'done', label: 'Pay 1R', player: 0 } as Option,
    { index: 5, kind: 'cancel_cast', label: 'Cancel', player: 0 } as Option,
  ],
} as never;
mount(ManaPaymentPanel, {
  target: document.querySelector('#mp')!,
  props: {
    decision: mpDecision,
    view,
    busy: false,
    onPick: (index: number) => { win.__picks.push(index); },
  },
});

// F5: a real pile opener (ZonePiles over the viewer's graveyard) and the one
// modal host reading the shared opener.
mount(ZonePiles, {
  target: document.querySelector('#zones')!,
  props: { player, who: 'Ari', width: 220, options: null },
});
mount(PileHost, { target: document.querySelector('#pilehost')!, props: { view, seats, options: null } });

// F6: the DVR transport.
const dvr: DvrState = { ...initialDvr, head: 3, cursor: 3, events: [] };
mount(DvrBar, {
  target: document.querySelector('#dvr')!,
  props: {
    dvr,
    onAction: (a: DvrAction) => { win.__dvrActions.push(a); },
  },
});

// F7: the settings panel over the real seat state (settings edits land on
// state.settings, which the tests read through __state).
mount(PlaySettingsPanel, {
  target: document.querySelector('#settings')!,
  props: { state, showLog: true, onToggleLog: () => {} },
});

// F3's persistent half: ListPrompt's multi-pick chips stay enabled after a
// click (a modes decision toggles into `picked`; nothing unmounts), so the
// focus-released assertion is NOT vacuous the way the single-post answer
// buttons' busy-disable cycle would make it. Its logic is its own manual
// SeatPanelState with the modes window pending.
const listState = new SeatPanelState('fx', 1, { seat: 0, token: 'tok' }, null);
listState.setAuto(false);
listState.setActPass(false);
listState.settings = { ...listState.settings, pacing: { stepMs: 0, resolveMs: 0 } };
const modesDecision: Decision = {
  seq: 12, player: 0, kind: 'modes', prompt: 'Choose modes.', min: 1, max: 2,
  options: [
    { index: 31, kind: 'mode', label: 'Mode A', player: 0 } as Option,
    { index: 32, kind: 'mode', label: 'Mode B', player: 0 } as Option,
  ],
};
listState.adoptView(modesDecision);
mount(ListPrompt, {
  target: document.querySelector('#listmount')!,
  props: { decision: modesDecision, logic: listState },
});

// F6-class chrome: the wire notice dismiss.
mount(WireNotice, {
  target: document.querySelector('#wire')!,
  props: { text: 'reconnected', onDismiss: () => { win.__dismissed = true; } },
});

// Exported for the tests' pile assertions.
(win as unknown as { __pile: typeof pileOpener }).__pile = pileOpener;
