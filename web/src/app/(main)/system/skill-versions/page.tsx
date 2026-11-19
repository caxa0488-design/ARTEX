"use client";
import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { api, http } from "@/lib/api";
import {
  managedBindingPayload,
  managedVersionPayload,
  type SkillManagement,
  type SkillRuntimeMetadata,
  skillRuntimeEventMeaning,
} from "@/lib/phase2-management";
export default function SkillVersionsPage() {
  const [runtimeTask, setRuntimeTask] = useState("");
  const [runtimeView, setRuntimeView] = useState<SkillRuntimeMetadata | null>(null);
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
        Managed runtime pins support one SKILL.md (128 KiB), without resource files or MCPs. Unmanaged skills keep their
        behavior. Registration never enables a skill. Managed assembly and Skill loads check content/binding/revocation;
        task/chat pins persist fixed instruction bytes. Revocation cannot retract text already delivered to a model.
        Safe capture requires Unix.
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
                <Label htmlFor="revoke-reason">Revocation reason (blocks future managed assemblies/loads)</Label>
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
                  Binding requires existing visibility and a supported manifest. Use a new task after changing versions.
                  Clearing the binding leaves visibility for new legacy runs, but existing pinned tasks stop rather than
                  silently fall back. This action never enables a skill.
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
                    Verify and bind runtime version
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

        <Card>
          <CardHeader>
            <CardTitle>Actual task skill versions</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <p className="text-sm">
              Read-only pins and separate assembled/loaded events. Loaded means instructions returned, not tests
              executed. Bodies and credentials are never returned here.
            </p>
            <Input
              aria-label="Runtime task ID"
              value={runtimeTask}
              onChange={(e) => {
                setRuntimeTask(e.target.value);
                setRuntimeView(null);
              }}
              placeholder="Task ID"
            />
            <Button
              variant="outline"
              disabled={!/^[1-9][0-9]*$/.test(runtimeTask)}
              onClick={() =>
                action(async () =>
                  setRuntimeView(
                    await http<SkillRuntimeMetadata>(
                      `/operator/tasks/${encodeURIComponent(runtimeTask)}/skill-runtime`,
                    ),
                  ),
                )
              }
            >
              Load actual versions / events
            </Button>
            {runtimeView && (
              <>
                <p className="text-sm">
                  {runtimeView.scope} · {runtimeView.limits}
                </p>
                <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-all text-xs">
                  {JSON.stringify(
                    {
                      ...runtimeView,
                      events: runtimeView.events.map((event) => ({
                        ...event,
                        meaning: skillRuntimeEventMeaning(event.stage),
                      })),
                    },
                    null,
                    2,
                  )}
                </pre>
              </>
            )}
          </CardContent>
        </Card>
      </fieldset>
    </div>
  );
}
