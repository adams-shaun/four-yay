import { describe, expect, it, vi } from 'vitest';
import { render } from 'svelte/server';
import type { CardView, Decision, Option, PlayerView, SeatInfo, StackView, View } from '../../protocol';
import { SeatPanelState } from '../../lib/seatpanel.svelte';
import PromptDock from './PromptDock.svelte';
import SeatPanel from '../SeatPanel.svelte';

// SSR via svelte/server (the repo's component-test pattern): onMount and
// $effect never run, nothing reaches the network.
vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  postIntent: vi.fn(),
  fetchPending: vi.fn(),
}));
vi.mock('../../lib/images', () => ({ images: { url: () => new Promise<string | null>(() => {}), offline: () => false } }));

const ctx = { seat: 0, token: 'tok' };
const seats: SeatInfo[] = [
  { name: 'Ari', deck: 'burn', colour: '#e5484d' },
  { name: 'Mira', deck: 'stompy', colour: '#30a46c' },
];
const card = (id: number, name: string, controller: number, extra: Partial<CardView> = {}): CardView => ({
  id, name, types: 'Creature — Beast', printing: { name }, token: `#${id}`, tapped: false, power: 4, toughness: 5,
  damage: 0, attacking: false, controller, owner: controller, summon_sick: false, ...extra,
} as CardView);
const player = (seat: number, battlefield: CardView[] = []): PlayerView => ({
  seat, name: seats[seat].name, life: 20, lost: false, library_size: 53, hand_size: 0, graveyard_size: 0, hand: [],
  battlefield, graveyard: [], exile: [], pool: {}, command: [], commanders: [], commander_casts: [], completed_dungeons: 0,
} as unknown as PlayerView);
const opt = (index: number, kind: string, label: string, extra: Partial<Option> = {}): Option => ({ index, kind, label, player: 1, ...extra });

const bolt = card(7, 'Lightning Bolt', 0, { types: 'Instant' });
const goyf = card(20, 'Tarmogoyf', 1);
const view = (decision: Decision | null): View => ({
  viewer: 0, visibility: 'seat', turn: 7, round: 4, step: 'main1', phase: 'main1', active: 0, priority: 0,
  over: false, draw: false, winner: null, players: [player(0), player(1, [goyf])],
  stack: [{ id: 7, kind: 'spell', name: 'Lightning Bolt', text: '', controller: 0, targets: [], card: bolt, optional: false } as StackView],
  pending: [], decision,
} as unknown as View);

const target: Decision = {
  seq: 11, player: 0, kind: 'target', prompt: 'Choose a target for Lightning Bolt', min: 1, max: 1, source: 7,
  target_effect: { api: 'DealDamage', damage: { amount: 3 } },
  options: [opt(0, 'player', 'Mira', { player: 1 }), opt(1, 'permanent', 'Tarmogoyf (Mira)', { obj: 20 })],
} as Decision;

function seatState(d: Decision): SeatPanelState {
  const s = new SeatPanelState('t1', 1, ctx, null, null);
  s.adoptView(d);
  return s;
}

describe('PromptDock', () => {
  it('renders the anatomy: source line, serif title, plain line, numbered chips', () => {
    const logic = seatState(target);
    const { body } = render(PromptDock, { props: { view: view(target), logic, seat: 0, placement: 'rail' } });
    expect(body).toContain('data-prompt-dock');
    expect(body).toContain('data-placement="rail"');
    expect(body).toContain("Your Lightning Bolt · instant you're casting");
    expect(body).toMatch(/data-prompt-title[^>]*>Choose a target</);
    expect(body).toContain('Deals 3 damage to the target.');
    expect(body).toContain('data-target-chips');
    expect(body).toMatch(/data-digit="1"[^>]*>1</);
    expect(body).toMatch(/data-digit="2"[^>]*>2</);
    expect(body).toContain('Tarmogoyf');
    expect(body).toContain('4/5');
  });

  it('floats with a grip when placed floating', () => {
    const logic = seatState(target);
    const { body } = render(PromptDock, { props: { view: view(target), logic, seat: 0, placement: 'floating' } });
    expect(body).toContain('data-placement="floating"');
    expect(body).toContain('data-dock-grip');
  });

  it('never takes a priority window: that is the action button', () => {
    const prio: Decision = { seq: 12, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1, options: [opt(0, 'pass', 'Pass')] };
    const logic = seatState(prio);
    const { body } = render(PromptDock, { props: { view: view(prio), logic, seat: 0, placement: 'rail' } });
    expect(body).not.toContain('data-prompt-dock');
  });

  it('summarises blocks with the damage you would take and offers No blocks', () => {
    const thragtusk = card(30, 'Thragtusk', 1, { power: 5, toughness: 3, attacking: true, attacking_player: 0 });
    const confidant = card(10, 'Dark Confidant', 0, { power: 2, toughness: 1 });
    const blocks: Decision = {
      seq: 13, player: 0, kind: 'blockers', prompt: 'turn 7 — declare blockers', min: 0, max: 1,
      options: [opt(0, 'block', 'Dark Confidant blocks Thragtusk', { obj: 10, attacker: 30, player: 0 })],
    };
    const v = { ...view(blocks), stack: [], players: [player(0, [confidant]), player(1, [thragtusk])] } as View;
    const logic = seatState(blocks);
    const { body } = render(PromptDock, { props: { view: v, logic, seat: 0, placement: 'rail' } });
    expect(body).toMatch(/data-prompt-title[^>]*>Declare blockers</);
    expect(body).toContain('Thragtusk 5/3 attacking you');
    expect(body).toContain("you'd take 5 (20 → 15)");
    expect(body).toContain('data-no-blocks');
    expect(body).toContain('Dark Confidant blocks Thragtusk');
  });
});

describe('SeatPanel strip while a dock is mounted', () => {
  it('points at the dock instead of drawing a second answer surface', () => {
    const logic = seatState(target);
    logic.dockCount = 1;
    const { body } = render(SeatPanel, { props: { view: view(target), seats, ctx, table: 't1', match: 1, state: logic, placement: 'strip' } });
    expect(body).toContain('data-dock-pointer');
    expect(body).not.toContain('data-target-chips');
    expect(body).not.toContain('data-answer-surface="true"');
  });

  it('still answers priority itself', () => {
    const prio: Decision = { seq: 14, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1, options: [opt(0, 'cast', 'Cast Bolt'), opt(1, 'pass', 'Pass')] };
    const logic = seatState(prio);
    logic.dockCount = 1;
    const { body } = render(SeatPanel, { props: { view: view(prio), seats, ctx, table: 't1', match: 1, state: logic, placement: 'strip' } });
    expect(body).not.toContain('data-dock-pointer');
    expect(body).toContain('Cast Bolt');
  });
});
