import { useEffect, useState } from "react";
import { Button } from "./ui/button";

type Theme = "dark" | "light";

function initialTheme(): Theme {
  const saved = localStorage.getItem("kamino_theme");
  if (saved === "light" || saved === "dark") return saved;
  // Default dark, but respect an explicit light system preference on first run.
  return window.matchMedia?.("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

// ThemeToggle flips the root `dark` class Tailwind keys off, persisting the
// choice. Dark is the default per the spec.
export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(initialTheme);

  useEffect(() => {
    document.documentElement.classList.toggle("dark", theme === "dark");
    localStorage.setItem("kamino_theme", theme);
  }, [theme]);

  return (
    <Button variant="ghost" size="sm" aria-label="Toggle theme"
      onClick={() => setTheme((t) => (t === "dark" ? "light" : "dark"))}>
      {theme === "dark" ? "☀" : "☾"}
    </Button>
  );
}
