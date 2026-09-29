/**
 * wire.ts: the ManaBrew protocol's message shapes, hand-written from the
 * published spec (https://docs.manabrew.app/protocol/, CC-BY-4.0; condensed
 * in docs/superpowers/specs/2026-09-28-manabrew-protocol-scope.md Appendix A)
 * and from gorge's Go mirror in protocol/manabrew. Decoding is lenient:
 * every field a gorge server may omit is optional here, and unknown fields
 * are ignored.
 *
 * This module and its siblings under lib/manabrew are the only place the
 * client knows the ManaBrew wire; a protocol change touches only them.
 */

export type StepKind =
  | 'untap' | 'upkeep' | 'draw' | 'main1' | 'combatBegin' | 'combatDeclareAttackers'
  | 'combatDeclareBlockers' | 'combatFirstStrikeDamage' | 'combatDamage' | 'combatEnd'
  | 'main2' | 'endOfTurn' | 'cleanup';

export type ZoneKind = 'hand' | 'library' | 'graveyard' | 'exile' | 'command' | 'battlefield' | string;

export interface CardIdentity {
  name: string;
  setCode?: string;
  cardNumber?: string;
  isToken?: boolean;
  tokenScript?: string;
}

export interface CardDto {
  id: string;
  identity: CardIdentity;
  color?: string[];
  manaCost?: string;
  cmc?: number;
  types?: string[];
  subtypes?: string[];
  supertypes?: string[];
  power?: string | null;
  toughness?: string | null;
  text?: string;
  controllerId: string;
  ownerId: string;
  tapped?: boolean;
  isAttacking?: boolean;
  attackingPlayerId?: string;
  attackTargetId?: string;
  keywords?: string[];
  counters?: Record<string, number>;
  damage?: number;
  summoningSick?: boolean;
  isFaceDown?: boolean;
  attachedTo?: string;
  attachmentIds?: string[];
}

export type VisibleCard = { visibility: 'visible' } & CardDto;
export interface HiddenCard { visibility: 'hidden'; id: string }
export type CardView = VisibleCard | HiddenCard;

export interface ZoneDto {
  zone: ZoneKind;
  ownerId: string;
  cards: CardView[];
  count: number;
}

export interface PlayerDto {
  id: string;
  name: string;
  status?: string;
  isHuman?: boolean;
  life: number;
  manaPool?: Record<string, number>;
  commanderCasts?: Record<string, number>;
  commanderDamage?: Record<string, number>;
  counters?: Record<string, number>;
}

export interface TargetRef {
  kind: 'player' | 'card' | 'spell';
  id: string;
  intent?: string;
  oracle?: string;
}

export interface StackObjectDto {
  id: string;
  sourceId?: string;
  controllerId: string;
  ownerId?: string;
  identity: CardIdentity;
  text?: string;
  isPermanentSpell?: boolean;
  isCasting?: boolean;
  targets?: TargetRef[];
}

export interface CombatAssignmentDto { blockerId: string; attackerId: string }

export interface GameViewDto {
  gameId: string;
  turn: number;
  step: StepKind | '';
  combatAssignments?: CombatAssignmentDto[];
  activePlayerId: string;
  priorityPlayerId: string;
  players: PlayerDto[];
  zones: ZoneDto[];
  stack: StackObjectDto[];
  gameOver: boolean;
  winnerId?: string | null;
  monarchId?: string | null;
  initiativeHolderId?: string | null;
}

export interface PromptPresentation {
  title?: string;
  description?: string;
  text?: string;
  targets?: TargetRef[];
}

export interface Mana { color: string; amount: number }

export interface AvailableAction {
  id: string;
  type: 'cast' | 'activateAbility' | 'undoMana' | 'autofill' | string;
  cardId?: string;
  mode?: string;
  label?: string;
  modeLabel?: string;
  abilityIndex?: number;
  description?: string;
  isManaAbility?: boolean;
  producedMana?: Mana[];
}

