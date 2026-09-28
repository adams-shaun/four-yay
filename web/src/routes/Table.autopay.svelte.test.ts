import { describe, expect, it, vi } from 'vitest';
import { render } from 'svelte/server';
import type { CardView, Decision, EventBody, PlayerView, PotentialAction, SeatInfo, View } from '../protocol';
import { initSeatContext } from '../lib/seat';

// The board-badge surface of spec §8's manual-mana rule. Table.svelte builds
// boardOptions from the seat panel's pending decision; with the auto-pay
// preference on it must drop a source's manual "Activate … for mana" badge by
// the shared predicate (lib/manualmana.ts manualManaHidden) — the same rule
// as the option list and the hot strip.
//
// The preference lives on the SeatPanelState Table constructs, and the effect
// that feeds it the table capability never runs in SSR. So this file's mock
// hands Table a SeatPanelState whose table offers auto-pay and whose seat
// preference is ON — the only difference from the real class. MatchState and
// the session are faked exactly as Table.svelte.test.ts fakes them.
const { fakeMatch } = vi.hoisted(() => {
  const shared = { view: null as View | null, seats: [] as SeatInfo[], events: [] as EventBody[] };
  class FakeMatch {
    match: number | null = 1;
    view = shared.view;
    seats = shared.seats;
    dvr = { match: 't1/1', head: 0, cursor: 0, live: true, events: shared.events, turnStarts: [0], gap: false };
    decision = null;
    halted: string | null = null;
    loadError: string | null = null;
    dispatch() {}
    apply() {}
    loadFinished() {}
  }
  return { fakeMatch: { shared, MatchState: FakeMatch } };
});
vi.mock('../lib/match.svelte', () => ({ MatchState: fakeMatch.MatchState }));
vi.mock('../lib/session.svelte', () => ({
  session: { stream: { onFrame: () => () => {} }, focus: async () => {}, unfocus: async () => {} },
}));
vi.mock('../lib/images', () => ({ images: { url: () => new Promise<string | null>(() => {}), offline: () => false } }));
vi.mock('../lib/seatpanel.svelte', async (importOriginal) => {
  const mod = await importOriginal<typeof import('../lib/seatpanel.svelte')>();
  class AutoPayingSeatPanelState extends mod.SeatPanelState {
    constructor(...args: ConstructorParameters<typeof mod.SeatPanelState>) {
      super(...args);
      this.setAutoManaAvailable(true);
      this.setAutoPayMana(true);
    }
  }
  return { ...mod, SeatPanelState: AutoPayingSeatPanelState };
});

import Table from './Table.svelte';

const land = (id: number, name: string): CardView => ({
  id, name, types: `Basic Land ${name}`, printing: { name }, token: '', tapped: false,
  power: 0, toughness: 0, damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false,
});
const blade: CardView = {
  id: 84, name: 'Probe Blade', types: 'Artifact Equipment', printing: { name: 'Probe Blade' }, token: '', tapped: false,
  power: 0, toughness: 0, damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false,
};
const player = (seat: number, battlefield: CardView[] = [], potential?: PotentialAction[]): PlayerView => ({
  seat, name: `P${seat}`, life: 20, lost: false, library_size: 30, hand_size: 0, graveyard_size: 0,
  completed_dungeons: 0,
  hand: [], battlefield, graveyard: [], exile: [], pool: {}, command: [], commanders: [], commander_casts: [],
  ...(potential ? { potential_actions: potential } : {}),
});
const seats: SeatInfo[] = [{ name: 'Ari', deck: 'mono-white', colour: '#e5484d' }, { name: 'Bo', deck: 'mono-green', colour: '#22c55e' }];

// The measured empty-pool window (aph-web-autopay-policy's Go probe): the
// lands' manual taps, pass and concede — the Equip {1} is NOT an option yet.
const tapWindow: Decision = {
  seq: 43, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1,
  options: [
    { index: 0, kind: 'activate', label: 'Activate Plains for mana', obj: 81, player: 0 },
    { index: 8, kind: 'pass', label: 'Pass priority', player: 0 },
    { index: 9, kind: 'concede', label: 'Concede', player: 0 },
  ],
};

function board(potential?: PotentialAction[]): string {
  initSeatContext('?seat=0&token=t');
  fakeMatch.shared.view = {
    viewer: 0, visibility: 'seat', turn: 3, round: 3, step: 'main1', phase: 'main1', active: 0, priority: 0,
    over: false, draw: false, winner: null, stack: [], pending: [], decision: tapWindow,
    players: [player(0, [land(81, 'Plains'), blade], potential), player(1)],
  };
  fakeMatch.shared.seats = seats;
  const { html } = render(Table, { props: { table: 't1' } });
  initSeatContext('');
  return html;
}

/** plainsTile is the Plains' own card-tile element: its data-options attribute is the badge's option count, absent when the decision offers the tile nothing. */
function plainsTile(html: string): string {
  const tile = /<div[^>]*class="card-tile[^"]*"[^>]*data-obj="81"[^>]*>/.exec(html)?.[0];
  expect(tile, 'the Plains renders as a board tile').toBeDefined();
  return tile!;
}

describe('Table.svelte — board badges under auto-pay read the shared manual-mana predicate', () => {
  it('keeps the Plains’ manual tap badge while an Equip is reachable only by floating mana first', () => {
    const tile = plainsTile(board([{ kind: 'ability', obj: 84, label: 'Probe Blade: Equip 1' }]));
    expect(tile).toContain('data-options="1"');
  });

  it('drops the badge when nothing but a planned or offered cast could use the mana', () => {
    const tile = plainsTile(board());
    expect(tile).not.toContain('data-options');
  });

  it('keeps the badge while a cast no plan pays (an X spell) is reachable only by floating mana first', () => {
    const tile = plainsTile(board([{ kind: 'cast', obj: 31, label: 'Cast Fireball' }]));
    expect(tile).toContain('data-options="1"');
  });

  it('keeps the badge while a widened projection kind (a Room unlock) needs the mana', () => {
    const tile = plainsTile(board([{ kind: 'unlock', obj: 93, label: 'Unlock Prop Room' }]));
    expect(tile).toContain('data-options="1"');
  });
});
