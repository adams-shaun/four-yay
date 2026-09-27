import { type Browser, type Page } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import { render } from 'svelte/server';
import { SeatPanelState } from '../lib/seatpanel.svelte';
import { presetPatch, type StoppableStep } from '../lib/playsettings';
import PlaySettingsPanel, { clampMs, nextStop, stopPatch, stopWord } from './PlaySettingsPanel.svelte';
import { layoutStore } from '../lib/layoutsettings.svelte';

// Two layers, because the editor has two halves to defend:
//
//  1. SSR (svelte/server, the repo's render-only component-test pattern)
//     pins what the editor SHOWS for a given settings state — labels, blurbs,
//     aria affordances, the Custom segment, the grid's words/glyphs.
//
//  2. The playwright fixture below (PileModal's pattern) mounts the panel
//     against a REAL SeatPanelState in a REAL browser and CLICKS the actual
//     controls, so a disconnected or broken onclick fails: the cell cycle,
//     the preset picker, the Reset button and the paused-to-Casual brake
//     regression are exercised through the component's own handlers.

const ctx = { seat: 0, token: 'tok' };

/** panel renders the editor bound to a fresh (or caller-prepared) seat state.
 *  The optional log props (fb-20260917T231628Z's switch) ride along when given. */
function panel(state: SeatPanelState, log?: { showLog: boolean; onToggleLog: () => void }): string {
  return render(PlaySettingsPanel, { props: log ? { state, ...log } : { state } }).html;
}

/** tag returns the opening tag of the element carrying one data attribute ('' when absent). */
function tag(html: string, attr: string): string {
  return new RegExp(`<([a-z]+)[^>]*${attr.replace(/"/g, '\\"')}[^>]*>`).exec(html)?.[0] ?? '';
}

/** elem returns one element with its content, matched by a data attribute ('' when absent). */
function elem(html: string, attr: string): string {
  const m = new RegExp(`<([a-z]+)[^>]*${attr.replace(/"/g, '\\"')}[^>]*>([\\s\\S]*?)</\\1>`).exec(html);
  return m === null ? '' : m[0];
}

/** cell returns the opening tag of one step-stop cell ('' when absent). */
function cell(html: string, step: string, side: 'yours' | 'opponents'): string {
  return tag(html, `data-step-cell="${step}:${side}"`);
}

/** selectHtml returns the whole <select> element with one data-select value. */
function selectHtml(html: string, which: string): string {
  const m = new RegExp(`<select[^>]*data-select="${which}"[^>]*>[\\s\\S]*?</select>`).exec(html);
  if (m === null) throw new Error(`no select for ${which}`);
  return m[0];
}

describe('PlaySettingsPanel — Clear yields (prio6)', () => {
  // One DISTINCT table id per test: the yield store's in-memory layer is
  // module-level and shared by every SeatPanelState this file constructs.
  it('is disabled with nothing yielded, showing None', () => {
    const html = panel(new SeatPanelState('yt-none', 1, ctx, null));
    expect(html).toContain('data-clear-yields');
    expect(tag(html, 'data-clear-yields')).toContain('disabled');
    expect(elem(html, 'data-clear-yields')).toContain('None');
  });

  it('is enabled with yields, and the count says how many the clear would drop', () => {
    const state = new SeatPanelState('yt-two', 1, ctx, null);
    state.addYield('1:Blood Artist:drain');
    state.addYield('2:Soul Warden:gain');
    const html = panel(state);
    expect(tag(html, 'data-clear-yields')).not.toContain('disabled');
    expect(elem(html, 'data-clear-yields')).toContain('Clear yields (2)');
  });
});

describe('PlaySettingsPanel — Remembered trigger answers (fb-20260914T062319Z-88b4069a B4)', () => {
  it('with nothing remembered it says so and offers no list, no Forget, no clear', () => {
    const html = panel(new SeatPanelState('rt-empty', 1, ctx, null));
    expect(html).toContain('data-remembered-empty');
    expect(html).toContain('Remembered trigger answers');
    expect(html).not.toContain('data-remembered-list');
    expect(html).not.toContain('data-remembered-clear');
  });

  it('lists one row per remembered answer: label, the answer itself, a per-entry Forget, and a counted Forget all', () => {
    const state = new SeatPanelState('rt-two', 1, ctx, null);
    state.remembered = {
      version: 1,
      entries: [
        { key: 'trigger_optional\u0000p1', choice: 0, label: 'Bloodghast: Whenever a land enters, Bloodghast may return from the graveyard', savedAt: 1 },
        { key: 'trigger_optional\u0000p2', choice: 1, label: 'Miracle — reveal Thunderous Wrath and cast it for {R}', savedAt: 2 },
      ],
    };
    const html = panel(state);
    expect(html).toContain('data-remembered-list');
    expect(html).not.toContain('data-remembered-empty');
    expect([...html.matchAll(/data-remembered-entry/g)]).toHaveLength(2);
    expect(html).toContain('Bloodghast: Whenever a land enters, Bloodghast may return from the graveyard');
    expect(html).toContain('Miracle — reveal Thunderous Wrath and cast it for {R}');
    // the answer itself, as the word the player chose, keyed by its wire index
    expect(html).toContain('data-remembered-choice="0"');
    expect(html).toContain('data-remembered-choice="1"');
    expect([...html.matchAll(/data-remembered-delete="\d+"/g)]).toHaveLength(2);
    expect(elem(html, 'data-remembered-clear')).toContain('Forget all remembered answers (2)');
  });
});