export interface SelectionOption { label: string; weight?: number; canRepeat?: boolean }
export interface ReorderItem { id: string; card?: CardDto; oracle?: string }
export interface AttackerOptionDto { attackerId: string; validTargetIds: string[]; mustAttack?: boolean }
export interface AttackTargetDto { id: string; label: string; kind: string }
export interface BlockableAttackerDto { attackerId: string; validBlockerIds: string[]; minBlockers?: number; maxBlockers?: number; mustBeBlocked?: boolean }

interface WithPresentation { presentation?: PromptPresentation }

export type PromptInput =
  | ({ type: 'chooseAction'; actions: AvailableAction[] })
  | ({ type: 'chooseNumber'; min: number; max: number } & WithPresentation)
  | ({ type: 'chooseCards'; cards: CardDto[]; min: number; max: number } & WithPresentation)
  | ({ type: 'chooseColor'; validColors: string[]; amount: number; repeatAllowed?: boolean } & WithPresentation)
  | ({ type: 'chooseBoolean'; confirmLabel: string; denyLabel: string } & WithPresentation)
  | ({ type: 'chooseFromSelection'; options: SelectionOption[]; minTotal: number; maxTotal: number } & WithPresentation)
  | ({ type: 'revealCards'; cards: CardDto[] } & WithPresentation)
  | ({ type: 'scry'; cards: CardDto[]; zones: string[] } & WithPresentation)
  | ({ type: 'reorder'; items: ReorderItem[] } & WithPresentation)
  | ({ type: 'diceRolled'; sides: number } & WithPresentation)
  | ({ type: 'payManaCost'; cardId: string; cardName: string; manaCost: string; canConfirmFromPool: boolean; actions: AvailableAction[] } & WithPresentation)
  | ({ type: 'mulligan'; handCardIds: string[]; mulliganCount: number })
  | ({ type: 'mulliganPutBack'; handCardIds: string[]; cards: CardDto[]; count: number })
  | ({ type: 'chooseAttackers'; attackers: AttackerOptionDto[]; attackTargets: AttackTargetDto[] })
  | ({ type: 'chooseBlockers'; attackers: BlockableAttackerDto[]; availableBlockerIds: string[]; error?: string })
  | ({ type: 'chooseDamageAssignmentOrder'; attackerId: string; blockerIds: string[] })
  | ({ type: 'chooseCombatDamageAssignment'; attackerId: string; blockerIds: string[]; totalDamage: number })
  | ({ type: 'chooseBoardTargets'; candidates: TargetRef[]; hostile?: boolean; intent?: string; minTargets: number; maxTargets: number; cancellable?: boolean } & WithPresentation)
  | { type: 'gameOver' };

export type PromptType = PromptInput['type'];

export interface AgentPrompt {
  promptId: number;
  decidingPlayerId: string;
  sourceCard?: CardDto;
  input: PromptInput;
}

export type ErrorCode = 'stalePrompt' | 'wrongPlayer' | 'wrongPromptType' | 'unknownActionId' | 'invalidShape';

export interface ProtocolError {
  code: ErrorCode | string;
  message: string;
  promptId?: number;
}

/** Engine → client. `display` is WIP upstream and ignored. */
export type EngineMessage =
  | { kind: 'state'; gameView: GameViewDto }
  | { kind: 'stateDelta'; base: string; fingerprint: string; patch: unknown }
  | ({ kind: 'prompt' } & AgentPrompt)
  | { kind: 'error'; error: ProtocolError }
  | { kind: 'display'; event?: unknown };

/** The output half of a response: `{type, output:{type, …}}`. */
export interface PromptOutput {
  type: PromptType;
  output: { type: string } & Record<string, unknown>;
}

/** Client → engine. */
export type ClientMessage =
  | { kind: 'response'; promptId: number; action: PromptOutput }
  | { kind: 'directive'; directive: { type: 'concede' } };

const ENGINE_KINDS = new Set(['state', 'stateDelta', 'prompt', 'error', 'display']);

/** parseEngineMessage decodes one SSE data line; anything that is not an engine message is null. */
export function parseEngineMessage(data: string): EngineMessage | null {
  try {
    const m = JSON.parse(data) as { kind?: unknown };
    if (m === null || typeof m !== 'object' || typeof m.kind !== 'string' || !ENGINE_KINDS.has(m.kind)) return null;
    return m as EngineMessage;
  } catch {
    return null;
  }
}
