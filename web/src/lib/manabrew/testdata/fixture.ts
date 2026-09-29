import type { CardView, Decision, View } from '../../../protocol';
import type { EngineMessage, GameViewDto, AgentPrompt } from '../wire';
import raw from './capture.json';

/**
 * The captured fixture (capture.json, recorded from a real `gorged
 * -manabrew` by capture.py): at each decision of the human seat, the native
 * view + decision and the ManaBrew `GET …/state` answer for the same
 * instant. Every `text` field was blanked before commit.
 */
export interface CaptureRecord {
  native: { view: View; decision: Decision | null };
  manabrew: EngineMessage[];
}
export interface CaptureRun { run: string; records: CaptureRecord[] }

export const runs = raw as unknown as CaptureRun[];
export const allRecords = runs.flatMap((r) => r.records);

export const stateOf = (r: CaptureRecord): GameViewDto => {
  const m = r.manabrew.find((x) => x.kind === 'state');
  if (!m || m.kind !== 'state') throw new Error('record has no state');
  return m.gameView;
};
export const promptOf = (r: CaptureRecord): AgentPrompt | null => {
  const m = r.manabrew.find((x) => x.kind === 'prompt');
  return m && m.kind === 'prompt' ? m : null;
};

/** The card fields a ManaBrew GameViewDto carries, for comparing a projection with the native card. */
export function carriedCard(c: CardView) {
  const out: Record<string, unknown> = {
    id: c.id, name: c.name, types: c.types, token: c.token, tapped: c.tapped, power: c.power,
    toughness: c.toughness, damage: c.damage, attacking: c.attacking, controller: c.controller,
    owner: c.owner, summon_sick: c.summon_sick, printing: c.printing?.name ?? '',
  };
  if (c.mana_cost) out.mana_cost = c.mana_cost;
  if (c.face_down) out.face_down = true;
  if (c.counters && Object.keys(c.counters).length) out.counters = c.counters;
  if (c.keywords?.length) out.keywords = c.keywords;
  if (c.attacking_player !== undefined && c.attacking_player !== null) out.attacking_player = c.attacking_player;
  if (c.attached_to) out.attached_to = c.attached_to;
  if (c.blocked_by?.length) out.blocked_by = c.blocked_by;
  return out;
}

/** The view fields a ManaBrew GameViewDto carries. */
export function carriedView(v: View) {
  return {
    viewer: v.viewer, turn: v.turn, step: v.step, phase: v.phase, active: v.active, priority: v.priority,
    over: v.over, winner: v.winner,
    players: v.players.map((p) => ({
      seat: p.seat, name: p.name, life: p.life, lost: p.lost, library_size: p.library_size,
      hand_size: p.hand_size, graveyard_size: p.graveyard_size,
      hand: p.hand === null ? null : p.hand.map(carriedCard),
      battlefield: p.battlefield.map(carriedCard), graveyard: p.graveyard.map(carriedCard),
      exile: p.exile.map(carriedCard), command: p.command.map(carriedCard), pool: p.pool,
      has_initiative: p.has_initiative ?? false,
    })),
    stack: v.stack.map((s) => ({ id: s.id, spell: s.kind === 'spell', name: s.name, controller: s.controller })),
  };
}
