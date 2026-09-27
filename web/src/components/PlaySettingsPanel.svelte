<script module lang="ts">
  import { type PlaySettings, type PresetName, type StepStop, type StoppableStep } from '../lib/playsettings';
  import type { TurnSide } from '../lib/autopilot';

  /**
   * PRESET_LIST is the three clickable presets in picker order, each with
   * its one-line plain description (the blurb shown under the picker).
   * 'custom' is deliberately absent from the clickable list: it is rendered
   * as a non-clickable segment only while the settings carry it.
   */
  export const PRESET_LIST: { id: PresetName; label: string; blurb: string }[] = [
    {
      id: 'casual',
      label: 'Casual',
      blurb: 'Stops only when you can do something: your main phases, opponent attacks and end step, and opponent spells you can answer.',
    },
    {
      id: 'no-tells',
      label: 'No tells',
      blurb: "Also stops for every opponent spell and ability, so your pauses don't reveal your hand.",
    },
    {
      id: 'full-control',
      label: 'Full control',
      blurb: 'Stops at every priority window.',
    },
  ];

  /** nextStop is one step-stop cell's three-state cycle: Off → Smart → Always → Off. */
  export function nextStop(rule: StepStop): StepStop {
    return rule === 'off' ? 'smart' : rule === 'smart' ? 'forced' : 'off';
  }

  /** stopPatch is one cell's change as a withChange patch (deep-merged into steps field-wise). */
  export function stopPatch(step: StoppableStep, side: TurnSide, rule: StepStop): Partial<PlaySettings> {
    return { steps: { [side]: { [step]: rule } } } as unknown as Partial<PlaySettings>;
  }

  /** stopWord names each cell state in plain words — the state is text, never colour alone. */
  export function stopWord(rule: StepStop): string {
    return rule === 'off' ? 'Off' : rule === 'smart' ? 'Smart' : 'Always';
  }

  /** stopGlyph marks each cell state with a shape beside its word. */
  export function stopGlyph(rule: StepStop): string {
    return rule === 'off' ? '·' : rule === 'smart' ? '◐' : '●';
  }

  /**
   * clampMs parses one pacing input as an integer clamped to [0, 10000]
   * (fb-20260917T004341Z: 0 keeps the instant-post path; above 10s a
   * wedged-looking client is a footgun we don't ship). Returns null for a
   * non-numeric/empty input — the caller then leaves the current value
   * unchanged. Exported for tests; the Custom inputs' change handlers use it.
   */
  export function clampMs(raw: string): number | null {
    const n = Number.parseInt(raw, 10);
    if (Number.isNaN(n)) return null;
    return Math.min(10000, Math.max(0, n));
  }
</script>

