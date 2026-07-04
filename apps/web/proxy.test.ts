import { describe, expect, it } from "vitest";
import { NextRequest } from "next/server";
import { proxy } from "./proxy";

function request(path: string, cookie = "multica_logged_in=1; last_workspace_slug=nexai") {
  return new NextRequest(new URL(`https://aido.nexai.co.kr${path}`), {
    headers: cookie ? { cookie } : undefined,
  });
}

describe("proxy legacy workspace route redirects", () => {
  it("redirects top-level usage to the last workspace usage page", () => {
    const res = proxy(request("/usage?range=7d"));

    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe("https://aido.nexai.co.kr/nexai/usage?range=7d");
  });

  it("redirects top-level dashboard to the last workspace dashboard page", () => {
    const res = proxy(request("/dashboard"));

    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe("https://aido.nexai.co.kr/nexai/dashboard");
  });

  it("redirects top-level chat to the workspace issues page where chat is mounted", () => {
    const res = proxy(request("/chat"));

    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe("https://aido.nexai.co.kr/nexai/issues");
  });

  it("protects top-level chat when no session cookie is present", () => {
    const res = proxy(request("/chat", ""));

    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe("https://aido.nexai.co.kr/login");
  });
});
