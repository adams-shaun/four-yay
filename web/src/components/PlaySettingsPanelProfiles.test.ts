import { type Browser, type Page } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';

/**
 * PlaySettingsPanel — profiles: real clicks in a real browser.
 *
 * The existing PlaySettingsPanel.svelte.test.ts owns the panel's preset and
 * editor contracts; this file exercises the NEW profiles row against the
 * SAME fixture (a REAL SeatPanelState at casual, storage null), so every
 * click reaches the component's own handler and the state's write path.
 */
describe('PlaySettingsPanel — profiles row (real clicks)', () => {
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

  it('save → the profile appears in the list → apply marks it active → delete removes it', async () => {
    const page = await open();
    // PRECONDITION: no profiles to start, and the Save control really exists.
    expect(await page.locator('[data-profiles]').count()).toBe(1);
    expect(await page.locator('[data-profile-list]').count()).toBe(0);

    // Type a name and Save the current (casual) settings.
    await page.locator('[data-profile-name]').fill('My Casual');
    await page.locator('[data-profile-save]').click();

    // The list now shows the profile and the active label follows it.
    expect(await page.locator('[data-profile-list]').count()).toBe(1);
    expect(await page.locator('[data-profile-apply="My Casual"]').count()).toBe(1);
    expect(await page.locator('[data-active-profile]').textContent()).toContain('My Casual');

    // Edit the settings away from the profile (Upkeep stop) → "(modified)".
    await page.locator('[data-step-cell="upkeep:yours"]').click();
    expect(await page.locator('[data-active-profile]').textContent()).toContain('(modified)');

    // Apply the profile: the edit is undone and the active label clears the marker.
    await page.locator('[data-profile-apply="My Casual"]').click();
    expect(await page.locator('[data-step-cell="upkeep:yours"]').getAttribute('data-stop-value')).toBe('off');
    expect(await page.locator('[data-active-profile]').textContent()).not.toContain('(modified)');

    // Delete it: the list empties and the active label disappears.
    await page.locator('[data-profile-delete="My Casual"]').click();
    expect(await page.locator('[data-profile-list]').count()).toBe(0);
    expect(await page.locator('[data-active-profile]').count()).toBe(0);
    await page.close();
  });

  it('an empty Save reports an error and creates no profile', async () => {
    const page = await open();
    await page.locator('[data-profile-save]').click();
    expect(await page.locator('[data-profile-error]').textContent()).toContain('Enter a profile name');
    expect(await page.locator('[data-profile-list]').count()).toBe(0);
    await page.close();
  });

  it('rename preserves the profile under its new name through the component handler', async () => {
    const page = await open();
    await page.locator('[data-profile-name]').fill('Before');
    await page.locator('[data-profile-save]').click();
    await page.locator('[data-profile-rename="Before"]').click();
    await page.locator('[data-profile-rename-input="Before"]').fill('After');
    await page.locator('[data-profile-rename-commit="Before"]').click();
    expect(await page.locator('[data-profile-apply="After"]').count()).toBe(1);
    expect(await page.locator('[data-profile-apply="Before"]').count()).toBe(0);
    await page.close();
  });

  it('a profile that runs auto re-arms the machine (the runaway brake clears) — the applyProfile contract', async () => {
    const page = await open();
    // Trip the brake, then save the (casual, autoPass=true) settings as a profile.
    await page.evaluate(() => {
      (window as unknown as { playSettingsState: { suspendAuto: (r: string) => void } }).playSettingsState.suspendAuto('cap');
    });
    await page.locator('[data-profile-name]').fill('Auto');
    await page.locator('[data-profile-save]').click();
    // Applying the auto-running profile must clear the brake, exactly as
    // applyNamedPreset does.
    await page.locator('[data-profile-apply="Auto"]').click();
    const paused = await page.evaluate(() => (window as unknown as { playSettingsState: { machinePaused: boolean } }).playSettingsState.machinePaused);
    expect(paused).toBe(false);
    await page.close();
  });
});