<script lang="ts">
  import { STOPPABLE_STEPS } from '../lib/autopilot';
  import { stepFullName } from '../lib/phases';
  import { LAYOUT_ZONES, ZONE_ALIGNS, HAND_PEEKS, ZONE_LABELS, ALIGN_LABELS, HAND_PEEK_LABELS, SCALE_STEP, type ZoneAlign } from '../lib/layoutsettings';
  import { layoutStore } from '../lib/layoutsettings.svelte';
  import { storageWritable } from '../lib/storage';
  import type {
    OpponentObjectRule,
    OpponentTriggerRule,
    OwnObjectRule,
  } from '../lib/playsettings';
  import type { SeatPanelState } from '../lib/seatpanel.svelte';

  /**
   * The GAME OPTIONS editor for the whole play-settings model
   * (lib/playsettings.ts). It is bound to the seat panel's settings object:
   * every change goes through a SeatPanelState write path — the same write
   * path decide() reads: editSettings for the ordinary edits (withChange
   * relabels the preset Custom while the configuration matches none, and
   * back to a named preset when an edit is undone), pressAuto /
   * setActPass for the two switches that carry
   * machine-side consequences (re-arming a tripped runaway brake, disarming
   * an armed pass), and applyNamedPreset for the preset picker and the
   * Reset button, which need the same re-arm effects setAuto carries.
   */
  let {
    state: logic,
    showLog = false,
    onToggleLog = null,
  }: {
    state: SeatPanelState;
    /** fb-20260917T231628Z: the transcript's current visibility, mirrored by
     *  the Layout section's "Show game log" switch. It is a display preference
     *  owned by Table.svelte (logshown.ts, the persisted stops contract) — NOT
     *  a PlaySettings field — so it arrives as a prop and never touches
     *  logic.editSettings. Optional so every existing fixture/test renders
     *  unchanged. */
    showLog?: boolean;
    /** The switch's only write path; absent/null renders no switch (the Rail
     *  contract), so a caller that does not own the log never shows a dead
     *  control. */
    onToggleLog?: (() => void) | null;
  } = $props();

  const s = $derived(logic.settings);
  const STEPPABLE = STOPPABLE_STEPS as readonly StoppableStep[];

  /**
   * persistOK is the ONE storage-availability signal (lib/storage.ts):
   * false means every preference store — play settings, layout, remembered
   * answers, the transcript toggle — is silently memory-only in this browser
   * (site data refused or the write probe fails), so settings reset on every
   * reload. Probed once at mount; rendered as the notice under the step-stop
   * grid (fb-20260917T232814Z), the one place a player editing preferences
   * is guaranteed to look.
   */
  const persistOK = storageWritable();

  const OBJECT_OPTIONS: { value: OpponentObjectRule; label: string }[] = [
    { value: 'if-respondable', label: 'If I can respond' },
    { value: 'always', label: 'Always' },
    { value: 'never', label: 'Never' },
  ];
  const TRIGGER_OPTIONS: { value: OpponentTriggerRule; label: string }[] = [
    { value: 'targets-me-if-respondable', label: 'If it targets me or my stuff and I can respond' },
    { value: 'if-respondable', label: 'If I can respond' },
    { value: 'always', label: 'Always' },
    { value: 'never', label: 'Never' },
  ];
  const OWN_OPTIONS: { value: OwnObjectRule; label: string }[] = [
    { value: 'never', label: "Don't stop" },
    { value: 'if-respondable', label: 'Stop if I can respond' },
  ];

  /**
   * PACING_OPTIONS is the step delay (pause between auto-passes):
   * (step/resolve) ms pairs. fb-20260917T004341Z: 'slow' joined the canned
   * pairs and the Custom segment below exposes an arbitrary user-set pair —
   * the machine (paceMs) and the settings model already accept any numbers,
   * so this is purely picker surface.
   */
  const PACING_OPTIONS = [
    { id: 'off', label: 'Off', stepMs: 0, resolveMs: 0 },
    { id: 'short', label: 'Short', stepMs: 100, resolveMs: 200 },
    { id: 'normal', label: 'Normal', stepMs: 200, resolveMs: 400 },
    { id: 'slow', label: 'Slow', stepMs: 500, resolveMs: 1000 },
  ] as const;

  const blurb = $derived(PRESET_LIST.find((p) => p.id === s.preset)?.blurb ?? null);
  const pacingId = $derived(
    PACING_OPTIONS.find((p) => p.stepMs === s.pacing.stepMs && p.resolveMs === s.pacing.resolveMs)?.id ?? null,
  );
  // fb-20260917T004341Z: the Custom affordance. A pacing that matches no
  // canned pair IS the custom pair, so its segment reads selected and the
  // inputs show without a click; a canned pair hides them until the player
  // opens them deliberately (customOpen).
  let customOpen = $state(false);
  const showCustomInputs = $derived(pacingId === null || customOpen);
  const rows = $derived(
    STEPPABLE.map((step) => ({
      step,
      name: stepFullName(step),
      yours: s.steps.yours[step],
      opponents: s.steps.opponents[step],
    })),
  );

  function applyPreset(id: PresetName): void {
    // applyNamedPreset, not editSettings: a named preset that runs auto must
    // also clear the machine's runaway brake, or the panel would read
    // auto-pass on while the loop keeps refusing to act.
    logic.applyNamedPreset(id);
  }
  function cycleCell(step: StoppableStep, side: TurnSide): void {
    const cur = s.steps[side][step] ?? 'off';
    logic.editSettings(stopPatch(step, side, nextStop(cur)));
  }
  function setOwn(v: OwnObjectRule): void {
    logic.editSettings({ ownObjects: v });
  }

  /** setStepMs is the Step ms input's change handler: the SAME editSettings
   *  write path the segments use, the OTHER field taken from the current
   *  settings so a player can widen only one pause. */
  function setStepMs(raw: string): void {
    const ms = clampMs(raw);
    if (ms === null) return;
    logic.editSettings({ pacing: { stepMs: ms, resolveMs: s.pacing.resolveMs } });
  }
  /** setResolveMs is the Resolve ms input's change handler (see setStepMs). */
  function setResolveMs(raw: string): void {
    const ms = clampMs(raw);
    if (ms === null) return;
    logic.editSettings({ pacing: { stepMs: s.pacing.stepMs, resolveMs: ms } });
  }
