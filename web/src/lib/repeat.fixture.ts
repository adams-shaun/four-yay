import type { CardView, Decision, StackView, View } from '../protocol';

export const repeatCard: CardView = {
  id: 20, name: 'Altar', types: 'Artifact', printing: { name: 'Altar' }, token: '',
  tapped: false, power: 0, toughness: 0, damage: 0, attacking: false,
  controller: 0, owner: 0, summon_sick: false,
};
export const repeatDecision = (seq = 1): Decision => ({
  seq, kind: 'priority', player: 0, prompt: 'Priority', min: 1, max: 1,
  options: [
    { index: 9, kind: 'ability', label: 'Sacrifice a creature: mill', obj: 20, player: 0 },
    { index: 3, kind: 'pass', label: 'Pass', player: 0 },
  ],
});
export const repeatStack = (id = 30, controller = 0): StackView => ({
  id, name: 'Altar', source: 20, kind: 'ability', text: 'Mill', controller, targets: [], optional: false,
});
export const repeatView = (decision: Decision | null = repeatDecision(), stack: StackView[] = []): View => ({
  viewer: 0, visibility: 'seat', turn: 1, round: 1, step: 'main1', phase: 'main',
  active: 0, priority: 0, over: false, draw: false, winner: null,
  players: [{ seat: 0, name: 'Pilot', life: 20, lost: false, library_size: 40, hand_size: 0,
    graveyard_size: 0, hand: [], battlefield: [repeatCard], graveyard: [], exile: [], pool: {},
    command: [], commanders: [], commander_casts: [], completed_dungeons: 0 }],
  stack, pending: [], decision,
});
