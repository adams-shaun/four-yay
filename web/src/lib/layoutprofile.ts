/**
 * layoutprofile is the pure model of ONE layout profile (UI rework spec §2):
 * everything about how the table is drawn — the opponents' share of the
 * height, how opponents are arranged and oriented, the board template's
 * regions, how cards stack and overflow, how much of the hand shows, and
 * where the panels go. It is client-side only and never sent to the server.
 *
 * It absorbs the old per-zone `layoutsettings.ts` model: `migrateLegacy`
 * reads a `gorge.layoutsettings.v1` blob into a profile (the library does
 * that once, on first load). Per-zone card scale is gone — the sizing rules
 * in cardsizing.ts replace battlefield sizing; the hand's legacy scale is
 * retained as a hand-card size multiplier alongside alignments and hand peek.
 *
 * Pure TypeScript, the playsettings/profiles house pattern: a version field
 * and a strict validate (corrupt means null, never a partial merge), with
 * the documented exception that row numbers are normalised (compacted to
 * 0..n-1) rather than rejected, because the drawer edits one region at a
 * time and a gap in the middle is a transient state, not corruption.
 */

export type Arrangement = 'columns' | 'grid' | 'focus';
export type Orientation = 'mirrored' | 'same';
export type RegionKey = 'creatures' | 'lands' | 'others';
export type Anchor = 'start' | 'center' | 'end';
export type RegionOrder = 'entry' | 'name' | 'power';
export type Overflow = 'overlap' | 'scroll' | 'wrap';
export type RailSide = 'left' | 'right' | 'hidden';
export type LogMode = 'remember' | 'show' | 'hide';
export type PromptPlacement = 'table' | 'dock' | 'dock-bottom' | 'float';

export const ARRANGEMENTS: readonly Arrangement[] = ['columns', 'grid', 'focus'];
export const ORIENTATIONS: readonly Orientation[] = ['mirrored', 'same'];
export const REGION_KEYS: readonly RegionKey[] = ['creatures', 'lands', 'others'];
export const ANCHORS: readonly Anchor[] = ['start', 'center', 'end'];
export const REGION_ORDERS: readonly RegionOrder[] = ['entry', 'name', 'power'];
export const OVERFLOWS: readonly Overflow[] = ['overlap', 'scroll', 'wrap'];
export const RAIL_SIDES: readonly RailSide[] = ['left', 'right', 'hidden'];
export const LOG_MODES: readonly LogMode[] = ['remember', 'show', 'hide'];
export const PROMPT_PLACEMENTS: readonly PromptPlacement[] = ['table', 'dock', 'dock-bottom', 'float'];

export const ARRANGEMENT_LABELS: Record<Arrangement, string> = { columns: 'Columns', grid: 'Grid', focus: 'Focus' };
export const ORIENTATION_LABELS: Record<Orientation, string> = { mirrored: 'Mirrored', same: 'Same as mine' };
export const REGION_LABELS: Record<RegionKey, string> = { creatures: 'Creatures', lands: 'Lands', others: 'Other permanents' };
export const ANCHOR_LABELS: Record<Anchor, string> = { start: 'Left', center: 'Centre', end: 'Right' };
export const ORDER_LABELS: Record<RegionOrder, string> = { entry: 'Entry order', name: 'Name', power: 'Power' };
export const OVERFLOW_LABELS: Record<Overflow, string> = { overlap: 'Overlap', scroll: 'Scroll', wrap: 'Wrap' };
export const RAIL_LABELS: Record<RailSide, string> = { left: 'Left', right: 'Right', hidden: 'Hidden' };
export const LOG_LABELS: Record<LogMode, string> = { remember: 'Per table', show: 'Show', hide: 'Hide' };
export const PROMPT_LABELS: Record<PromptPlacement, string> = { table: 'Near table', dock: 'Rail top', 'dock-bottom': 'Rail bottom', float: 'Floating' };

/** The width weights the drawer offers (S/M/L/XL). Any value in range validates. */
export const WEIGHT_STEPS: readonly { value: number; label: string }[] = [
  { value: 0.4, label: 'S' },
  { value: 0.7, label: 'M' },
  { value: 1, label: 'L' },
  { value: 1.6, label: 'XL' },
];

export const SPLIT_MIN = 0.18;
export const SPLIT_MAX = 0.72;
export const RAIL_MIN = 0.14;
export const RAIL_MAX = 0.4;
export const WEIGHT_MIN = 0.2;
export const WEIGHT_MAX = 3;
export const ART_MIN = 0;
export const ART_MAX = 120;
export const HAND_MIN = 0.3;
export const HAND_MAX = 1;
export const HAND_SCALE_MIN = 0.6;
export const HAND_SCALE_MAX = 1.6;
export const HAND_SCALE_DEFAULT = 1;
export const MAX_ROWS = 3;

