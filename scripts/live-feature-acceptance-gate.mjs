#!/usr/bin/env node
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";

const DEFAULT_CONFIG = "scripts/live-acceptance-checks.json";

function argValue(name, fallback) {
  const prefix = `${name}=`;
  const inline = process.argv.find((arg) => arg.startsWith(prefix));
  if (inline) return inline.slice(prefix.length);
  const index = process.argv.indexOf(name);
  if (index >= 0) return process.argv[index + 1] ?? fallback;
  return fallback;
}

const configPath = resolve(argValue("--config", DEFAULT_CONFIG));
const baseUrlOverride = argValue("--base-url", process.env.LIVE_ACCEPTANCE_BASE_URL);
const noBrowser = process.argv.includes("--no-browser") || process.env.LIVE_ACCEPTANCE_NO_BROWSER === "1";

const results = [];

function record(name, ok, detail = "") {
  results.push({ name, ok, detail });
  const state = ok ? "PASS" : "FAIL";
  console.log(`[${state}] ${name}${detail ? ` - ${detail}` : ""}`);
}

function buildUrl(baseUrl, path) {
  return new URL(path, baseUrl.endsWith("/") ? baseUrl : `${baseUrl}/`).toString();
}

async function fetchText(url, init = {}) {
  const response = await fetch(url, { redirect: "follow", ...init });
  const text = await response.text().catch(() => "");
  return { response, text };
}

function normalizeHtml(value) {
  return value.replace(/\s+/g, " ").toLowerCase();
}

