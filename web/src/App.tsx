import { useEffect, useState } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "react-router-dom";
import { Toaster } from "./components/ui/sonner";
import { queryClient } from "./api/queryClient";
import { TokenGate } from "./auth/TokenGate";
import { ApiTokenProvider } from "./hooks/useApi";
import { useToken } from "./auth/useToken";
import { router } from "./router";

// The app's dark/light toggle (see ThemeToggle) works by flipping the `dark`
// class on <html> directly — there's no next-themes ThemeProvider in the
// tree. Sonner's shadcn Toaster reads theme via next-themes' useTheme(),
// which without a provider falls back to "system" and ignores that class
// entirely, so a toast could render light-on-light or dark-on-dark against
// the rest of the (differently-themed) page. Mirror the root class instead
// of pulling in a ThemeProvider just for this.
function useDomTheme(): "light" | "dark" {
  const [theme, setTheme] = useState<"light" | "dark">(() =>
    document.documentElement.classList.contains("dark") ? "dark" : "light"
  );

  useEffect(() => {
    const root = document.documentElement;
    const sync = () => setTheme(root.classList.contains("dark") ? "dark" : "light");
    const observer = new MutationObserver(sync);
    observer.observe(root, { attributes: true, attributeFilter: ["class"] });
    return () => observer.disconnect();
  }, []);

  return theme;
}

function Authed() {
  const { token } = useToken();
  // TokenGate guarantees a token before rendering children.
  return (
    <ApiTokenProvider token={token!}>
      <RouterProvider router={router} />
    </ApiTokenProvider>
  );
}

export default function App() {
  const theme = useDomTheme();
  return (
    <QueryClientProvider client={queryClient}>
      <TokenGate>
        <Authed />
      </TokenGate>
      <Toaster theme={theme} />
    </QueryClientProvider>
  );
}