</script>

<div class="panel" data-settings-panel>
  <section class="sec">
    <h3>Preset</h3>
    <div class="segments" data-preset-picker>
      {#each PRESET_LIST as p (p.id)}
        <button
          type="button"
          class="seg"
          class:on={s.preset === p.id}
          aria-pressed={s.preset === p.id}
          data-preset={p.id}
          onclick={() => applyPreset(p.id)}
        >{p.label}</button>
      {/each}
      {#if s.preset === 'custom'}
        <span class="seg custom" data-preset="custom">Custom</span>
      {/if}
    </div>
    {#if blurb !== null}
      <p class="blurb" data-preset-blurb>{blurb}</p>
    {/if}
  </section>

  <section class="sec">
    <button
      type="button"
      role="switch"
      class="row"
      class:on={s.autoPass}
      aria-checked={s.autoPass}
      data-toggle="auto-pass"
      onclick={() => logic.pressAuto()}
    >
      <span>Auto pass</span><span class="state" aria-hidden="true">{logic.machinePaused ? 'Paused' : s.autoPass ? 'On' : 'Off'}</span>
    </button>
  </section>

  <section class="sec">
    <h3>Stop when an opponent…</h3>
    <label class="row sel">
      <span>casts a spell</span>
      <select
        data-select="opponent-spell"
        onchange={(e) => logic.editSettings({ opponentSpell: e.currentTarget.value as OpponentObjectRule })}
      >
        {#each OBJECT_OPTIONS as o (o.value)}
          <option value={o.value} selected={s.opponentSpell === o.value}>{o.label}</option>
        {/each}
      </select>
    </label>
    <label class="row sel">
      <span>activates an ability</span>
      <select
        data-select="opponent-ability"
        onchange={(e) => logic.editSettings({ opponentAbility: e.currentTarget.value as OpponentObjectRule })}
      >
        {#each OBJECT_OPTIONS as o (o.value)}
          <option value={o.value} selected={s.opponentAbility === o.value}>{o.label}</option>
        {/each}
      </select>
    </label>
    <label class="row sel">
      <span>has a trigger</span>
      <select
        data-select="opponent-trigger"
        onchange={(e) => logic.editSettings({ opponentTrigger: e.currentTarget.value as OpponentTriggerRule })}
      >
        {#each TRIGGER_OPTIONS as o (o.value)}
          <option value={o.value} selected={s.opponentTrigger === o.value}>{o.label}</option>
        {/each}
      </select>
    </label>
  </section>

  <section class="sec">
    <h3>My own spells and abilities</h3>
    <div class="segments" data-own-picker>
      {#each OWN_OPTIONS as o (o.value)}
        <button
          type="button"
          class="seg"
          class:on={s.ownObjects === o.value}
          aria-pressed={s.ownObjects === o.value}
          data-own={o.value}
          onclick={() => setOwn(o.value)}
        >{o.label}</button>
      {/each}
    </div>
    <p class="legend" data-own-legend>
      Covers your own spell or ability while it is on the stack. “Don’t stop” lets it resolve; “Stop if I can respond” pauses only when you have a response. Step stops resume after it resolves.
    </p>
  </section>

  <section class="sec">
    <h3>Step stops</h3>
    <table class="grid">
      <thead>
        <tr>
          <th scope="col" class="stepcol"><span class="vh">Step</span></th>
          <th scope="col">My turn</th>
          <th scope="col">Opponent’s turn</th>
        </tr>
      </thead>
      <tbody>
        {#each rows as r (r.step)}
          <tr>
            <th scope="row" class="stepcol">{r.name}</th>
            <td>
              <button
                type="button"
                class="cell {r.yours}"
                data-step-cell={`${r.step}:yours`}
                data-stop-value={r.yours}
                aria-label={`${r.name}, my turn: ${stopWord(r.yours).toLowerCase()}`}
                onclick={() => cycleCell(r.step, 'yours')}
              ><span class="glyph" aria-hidden="true">{stopGlyph(r.yours)}</span>{stopWord(r.yours)}</button>
            </td>
            <td>
              <button
                type="button"
                class="cell {r.opponents}"
                data-step-cell={`${r.step}:opponents`}
                data-stop-value={r.opponents}
                aria-label={`${r.name}, opponent’s turn: ${stopWord(r.opponents).toLowerCase()}`}
                onclick={() => cycleCell(r.step, 'opponents')}
              ><span class="glyph" aria-hidden="true">{stopGlyph(r.opponents)}</span>{stopWord(r.opponents)}</button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
    {#if !persistOK}
      <p class="persist-warn" data-persist-warn role="status">
        Preferences are not being saved — this browser is refusing site data, so settings reset on every reload.
      </p>
    {/if}
    <p class="legend">Smart = only if I have a play. Always stops even at an empty window; Off never stops.</p>
  </section>

  <section class="sec">
    <button
      type="button"
      role="switch"
      class="row"
      class:on={s.passAfterAct}
      aria-checked={s.passAfterAct}
      data-toggle="pass-after-cast"
      data-actpass-toggle
      onclick={() => logic.setActPass(!s.passAfterAct)}
    >
      <span>Pass after I cast</span><span class="state" aria-hidden="true">{s.passAfterAct ? 'On' : 'Off'}</span>
    </button>
    <button
      type="button"
      role="switch"
      class="row"
      class:on={s.autoOrderIdenticalTriggers}
      aria-checked={s.autoOrderIdenticalTriggers}
      data-toggle="auto-order-triggers"
      onclick={() => logic.editSettings({ autoOrderIdenticalTriggers: !s.autoOrderIdenticalTriggers })}
    >
      <span>Auto-order identical triggers</span><span class="state" aria-hidden="true">{s.autoOrderIdenticalTriggers ? 'On' : 'Off'}</span>
    </button>
    <button
      type="button"
      role="switch"
      class="row"
      class:on={s.autoOrderAllTriggers}
      aria-checked={s.autoOrderAllTriggers}
      data-toggle="auto-order-all-triggers"
      onclick={() => logic.editSettings({ autoOrderAllTriggers: !s.autoOrderAllTriggers })}
    >
      <span>Auto-order all triggers</span><span class="state" aria-hidden="true">{s.autoOrderAllTriggers ? 'On' : 'Off'}</span>
    </button>
    <!-- fb-20260917T004341Z: the reporter's own word, "step delay", leads the
         label; the old meaning (pause between auto-passes) is kept. The four
         canned segments post whole pairs through the ordinary editSettings
         path, and the Custom segment reveals two numeric inputs for an
         arbitrary pair — the same write path, on change (not per keystroke). -->
    <div class="row sel">
      <span id="pacing-label">Step delay (pause between auto-passes)</span>
      <div class="segments" role="group" aria-labelledby="pacing-label" data-pacing-picker>
        {#each PACING_OPTIONS as p (p.id)}
          <button
            type="button"
            class="seg"
            class:on={pacingId === p.id}
            aria-pressed={pacingId === p.id}
            data-pacing={p.id}
            onclick={() => logic.editSettings({ pacing: { stepMs: p.stepMs, resolveMs: p.resolveMs } })}
          >{p.label}</button>
        {/each}
        <button
          type="button"
          class="seg"
          class:on={pacingId === null}
          aria-pressed={pacingId === null}
          aria-expanded={showCustomInputs}
          data-pacing="custom"
          onclick={() => (customOpen = true)}
        >Custom</button>
      </div>
    </div>
    {#if showCustomInputs}
      <div class="row sel pacing-inputs" data-pacing-custom>
        <label class="pacing-input">
          <span>Step ms</span>
          <input
            type="number"
            min="0"
            max="10000"
            step="1"
            value={s.pacing.stepMs}
            data-pacing-input="step"
            onchange={(e) => setStepMs(e.currentTarget.value)}
          />
        </label>
        <label class="pacing-input">
          <span>Resolve ms</span>
          <input
            type="number"
            min="0"
            max="10000"
            step="1"
            value={s.pacing.resolveMs}
            data-pacing-input="resolve"
            onchange={(e) => setResolveMs(e.currentTarget.value)}
          />
        </label>
      </div>
    {/if}
    <p class="legend" data-pacing-legend>Off = 0/0, Short = 100/200, Normal = 200/400, Slow = 500/1000 ms between automatic passes (step/resolve). Custom pairs clamp to 0–10000 ms.</p>
    <button
      type="button"
      role="switch"
      class="row"
      class:on={s.logAutoPasses}
      aria-checked={s.logAutoPasses}
      data-toggle="log-auto-passes"
      onclick={() => logic.editSettings({ logAutoPasses: !s.logAutoPasses })}
    >
      <span>Log auto-passes</span><span class="state" aria-hidden="true">{s.logAutoPasses ? 'On' : 'Off'}</span>
    </button>
    <!-- Clear yields (prio6): the GAME OPTIONS action for the stack tile
         menus' "Always pass for …" set — game-scoped, per table, so this is
         a state write (logic.clearYields), not a settings edit. Disabled
         with nothing yielded, and the count says how much it would clear. -->
    <button
      type="button"
      class="row"
      data-clear-yields
      disabled={logic.yieldList.length === 0}
      onclick={() => logic.clearYields()}
    >
      <span>Clear yields{logic.yieldList.length > 0 ? ` (${logic.yieldList.length})` : ''}</span>
      <span class="state" aria-hidden="true">{logic.yieldList.length > 0 ? 'Clear' : 'None'}</span>
    </button>
    <button type="button" class="reset" data-reset-settings onclick={() => applyPreset('casual')}>Reset to Casual</button>
  </section>

  <!-- Layout & display (fb-20260916T182801Z): per-zone card size and
       alignment, and the hand's peek mode (the three readings of the
       "slide up" ask). These are deliberately NOT play settings: they go
       through the separate layoutsettings store — its own localStorage key,
       its own model (lib/layoutsettings.ts) — never through
       logic.editSettings, so the preset relabelling above is untouched and
       a geometry change can never be mistaken for an auto-pass rule. The
       on-board − / + steppers on the viewer's own rows edit the same store. -->
  <section class="sec" data-layout-section>
    <h3>Layout</h3>
    <!-- fb-20260916T200925Z: the show/hide toggle for the ON-BOARD − / +
         steppers (Quadrant rows + HandFan). Same role="switch" row pattern
         as the auto-pass toggles, writing through the layout store, not
         state.editSettings — it is a layout preference. Default OFF since
         fb-20260917T004304Z (the board carries nothing but cards out of the
         box); a pre-toggle saved blob also loads as off (the field is
         optional in lib/layoutsettings.ts' validate). -->
    <button
      type="button"
      role="switch"
      class="row"
      class:on={layoutStore.steppersOnBoard}
      aria-checked={layoutStore.steppersOnBoard}
      data-toggle="steppers-on-board"
      data-layout-steppers-toggle
      onclick={() => layoutStore.setSteppersOnBoard(!layoutStore.steppersOnBoard)}
    >
      <span>−/+ size controls on the board</span><span class="state" aria-hidden="true">{layoutStore.steppersOnBoard ? 'Shown' : 'Hidden'}</span>
    </button>
    <!-- fb-20260917T231628Z: the game log's show/hide switch moved here from
         the rail's top row (the player's ask: the rail row was in the way).
         Same role="switch" row pattern as the toggles above, but its state and
         write path are the props — Table.svelte's showLog/toggleLog, persisted
         per table + scope through logshown.ts — never the layout store and
         never logic.editSettings. Rendered only when a toggle is supplied: the
         hot strip is mounted only in ordinary seated play, and the other
         states (spectator, mulligan, game over, finished replay) keep the
         rail's own LOGS control instead. -->
    {#if onToggleLog}
      <button
        type="button"
        role="switch"
        class="row"
        class:on={showLog}
        aria-checked={showLog}
        data-toggle="show-game-log"
        onclick={() => onToggleLog?.()}
      >
        <span>Show game log</span><span class="state" aria-hidden="true">{showLog ? 'Shown' : 'Hidden'}</span>
      </button>
    {/if}
    <!-- The panel's own per-zone steppers stay mounted regardless of the
         toggle: they are already "in options" and are the only way back to
         the board steppers once it is off. -->
    {#each LAYOUT_ZONES as z (z)}
      <div class="row sel" data-layout-zone={z}>
        <span>{ZONE_LABELS[z]}</span>
        <span class="layctl">
          <span class="zstep" role="group" aria-label="Card size, {ZONE_LABELS[z]}">
            <button type="button" class="zsbtn" data-layout-smaller={z} aria-label="Smaller {ZONE_LABELS[z]} cards" onclick={() => layoutStore.bump(z, -SCALE_STEP)}>−</button>
            <span class="zsval" data-layout-scale={z}>{Math.round(layoutStore.scale(z) * 100)}%</span>
            <button type="button" class="zsbtn" data-layout-larger={z} aria-label="Larger {ZONE_LABELS[z]} cards" onclick={() => layoutStore.bump(z, SCALE_STEP)}>+</button>
          </span>
          <select
            data-layout-align={z}
            aria-label="{ZONE_LABELS[z]} alignment"
            onchange={(e) => layoutStore.setAlign(z, e.currentTarget.value as ZoneAlign)}
          >
            {#each ZONE_ALIGNS as a (a)}
              <option value={a} selected={layoutStore.align(z) === a}>{ALIGN_LABELS[a]}</option>
            {/each}
          </select>
        </span>
      </div>
    {/each}
    <div class="row sel">
      <span id="peek-label">Hand cards</span>
      <div class="segments" role="group" aria-labelledby="peek-label" data-peek-picker>
        {#each HAND_PEEKS as p (p)}
          <button
            type="button"
            class="seg"
            class:on={layoutStore.peek === p}
            aria-pressed={layoutStore.peek === p}
            data-peek={p}
            onclick={() => layoutStore.setHandPeek(p)}
          >{HAND_PEEK_LABELS[p]}</button>
        {/each}
      </div>
    </div>
    <p class="legend">Card size and alignment save in this browser and apply to your board. The − / + marks on your own battlefield rows are the same controls — while “−/+ size controls on the board” above is Shown.</p>
    <button type="button" class="reset" data-layout-reset onclick={() => layoutStore.reset()}>Reset layout</button>
  </section>

  <!-- Remembered trigger answers (fb-20260914T062319Z-88b4069a B4): the
       management list for the remember checkbox on optional-trigger prompts.
       One row per remembered answer — the prompt's label (card + trigger
       text), the answer itself, and a Forget button — plus Forget all. This
       is a state write (logic.removeRemembered / logic.clearRemembered), not
       a settings edit, exactly like Clear yields. -->
  <section class="sec">
    <h3>Remembered trigger answers</h3>
    {#if logic.remembered.entries.length === 0}
      <p class="legend" data-remembered-empty>None. Tick “Remember this answer” on an optional-trigger prompt to store one.</p>
    {:else}
      <ul class="remlist" data-remembered-list>
        {#each logic.remembered.entries as e, i (i)}
          <li class="remrow" data-remembered-entry>
            <span class="remlabel" title={e.label}>{e.label}</span>
            <span class="remchoice" data-remembered-choice={e.choice}>{e.choice === 0 ? 'Yes' : 'No'}</span>
            <button
              type="button"
              class="remforget"
              data-remembered-delete={i}
              aria-label={`Forget ${e.label}`}
              onclick={() => logic.removeRemembered(logic.remembered.entries[i]?.key ?? '')}
            >Forget</button>
          </li>
        {/each}
      </ul>
      <button type="button" class="row" data-remembered-clear onclick={() => logic.clearRemembered()}>
        <span>Forget all remembered answers ({logic.remembered.entries.length})</span>
        <span class="state" aria-hidden="true">Clear</span>
      </button>
    {/if}
  </section>
</div>

<style>
  .panel {
    font-family: var(--font-ui);
    font-size: var(--t-12);
  }
  .sec {
    padding: var(--sp-2);
    border-bottom: 1px solid var(--edge-inst);
  }
  .sec:last-child { border-bottom: 0; }
  h3 {
    margin: 0 0 var(--sp-1);
    color: var(--ink-faint);
    font-size: var(--t-10);
    font-weight: 700;
    letter-spacing: 0.02em;
  }
  .segments {
    display: flex;
    gap: 1px;
    max-width: 100%;
    border: 1px solid var(--edge-inst);
    background: var(--edge-inst);
  }
  .seg {
    flex: 1 1 0;
    min-width: 0;
    padding: var(--sp-1) var(--sp-2);
    border: 0;
    background: var(--instrument-raised);
    color: var(--ink-inst);
    font-family: var(--font-ui);
    font-size: var(--t-11);
    text-align: center;
    cursor: pointer;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .seg:hover { color: var(--ink); }
  .seg.on {
    background: var(--offered);
    color: var(--felt-sunk);
  }
  .seg.custom {
    cursor: default;
    font-style: italic;
    background: var(--instrument);
    color: var(--ink-dim);
  }
  .blurb {
    margin: var(--sp-1) 0 0;
    color: var(--ink-dim);
    font-size: var(--t-11);
  }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-2);
    width: 100%;
    padding: var(--sp-2);
    border: 1px solid var(--edge-inst);
    background: var(--instrument-raised);
    color: var(--ink-inst);
    font-family: var(--font-ui);
    font-size: var(--t-12);
    text-align: left;
    cursor: pointer;
  }
  button.row:hover { color: var(--ink); }
  button.row.on {
    background: var(--offered);
    color: var(--felt-sunk);
  }
  .row:disabled {
    opacity: 0.5;
    cursor: default;
  }
  button.row:disabled:hover { color: var(--ink-inst); }
  button.row + button.row { margin-top: 1px; }
  .row.sel {
    padding: var(--sp-1) 0;
    border: 0;
    background: none;
  }
  .row.sel select {
    flex: 1 1 auto;
    min-width: 0;
    max-width: 62%;
    padding: var(--sp-1);
    border: 1px solid var(--edge-inst);
    background: var(--instrument-raised);
    color: var(--ink);
    font-family: var(--font-ui);
    font-size: var(--t-11);
  }
  /* fb-20260917T004341Z: the Custom pair of numeric inputs sits under the
     pacing segments row. */
  .pacing-inputs {
    gap: var(--sp-2);
  }
  .pacing-input {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
    flex: 1 1 0;
    min-width: 0;
    color: var(--ink-dim);
    font-size: var(--t-10);
  }
  .pacing-input input {
    width: 4.5em;
    min-width: 0;
    padding: var(--sp-1);
    border: 1px solid var(--edge-inst);
    background: var(--instrument-raised);
    color: var(--ink);
    font-family: var(--font-data);
    font-variant-numeric: tabular-nums;
    font-size: var(--t-11);
  }
  table.grid {
    width: 100%;
    border-collapse: collapse;
  }
  .grid th,
  .grid td {
    padding: 2px 0;
    text-align: center;
    font-weight: 400;
  }
  .grid thead th {
    color: var(--ink-faint);
    font-size: var(--t-10);
  }
  .grid .stepcol {
    width: 42%;
    text-align: left;
    color: var(--ink-dim);
    font-size: var(--t-10);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    padding-right: var(--sp-1);
  }
  .cell {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 3px;
    min-width: 4.4rem;
    max-width: 100%;
    padding: var(--sp-1);
    border: 1px solid var(--edge-inst);
    background: var(--instrument-raised);
    color: var(--ink-inst);
    font-family: var(--font-ui);
    font-size: var(--t-10);
    cursor: pointer;
  }
  .cell:hover { color: var(--ink); }
  .cell.smart {
    background: color-mix(in srgb, var(--offered) 28%, var(--instrument-raised));
  }
  .cell.forced {
    background: var(--offered);
    color: var(--felt-sunk);
  }
  .legend {
    margin: var(--sp-1) 0 0;
    color: var(--ink-faint);
    font-size: var(--t-10);
  }
  /* fb-20260917T232814Z: the storage-refusal notice under the step-stop grid.
     Same box rhythm as .legend but in --danger so it reads as a warning, not
     as a cell explanation. */
  .persist-warn {
    margin: var(--sp-2) 0 0;
    color: var(--danger);
    font-size: var(--t-12);
  }
  .reset {
    width: 100%;
    margin-top: var(--sp-2);
    padding: var(--sp-2);
    border: 1px solid var(--edge-inst);
    background: var(--instrument-raised);
    color: var(--ink);
    font-family: var(--font-ui);
    font-size: var(--t-12);
    font-weight: 700;
    cursor: pointer;
  }
  .reset:hover {
    background: var(--offered);
    color: var(--felt-sunk);
  }
  /* Layout section (fb-20260916T182801Z): the per-zone size stepper and the
     alignment select share one right-hand control cluster. The select
     inherits .row.sel select's styling (descendant selector). */
  .layctl {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    flex: 1 1 auto;
    min-width: 0;
    justify-content: flex-end;
  }
  .zstep {
    display: inline-flex;
    align-items: center;
    gap: 1px;
    border: 1px solid var(--edge-inst);
    background: var(--instrument-raised);
  }
  .zsbtn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 1.3rem;
    height: 1.5rem;
    padding: 0;
    border: 0;
    background: none;
    color: var(--ink-inst);
    font-family: var(--font-data);
    font-size: var(--t-12);
    line-height: 1;
    cursor: pointer;
  }
  .zsbtn:hover,
  .zsbtn:focus-visible {
    color: var(--ink);
    background: var(--offered);
  }
  .zsval {
    min-width: 2.8em;
    text-align: center;
    font-family: var(--font-data);
    font-variant-numeric: tabular-nums;
    font-size: var(--t-11);
    color: var(--ink);
  }
  ul.remlist {
    margin: 0;
    padding: 0;
    list-style: none;
    max-height: 12rem;
    overflow-y: auto;
  }
  .remrow {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: var(--sp-1) 0;
    border-bottom: 1px solid var(--edge-inst);
  }
  .remrow:last-child { border-bottom: 0; }
  .remlabel {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--ink-inst);
  }
  .remchoice {
    flex: 0 0 auto;
    padding: 0 var(--sp-1);
    color: var(--ink-dim);
    font-variant-numeric: tabular-nums;
  }
  .remforget {
    flex: 0 0 auto;
    padding: 2px var(--sp-2);
    border: 1px solid var(--edge-inst);
    background: var(--instrument-raised);
    color: var(--ink-inst);
    font-family: var(--font-ui);
    font-size: var(--t-10);
    cursor: pointer;
  }
  .remforget:hover { color: var(--ink); }
  .vh {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
</style>
