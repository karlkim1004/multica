import { NextResponse, type NextRequest } from "next/server";

const LEGACY_HOST = "multica.nexai.co.kr";
const MARKETING_URL = "https://nexai.co.kr";
const LOGGED_IN_COOKIE = "multica_logged_in";

function isExemptPath(pathname: string): boolean {
  return (
    pathname.startsWith("/api/") ||
    pathname.startsWith("/auth/callback") ||
    pathname.startsWith("/_next/") ||
    pathname === "/favicon.ico" ||
    pathname === "/robots.txt" ||
    pathname === "/sitemap.xml"
  );
}

function wantsDocument(req: NextRequest): boolean {
  const accept = req.headers.get("accept") ?? "";
  return accept.includes("text/html") || accept.includes("*/*");
}

export function middleware(req: NextRequest) {
  const host = req.headers.get("host")?.split(":")[0];
  if (host !== LEGACY_HOST) return NextResponse.next();
  if (req.cookies.get(LOGGED_IN_COOKIE)?.value) return NextResponse.next();
  if (isExemptPath(req.nextUrl.pathname)) return NextResponse.next();
  if (!wantsDocument(req)) return NextResponse.next();

  return NextResponse.redirect(MARKETING_URL, 307);
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