describe('PlaySettingsPanel — the OPTIONS editor (rendered)', () => {
  it('renders casual by default: preset pressed, its blurb shown, the opponent rules reflecting it', () => {
    const html = panel(new SeatPanelState('t1', 1, ctx, null));
    expect(tag(html, 'data-preset="casual"')).toContain('aria-pressed="true"');
    expect(html).not.toContain('data-preset="custom"');
    expect(elem(html, 'data-preset-blurb')).toContain('Stops only when you can do something');
    // casual's trigger rule is the targeting one; the spell/ability rules are
    // the respondable ones. SSR marks the selected option with `selected`.
    expect(selectHtml(html, 'opponent-trigger')).toContain('value="targets-me-if-respondable" selected');
    expect(selectHtml(html, 'opponent-spell')).toContain('value="if-respondable" selected');
  });

  it('a cell cycles off → smart → forced → off, with word, glyph and aria-label at each state', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    const step: StoppableStep = 'upkeep'; // off on both sides in casual, so the cycle starts clean
    const cycle = (side: 'yours' | 'opponents'): string => {
      // cycleCell's body: the next rule on the current one, through editSettings.
      const cur = state.settings.steps[side][step] ?? 'off';
      state.editSettings(stopPatch(step, side, nextStop(cur)));
      return panel(state);
    };
    const smart = cycle('yours');
    expect(tag(smart, 'data-step-cell="upkeep:yours"')).toContain('data-stop-value="smart"');
    expect(cell(smart, 'upkeep', 'yours')).toContain('aria-label="Upkeep, my turn: smart"');
    expect(elem(smart, 'data-step-cell="upkeep:yours"')).toContain('Smart');
    expect(elem(smart, 'data-step-cell="upkeep:yours"')).toContain('◐');
    const forced = cycle('yours');
    expect(tag(forced, 'data-step-cell="upkeep:yours"')).toContain('data-stop-value="forced"');
    expect(cell(forced, 'upkeep', 'yours')).toContain('aria-label="Upkeep, my turn: always"');
    expect(elem(forced, 'data-step-cell="upkeep:yours"')).toContain('Always');
    expect(elem(forced, 'data-step-cell="upkeep:yours"')).toContain('●');
    const off = cycle('yours');
    expect(tag(off, 'data-step-cell="upkeep:yours"')).toContain('data-stop-value="off"');
    expect(cell(off, 'upkeep', 'yours')).toContain('aria-label="Upkeep, my turn: off"');
    // The sides stay independent: the opponents cell never moved.
    expect(cell(smart, 'upkeep', 'opponents')).toContain('data-stop-value="off"');
    expect(cell(forced, 'upkeep', 'opponents')).toContain('data-stop-value="off"');
  });

  it('editing one cell flips the preset to Custom, and undoing the edit flips it back', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    state.editSettings(stopPatch('main1', 'yours', nextStop(state.settings.steps.yours['main1' as StoppableStep])));
    const custom = panel(state);
    expect(custom).toContain('data-preset="custom"');
    // Custom is a non-clickable segment: a span, not a button.
    expect(custom).toMatch(/<span[^>]*data-preset="custom"[^>]*>Custom<\/span>/);
    expect([...custom.matchAll(/data-preset="custom"/g)]).toHaveLength(1);
    // No preset button stays pressed.
    for (const id of ['casual', 'no-tells', 'full-control']) {
      expect(tag(custom, `data-preset="${id}"`)).not.toContain('aria-pressed="true"');
    }
    // Undoing the edit (back to casual's value) restores the label.
    state.editSettings(stopPatch('main1', 'yours', 'smart'));
    const restored = panel(state);
    expect(tag(restored, 'data-preset="casual"')).toContain('aria-pressed="true"');
    expect(restored).not.toContain('data-preset="custom"');
  });

  it('Reset to Casual returns a heavily edited configuration to the casual preset', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    state.editSettings({ opponentSpell: 'always', logAutoPasses: false, pacing: { stepMs: 100, resolveMs: 200 } });
    state.editSettings(stopPatch('upkeep', 'opponents', 'forced'));
    expect(state.settings.preset).toBe('custom');
    // The Reset button's onclick: applyPreset('casual') → state.applyNamedPreset.
    state.applyNamedPreset('casual');
    expect(state.settings.preset).toBe('casual');
    const html = panel(state);
    expect(tag(html, 'data-preset="casual"')).toContain('aria-pressed="true"');
    expect(html).not.toContain('data-preset="custom"');
    for (const [key, value] of Object.entries(presetPatch('casual'))) {
      expect((state.settings as unknown as Record<string, unknown>)[key]).toEqual(value);
    }
  });

  it('carries the machine-readable affordances: cell aria-labels, switch states, the Smart legend', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    state.editSettings(stopPatch('end', 'opponents', 'smart'));
    const html = panel(state);
    // One labelled cell per stoppable step per side.
    expect([...html.matchAll(/data-step-cell="[^"]+"/g)]).toHaveLength(20);
    expect(cell(html, 'end', 'opponents')).toContain('aria-label="End step, opponent’s turn: smart"');
    expect(cell(html, 'upkeep', 'yours')).toContain('aria-label="Upkeep, my turn: off"');
    // Switches are real switches, checked by the settings.
    expect(tag(html, 'data-toggle="auto-pass"')).toContain('aria-checked="true"');
    expect(tag(html, 'data-actpass-toggle')).toContain('aria-checked="true"');
    expect(tag(html, 'data-toggle="auto-order-triggers"')).toContain('aria-checked="true"');
    expect(tag(html, 'data-toggle="log-auto-passes"')).toContain('aria-checked="true"');
    // The plain-word legend for Smart.
    expect(html).toContain('Smart = only if I have a play');
    // The plain-word option labels the brief names.
    for (const label of ['If I can respond', 'Always', 'Never', 'If it targets me or my stuff and I can respond', "Don't stop", 'Stop if I can respond']) {
      expect(html).toContain(label);
    }
    // Reset is a plain button with its plain name.
    expect(elem(html, 'data-reset-settings')).toContain('Reset to Casual');
  });

  it('the Auto pass switch goes through pressAuto (the brake clears, auto REARMS), not a raw patch', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    // Trip the brake the way the loop guard does, then perform the exact
    // call the Auto pass switch's onclick makes.
    state.suspendAuto('cap');
    expect(state.machinePaused).toBe(true);
    state.pressAuto();
    expect(state.machinePaused).toBe(false);
    // The brake's own note promises "Press the Auto switch to rearm it" —
    // which the old setAuto(!autoPass) onclick BROKE for an auto-on player
    // (pressing turned auto off). pressAuto calls setAuto(true): the brake
    // lifts and auto is rearmed, exactly what the note says.
    expect(state.settings.autoPass).toBe(true);
    const html = panel(state);
    expect(tag(html, 'data-toggle="auto-pass"')).toContain('aria-checked="true"');
  });

  it('while the undo pause holds, the Auto pass switch reads Paused (fb-20260914T063523Z)', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    state.rewind();
    const html = panel(state);
    // The machine is paused but the preference is untouched: the switch
    // shows its paused state, not Off.
    expect(elem(html, 'data-toggle="auto-pass"')).toContain('Paused');
    expect(tag(html, 'data-toggle="auto-pass"')).toContain('aria-checked="true"');
  });

  it('applyNamedPreset re-arms a tripped runaway brake when the preset runs auto (paused-to-Casual)', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    // The brake tripped; the panel still reads auto-pass on (autoPass was
    // never flipped) but considerAuto keeps refusing to act.
    state.suspendAuto('cap');
    expect(state.machinePaused).toBe(true);
    expect(state.settings.autoPass).toBe(true);
    // The exact call the Casual segment's / Reset button's onclick makes.
    state.applyNamedPreset('casual');
    expect(state.machinePaused).toBe(false);
    expect(state.settings.autoPass).toBe(true);
    expect(state.settings.preset).toBe('casual');
    // The machine note is the armed one, as after setAuto(true).
    expect(state.note.kind).toBe('armed');
  });

  it('applyNamedPreset on full-control leaves the machine off, as setAuto(false) would', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    state.applyNamedPreset('full-control');
    expect(state.settings.autoPass).toBe(false);
    expect(state.settings.preset).toBe('full-control');
    expect(state.machinePaused).toBe(false);
    expect(state.note.kind).toBe('off');
  });

  it('the own-objects segments carry both rules, write through editSettings, and explain that they control the own-stack window', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    const html = panel(state);
    expect(tag(html, 'data-own="never"')).toContain('aria-pressed="true"');
    expect(tag(html, 'data-own="if-respondable"')).not.toContain('aria-pressed="true"');
    // The exact call the segment's onclick makes.
    state.editSettings({ ownObjects: 'if-respondable' });
    const next = panel(state);
    expect(tag(next, 'data-own="if-respondable"')).toContain('aria-pressed="true"');
    const legend = elem(next, 'data-own-legend');
    expect(legend).toContain('lets it resolve');
    expect(legend).toContain('Step stops resume after it resolves');
  });

  it('the pacing picker matches an exact (step/resolve) pair and writes through editSettings', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    // casual is 200/400 = Normal.
    expect(tag(panel(state), 'data-pacing="normal"')).toContain('aria-pressed="true"');
    // The exact call the Short segment's onclick makes.
    state.editSettings({ pacing: { stepMs: 100, resolveMs: 200 } });
    expect(tag(panel(state), 'data-pacing="short"')).toContain('aria-pressed="true"');
    // An unmatched pair (fb-20260917T004341Z): Custom now reads selected.
    state.editSettings({ pacing: { stepMs: 37, resolveMs: 91 } });
    const html = panel(state);
    for (const id of ['off', 'short', 'normal', 'slow']) {
      expect(tag(html, `data-pacing="${id}"`)).not.toContain('aria-pressed="true"');
    }
    expect(tag(html, 'data-pacing="custom"')).toContain('aria-pressed="true"');
  });

  // fb-20260917T004341Z: the row is the reporter's "step delay", and the
  // picker carries four canned pairs plus the Custom affordance.
  describe('the step delay row (fb-20260917T004341Z)', () => {
    it('is labelled with the reporter\'s term, keeping the old meaning in parentheses', () => {
      const html = panel(new SeatPanelState('t1', 1, ctx, null));
      expect(html).toContain('Step delay (pause between auto-passes)');
      expect(html).not.toContain('Pause between auto-passes</span>');
      expect(tag(html, 'data-pacing-picker')).toContain('aria-labelledby="pacing-label"');
    });

    it('carries four canned segments, and selecting Slow writes {stepMs: 500, resolveMs: 1000} through editSettings', () => {
      const state = new SeatPanelState('t1', 1, ctx, null);
      const html = panel(state);
      for (const id of ['off', 'short', 'normal', 'slow', 'custom']) {
        expect(tag(html, `data-pacing="${id}"`)).not.toBe('');
      }
      // The exact call the Slow segment's onclick makes.
      state.editSettings({ pacing: { stepMs: 500, resolveMs: 1000 } });
      const slow = panel(state);
      expect(tag(slow, 'data-pacing="slow"')).toContain('aria-pressed="true"');
      expect(state.settings.pacing).toEqual({ stepMs: 500, resolveMs: 1000 });
      // Away from a canned pair the preset relabels to custom (withChange).
      expect(state.settings.preset).toBe('custom');
      expect(tag(slow, 'data-pacing="custom"')).not.toContain('aria-pressed="true"');
      // A canned pair never reveals the inputs without the Custom click.
      expect(slow).not.toContain('data-pacing-input');
      // The legend lists every pair, Slow included.
      expect(elem(slow, 'data-pacing-legend')).toContain('Slow = 500/1000');
      expect(elem(slow, 'data-pacing-legend')).toContain('Off = 0/0');
    });

    it('an unmatched pair selects the Custom segment and reveals the inputs pre-filled with the current values', () => {
      const state = new SeatPanelState('t1', 1, ctx, null);
      state.editSettings({ pacing: { stepMs: 37, resolveMs: 91 } });
      const html = panel(state);
      expect(tag(html, 'data-pacing="custom"')).toContain('aria-pressed="true"');
      expect(tag(html, 'data-pacing="custom"')).toContain('aria-expanded="true"');
      const stepInput = tag(html, 'data-pacing-input="step"');
      const resolveInput = tag(html, 'data-pacing-input="resolve"');
      expect(stepInput).toContain('value="37"');
      expect(resolveInput).toContain('value="91"');
      expect(elem(html, 'data-pacing-custom')).toContain('Step ms');
      expect(elem(html, 'data-pacing-custom')).toContain('Resolve ms');
    });

    it('a canned pair hides the inputs (Custom is opt-in; the browser test drives the real reveal)', () => {
      const state = new SeatPanelState('t1', 1, ctx, null);
      let html = panel(state);
      expect(html).not.toContain('data-pacing-input'); // Normal 200/400: no inputs
      // A canned pair keeps them hidden even after an unrelated re-render.
      state.editSettings({ pacing: { stepMs: 100, resolveMs: 200 } });
      html = panel(state);
      expect(tag(html, 'data-pacing="short"')).toContain('aria-pressed="true"');
      expect(html).not.toContain('data-pacing-input');
    });

    it('clampMs parses an integer clamped to [0, 10000]; non-numeric or empty means leave unchanged', () => {
      expect(clampMs('750')).toBe(750);
      expect(clampMs('0')).toBe(0);
      expect(clampMs('9999')).toBe(9999);
      expect(clampMs('10000')).toBe(10000);
      expect(clampMs('20000')).toBe(10000);
      expect(clampMs('-5')).toBe(0);
      expect(clampMs('440px')).toBe(440); // Number.parseInt semantics
      expect(clampMs('')).toBeNull();
      expect(clampMs('abc')).toBeNull();
      expect(clampMs('12.9')).toBe(12); // parseInt truncates
    });
  });
});

