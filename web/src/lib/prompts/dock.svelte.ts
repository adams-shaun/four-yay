import { safeStorage } from '../storage';
import { loadDockLayout, saveDockLayout, type DockLayout, type DockPlacement, type DockPoint } from './dock';

/**
 * localDock is the dock placement's reactive local default: what the prompt
 * dock uses when its mount passes no placement of its own. Sub-project 2's
 * layout profiles replace it by passing placement/position props.
 */
class LocalDock {
  layout = $state<DockLayout>(loadDockLayout(safeStorage()));

  setPlacement(placement: DockPlacement): void {
    this.layout = { ...this.layout, placement };
    saveDockLayout(safeStorage(), this.layout);
  }

  setPosition(position: DockPoint): void {
    this.layout = { ...this.layout, position };
    saveDockLayout(safeStorage(), this.layout);
  }
}

export const localDock = new LocalDock();
