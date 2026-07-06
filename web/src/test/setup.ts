import { Window } from 'happy-dom';

const testWindow = new Window({ url: 'https://oma.duck.ai/' });
const globalScope = globalThis as unknown as Record<string, unknown>;

globalScope.window = testWindow;
globalScope.document = testWindow.document;
globalScope.Element = testWindow.Element;
globalScope.Node = testWindow.Node;
globalScope.HTMLElement = testWindow.HTMLElement;
globalScope.HTMLButtonElement = testWindow.HTMLButtonElement;
globalScope.HTMLInputElement = testWindow.HTMLInputElement;
globalScope.HTMLSelectElement = testWindow.HTMLSelectElement;
globalScope.MutationObserver = testWindow.MutationObserver;
globalScope.ResizeObserver = testWindow.ResizeObserver;
globalScope.navigator = testWindow.navigator;
globalScope.requestAnimationFrame = testWindow.requestAnimationFrame.bind(testWindow);
globalScope.cancelAnimationFrame = testWindow.cancelAnimationFrame.bind(testWindow);
globalScope.getComputedStyle = testWindow.getComputedStyle.bind(testWindow);

export function resetTestDom(url: string) {
  testWindow.history.replaceState(null, '', url);
  testWindow.document.body.innerHTML = '';
  testWindow.document.documentElement.lang = 'en';
  testWindow.document.documentElement.dir = 'ltr';
  delete testWindow.document.documentElement.dataset.locale;
  delete testWindow.document.documentElement.dataset.theme;
  delete testWindow.document.documentElement.dataset.themeMode;
  testWindow.document.documentElement.className = '';
  testWindow.document.body.removeAttribute('style');
}
