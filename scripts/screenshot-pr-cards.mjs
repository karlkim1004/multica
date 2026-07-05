// Standalone screenshot capture for the 6 PR-card demo issues. Logs in as
// dev@localhost via a local DB fixture + test JWT, then opens
// each /dev/issues/DEV-N and saves a clipped PNG focused on the right sidebar.
//
// Run: pnpm exec node scripts/screenshot-pr-cards.mjs
// Output: ./.screenshots/pr-card-DEV-{2..7}.png

import { chromium } from "@playwright/test";
import pg from "pg";
import { mkdirSync } from "node:fs";
import { createHmac } from "node:crypto";

const FRONTEND = process.env.FRONTEND_ORIGIN || "http://localhost:13101";
const DB = process.env.DATABASE_URL || "postgres://multica:multica@localhost:5432/multica_multica_101?sslmode=disable";
const JWT_SECRET = process.env.JWT_SECRET || "multica-dev-secret-change-in-production";
const EMAIL = "dev@localhost";
const SLUG = "dev";
const ISSUES = [2, 3, 4, 5, 6, 7];

async function loginAndGetToken() {
  const client = new pg.Client(DB);
  await client.connect();
  try {
    const row = await client.query(
      `
        INSERT INTO "user" (name, email)
        VALUES ($1, $2)
        ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
        RETURNING id, name, email
      `,
      ["dev", EMAIL],
    );
    return signJwt(row.rows[0]);
  } finally {
    await client.end();
  }
}

function base64url(value) {
  return Buffer.from(value).toString("base64url");
}

function signJwt(user) {
  const now = Math.floor(Date.now() / 1000);
  const header = base64url(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const payload = base64url(JSON.stringify({
    sub: user.id,
    email: user.email,
    name: user.name,
    iat: now,
    exp: now + 30 * 24 * 60 * 60,
  }));
  const input = `${header}.${payload}`;
  const signature = createHmac("sha256", JWT_SECRET).update(input).digest("base64url");
  return `${input}.${signature}`;
}

async function main() {
  mkdirSync(".screenshots", { recursive: true });
  const token = await loginAndGetToken();
  const browser = await chromium.launch({ headless: true });
  const ctx = await browser.newContext({ viewport: { width: 1600, height: 1200 } });
  const page = await ctx.newPage();

  // Set token before navigation so the renderer hits an authenticated state.
  await page.goto(`${FRONTEND}/login`);
  await page.evaluate((t) => localStorage.setItem("multica_token", t), token);

  for (const n of ISSUES) {
    const url = `${FRONTEND}/${SLUG}/issues/DEV-${n}`;
    console.log("→", url);
    await page.goto(url, { waitUntil: "domcontentloaded" });
    // Wait for the PR section header to appear so the card has rendered.
    const prSection = page.locator("text=Pull requests").first();
    await prSection.waitFor({ timeout: 15000 }).catch(() => undefined);

    // Close the floating chat widget — it overlays the right sidebar and
    // hides exactly the area we want to capture. The minimize button has
    // aria-label or just an icon; brute-force any chat close/minimize.
    const closeChat = page.locator('[aria-label="Minimize" i], [aria-label="Close" i], button[title="Minimize" i]').first();
    if (await closeChat.count()) {
      await closeChat.click({ trial: false }).catch(() => undefined);
    }
    // Fallback: click on the chat header to collapse it.
    await page.locator("text=New chat").first().click({ timeout: 1000 }).catch(() => undefined);

    await page.waitForTimeout(400);
    // Scroll the PR section into view.
    await prSection.scrollIntoViewIfNeeded().catch(() => undefined);
    await page.waitForTimeout(300);
    const out = `.screenshots/pr-card-DEV-${n}.png`;
    await page.screenshot({ path: out, fullPage: false });
    console.log("   saved", out);
  }
  await browser.close();
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
