import type { CardView, Decision, PlayerView, StackView, View } from '../../protocol';
import { hiddenObj, objOfCardId, objOfStackId, seatOfPlayerId } from './ids';
import type { CardDto, CardView as WireCard, GameViewDto, StackObjectDto, StepKind, ZoneDto } from './wire';

/**
 * project.ts turns a ManaBrew `GameViewDto` into the client's `View` — the
 * inverse of gorge's server projection (internal/manabrew/state.go). The
 * result is the same shape the native stream paints, so every component
 * reads it unchanged. Fields ManaBrew does not carry (potential actions,
 * ability costs, mana production, pool restrictions, pending triggers, …)
 * are left absent; the plan's Degradations list names each one.
 */

const STEP: Record<StepKind, string> = {
  untap: 'untap', upkeep: 'upkeep', draw: 'draw', main1: 'main1',
  combatBegin: 'begin-combat', combatDeclareAttackers: 'declare-attackers',
  combatDeclareBlockers: 'declare-blockers', combatFirstStrikeDamage: 'combat-damage',
  combatDamage: 'combat-damage', combatEnd: 'end-combat', main2: 'main2',
  endOfTurn: 'end', cleanup: 'cleanup',
};

const PHASE: Record<string, string> = {
  untap: 'beginning', upkeep: 'beginning', draw: 'beginning', main1: 'main1',
  'begin-combat': 'combat', 'declare-attackers': 'combat', 'declare-blockers': 'combat',
  'combat-damage': 'combat', 'end-combat': 'combat', main2: 'main2', end: 'ending', cleanup: 'ending',
};

/** gorgeStep maps a ManaBrew StepKind onto gorge's step name ('' when unknown). */
export const gorgeStep = (s: string): string => STEP[s as StepKind] ?? '';

/** mbStep is gorgeStep's inverse (first-strike damage reads as combatDamage). */
export function mbStep(step: string): StepKind | '' {
  for (const [k, v] of Object.entries(STEP)) if (v === step && k !== 'combatFirstStrikeDamage') return k as StepKind;
  return '';
}

export interface ProjectOptions {
  /** The seat this view is for (the seat claim's own seat). */
  viewer: number;
  /** Last-known card faces by object id, for stack spells whose DTO carries only a name. */
  known?: ReadonlyMap<number, CardView>;
  /** The viewer's pending decision, already reconstructed from the open prompt. */
  decision?: Decision | null;
}

const seat = (id: string | null | undefined): number => seatOfPlayerId(id) ?? 0;

function typeLine(c: CardDto): string {
  const head = [...(c.supertypes ?? []), ...(c.types ?? [])].join(' ');
  const sub = (c.subtypes ?? []).join(' ');
  return sub === '' ? head : `${head} — ${sub}`;
}

const int = (s: string | null | undefined): number => {
  const n = s === null || s === undefined ? NaN : Number.parseInt(s, 10);
  return Number.isFinite(n) ? n : 0;
};

/** projectCard maps one visible CardDto onto a CardView. */
export function projectCard(c: CardDto, blockedBy?: ReadonlyMap<number, number[]>): CardView {
  const id = objOfCardId(c.id) ?? 0;
  const name = c.isFaceDown ? '' : (c.identity?.name ?? '');
  const out: CardView = {
    id, name, types: typeLine(c),
    printing: { name, ...(c.identity?.setCode ? { set: c.identity.setCode } : {}), ...(c.identity?.cardNumber ? { number: c.identity.cardNumber } : {}) },
    token: `#${id}`,
    tapped: c.tapped ?? false,
    power: int(c.power), toughness: int(c.toughness),
    damage: c.damage ?? 0,
    attacking: c.isAttacking ?? false,
    controller: seat(c.controllerId), owner: seat(c.ownerId),
    summon_sick: c.summoningSick ?? false,
  };
  if (c.manaCost) out.mana_cost = c.manaCost;
  if (c.isFaceDown) out.face_down = true;
  if (c.counters && Object.keys(c.counters).length > 0) out.counters = { ...c.counters };
  if (c.keywords && c.keywords.length > 0) out.keywords = [...c.keywords];
  const ap = seatOfPlayerId(c.attackingPlayerId);
  if (ap !== null) out.attacking_player = ap;
  const at = objOfCardId(c.attachedTo);
  if (at !== null) out.attached_to = at;
  const blockers = blockedBy?.get(id);
  if (blockers && blockers.length > 0) out.blocked_by = [...blockers];
  return out;
}

function hiddenCard(zone: string, owner: number, index: number): CardView {
  return {
    id: hiddenObj(zone, owner, index), name: '', types: '', face_down: true, printing: { name: '' }, token: '',
    tapped: false, power: 0, toughness: 0, damage: 0, attacking: false, controller: owner, owner, summon_sick: false,
  };
}

function zoneCards(z: ZoneDto | undefined, owner: number, blockedBy: ReadonlyMap<number, number[]>): CardView[] {
  if (!z) return [];
  return z.cards.map((c: WireCard, i) => (c.visibility === 'hidden' ? hiddenCard(String(z.zone), owner, i) : projectCard(c, blockedBy)));
}

