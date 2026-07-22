import { NavLink, Outlet } from "react-router-dom";
import { Settings } from "lucide-react";
import { cn } from "../lib/utils";
import { ThemeToggle } from "./ThemeToggle";

const linkClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    "rounded px-3 py-1.5 text-sm font-medium transition-colors",
    isActive ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:text-foreground"
  );

// AppLayout renders the top nav bar (Setup, History, a Connect settings gear,
// and the theme toggle) plus the routed screen below it.
export function AppLayout() {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="flex items-center justify-between border-b px-4 py-2">
        <nav className="flex items-center gap-1">
          <span className="mr-2 text-sm font-semibold">Kamino</span>
          <NavLink to="/setup" className={linkClass}>Setup</NavLink>
          <NavLink to="/history" className={linkClass}>History</NavLink>
        </nav>
        <div className="flex items-center gap-1">
          <NavLink
            to="/connect"
            aria-label="Connection settings"
            className={({ isActive }) =>
              cn(
                "rounded p-1.5 transition-colors",
                isActive ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:text-foreground"
              )
            }
          >
            <Settings className="h-4 w-4" />
          </NavLink>
          <ThemeToggle />
        </div>
      </header>
      <main className="p-4">
        <Outlet />
      </main>
    </div>
  );
}
