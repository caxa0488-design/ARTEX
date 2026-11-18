"use client";
import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { api, http } from "@/lib/api";
import { managedBindingPayload, managedVersionPayload, type SkillManagement } from "@/lib/phase2-management";
export default function SkillVersionsPage() {
  const [names, setNames] = useState<string[]>([]);
  const [name, setName] = useState("");
  const [view, setView] = useState<SkillManagement | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [label, setLabel] = useState("");
  const [source, setSource] = useState("");
  const [license, setLicense] = useState("");
  const [reason, setReason] = useState("");
  const [agent, setAgent] = useState("");
  const [version, setVersion] = useState("");
  useEffect(() => {
    let alive = true;
    Promise.allSettled([api.skills(), http<{ names: string[]; possibly_truncated: boolean }>("/managed-skills")])
      .then(([installed, index]) => {
        if (!alive) return;
        const localNames = installed.status === "fulfilled" ? installed.value.map((s) => s.name) : [];
        const registeredNames = index.status === "fulfilled" ? index.value.names : [];
        setNames([...new Set([...localNames, ...registeredNames])].sort());
        const warnings: string[] = [];
        if (installed.status === "rejected")
          warnings.push("Installed skill index unavailable; registered names remain inspectable.");
        if (index.status === "rejected")
          warnings.push("Registered skill index unavailable; installed names remain inspectable.");
        if (index.status === "fulfilled" && index.value.possibly_truncated)
          warnings.push("Registered skill index may be truncated to 500 names.");
        setError(warnings.join(" "));
      })
      .catch((e: unknown) => {
        if (alive) setError(e instanceof Error ? e.message : "Skill index failed");
      });
    return () => {
      alive = false;
    };
  }, []);
  async function action(run: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await run();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Skill management failed");
    } finally {
      setBusy(false);
    }
  }
  async function load() {
    const result = await http<SkillManagement>(`/managed-skills/${encodeURIComponent(name)}`);
    setView(result);
    if (!result.agents.some((a) => a.agent_key === agent)) setAgent(result.agents[0]?.agent_key ?? "");
  }
  const binding = view?.agents.find((a) => a.agent_key === agent);
  return (
    <div className="space-y-5">
      <h1 className="font-semibold text-2xl">Skill Versions & Bindings</h1>
      <p className="rounded border p-3 text-sm">
        Management-only version references, not runtime locking. Old visible/unversioned skills keep their behavior.
        Recording does not install or enable anything; revocation does not stop an already-running task or disable a
        legacy skill. No skill content is imported, shown, or executed. Safe digest registration currently requires a
        Unix server.
      </p>
      {error && (
        <p role="alert" className="text-destructive">
          {error}
        </p>
      )}
      <fieldset disabled={busy} className="space-y-4">
        <div className="flex flex-wrap gap-2">
          <NativeSelect
            aria-label="Installed skill"
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              setView(null);
              setVersion("");
            }}
          >
            <NativeSelectOption value="">Choose installed / registered skill</NativeSelectOption>
            {names.map((n) => (
              <NativeSelectOption key={n} value={n}>
                {n}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          <Button disabled={!name} onClick={() => action(load)}>
            Inspect current content / refresh
          </Button>
        </div>
        {view && (
          <>
            <p className="text-sm">{view.limits}</p>
            {view.current_error && (
              <p role="alert" className="text-destructive">
                Current digest unavailable: {view.current_error}. Registration/binding cannot silently succeed.
              </p>
            )}
            <Card>
              <CardHeader>
                <CardTitle>Record real content version</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3">
                <Label htmlFor="skill-label">Unique version label</Label>
                <Input id="skill-label" maxLength={128} value={label} onChange={(e) => setLabel(e.target.value)} />
                <Label htmlFor="skill-source">Source (credential-free HTTPS without query, or URN)</Label>
                <Input id="skill-source" maxLength={2048} value={source} onChange={(e) => setSource(e.target.value)} />
                <Label htmlFor="skill-license">License / provenance statement</Label>
                <Input
                  id="skill-license"
                  maxLength={256}
                  value={license}
                  onChange={(e) => setLicense(e.target.value)}
                />
                <Button
                  disabled={!label || !source || !license || !!view.current_error}
                  onClick={() =>
                    action(async () => {
                      await http(`/managed-skills/${encodeURIComponent(name)}`, {
                        method: "POST",
                        body: JSON.stringify(managedVersionPayload(label, source, license)),
                      });
                      await load();
                    })
                  }
                >
                  Hash current tree and record version
                </Button>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>Version records</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3">
                <Label htmlFor="revoke-reason">Revocation reason (management record only)</Label>
                <Input id="revoke-reason" value={reason} maxLength={2000} onChange={(e) => setReason(e.target.value)} />
                {view.versions.map((v) => (
                  <div key={v.id} className="space-y-1 rounded border p-3 text-sm">
                    <p>
                      {v.label} · revision {v.revision} · {view.checks[String(v.id)]}
                    </p>
                    <p className="break-all font-mono text-xs">{v.content_hash}</p>
                    <p>
                      {v.file_count} files / {v.total_bytes} bytes · {v.license}
                    </p>
                    <p className="break-all">{v.source_ref}</p>
                    <Button
                      variant="outline"
                      disabled={v.revoked || !reason.trim()}
                      onClick={() =>
                        action(async () => {
                          await http(`/managed-skills/${encodeURIComponent(name)}/versions/${v.id}/revoke`, {
                            method: "POST",
                            body: JSON.stringify({ revision: v.revision, reason }),
                          });
                          await load();
                        })
                      }
                    >
                      Revoke version record
                    </Button>
                  </div>
                ))}
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>Bind a version to an actual agent key</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3">
                <p className="text-sm">
                  Binding requires existing skill visibility. Use the existing Agent visibility settings separately;
                  this action never enables a skill.
                </p>
                <NativeSelect aria-label="Agent key" value={agent} onChange={(e) => setAgent(e.target.value)}>
                  {view.agents.map((a) => (
                    <NativeSelectOption key={a.agent_key} value={a.agent_key}>
                      {a.agent_key} · {a.visible ? "visible" : "not visible"}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
                <p className="text-sm">
                  Current binding: {binding?.version_id ?? "legacy/unversioned or explicitly unbound"}; revision{" "}
                  {binding?.revision ?? 0} · check:{" "}
                  {binding?.version_id ? (view.checks[String(binding.version_id)] ?? "not inspected") : "unversioned"}
                </p>
                <NativeSelect aria-label="Version to bind" value={version} onChange={(e) => setVersion(e.target.value)}>
                  <NativeSelectOption value="">Select matching version</NativeSelectOption>
                  {view.versions.map((v) => (
                    <NativeSelectOption
                      key={v.id}
                      value={String(v.id)}
                      disabled={view.checks[String(v.id)] !== "matches"}
                    >
                      {v.label} · {view.checks[String(v.id)]}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
                <div className="flex gap-2">
                  <Button
                    disabled={!binding?.visible || !version}
                    onClick={() =>
                      action(async () => {
                        await http(
                          `/managed-skills/${encodeURIComponent(name)}/bindings/${encodeURIComponent(agent)}`,
                          {
                            method: "PUT",
                            body: JSON.stringify(managedBindingPayload(Number(version), binding?.revision ?? 0)),
                          },
                        );
                        await load();
                      })
                    }
                  >
                    Recheck hash and bind management version
                  </Button>
                  <Button
                    variant="outline"
                    disabled={!binding?.version_id}
                    onClick={() =>
                      action(async () => {
                        await http(
                          `/managed-skills/${encodeURIComponent(name)}/bindings/${encodeURIComponent(agent)}`,
                          { method: "PUT", body: JSON.stringify(managedBindingPayload(0, binding?.revision ?? 0)) },
                        );
                        await load();
                      })
                    }
                  >
                    Clear version binding only
                  </Button>
                </div>
                <pre className="max-h-80 overflow-auto whitespace-pre-wrap text-xs">
                  {JSON.stringify(view.agents, null, 2)}
                </pre>
              </CardContent>
            </Card>
            <details>
              <summary>Latest management events</summary>
              <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-all text-xs">
                {JSON.stringify(view.events, null, 2)}
              </pre>
            </details>
          </>
        )}
      </fieldset>
    </div>
  );
}
