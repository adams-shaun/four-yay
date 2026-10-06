import { mount } from 'svelte';
import '../app.css';
import PromptDockFeedbackFixture from './PromptDock.feedback.geometry.fixture.svelte';

// The real PromptDock, PromptRailSlot and FeedbackButton over a plain rail;
// PromptDock.feedback.geometry.test.ts measures them. See the fixture body.
mount(PromptDockFeedbackFixture, { target: document.getElementById('fixture')! });