describe('PlaySettingsPanel helpers', () => {
  it('nextStop is the documented three-state cycle', () => {
    expect(nextStop('off')).toBe('smart');
    expect(nextStop('smart')).toBe('forced');
    expect(nextStop('forced')).toBe('off');
  });

  it('stopWord names every state, including the forced one the grid calls Always', () => {
    expect(stopWord('off')).toBe('Off');
    expect(stopWord('smart')).toBe('Smart');
    expect(stopWord('forced')).toBe('Always');
  });

  it('presetPatch covers every settings field, so withChange relabels the preset itself', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    for (const id of ['casual', 'no-tells', 'full-control'] as const) {
      state.editSettings(presetPatch(id));
      expect(state.settings.preset).toBe(id);
      const patch = presetPatch(id) as Record<string, unknown>;
      const live = state.settings as unknown as Record<string, unknown>;
      for (const key of Object.keys(patch)) {
        expect(live[key]).toEqual(patch[key]);
      }
    }
  });
});

describe('PlaySettingsPanel — real clicks in a real browser (PlaySettingsPanel.fixture.html)', () => {
  let browser: Browser;
  const url = browserURL;

  beforeAll(async () => {
    browser = await sharedBrowser();
  });

  /** open mounts the fixture page (a fresh SeatPanelState at casual, null storage). */
  async function open(): Promise<Page> {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/PlaySettingsPanel.fixture.html`);
    return page;
  }

  /** stateOf reads the fixture's live SeatPanelState through the page. */
  async function stateOf(page: Page): Promise<{ paused: boolean; autoPass: boolean; preset: string }> {
    return page.evaluate(() => {
      const s = (window as unknown as { playSettingsState: { machinePaused: boolean; settings: { autoPass: boolean; preset: string } } }).playSettingsState;
      return { paused: s.machinePaused, autoPass: s.settings.autoPass, preset: s.settings.preset };
    });
  }

  it('clicking a cell cycles it off → smart → forced → off through the component handler', async () => {
    const page = await open();
    const cell = page.locator('[data-step-cell="upkeep:yours"]');
    expect(await cell.getAttribute('data-stop-value')).toBe('off');
    expect(await cell.getAttribute('aria-label')).toBe('Upkeep, my turn: off');

    await cell.click();
    expect(await cell.getAttribute('data-stop-value')).toBe('smart');
    expect(await cell.getAttribute('aria-label')).toBe('Upkeep, my turn: smart');
    expect(await cell.textContent()).toContain('Smart');

    await cell.click();
    expect(await cell.getAttribute('data-stop-value')).toBe('forced');
    expect(await cell.getAttribute('aria-label')).toBe('Upkeep, my turn: always');
    expect(await cell.textContent()).toContain('Always');

    await cell.click();
    expect(await cell.getAttribute('data-stop-value')).toBe('off');
    expect(await cell.getAttribute('aria-label')).toBe('Upkeep, my turn: off');
    // The opponents cell never moved.
    expect(await page.locator('[data-step-cell="upkeep:opponents"]').getAttribute('data-stop-value')).toBe('off');
    await page.close();
  });

  it('editing a cell with a real click flips the preset picker to Custom (and Enter works from the keyboard)', async () => {
    const page = await open();
    // Keyboard path first: a native button, focused, activated with Enter.
    const cell = page.locator('[data-step-cell="upkeep:yours"]');
    await cell.focus();
    await page.keyboard.press('Enter');
    expect(await cell.getAttribute('data-stop-value')).toBe('smart');
    // The picker now shows the non-clickable Custom segment and no pressed preset.
    expect(await page.locator('span[data-preset="custom"]').textContent()).toBe('Custom');
    expect(await page.locator('button[data-preset="casual"]').getAttribute('aria-pressed')).toBe('false');
    await page.close();
  });

  it('clicking the Full control segment applies the preset through the state write path', async () => {
    const page = await open();
    await page.locator('[data-preset="full-control"]').click();
    expect(await page.locator('[data-preset="full-control"]').getAttribute('aria-pressed')).toBe('true');
    expect(await page.locator('[data-preset-blurb]').textContent()).toContain('Stops at every priority window');
    expect(await page.locator('[data-toggle="auto-pass"]').getAttribute('aria-checked')).toBe('false');
    expect(await page.locator('[data-stop-value="forced"]').count()).toBe(20);
    const st = await stateOf(page);
    expect(st).toEqual({ paused: false, autoPass: false, preset: 'full-control' });
    await page.close();
  });

  it('choosing Casual after the runaway brake tripped re-arms auto (the machine actually resumes)', async () => {
    const page = await open();
    // Trip the brake the way the loop guard does. Before the fix the preset
    // click left machinePaused set: the panel read Casual/auto-pass while
    // considerAuto kept refusing to act.
    await page.evaluate(() => {
      (window as unknown as { playSettingsState: { suspendAuto: (r: string) => void } }).playSettingsState.suspendAuto('cap');
    });
    expect((await stateOf(page)).paused).toBe(true);
    await page.locator('[data-preset="casual"]').click();
    expect(await stateOf(page)).toEqual({ paused: false, autoPass: true, preset: 'casual' });
    expect(await page.locator('[data-toggle="auto-pass"]').getAttribute('aria-checked')).toBe('true');
    await page.close();
  });

  it('pressing the real Auto pass switch while the UNDO pause holds resumes the machine and preserves the enabled preference (r2 finding)', async () => {
    const page = await open();
    // Rewind the fixture's real state the way the stream's rewind frame
    // does: the machine pauses, the persisted preference stays on.
    await page.evaluate(() => {
      (window as unknown as { playSettingsState: { rewind: () => void } }).playSettingsState.rewind();
    });
    expect(await stateOf(page)).toEqual({ paused: true, autoPass: true, preset: 'casual' });
    // The switch itself reads Paused while the machine is paused.
    expect(await page.locator('[data-toggle="auto-pass"]').textContent()).toContain('Paused');
    // The r2 break: the old onclick was setAuto(!autoPass), which for the
    // default auto-ON player DISABLED auto, cleared the brake and left the
    // empty-window floor to instantly pass the restored window. The
    // pause-aware switch must resume instead — real click, real handler.
    await page.locator('[data-toggle="auto-pass"]').click();
    expect(await stateOf(page)).toEqual({ paused: false, autoPass: true, preset: 'casual' });
    expect(await page.locator('[data-toggle="auto-pass"]').getAttribute('aria-checked')).toBe('true');
    await page.close();
  });

  it('Reset to Casual returns a heavily edited panel to the casual preset', async () => {
    const page = await open();
    // Heavily edit by real clicks: two cells and the Short pacing segment.
    await page.locator('[data-step-cell="upkeep:yours"]').click();
    await page.locator('[data-step-cell="combat-damage:opponents"]').click();
    await page.locator('[data-step-cell="combat-damage:opponents"]').click(); // → forced
    await page.locator('[data-pacing="short"]').click();
    expect((await stateOf(page)).preset).toBe('custom');
    expect(await page.locator('[data-step-cell="combat-damage:opponents"]').getAttribute('data-stop-value')).toBe('forced');

    await page.locator('[data-reset-settings]').click();
    expect((await stateOf(page)).preset).toBe('casual');
    expect(await page.locator('[data-step-cell="upkeep:yours"]').getAttribute('data-stop-value')).toBe('off');
    expect(await page.locator('[data-step-cell="combat-damage:opponents"]').getAttribute('data-stop-value')).toBe('off');
    expect(await page.locator('[data-pacing="normal"]').getAttribute('aria-pressed')).toBe('true');
    expect(await page.locator('button[data-preset="casual"]').getAttribute('aria-pressed')).toBe('true');
    await page.close();
  });

  it('a real click on the fixture game log switch calls onToggleLog — its only write path (fb-20260917T231628Z)', async () => {
    const page = await open();
    const sw = page.locator('[data-toggle="show-game-log"]');
    // The fixture mounts with showLog: false (the seated default).
    expect(await sw.getAttribute('aria-checked')).toBe('false');
    expect(await sw.textContent()).toContain('Show game log');
    await sw.click();
    expect(await page.evaluate(() => (window as unknown as { gameLogToggles: number }).gameLogToggles)).toBe(1);
    await sw.click();
    expect(await page.evaluate(() => (window as unknown as { gameLogToggles: number }).gameLogToggles)).toBe(2);
    // The click wrote nothing into the play settings — the switch is outside
    // that model entirely (no editSettings call, no preset relabelling).
    expect((await stateOf(page)).preset).toBe('casual');
    await page.close();
  });

  // fb-20260914T062319Z-88b4069a B4: the remembered-answers management list
  // against the REAL state — a real click on Forget deletes just that entry,
  // Forget all empties it, and the panel follows the store (the empty-state
  // line returns). The store is seeded through the published state, exactly
  // how the brake is tripped above.
  it('Forget deletes one remembered answer and Forget all empties the store, through the component handlers', async () => {
    const page = await open();
    await page.evaluate(() => {
      const s = (window as unknown as { playSettingsState: { remembered: unknown } }).playSettingsState;
      s.remembered = {
        version: 1,
        entries: [
          { key: 'trigger_optional\u0000p1', choice: 0, label: 'First trigger', savedAt: 1 },
          { key: 'trigger_optional\u0000p2', choice: 1, label: 'Second trigger', savedAt: 2 },
        ],
      };
    });
    await page.waitForSelector('[data-remembered-list]');
    await page.locator('[data-remembered-delete="0"]').click();
    let labels = await page.evaluate(() =>
      (window as unknown as { playSettingsState: { remembered: { entries: { label: string }[] } } }).playSettingsState.remembered.entries.map((e) => e.label));
    expect(labels).toEqual(['Second trigger']);
    expect(await page.locator('[data-remembered-entry]').count()).toBe(1);
    await page.locator('[data-remembered-clear]').click();
    labels = await page.evaluate(() =>
      (window as unknown as { playSettingsState: { remembered: { entries: { label: string }[] } } }).playSettingsState.remembered.entries.map((e) => e.label));
    expect(labels).toEqual([]);
    await page.waitForSelector('[data-remembered-empty]');
    await page.close();
  });

  // fb-20260917T004341Z: the step delay row against the REAL state — Slow
  // writes its pair, the Custom inputs write through on change (not per
  // keystroke), out-of-range clamps / empty leaves the value, and a canned
  // pair re-selected highlights its segment with the revealed inputs showing
  // that pair.
  it('clicking the Slow segment writes {stepMs: 500, resolveMs: 1000} through the state write path', async () => {
    const page = await open();
    await page.locator('[data-pacing="slow"]').click();
    expect(await page.locator('[data-pacing="slow"]').getAttribute('aria-pressed')).toBe('true');
    expect(await page.locator('[data-pacing="custom"]').getAttribute('aria-pressed')).toBe('false');
    const pacing = await page.evaluate(() =>
      (window as unknown as { playSettingsState: { settings: { pacing: { stepMs: number; resolveMs: number } } } }).playSettingsState.settings.pacing);
    expect(pacing).toEqual({ stepMs: 500, resolveMs: 1000 });
    // The inputs stay hidden for a canned pair (Custom was never clicked).
    expect(await page.locator('[data-pacing-input]').count()).toBe(0);
    await page.close();
  });

  it('the Custom segment reveals the inputs, and typing values writes them on change (not per keystroke)', async () => {
    const page = await open();
    await page.locator('[data-pacing="custom"]').click();
    // Revealed pre-filled with the current (casual 200/400) pair.
    expect(await page.locator('[data-pacing-input="step"]').inputValue()).toBe('200');
    expect(await page.locator('[data-pacing-input="resolve"]').inputValue()).toBe('400');
    // fill fires only `input`; the write is on `change`, which blur commits.
    // Before any blur, nothing is written (not per keystroke / per input).
    await page.locator('[data-pacing-input="step"]').fill('750');
    let pacing = await page.evaluate(() =>
      (window as unknown as { playSettingsState: { settings: { pacing: { stepMs: number; resolveMs: number } } } }).playSettingsState.settings.pacing);
    expect(pacing).toEqual({ stepMs: 200, resolveMs: 400 }); // input event alone does not write
    await page.locator('[data-pacing-input="step"]').blur();
    await page.locator('[data-pacing-input="resolve"]').fill('1234');
    await page.locator('[data-pacing-input="resolve"]').blur();
    pacing = await page.evaluate(() =>
      (window as unknown as { playSettingsState: { settings: { pacing: { stepMs: number; resolveMs: number } } } }).playSettingsState.settings.pacing);
    expect(pacing).toEqual({ stepMs: 750, resolveMs: 1234 });
    // Custom reads selected and the preset relabelled (withChange behaviour).
    expect(await page.locator('[data-pacing="custom"]').getAttribute('aria-pressed')).toBe('true');
    expect((await stateOf(page)).preset).toBe('custom');
    await page.close();
  });

  it('an out-of-range input clamps to [0, 10000]; an empty or non-numeric one leaves the value unchanged', async () => {
    const page = await open();
    await page.locator('[data-pacing="custom"]').click();
    const step = page.locator('[data-pacing-input="step"]');
    const resolve = page.locator('[data-pacing-input="resolve"]');
    /** pacing reads the fixture's live settings through the page. */
    const pacing = () =>
      page.evaluate(() =>
        (window as unknown as { playSettingsState: { settings: { pacing: { stepMs: number; resolveMs: number } } } }).playSettingsState.settings.pacing);
    await step.fill('20000');
    await step.blur();
    expect(await pacing()).toEqual({ stepMs: 10000, resolveMs: 400 });
    expect(await step.inputValue()).toBe('10000'); // the clamp is visible
    await resolve.fill('-5');
    await resolve.blur();
    expect(await pacing()).toEqual({ stepMs: 10000, resolveMs: 0 });
    // A number input cannot even hold non-numeric text (the browser rejects
    // the fill), so the reachable leave-unchanged case in the DOM is the
    // empty string; the non-numeric guard itself is pinned on clampMs above.
    await step.fill('');
    await step.blur();
    expect(await pacing()).toEqual({ stepMs: 10000, resolveMs: 0 }); // unchanged
    await page.close();
  });

  it('re-selecting a canned pair highlights its segment, and the revealed inputs show that pair', async () => {
    const page = await open();
    await page.locator('[data-pacing="custom"]').click();
    await page.locator('[data-pacing-input="step"]').fill('750');
    await page.locator('[data-pacing-input="step"]').blur();
    await page.locator('[data-pacing="short"]').click();
    expect(await page.locator('[data-pacing="short"]').getAttribute('aria-pressed')).toBe('true');
    expect(await page.locator('[data-pacing="custom"]').getAttribute('aria-pressed')).toBe('false');
    const pacing = await page.evaluate(() =>
      (window as unknown as { playSettingsState: { settings: { pacing: { stepMs: number; resolveMs: number } } } }).playSettingsState.settings.pacing);
    expect(pacing).toEqual({ stepMs: 100, resolveMs: 200 });
    // The inputs stay revealed (the Custom click opened them) and now show
    // the canned pair.
    expect(await page.locator('[data-pacing-input="step"]').inputValue()).toBe('100');
    expect(await page.locator('[data-pacing-input="resolve"]').inputValue()).toBe('200');
    await page.close();
  });
});

describe('PlaySettingsPanel — Layout section (fb-20260916T182801Z)', () => {
  it('renders a row per zone with a size stepper, a live readout and an alignment select', () => {
    const html = panel(new SeatPanelState('yt-layout', 1, ctx, null));
    expect(html).toContain('data-layout-section');
    for (const zone of ['creatures', 'others', 'lands', 'command', 'hand'] as const) {
      const row = elem(html, `data-layout-zone="${zone}"`);
      expect(row).not.toBe('');
      expect(row).toContain('data-layout-smaller');
      expect(row).toContain('data-layout-larger');
      expect(elem(html, `data-layout-scale="${zone}"`)).toContain('100%');
      expect(row).toContain('<select');
      expect(row).toContain('data-layout-align');
    }
    // fb-20260917T232202Z: the command zone has its own row, labelled, ordered
    // between the lands row and the hand row (the seat's rim zone, not a
    // battlefield row).
    const cmdRow = elem(html, 'data-layout-zone="command"');
    expect(cmdRow).toContain('Command zone');
    expect(html.indexOf('data-layout-zone="lands"')).toBeLessThan(html.indexOf('data-layout-zone="command"'));
    expect(html.indexOf('data-layout-zone="command"')).toBeLessThan(html.indexOf('data-layout-zone="hand"'));
    expect(html).toContain('data-peek-picker');
    expect(html).toContain('data-layout-reset');
    // the hand's peek segmented control marks the shipped default
    expect(tag(html, 'data-peek="hover"')).toContain('aria-pressed="true"');
  });

  it('the size readout follows the shared layout store, and Reset returns it', () => {
    // The layout section edits the SEPARATE layoutsettings store, not the
    // seat state: bumping it here is exactly what the panel's + button does,
    // and the readout must show the new number (and Reset restore it).
    const store = layoutStore;
    store.bump('creatures', 0.1);
    const html = panel(new SeatPanelState('yt-layout2', 1, ctx, null));
    expect(elem(html, 'data-layout-scale="creatures"')).toContain('110%');
    store.reset();
    store.dispose();
    const calm = panel(new SeatPanelState('yt-layout3', 1, ctx, null));
    expect(elem(calm, 'data-layout-scale="creatures"')).toContain('100%');
  });

  it('the on-board steppers show/hide toggle reads Hidden by default and flips the store (fb-20260917T004304Z default flip)', () => {
    const store = layoutStore;
    try {
      const html = panel(new SeatPanelState('yt-toggle1', 1, ctx, null));
      expect(html).toContain('data-layout-steppers-toggle');
      // a role="switch" row like the other toggles, OFF at the shipped default
      expect(tag(html, 'data-toggle="steppers-on-board"')).toContain('role="switch"');
      expect(tag(html, 'data-layout-steppers-toggle')).toContain('aria-checked="false"');
      expect(elem(html, 'data-layout-steppers-toggle')).toContain('Hidden');

      // the exact call the toggle's onclick makes; aria-checked follows
      store.setSteppersOnBoard(true);
      const on = panel(new SeatPanelState('yt-toggle2', 1, ctx, null));
      expect(tag(on, 'data-layout-steppers-toggle')).toContain('aria-checked="true"');
      expect(elem(on, 'data-layout-steppers-toggle')).toContain('Shown');

      // ...and the panel's OWN per-zone steppers/alignment rows stay mounted
      // regardless of the toggle: they are the way back once the board marks
      // are hidden.
      for (const zone of ['creatures', 'others', 'lands', 'command', 'hand'] as const) {
        const row = elem(on, `data-layout-zone="${zone}"`);
        expect(row).not.toBe('');
        expect(row).toContain('data-layout-smaller');
        expect(row).toContain('data-layout-larger');
        expect(row).toContain('data-layout-align');
      }
      expect(on).toContain('data-peek-picker');
      expect(on).toContain('data-layout-reset');
    } finally {
      store.reset();
      store.dispose();
    }
  });

  // fb-20260917T231628Z: the transcript's show/hide switch moved into the
  // Layout section from the rail's top row. Same role="switch" row idiom as
  // the toggle above, but its state and write path are PROPS (Table.svelte's
  // showLog/toggleLog, persisted through logshown.ts) — never the layout
  // store, never logic.editSettings, never a PlaySettings field.
  it('renders the game log switch bound to its props, and nothing when no toggle is supplied', () => {
    // No onToggleLog: a caller that does not own the log (every existing
    // fixture/test) renders no switch — the Rail contract, mirrored.
    const absent = panel(new SeatPanelState('lg-absent', 1, ctx, null));
    expect(absent).not.toContain('data-toggle="show-game-log"');
    expect(absent).toContain('data-layout-section'); // the section itself is untouched

    const shown = panel(new SeatPanelState('lg-on', 1, ctx, null), { showLog: true, onToggleLog: () => {} });
    const on = tag(shown, 'data-toggle="show-game-log"');
    expect(on).toContain('role="switch"');
    expect(on).toContain('aria-checked="true"');
    expect(elem(shown, 'data-toggle="show-game-log"')).toContain('Show game log');
    expect(elem(shown, 'data-toggle="show-game-log"')).toContain('Shown');

    const hidden = panel(new SeatPanelState('lg-off', 1, ctx, null), { showLog: false, onToggleLog: () => {} });
    expect(tag(hidden, 'data-toggle="show-game-log"')).toContain('aria-checked="false"');
    expect(elem(hidden, 'data-toggle="show-game-log"')).toContain('Hidden');
  });
});

describe('PlaySettingsPanel — persistence notice (fb-20260917T232814Z)', () => {
  // The notice is the ONE visible signal that every preference store is
  // memory-only (lib/storage.ts storageWritable, probed live at mount). The
  // node test env has no localStorage, so the "works" case stubs a fake and
  // the "refuses" case stubs the throwing shape the brief names.
  const refusing = {
    getItem: () => null,
    setItem: () => {
      throw new Error('refused');
    },
    removeItem: () => undefined,
    clear: () => undefined,
  };

  it('is hidden when localStorage writes round-trip', () => {
    vi.stubGlobal('localStorage', {
      getItem: () => null,
      setItem: () => undefined,
      removeItem: () => undefined,
      clear: () => undefined,
    });
    try {
      const html = panel(new SeatPanelState('pw-ok', 1, ctx, null));
      expect(html).not.toContain('data-persist-warn');
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it('is shown when the browser refuses site data (setItem throws)', () => {
    vi.stubGlobal('localStorage', refusing);
    try {
      const html = panel(new SeatPanelState('pw-refuses', 1, ctx, null));
      expect(html).toContain('data-persist-warn');
      expect(elem(html, 'data-persist-warn')).toContain('Preferences are not being saved');
      // One-line notice UNDER the step-stop grid, inside its section.
      expect(html.indexOf('data-persist-warn')).toBeGreaterThan(html.indexOf('data-step-cell'));
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it('is shown when there is no localStorage global at all (the node/SSR default)', () => {
    const html = panel(new SeatPanelState('pw-none', 1, ctx, null));
    expect(html).toContain('data-persist-warn');
  });
});
