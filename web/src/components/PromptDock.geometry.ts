import { mount } from 'svelte';
import '../app.css';
import PromptDockFixture from './PromptDock.geometry.fixture.svelte';

// The whole fixture is the Svelte component: one real SeatPanelState with a
// multi-target ask, and a fake board whose pile badge the geometry test pins
// under the near-table dock's measured rect. See
// PromptDock.geometry.fixture.svelte for the body.
mount(PromptDockFixture, { target: document.getElementById('fixture')! });
