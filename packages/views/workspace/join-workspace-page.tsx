"use client";

import { useState } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { api } from "@multica/core/api";
import { useT } from "../i18n";

/** Accepts a join code/link only; workspace enumeration stays unavailable. */
export function JoinWorkspacePage() {
  const { t } = useT("settings");
  const [joinCode, setJoinCode] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const submit = async () => {
    setError(null);
    try { await api.createWorkspaceJoinRequest(joinCode.trim()); setPending(true); }
    catch (e) { setError(e instanceof Error ? e.message : t(($) => $.members.join_request_submit_failed)); }
  };
  if (pending) return <main className="mx-auto flex min-h-screen max-w-md items-center px-6"><div><h1 className="text-xl font-semibold">{t(($) => $.members.join_request_pending_title)}</h1><p className="mt-2 text-sm text-muted-foreground">{t(($) => $.members.join_request_pending_description)}</p></div></main>;
  return <main className="mx-auto flex min-h-screen max-w-md items-center px-6"><div className="w-full space-y-4"><div><h1 className="text-xl font-semibold">{t(($) => $.members.join_request_title)}</h1><p className="mt-2 text-sm text-muted-foreground">{t(($) => $.members.join_request_description)}</p></div><Input aria-label={t(($) => $.members.join_code_label)} value={joinCode} onChange={(e) => setJoinCode(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter" && joinCode.trim()) void submit(); }} /><Button className="w-full" disabled={!joinCode.trim()} onClick={() => void submit()}>{t(($) => $.members.join_request_submit)}</Button>{error && <p role="alert" className="text-sm text-destructive">{error}</p>}</div></main>;
}
