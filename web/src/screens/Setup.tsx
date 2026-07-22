import { useState } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "../components/ui/card";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "../components/ui/accordion";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { ErrorNotice } from "../components/ErrorNotice";
import { WarningBanner } from "../components/WarningBanner";
import { ApiError } from "../api/client";
import { useConfig, usePlan, useSettings, useStartRun, useSystem } from "../hooks";

// Setup is the pre-install screen: it shows what server and config repo
// Kamino is about to act on, lets the operator pick a profile, and — because
// the config repo is arbitrary and user-supplied and its scripts run as
// root — makes every step of the resolved plan and every safety warning
// visible before the Install button can even be pressed.
export function Setup() {
  const navigate = useNavigate();
  const { data: settings } = useSettings();
  const { data: system } = useSystem();
  const { data: config, refetch: refetchConfig } = useConfig();
  const plan = usePlan();
  const startRun = useStartRun();

  const [profile, setProfile] = useState("");
  const [secrets, setSecrets] = useState<Record<string, string>>({});

  // Settings load asynchronously; only redirect once we actually know the
  // repo isn't configured, not while `settings` is still undefined.
  if (settings && !settings.configured) {
    return <Navigate to="/connect" />;
  }

  function handleReview() {
    if (!profile) return;
    setSecrets({});
    plan.mutate({ profile, arch: system?.arch });
  }

  function setSecret(name: string, value: string) {
    setSecrets((prev) => ({ ...prev, [name]: value }));
  }

  const declaredSecrets = plan.data?.secrets ?? [];
  const missingSecret = declaredSecrets.some((name) => !secrets[name]);

  async function handleInstall() {
    if (!profile) return;
    try {
      const { run_id } = await startRun.mutateAsync({ profile, arch: system?.arch, secrets });
      navigate(`/run/${run_id}`);
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        toast.error("a run is already in progress");
      }
      // A 422 is surfaced below via startRun.isError + ErrorNotice; the UI
      // shouldn't be able to produce one since Install is gated, but the
      // server is still the source of truth.
    }
  }

  return (
    <div className="mx-auto max-w-3xl space-y-4">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            Server
            {system && system.root === false && <Badge variant="destructive">not root</Badge>}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-1 text-sm">
          {system ? (
            <>
              <p>
                Hostname <span className="font-medium">{system.hostname}</span>
              </p>
              <p>
                OS <span className="font-medium">{system.os} {system.version_id}</span>
              </p>
              <p>
                Arch <span className="font-medium">{system.arch}</span>
              </p>
            </>
          ) : (
            <p className="text-muted-foreground">Loading…</p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Config repo</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm">
          {config ? (
            <>
              <p>
                Name <span className="font-medium">{config.name}</span>
              </p>
              <p>
                SHA <span className="font-mono">{config.sha}</span>
              </p>
              {config.stale && (
                <WarningBanner
                  message="Stale config"
                  detail={config.fetched_at ? `Serving the last known-good copy, fetched ${config.fetched_at}` : undefined}
                />
              )}
            </>
          ) : (
            <p className="text-muted-foreground">Loading…</p>
          )}
        </CardContent>
        <CardFooter>
          <Button type="button" variant="outline" onClick={() => refetchConfig()}>
            Refresh
          </Button>
        </CardFooter>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Choose a profile</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {config ? (
            <>
              {/* The label (and its options) only mount once the config has
                  actually loaded, so a test/user awaiting the "Profile"
                  label by role never race a select that's still empty. */}
              <div className="space-y-1.5">
                <Label htmlFor="profile-select">Profile</Label>
                <select
                  id="profile-select"
                  className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  value={profile}
                  onChange={(e) => setProfile(e.target.value)}
                >
                  <option value="" disabled>
                    Select a profile…
                  </option>
                  {config.profiles.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </select>
              </div>

              {config.categories.length > 0 && (
                <Accordion type="multiple">
                  {config.categories.map((category) => (
                    <AccordionItem key={category.id} value={category.id}>
                      <AccordionTrigger>{category.name}</AccordionTrigger>
                      <AccordionContent>
                        <ul className="space-y-1.5">
                          {category.items.map((item) => (
                            <li key={item.ref} className="flex flex-wrap items-center gap-2">
                              <span className="font-medium">{item.name}</span>
                              {item.version && <span className="text-muted-foreground">v{item.version}</span>}
                              {item.depends_on && item.depends_on.length > 0 && (
                                <span className="text-xs text-muted-foreground">
                                  depends on {item.depends_on.join(", ")}
                                </span>
                              )}
                            </li>
                          ))}
                        </ul>
                      </AccordionContent>
                    </AccordionItem>
                  ))}
                </Accordion>
              )}
            </>
          ) : (
            <p className="text-muted-foreground">Loading…</p>
          )}
        </CardContent>
        <CardFooter>
          <Button type="button" onClick={handleReview} disabled={!profile || plan.isPending}>
            {plan.isPending ? "Reviewing…" : "Review plan"}
          </Button>
        </CardFooter>
      </Card>

      {plan.isError && <ErrorNotice error={plan.error} />}

      {plan.isSuccess && (
        <Card>
          <CardHeader>
            <CardTitle>Plan</CardTitle>
            <CardDescription>
              {plan.data.steps.length} step{plan.data.steps.length === 1 ? "" : "s"}, executed as root on{" "}
              {plan.data.arch}.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <ol className="space-y-1 text-sm">
              {plan.data.steps.map((step) => (
                <li key={step.ref} className="flex items-center gap-2">
                  <span className="font-medium">{step.name}</span>
                  <span className="text-xs text-muted-foreground">{step.type}</span>
                </li>
              ))}
            </ol>

            {(plan.data.warnings ?? []).map((warning, i) => (
              <WarningBanner key={i} message={warning} />
            ))}

            {declaredSecrets.length > 0 && (
              <div className="space-y-3">
                {declaredSecrets.map((name) => (
                  <div key={name} className="space-y-1.5">
                    <Label htmlFor={`secret-${name}`}>{name}</Label>
                    <Input
                      id={`secret-${name}`}
                      type="password"
                      value={secrets[name] ?? ""}
                      onChange={(e) => setSecret(name, e.target.value)}
                    />
                  </div>
                ))}
              </div>
            )}
          </CardContent>
          <CardFooter className="flex-col items-stretch gap-2">
            <Button type="button" onClick={handleInstall} disabled={missingSecret || startRun.isPending}>
              {startRun.isPending ? "Starting…" : "Install"}
            </Button>
            {startRun.isError && startRun.error instanceof ApiError && startRun.error.status === 422 && (
              <ErrorNotice error={startRun.error} />
            )}
          </CardFooter>
        </Card>
      )}
    </div>
  );
}
