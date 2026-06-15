import { NextResponse } from "next/server";
import { readFile } from "node:fs/promises";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const DEFAULT_TOKEN_SNAPSHOT_PATH = "/home/iaas/nexai/state/token_snapshot.json";
const DEFAULT_CODEX_STATUS_SNAPSHOT_PATH = "/home/iaas/nexai/state/codex_status_snapshot.json";

type TokenSnapshot = Record<string, unknown>;

function numberFrom(snapshot: TokenSnapshot, keys: string[], fallback = 0) {
	for (const key of keys) {
		const value = snapshot[key];
		if (typeof value === "number" && Number.isFinite(value)) {
			return Math.max(0, Math.min(100, value));
		}
		if (typeof value === "string") {
			const parsed = Number.parseFloat(value);
			if (Number.isFinite(parsed)) {
				return Math.max(0, Math.min(100, parsed));
			}
		}
	}
	return fallback;
}

function stringFrom(snapshot: TokenSnapshot, keys: string[]) {
	for (const key of keys) {
		const value = snapshot[key];
		if (typeof value === "string" && value.length > 0) {
			return value;
		}
	}
	return new Date().toISOString();
}

function usageFromRemaining(snapshot: TokenSnapshot, keys: string[], fallback = 0) {
	const remaining = numberFrom(snapshot, keys, Number.NaN);
	if (!Number.isFinite(remaining)) return fallback;
	return Math.max(0, Math.min(100, 100 - remaining));
}

async function readJsonSnapshot(pathname: string) {
	const raw = await readFile(pathname, "utf8");
	return JSON.parse(raw) as TokenSnapshot;
}

export async function GET() {
	let snapshot: TokenSnapshot = {};
	let codexStatus: TokenSnapshot = {};
	try {
		snapshot = await readJsonSnapshot(process.env.NEXAI_TOKEN_SNAPSHOT_PATH ?? DEFAULT_TOKEN_SNAPSHOT_PATH);
	} catch {
		snapshot = {};
	}
	try {
		codexStatus = await readJsonSnapshot(process.env.NEXAI_CODEX_STATUS_SNAPSHOT_PATH ?? DEFAULT_CODEX_STATUS_SNAPSHOT_PATH);
	} catch {
		codexStatus = {};
	}

	return NextResponse.json({
		five_hour_pct: numberFrom(snapshot, ["usage_5h_pct", "five_hour_pct", "five_hour_utilization"]),
		seven_day_pct: numberFrom(snapshot, ["usage_7d_pct", "seven_day_pct", "seven_day_utilization"]),
		sonnet_pct: numberFrom(snapshot, ["sonnet_pct", "seven_day_sonnet_utilization"]),
		gpt_five_hour_pct: numberFrom(
			snapshot,
			["gpt_five_hour_pct", "gpt_five_used_pct"],
			numberFrom(codexStatus, ["five_hour_used_pct"], usageFromRemaining(codexStatus, ["five_hour_left_pct"])),
		),
		gpt_seven_day_pct: numberFrom(
			snapshot,
			["gpt_seven_day_pct", "gpt_seven_used_pct"],
			numberFrom(codexStatus, ["seven_day_used_pct"], usageFromRemaining(codexStatus, ["seven_day_left_pct"])),
		),
		weekly_progress_pct: numberFrom(snapshot, ["weekly_progress_pct"]),
		updated_at: stringFrom(snapshot, ["updated_at", "timestamp"]),
	});
}
