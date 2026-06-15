import { NextResponse } from "next/server";
import { readFile } from "node:fs/promises";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const DEFAULT_TOKEN_SNAPSHOT_PATH = "/home/iaas/nexai/state/token_snapshot.json";

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

async function readTokenSnapshot() {
	const snapshotPath = process.env.NEXAI_TOKEN_SNAPSHOT_PATH ?? DEFAULT_TOKEN_SNAPSHOT_PATH;
	const raw = await readFile(snapshotPath, "utf8");
	return JSON.parse(raw) as TokenSnapshot;
}

export async function GET() {
	let snapshot: TokenSnapshot = {};
	try {
		snapshot = await readTokenSnapshot();
	} catch {
		snapshot = {};
	}

	return NextResponse.json({
		five_hour_pct: numberFrom(snapshot, ["usage_5h_pct", "five_hour_pct", "five_hour_utilization"]),
		seven_day_pct: numberFrom(snapshot, ["usage_7d_pct", "seven_day_pct", "seven_day_utilization"]),
		sonnet_pct: numberFrom(snapshot, ["sonnet_pct", "seven_day_sonnet_utilization"]),
		gpt_five_hour_pct: numberFrom(snapshot, ["gpt_five_hour_pct"]),
		gpt_seven_day_pct: numberFrom(snapshot, ["gpt_seven_day_pct"]),
		weekly_progress_pct: numberFrom(snapshot, ["weekly_progress_pct"]),
		updated_at: stringFrom(snapshot, ["updated_at", "timestamp"]),
	});
}
