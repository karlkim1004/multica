import { describe, expect, it } from "vitest";
import { NextRequest } from "next/server";
import { proxy } from "./proxy";

function request(pathname: string, cookie = "") {
  return new NextRequest(`https://aido.nexai.co.kr${pathname}`, {
    headers: cookie ? { cookie } : undefined,
  });
}

describe("proxy legacy workspace routes", () => {
  it.each(["/dashboard", "/usage", "/chat"])(
    "redirects logged-out %s requests to login instead of 404",
    (pathname) => {
      const res = proxy(request(pathname));

      expect(res.status).toBe(307);
      expect(res.headers.get("location")).toBe(
        `https://aido.nexai.co.kr/login`,
      );
    },
  );

  it.each([
    ["/dashboard", "/nexai/dashboard"],
    ["/usage", "/nexai/usage"],
    ["/chat", "/nexai/issues"],
  ])("redirects logged-in %s requests to %s", (pathname, target) => {
    const res = proxy(
      request(pathname, "multica_logged_in=1; last_workspace_slug=nexai"),
    );

    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe(
      `https://aido.nexai.co.kr${target}`,
    );
  });
});
