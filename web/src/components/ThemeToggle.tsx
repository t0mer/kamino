import { useEffect, useState } from "react";
import { Button } from "./ui/button";
import { applyTheme, resolveTheme, type Theme } from "../lib/theme";

// ThemeToggle flips the root `dark` class Tailwind keys off, persisting the
// choice. Dark is the default per the spec; the initial application happens at
// startup in main.tsx so the pre-auth token gate is themed too — this component
// only owns the interactive toggle.
export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(resolveTheme);

  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  return (
    <Button variant="ghost" size="sm" aria-label="Toggle theme"
      onClick={() => setTheme((t) => (t === "dark" ? "light" : "dark"))}>
      {theme === "dark" ? "☀" : "☾"}
    </Button>
  );
}
