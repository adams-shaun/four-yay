import type { CardView, PlayerView, StackView, View } from '../../protocol';

/** Test builders for client-model views. Not imported by app code. */
export const card = (id: number, extra: Partial<CardView> = {}): CardView => ({
  id, name: `c${id}`, types: 'Creature', tapped: false, power: 1, toughness: 1, damage: 0, attacking: false,
  controller: 0, owner: 0, summon_sick: false, printing: { name: `c${id}` }, token: `#${id}`, ...extra,
});

export const player = (seat: number, extra: Partial<PlayerView> = {}): PlayerView => ({
  seat, name: `p${seat}`, life: 20, lost: false, library_size: 40, hand_size: 0, graveyard_size: 0,
  hand: [], battlefield: [], graveyard: [], exile: [], command: [], commanders: [], pool: {},
  completed_dungeons: 0, commander_casts: [], ...extra,
} as PlayerView);

export const spell = (id: number, controller: number, c: CardView): StackView => ({ id, kind: 'spell', name: c.name, text: '', controller, targets: [], card: c, optional: false });
export const ability = (id: number, controller: number, source: number): StackView => ({ id, kind: 'trigger', name: `t${id}`, text: '', controller, source, targets: [], optional: false });

export const view = (players: PlayerView[], stack: StackView[] = [], extra: Partial<View> = {}): View => ({
  viewer: 0, visibility: 'seat', turn: 1, round: 1, step: 'main1', phase: 'main1', active: 0, priority: 0,
  over: false, draw: false, winner: null, players, stack, pending: [], decision: null, ...extra,
});