function stackView(s: StackObjectDto, known: ReadonlyMap<number, CardView> | undefined): StackView {
  const id = objOfStackId(s.id) ?? 0;
  const controller = seat(s.controllerId);
  const spell = s.isPermanentSpell === true || s.isCasting === true;
  const out: StackView = { id, kind: spell ? 'spell' : 'ability', name: s.identity?.name ?? '', text: s.text ?? '', controller, targets: [], optional: false };
  const src = objOfCardId(s.sourceId);
  if (src !== null && src !== 0) out.source = src;
  if (spell) {
    const seen = known?.get(id);
    out.card = seen
      ? { ...seen, controller, tapped: false, attacking: false }
      : { id, name: out.name, types: '', printing: { name: out.name }, token: `#${id}`, tapped: false, power: 0, toughness: 0, damage: 0, attacking: false, controller, owner: seat(s.ownerId ?? s.controllerId), summon_sick: false };
  }
  for (const t of s.targets ?? []) {
    if (t.kind === 'player') out.targets.push({ player: seat(t.id), is_player: true, ...(t.oracle ? { label: t.oracle } : {}) });
    else {
      const obj = t.kind === 'spell' ? objOfStackId(t.id) : objOfCardId(t.id);
      if (obj !== null) out.targets.push({ obj, player: 0, is_player: false, ...(t.oracle ? { label: t.oracle } : {}) });
    }
  }
  return out;
}

/** projectView maps a GameViewDto onto the View the board paints for `viewer`. */
export function projectView(gv: GameViewDto, opts: ProjectOptions): View {
  const blockedBy = new Map<number, number[]>();
  for (const a of gv.combatAssignments ?? []) {
    const atk = objOfCardId(a.attackerId);
    const blk = objOfCardId(a.blockerId);
    if (atk === null || blk === null) continue;
    const list = blockedBy.get(atk) ?? [];
    list.push(blk);
    blockedBy.set(atk, list);
  }
  const zone = (kind: string, owner: string) => gv.zones.find((z) => z.zone === kind && z.ownerId === owner);
  const players: PlayerView[] = gv.players.map((p) => {
    const s = seat(p.id);
    const hand = zone('hand', p.id);
    const library = zone('library', p.id);
    const graveyard = zoneCards(zone('graveyard', p.id), s, blockedBy);
    const listed = hand !== undefined && hand.cards.length === hand.count && (s === opts.viewer || hand.count > 0);
    const out: PlayerView = {
      seat: s, name: p.name, life: p.life, lost: p.status === 'lost',
      library_size: library?.count ?? 0,
      hand_size: hand?.count ?? 0,
      graveyard_size: zone('graveyard', p.id)?.count ?? graveyard.length,
      hand: listed ? zoneCards(hand, s, blockedBy) : (null as unknown as CardView[]),
      battlefield: zoneCards(zone('battlefield', p.id), s, blockedBy),
      graveyard,
      exile: zoneCards(zone('exile', p.id), s, blockedBy),
      command: zoneCards(zone('command', p.id), s, blockedBy),
      pool: { ...(p.manaPool ?? {}) },
      commanders: [],
      completed_dungeons: 0,
      commander_casts: [],
    };
    const top = library?.cards[0];
    if (s === opts.viewer && top && top.visibility === 'visible') out.library_top = projectCard(top, blockedBy);
    if (gv.initiativeHolderId && gv.initiativeHolderId === p.id) out.has_initiative = true;
    const dmg = Object.entries(p.commanderDamage ?? {});
    if (dmg.length > 0) {
      out.cmd_damage = {};
      for (const [k, n] of dmg) {
        const obj = objOfCardId(k);
        if (obj !== null) out.cmd_damage[String(obj)] = n;
      }
    }
    return out;
  });
  // Commanders: the ids the commander-cast table names, resolved to
  // whichever visible zone holds them now.
  const everywhere = new Map<number, CardView>();
  for (const p of players) for (const z of [p.command, p.battlefield, p.graveyard, p.exile, p.hand ?? []]) for (const c of z) everywhere.set(c.id, c);
  gv.players.forEach((p, i) => {
    for (const [k, n] of Object.entries(p.commanderCasts ?? {})) {
      const obj = objOfCardId(k);
      const c = obj === null ? undefined : everywhere.get(obj);
      if (!c) continue;
      players[i].commanders.push(c);
      players[i].commander_casts.push(n);
    }
  });
  const step = gorgeStep(gv.step);
  const winner = gv.winnerId ? seatOfPlayerId(gv.winnerId) : null;
  const seats = Math.max(1, gv.players.length);
  return {
    viewer: opts.viewer, visibility: 'seat',
    turn: gv.turn, round: Math.max(1, Math.ceil(gv.turn / seats)),
    step, phase: PHASE[step] ?? '',
    active: seat(gv.activePlayerId), priority: seat(gv.priorityPlayerId),
    over: gv.gameOver, draw: gv.gameOver && winner === null, winner,
    players,
    stack: gv.stack.map((s) => stackView(s, opts.known)),
    pending: [],
    decision: opts.decision ?? null,
  };
}

/** rememberCards records every visible card face in a view by id, so a later stack spell can show its face. */
export function rememberCards(v: View, into: Map<number, CardView>): void {
  for (const p of v.players) for (const z of [p.hand ?? [], p.battlefield, p.graveyard, p.exile, p.command]) for (const c of z) if (c.id > 0 && !c.face_down) into.set(c.id, c);
  for (const s of v.stack) if (s.card && s.card.id > 0) into.set(s.card.id, s.card);
}
