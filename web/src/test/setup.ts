import { Window } from 'happy-dom';

const testWindow = new Window({ url: 'https://oma.duck.ai/' });
const globalScope = globalThis as unknown as Record<string, unknown>;

globalScope.window = testWindow;
globalScope.document = testWindow.document;
globalScope.Element = testWindow.Element;
globalScope.Node = testWindow.Node;
globalScope.NodeFilter = testWindow.NodeFilter;
globalScope.HTMLElement = testWindow.HTMLElement;
globalScope.HTMLButtonElement = testWindow.HTMLButtonElement;
globalScope.HTMLInputElement = testWindow.HTMLInputElement;
globalScope.HTMLSelectElement = testWindow.HTMLSelectElement;
globalScope.MutationObserver = testWindow.MutationObserver;
globalScope.ResizeObserver = testWindow.ResizeObserver;
globalScope.DOMRect = testWindow.DOMRect;
globalScope.navigator = testWindow.navigator;
globalScope.requestAnimationFrame = testWindow.requestAnimationFrame.bind(testWindow);
globalScope.cancelAnimationFrame = testWindow.cancelAnimationFrame.bind(testWindow);
globalScope.getComputedStyle = testWindow.getComputedStyle.bind(testWindow);

if (typeof HTMLElement.prototype.getAnimations !== 'function') {
  Object.defineProperty(HTMLElement.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  });
}

export function resetTestDom(url: string) {
  testWindow.history.replaceState(null, '', url);
  testWindow.document.body.innerHTML = '';
  for (const cookie of testWindow.document.cookie.split(';')) {
    const name = cookie.split('=')[0]?.trim();
    if (name) testWindow.document.cookie = `${name}=; path=/; max-age=0`;
  }
  testWindow.document.documentElement.lang = 'en';
  testWindow.document.documentElement.dir = 'ltr';
  delete testWindow.document.documentElement.dataset.locale;
  delete testWindow.document.documentElement.dataset.theme;
  delete testWindow.document.documentElement.dataset.themeMode;
  testWindow.document.documentElement.className = '';
  testWindow.document.body.removeAttribute('style');
}
