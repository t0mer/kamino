import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { ChevronDown, ChevronRight } from "lucide-react";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "../components/ui/card";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { Button } from "../components/ui/button";
import { ErrorNotice } from "../components/ErrorNotice";
import { useSettings, useTestSettings, useSaveSettings } from "../hooks";
import { cn } from "../lib/utils";

// Connect is the first-run / settings screen: point Kamino at a config repo,
// verify it resolves before trusting it, then persist. The server is the
// source of truth for "did the save actually take" — a 422 here means the
// previous working config was left untouched, so the UI just needs to say so.
export function Connect() {
  const navigate = useNavigate();
  const { data: settings } = useSettings();
  const testSettings = useTestSettings();
  const saveSettings = useSaveSettings();

  const [repoUrl, setRepoUrl] = useState(settings?.repo_url ?? "");
  const [ref, setRef] = useState(settings?.ref ?? "main");
  const [repoToken, setRepoToken] = useState("");
  const [rawBaseTemplate, setRawBaseTemplate] = useState(settings?.raw_base_template ?? "");
  const [advancedOpen, setAdvancedOpen] = useState(false);

  function buildBody() {
    return {
      repo_url: repoUrl,
      ref,
      raw_base_template: rawBaseTemplate || undefined,
      // A trimmed-empty token means "don't touch the stored one" — the server
      // treats a null/absent repo_token as keep-as-is, and an explicit "" as
      // clear-it, so only send the field when the operator actually typed one.
      ...(repoToken !== "" ? { repo_token: repoToken } : {}),
    };
  }

  function handleTest() {
    testSettings.mutate(buildBody());
  }

  async function handleSave() {
    try {
      await saveSettings.mutateAsync(buildBody());
      navigate("/setup");
    } catch {
      // The mutation's own error state drives the ErrorNotice below; nothing
      // further to do here besides swallowing the rejection.
    }
  }

  return (
    <div className="mx-auto max-w-xl space-y-4">
      <Card>
        <CardHeader>
          <CardTitle>Connect a config repo</CardTitle>
          <CardDescription>
            Point Kamino at the repository holding your categories, profiles, and install scripts.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="repo-url">Repo URL</Label>
            <Input
              id="repo-url"
              placeholder="https://github.com/you/kamino-config"
              value={repoUrl}
              onChange={(e) => setRepoUrl(e.target.value)}
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="repo-ref">Branch / ref</Label>
            <Input id="repo-ref" value={ref} onChange={(e) => setRef(e.target.value)} />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="repo-token">Access token</Label>
            <Input
              id="repo-token"
              type="password"
              placeholder="leave blank for a public repo"
              value={repoToken}
              onChange={(e) => setRepoToken(e.target.value)}
            />
            {settings?.has_repo_token && repoToken === "" && (
              <p className="text-xs text-muted-foreground">A token is already stored; leave blank to keep it.</p>
            )}
          </div>

          <div>
            <button
              type="button"
              onClick={() => setAdvancedOpen((v) => !v)}
              className="flex items-center gap-1 text-sm font-medium text-muted-foreground hover:text-foreground"
            >
              {advancedOpen ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
              Advanced
            </button>
            <div className={cn("mt-2 space-y-1.5", !advancedOpen && "hidden")}>
              <Label htmlFor="raw-base-template">Raw base URL override</Label>
              <Input
                id="raw-base-template"
                placeholder="https://example.com/{path}?ref={ref}"
                value={rawBaseTemplate}
                onChange={(e) => setRawBaseTemplate(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                Only needed for hosts that aren't GitHub, GitLab, or Gitea/Forgejo. Use <code>{"{ref}"}</code> and{" "}
                <code>{"{path}"}</code> as placeholders.
              </p>
            </div>
          </div>
        </CardContent>
        <CardFooter className="flex gap-2">
          <Button type="button" variant="outline" onClick={handleTest} disabled={testSettings.isPending}>
            {testSettings.isPending ? "Testing…" : "Test connection"}
          </Button>
          <Button type="button" onClick={handleSave} disabled={saveSettings.isPending}>
            {saveSettings.isPending ? "Saving…" : "Save"}
          </Button>
        </CardFooter>
      </Card>

      {testSettings.isSuccess && (
        <Card>
          <CardHeader>
            <CardTitle>Found it</CardTitle>
          </CardHeader>
          <CardContent className="space-y-1 text-sm">
            <p>
              Manifest <span className="font-medium">{testSettings.data.name}</span>
            </p>
            <p>{testSettings.data.categories} categories</p>
            <p>{testSettings.data.profiles} profiles</p>
            <p>
              SHA <span className="font-mono">{testSettings.data.sha}</span>
            </p>
          </CardContent>
        </Card>
      )}
      {testSettings.isError && <ErrorNotice error={testSettings.error} />}

      {saveSettings.isError && (
        <div className="space-y-1">
          <ErrorNotice error={saveSettings.error} />
          <p className="text-xs text-muted-foreground">The previous config was kept.</p>
        </div>
      )}
    </div>
  );
}