export interface Region {
  /** 0-based row of the seat's board, nearest the centre first */
  row: number;
  /** share of the row's width against the row's other regions */
  weight: number;
  /** which end the region fills from */
  anchor: Anchor;
  /** how the region's cards are ordered; ties always break by entry order */
  order: RegionOrder;
}

export interface LayoutProfile {
  version: 1;
  table: {
    /** the opponents' share of the board height (the centre bar's position) */
    split: number;
    arrangement: Arrangement;
    orientation: Orientation;
  };
  regions: Record<RegionKey, Region>;
  cards: {
    /** stack identical permanents into one ×N pile */
    stacking: boolean;
    /** what a full row does */
    overflow: Overflow;
    /** card width (px) below which cards render as art tiles; 0 never */
    artBelow: number;
    /** draw each region's outline and label */
    outlines: boolean;
  };
  hand: {
    /** fraction of a hand card visible at rest */
    visible: number;
    /** full hand-card size multiplier */
    scale: number;
    /** whether a hand card rises to full height on hover/focus */
    raise: boolean;
  };
  panels: {
    rail: RailSide;
    /** rail width as a fraction of the viewport */
    railWidth: number;
    log: LogMode;
    /** where the decision prompt goes; x/y are the floating prompt's top-left as a fraction of the viewport */
    prompt: { placement: PromptPlacement; x: number; y: number };
  };
}

export type PresetId = 'duel' | 'duel3' | 'cmd4' | 'grid6' | 'focus8';
export const PRESET_IDS: readonly PresetId[] = ['duel', 'duel3', 'cmd4', 'grid6', 'focus8'];
export const PRESET_LABELS: Record<PresetId, string> = {
  duel: 'Duel',
  duel3: 'Duel, 3 rows',
  cmd4: 'Commander 4',
  grid6: '6 players, grid',
  focus8: '8 players, focus',
};

const R = (row: number, weight: number, anchor: Anchor, order: RegionOrder): Region => ({ row, weight, anchor, order });

/** A preset fixes the table and the board template; cards, hand and panels stay the player's. */
interface PresetShape {
  table: LayoutProfile['table'];
  regions: Record<RegionKey, Region>;
}

const PRESET_SHAPES: Record<PresetId, PresetShape> = {
  duel: {
    table: { split: 0.4, arrangement: 'columns', orientation: 'mirrored' },
    regions: { creatures: R(0, 1, 'center', 'entry'), lands: R(1, 1, 'start', 'name'), others: R(1, 0.7, 'end', 'entry') },
  },
  duel3: {
    table: { split: 0.4, arrangement: 'columns', orientation: 'mirrored' },
    regions: { creatures: R(0, 1, 'center', 'entry'), others: R(1, 1, 'center', 'entry'), lands: R(2, 1, 'start', 'name') },
  },
  cmd4: {
    table: { split: 0.46, arrangement: 'columns', orientation: 'mirrored' },
    regions: { creatures: R(0, 1, 'center', 'entry'), lands: R(1, 1, 'start', 'name'), others: R(1, 0.7, 'end', 'entry') },
  },
  grid6: {
    table: { split: 0.52, arrangement: 'grid', orientation: 'mirrored' },
    regions: { creatures: R(0, 1, 'center', 'entry'), lands: R(1, 1, 'start', 'name'), others: R(1, 0.7, 'end', 'entry') },
  },
  focus8: {
    table: { split: 0.5, arrangement: 'focus', orientation: 'mirrored' },
    regions: { creatures: R(0, 1, 'center', 'entry'), lands: R(1, 1, 'start', 'name'), others: R(1, 0.7, 'end', 'entry') },
  },
};

/**
 * defaultSplit is the centre bar's reset position for a seat count (the
 * double-click reset): the split of the preset built for that many seats.
 */
export function defaultRailWidth(_seats = 2): number {
  return 0.21;
}

export function defaultSplit(seats: number): number {
  if (seats <= 2) return PRESET_SHAPES.duel.table.split;
  if (seats <= 4) return PRESET_SHAPES.cmd4.table.split;
  if (seats <= 6) return PRESET_SHAPES.grid6.table.split;
  return PRESET_SHAPES.focus8.table.split;
}

/**
 * defaultProfile is the shipped layout: the Duel preset with the default cards, hand and panels.
 * Prompts default to the lower slot of the rail ('dock-bottom'; 'dock' is the rail's top).
 */
