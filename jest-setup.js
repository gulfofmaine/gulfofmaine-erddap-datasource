// Jest setup provided by Grafana scaffolding
import './.config/jest-setup';

// The scaffolded setup stubs getContext() with a no-op, but @grafana/ui's
// Combobox measures text through a real 2D context to size its dropdown.
HTMLCanvasElement.prototype.getContext = () => ({
  measureText: (text) => ({ width: String(text).length * 8 }),
});

// Combobox's floating dropdown observes its own size and scroll position;
// jsdom implements neither observer.
class NoopObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}

global.ResizeObserver = global.ResizeObserver ?? NoopObserver;
global.IntersectionObserver = global.IntersectionObserver ?? NoopObserver;

// Combobox virtualizes its option list, which needs a non-zero element height.
Object.defineProperty(HTMLElement.prototype, 'offsetHeight', {
  configurable: true,
  value: 300,
});
