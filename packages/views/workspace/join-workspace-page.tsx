"use client";

import { useState } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { api } from "@multica/core/api";

/** Accepts a join code/link only; workspace enumeration stays unavailable. */
export function JoinWorkspacePage() {
  const [joinCode, setJoinCode] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const submit = async () => {
    setError(null);
    try { await api.createWorkspaceJoinRequest(joinCode.trim()); setPending(true); }
    catch (e) { setError(e instanceof Error ? e.message : "Could not submit join request."); }
  };
  if (pending) return <main className="mx-auto flex min-h-screen max-w-md items-center px-6"><div><h1 className="text-xl font-semibold">Request pending</h1><p className="mt-2 text-sm text-muted-foreground">A workspace admin must approve your request before you can access it.</p></div></main>;
  return <main className="mx-auto flex min-h-screen max-w-md items-center px-6"><div className="w-full space-y-4"><div><h1 className="text-xl font-semibold">Join a workspace</h1><p className="mt-2 text-sm text-muted-foreground">Enter the join code or link given to you by an admin.</p></div><Input aria-label="Join code or link" value={joinCode} onChange={(e) => setJoinCode(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter" && joinCode.trim()) void submit(); }} /><Button className="w-full" disabled={!joinCode.trim()} onClick={() => void submit()}>Request to join</Button>{error && <p role="alert" className="text-sm text-destructive">{error}</p>}</div></main>;
}
