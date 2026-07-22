export type Theme = "dark" | "light";

const KEY = "kamino_theme";

// resolveTheme picks the theme to use: a previously saved choice wins;
// otherwise dark is the default, unless the OS explicitly prefers light on a
// first visit.
export function resolveTheme(): Theme {
  const saved = localStorage.getItem(KEY);
  if (saved === "light" || saved === "dark") return saved;
  return window.matchMedia?.("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

// applyTheme reflects a theme onto the document root (the `dark` class Tailwind
// keys off) and persists it.
export function applyTheme(theme: Theme): void {
  document.documentElement.classList.toggle("dark", theme === "dark");
  localStorage.setItem(KEY, theme);
}

// initTheme applies the resolved theme at startup, before React renders, so the
// whole app — including the pre-auth token gate — honours the dark-mode default
// rather than flashing the browser's light default until a component mounts.
export function initTheme(): void {
  applyTheme(resolveTheme());
}
