import "@testing-library/jest-dom";

// jsdom does not implement matchMedia. Several shadcn/ui pieces (Sonner's
// Toaster via next-themes, ThemeToggle's initial-theme detection) call it
// directly, so stub it once here rather than in every test file.
if (!window.matchMedia) {
  window.matchMedia = (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }) as unknown as MediaQueryList;
}