export function defaultProfile(): LayoutProfile {
  return withPreset(
    {
      version: 1,
      table: { ...PRESET_SHAPES.duel.table },
      regions: cloneRegions(PRESET_SHAPES.duel.regions),
      cards: { stacking: true, overflow: 'overlap', artBelow: 58, outlines: false },
      hand: { visible: 0.72, scale: HAND_SCALE_DEFAULT, raise: true },
      panels: { rail: 'right', railWidth: defaultRailWidth(), log: 'remember', prompt: { placement: 'dock-bottom', x: 0.6, y: 0.12 } },
    },
    'duel',
  );
}

function cloneRegions(r: Record<RegionKey, Region>): Record<RegionKey, Region> {
  return { creatures: { ...r.creatures }, lands: { ...r.lands }, others: { ...r.others } };
}

/** cloneProfile is a deep copy; every write path returns fresh objects. */
export function cloneProfile(p: LayoutProfile): LayoutProfile {
  return {
    version: 1,
    table: { ...p.table },
    regions: cloneRegions(p.regions),
    cards: { ...p.cards },
    hand: { ...p.hand },
    panels: { ...p.panels, prompt: { ...p.panels.prompt } },
  };
}

/** withPreset replaces the table and the board template with a preset's, keeping the player's cards, hand and panels. */
export function withPreset(p: LayoutProfile, id: PresetId): LayoutProfile {
  const shape = PRESET_SHAPES[id];
  return { ...cloneProfile(p), table: { ...shape.table }, regions: cloneRegions(shape.regions) };
}

/** presetOf names the preset whose table and template this profile carries, or null. */
export function presetOf(p: LayoutProfile): PresetId | null {
  for (const id of PRESET_IDS) {
    const s = PRESET_SHAPES[id];
    if (
      p.table.split === s.table.split &&
      p.table.arrangement === s.table.arrangement &&
      p.table.orientation === s.table.orientation &&
      REGION_KEYS.every((k) => sameRegion(p.regions[k], s.regions[k]))
    ) {
      return id;
    }
  }
  return null;
}

function sameRegion(a: Region, b: Region): boolean {
  return a.row === b.row && a.weight === b.weight && a.anchor === b.anchor && a.order === b.order;
}

/** sameProfile is deep equality over every field (the pill's "is this still the saved profile" test). */
export function sameProfile(a: LayoutProfile, b: LayoutProfile): boolean {
  return JSON.stringify(canonical(a)) === JSON.stringify(canonical(b));
}

/** canonical orders keys deterministically so equality and export never depend on insertion order. */
function canonical(p: LayoutProfile): unknown {
  return {
    version: 1,
    table: { split: p.table.split, arrangement: p.table.arrangement, orientation: p.table.orientation },
    regions: Object.fromEntries(REGION_KEYS.map((k) => [k, { row: p.regions[k].row, weight: p.regions[k].weight, anchor: p.regions[k].anchor, order: p.regions[k].order }])),
    cards: { stacking: p.cards.stacking, overflow: p.cards.overflow, artBelow: p.cards.artBelow, outlines: p.cards.outlines },
    hand: { visible: p.hand.visible, scale: p.hand.scale, raise: p.hand.raise },
    panels: { rail: p.panels.rail, railWidth: p.panels.railWidth, log: p.panels.log, prompt: { placement: p.panels.prompt.placement, x: p.panels.prompt.x, y: p.panels.prompt.y } },
  };
}

/** rowCount is how many rows the template uses (1..MAX_ROWS). */
export function rowCount(p: LayoutProfile): number {
  return Math.max(...REGION_KEYS.map((k) => p.regions[k].row)) + 1;
}

/**
 * compactRows renumbers the used rows to 0..n-1 preserving their order, so
 * moving the only region off row 1 onto row 2 leaves no empty row between.
 */
export function compactRows(regions: Record<RegionKey, Region>): Record<RegionKey, Region> {
  const used = [...new Set(REGION_KEYS.map((k) => regions[k].row))].sort((a, b) => a - b);
  const out = cloneRegions(regions);
  for (const k of REGION_KEYS) out[k].row = used.indexOf(regions[k].row);
  return out;
}

export function clamp(n: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, n));
}

function isObj(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}
function oneOf<T extends string>(v: unknown, allowed: readonly T[]): v is T {
  return typeof v === 'string' && (allowed as readonly string[]).includes(v);
}
function num(v: unknown, lo: number, hi: number): v is number {
  return typeof v === 'number' && Number.isFinite(v) && v >= lo && v <= hi;
}

/**
 * validate returns a LayoutProfile from a parsed value or null when it is
 * corrupt: a wrong version, a missing or wrong-typed field, an unknown word,
 * a number out of range. Rows are compacted (see the module doc).
 */
