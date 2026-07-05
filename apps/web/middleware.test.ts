import { describe, expect, it } from "vitest";
import { NextRequest } from "next/server";
import { proxy } from "./proxy";

function req(path: string, init: { host?: string; cookie?: string; accept?: string } = {}) {
  return new NextRequest(`https://${init.host ?? "multica.nexai.co.kr"}${path}`, {
    headers: {
      host: init.host ?? "multica.nexai.co.kr",
      accept: init.accept ?? "text/html",
      ...(init.cookie ? { cookie: init.cookie } : {}),
    },
  });
}

describe("legacy host middleware", () => {
  it("redirects unauthenticated document requests to nexai.co.kr", () => {
    const res = proxy(req("/"));

    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe("https://nexai.co.kr/");
  });

  it("allows OAuth callback and authenticated browser sessions", () => {
    expect(proxy(req("/auth/callback?code=ok")).headers.get("location")).toBeNull();
    expect(proxy(req("/", { cookie: "multica_logged_in=1" })).headers.get("location")).toBeNull();
  });
});
