<script module lang="ts">
  import { type PlaySettings, type PresetName, type StepStop, type StoppableStep } from '../lib/playsettings';
  import type { TurnSide } from '../lib/autopilot';
  import { clampMs, nextStop, stopGlyph, stopPatch, stopWord } from '../lib/playsettings-panel';

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

</script>

<script lang="ts">
  import { STOPPABLE_STEPS } from '../lib/autopilot';
  import { stepFullName } from '../lib/phases';
  import { layoutStore } from '../lib/layouts.svelte';
  import { storageWritable } from '../lib/storage';
  import type {
    OpponentObjectRule,
    OpponentTriggerRule,
    OwnObjectRule,
  } from '../lib/playsettings';
  import type { SeatPanelState } from '../lib/seatpanel.svelte';
  import BreakpointsSection from './BreakpointsSection.svelte';
  import ProtocolSetting from './ProtocolSetting.svelte';
  import KeymapEditor from './KeymapEditor.svelte';
  import { keymapStore } from '../lib/keymap.svelte';
  import { downloadText } from '../lib/download';

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
  // The blurb line shows the active profile's name when one is applied, with
  // a "(modified)" marker once the live settings have diverged from the
  // stored copy (editing never auto-saves).
  const activeProfile = $derived(logic.activeProfileName);
  const profileNote = $derived(
    activeProfile === null ? null : `${activeProfile}${logic.profileModified ? ' (modified)' : ''}`,
  );
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

  // The profiles row's own ephemeral UI state: the name input, which profile
  // is being renamed (and to what), and the last rejected-name message.
  let profileName = $state('');
  let renaming = $state<string | null>(null);
  let renameName = $state('');
  let profileError = $state<string | null>(null);

  // Export/import of the saved profiles as a file: the hidden picker and the
  // last import's plain-words result.
  let profileFile = $state<HTMLInputElement | null>(null);
  let transferNote = $state<string | null>(null);

  /** importProfilesFile reads the picked file, merges it in and reports the result. */
  async function importProfilesFile(input: HTMLInputElement): Promise<void> {
    const file = input.files?.[0];
    if (file === undefined) return;
    try {
      transferNote = logic.importProfilesText(await file.text());
    } catch {
      transferNote = 'Could not read that file.';
    } finally {
      // cleared either way, so picking the same file again fires onchange
      input.value = '';
    }
  }

  /** saveCurrentProfile is the Save button's handler: the typed name, or a default. */
  function saveCurrentProfile(): void {
    const name = profileName.trim();
    if (name.length === 0) {
      profileError = 'Enter a profile name';
      return;
    }
    if (logic.saveProfile(name)) {
      profileName = '';
      profileError = null;
    } else {
      profileError = 'Name must be 1–24 characters';
    }
  }
  /** applyOne applies a saved profile through the state write path (re-arm + note). */
  function applyOne(name: string): void {
    logic.applyProfile(name);
    profileError = null;
  }
  function deleteOne(name: string): void {
    logic.deleteProfile(name);
  }
  function startRename(name: string): void {
    renaming = name;
    renameName = name;
  }
  function commitRename(): void {
    if (renaming === null) return;
    if (logic.renameProfile(renaming, renameName)) {
      renaming = null;
      profileError = null;
    } else {
      profileError = 'Name must be 1–24 characters and not already used';
    }
  }
  function cancelRename(): void {
    renaming = null;
    profileError = null;
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
    <div class="profiles" data-profiles>
      <div class="profile-save">
        <input
          type="text"
          class="name-input"
          data-profile-name
          placeholder="Profile name"
          maxlength="24"
          bind:value={profileName}
          onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); saveCurrentProfile(); } }}
        />
        <button type="button" class="seg" data-profile-save onclick={saveCurrentProfile}>Save</button>
        <button type="button" class="seg" data-profile-export onclick={() => downloadText('gorge-flow-profiles.json', logic.exportProfilesText())}>Export profiles…</button>
        <button type="button" class="seg" data-profile-import onclick={() => profileFile?.click()}>Import profiles…</button>
        <input type="file" accept="application/json" hidden bind:this={profileFile} onchange={(e) => importProfilesFile(e.currentTarget)} />
      </div>
      <!-- Mounted empty so a screen reader is already watching it when the first result arrives. -->
      <p class="blurb" data-profile-transfer role="status">{transferNote ?? ''}</p>
      {#if profileNote !== null}
        <p class="blurb" data-active-profile>{profileNote}</p>
      {/if}
      {#if logic.profileNames.length > 0}
        <ul class="profile-list" data-profile-list>
          {#each logic.profileNames as name (name)}
            <li class="profile-item" data-profile-item={name}>
              {#if renaming === name}
                <input
                  type="text"
                  class="name-input"
                  data-profile-rename-input={name}
                  maxlength="24"
                  bind:value={renameName}
                  onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); commitRename(); } if (e.key === 'Escape') cancelRename(); }}
                />
                <button type="button" class="seg" data-profile-rename-commit={name} onclick={commitRename}>OK</button>
                <button type="button" class="seg" data-profile-rename-cancel={name} onclick={cancelRename}>Cancel</button>
              {:else}
                <button
                  type="button"
                  class="seg"
                  class:on={activeProfile === name}
                  aria-pressed={activeProfile === name}
                  data-profile-apply={name}
                  onclick={() => applyOne(name)}
                >{name}{activeProfile === name && logic.profileModified ? ' *' : ''}</button>
                <button type="button" class="seg" data-profile-rename={name} onclick={() => startRename(name)}>Rename</button>
                <button type="button" class="seg" data-profile-delete={name} onclick={() => deleteOne(name)}>Delete</button>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
      {#if profileError !== null}
        <p class="blurb" data-profile-error role="alert">{profileError}</p>
      {/if}
    </div>
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

  <BreakpointsSection state={logic} />

  <KeymapEditor store={keymapStore} />

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

  <!-- Layout: the board's appearance lives in the layout profile library
       (lib/layouts.svelte.ts) and its drawer, never in play settings — the
       preset relabelling above is untouched by it. The game log's
       show/hide switch stays here (fb-20260917T231628Z); its state and write
       path are Table.svelte's (per table + scope, or pinned by the layout
       profile). -->
  <section class="sec" data-layout-section>
    <h3>Layout</h3>
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
    <button type="button" class="row" data-open-layout-drawer onclick={() => (layoutStore.drawerOpen = true)}>
      <span>Board layout</span><span class="state">{layoutStore.label}</span>
    </button>
    <p class="legend">Arrangement, card sizes, stacking and the rail are layout profiles: they save in this browser and can be exported to a file.</p>
  </section>
  <section class="sec"><h3>Connection</h3><ProtocolSetting /></section>

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
  .blurb:empty {
    margin: 0;
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