export function validate(v: unknown): LayoutProfile | null {
  if (!isObj(v) || v.version !== 1) return null;
  const { table, regions, cards, hand, panels } = v;
  if (!isObj(table) || !isObj(regions) || !isObj(cards) || !isObj(hand) || !isObj(panels)) return null;
  if (!num(table.split, SPLIT_MIN, SPLIT_MAX) || !oneOf(table.arrangement, ARRANGEMENTS) || !oneOf(table.orientation, ORIENTATIONS)) return null;
  const regs = {} as Record<RegionKey, Region>;
  for (const k of REGION_KEYS) {
    const r = regions[k];
    if (!isObj(r)) return null;
    if (!(typeof r.row === 'number' && Number.isInteger(r.row) && r.row >= 0 && r.row < MAX_ROWS)) return null;
    if (!num(r.weight, WEIGHT_MIN, WEIGHT_MAX) || !oneOf(r.anchor, ANCHORS) || !oneOf(r.order, REGION_ORDERS)) return null;
    regs[k] = { row: r.row, weight: r.weight, anchor: r.anchor, order: r.order };
  }
  if (typeof cards.stacking !== 'boolean' || !oneOf(cards.overflow, OVERFLOWS) || !num(cards.artBelow, ART_MIN, ART_MAX) || typeof cards.outlines !== 'boolean') return null;
  const handScale = hand.scale === undefined ? HAND_SCALE_DEFAULT : hand.scale;
  if (!num(hand.visible, HAND_MIN, HAND_MAX) || !num(handScale, HAND_SCALE_MIN, HAND_SCALE_MAX) || typeof hand.raise !== 'boolean') return null;
  const prompt = panels.prompt;
  if (!oneOf(panels.rail, RAIL_SIDES) || !oneOf(panels.log, LOG_MODES) || !isObj(prompt)) return null;
  const railWidth = panels.railWidth === undefined ? defaultRailWidth() : panels.railWidth;
  if (!num(railWidth, RAIL_MIN, RAIL_MAX)) return null;
  if (!oneOf(prompt.placement, PROMPT_PLACEMENTS) || !num(prompt.x, 0, 1) || !num(prompt.y, 0, 1)) return null;
  return {
    version: 1,
    table: { split: table.split, arrangement: table.arrangement, orientation: table.orientation },
    regions: compactRows(regs),
    cards: { stacking: cards.stacking, overflow: cards.overflow, artBelow: cards.artBelow, outlines: cards.outlines },
    hand: { visible: hand.visible, scale: handScale, raise: hand.raise },
    panels: { rail: panels.rail, railWidth, log: panels.log, prompt: { placement: prompt.placement, x: prompt.x, y: prompt.y } },
  };
}

/** The legacy `gorge.layoutsettings.v1` key the library migrates from once. */
export const LEGACY_LAYOUT_KEY = 'gorge.layoutsettings.v1';

const LEGACY_ANCHOR: Record<string, Anchor> = { left: 'start', center: 'center', right: 'end' };

/**
 * migrateLegacy turns a parsed `gorge.layoutsettings.v1` blob into a profile,
 * or null when the blob is not a v1 layout. What survives (see the plan's
 * migration ruling): each battlefield row's alignment becomes its region's
 * anchor, and the hand peek becomes the hand's visible fraction and raise.
 * Per-zone scale, the command/hand alignment and the on-board steppers are
 * dropped except for the hand scale, which is retained as its new bounded
 * hand-card size control. A field that is absent or unreadable keeps the
 * default rather than failing the whole migration:
 * the point is to keep what the player chose, not to judge the old blob.
 */
export function migrateLegacy(v: unknown): LayoutProfile | null {
  if (!isObj(v) || v.version !== 1) return null;
  const p = defaultProfile();
  const align = isObj(v.align) ? v.align : {};
  const scale = isObj(v.scale) ? v.scale : {};
  if (num(scale.hand, HAND_SCALE_MIN, HAND_SCALE_MAX)) p.hand.scale = scale.hand;
  for (const k of REGION_KEYS) {
    const a = align[k];
    if (typeof a === 'string' && a in LEGACY_ANCHOR) p.regions[k].anchor = LEGACY_ANCHOR[a];
  }
  switch (v.handPeek) {
    case 'hover':
      p.hand = { ...p.hand, visible: 0.5, raise: true };
      break;
    case 'always':
      p.hand = { ...p.hand, visible: 1, raise: true };
      break;
    case 'never':
      p.hand = { ...p.hand, visible: 0.5, raise: false };
      break;
  }
  return p;
}