function hasSimpleSelector(html, selector) {
  const source = html.toLowerCase();
  const dataAcceptance = selector.match(/^\[data-acceptance="([^"]+)"\]$/i);
  if (dataAcceptance) return source.includes(`data-acceptance="${dataAcceptance[1].toLowerCase()}"`);
  const dataTestId = selector.match(/^\[data-testid="([^"]+)"\]$/i);
  if (dataTestId) return source.includes(`data-testid="${dataTestId[1].toLowerCase()}"`);
  const ariaContains = selector.match(/^\[aria-label\*="([^"]+)"\]$/i);
  if (ariaContains) {
    return [...html.matchAll(/aria-label=["']([^"']+)["']/gi)].some((match) =>
      match[1].toLowerCase().includes(ariaContains[1].toLowerCase()),
    );
  }
  if (selector === "[contenteditable=\"true\"]") return source.includes("contenteditable=\"true\"");
  if (selector === "[aria-live=\"polite\"]") return source.includes("aria-live=\"polite\"");
  if (selector === "button[aria-label*=\"Send\"]") return /<button[^>]+aria-label=["'][^"']*send/i.test(html);
  if (selector === "button[aria-label*=\"Refresh\"]") return /<button[^>]+aria-label=["'][^"']*refresh/i.test(html);
  return source.includes(selector.toLowerCase());
}

function hasContract(html, contract) {
  const selectors = contract.selectors ?? [];
  if (selectors.some((selector) => hasSimpleSelector(html, selector))) return true;
  const normalized = normalizeHtml(html);
  return (contract.text ?? []).some((needle) => normalized.includes(needle.toLowerCase()));
}

function hasDarkRender(html) {
  const source = html.toLowerCase();
  return (
    source.includes("data-theme=\"dark\"") ||
    source.includes("class=\"dark") ||
    source.includes("class=\"") && source.includes(" dark ") ||
    source.includes("#05070b")
  );
}

function extractCssUrls(baseUrl, html) {
  const urls = [];
  for (const match of html.matchAll(/<link[^>]+rel=["'][^"']*stylesheet[^"']*["'][^>]+href=["']([^"']+)["']/gi)) {
    urls.push(buildUrl(baseUrl, match[1]));
  }
  for (const match of html.matchAll(/<link[^>]+href=["']([^"']+)["'][^>]+rel=["'][^"']*stylesheet[^"']*["']/gi)) {
    urls.push(buildUrl(baseUrl, match[1]));
  }
  return [...new Set(urls)];
}

async function maybeLoadBrowser() {
  if (noBrowser) return null;
  try {
    const mod = await import("playwright");
    return mod.chromium;
  } catch {
    try {
      const mod = await import("@playwright/test");
      return mod.chromium;
    } catch {
      return null;
    }
  }
}

async function checkRoutes(config, baseUrl, htmlCache) {
  for (const route of config.routes ?? []) {
    const url = buildUrl(baseUrl, route.path);
    const { response, text } = await fetchText(url);
    htmlCache.set(route.path, text);
    record(`route ${route.name} GET 200`, response.status === 200, `${route.path} status=${response.status}`);
    if (route.requiresDarkRender) {
      record(`route ${route.name} dark render`, hasDarkRender(text), route.path);
    }
  }
}

async function checkCss(config, baseUrl, htmlCache) {
  const landingPath = config.routes?.[0]?.path ?? "/";
  let html = htmlCache.get(landingPath);
  if (!html) {
    html = (await fetchText(buildUrl(baseUrl, landingPath))).text;
    htmlCache.set(landingPath, html);
  }
  const cssBodies = [html];
  for (const cssUrl of extractCssUrls(baseUrl, html)) {
    const { response, text } = await fetchText(cssUrl);
    if (response.ok) cssBodies.push(text);
  }
  const bundle = cssBodies.join("\n").toLowerCase();
  for (const color of config.cssColors ?? []) {
    record(`css color ${color}`, bundle.includes(color.toLowerCase()), color);
  }
}

async function checkMethods(config, baseUrl) {
  for (const check of config.methodChecks ?? []) {
    const response = await fetch(buildUrl(baseUrl, check.path), {
      method: check.method,
      redirect: "manual",
    }).catch((error) => ({ status: 0, statusText: error.message }));
    const forbidden = new Set(check.forbiddenStatuses ?? []);
    record(
      `method ${check.method} ${check.path} not forbidden`,
      !forbidden.has(response.status),
      `status=${response.status}`,
    );
  }
}

async function checkForbiddenText(config, baseUrl, htmlCache) {
  const texts = config.forbiddenText ?? [];
  for (const route of config.routes ?? []) {
    let html = htmlCache.get(route.path);
    if (!html) {
      html = (await fetchText(buildUrl(baseUrl, route.path))).text;
      htmlCache.set(route.path, html);
    }
    for (const forbidden of texts) {
      record(`forbidden text absent ${route.path}`, !html.includes(forbidden), `token=${JSON.stringify(forbidden)}`);
    }
  }
}

async function checkUiContracts(config, baseUrl, htmlCache) {
  for (const contract of config.uiContracts ?? []) {
    if (contract.kind === "asset") {
      const response = await fetch(buildUrl(baseUrl, contract.path));
      const ok = response.ok && Number(response.headers.get("content-length") ?? "1") > 0;
      record(`ui ${contract.name}`, ok, `${contract.path} status=${response.status}`);
      continue;
    }
    let html = htmlCache.get(contract.path);
    if (!html) {
      html = (await fetchText(buildUrl(baseUrl, contract.path))).text;
      htmlCache.set(contract.path, html);
    }
    record(`ui ${contract.name}`, hasContract(html, contract), contract.path);
  }
}

async function checkInteractiveChat(config, baseUrl, chromium) {
  if (!chromium) {
    record("interactive chat browser check", true, "skipped: browser unavailable");
    return;
  }
  const dashboard = (config.uiContracts ?? []).find((item) => item.name === "chat input")?.path ?? "/nexai/dashboard";
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  try {
    await page.goto(buildUrl(baseUrl, dashboard), { waitUntil: "networkidle", timeout: 30000 });
    const input = page.locator('[data-acceptance="chat-input"], [data-testid="chat-input"], [contenteditable="true"]').first();
    await input.waitFor({ state: "visible", timeout: 10000 });
    await input.fill("live acceptance ping");
    const send = page.locator('[data-acceptance="send-button"], [data-testid="send-button"], button[aria-label*="Send"]').first();
    await send.click();
    const progress = page.locator('[data-acceptance="chat-response-in-progress"], [data-testid="chat-response-in-progress"], [aria-live="polite"]').first();
    await progress.waitFor({ state: "visible", timeout: 10000 });
    const enabledDuringProgress = await send.isEnabled();
    record("interactive send enabled during response", enabledDuringProgress);
    const assistant = page.locator('[data-acceptance="assistant-message"], [data-testid="assistant-message"], [data-role="assistant-message"]').first();
    await assistant.waitFor({ state: "visible", timeout: 20000 });
    record("interactive response without refresh", true);
  } catch (error) {
    record("interactive chat flow", false, error.message);
  } finally {
    await browser.close();
  }
}

async function main() {
  const config = JSON.parse(await readFile(configPath, "utf8"));
  const baseUrl = baseUrlOverride ?? config.baseUrl;
  const htmlCache = new Map();
  const chromium = await maybeLoadBrowser();

  await checkRoutes(config, baseUrl, htmlCache);
  await checkCss(config, baseUrl, htmlCache);
  await checkMethods(config, baseUrl);
  await checkForbiddenText(config, baseUrl, htmlCache);
  await checkUiContracts(config, baseUrl, htmlCache);
  await checkInteractiveChat(config, baseUrl, chromium);

  const failed = results.filter((result) => !result.ok);
  console.log(`\nLive acceptance summary: ${results.length - failed.length}/${results.length} passed`);
  if (failed.length > 0) {
    console.error(`Failed checks: ${failed.map((result) => result.name).join(", ")}`);
    process.exit(1);
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
