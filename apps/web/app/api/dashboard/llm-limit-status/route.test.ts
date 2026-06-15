import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";

let snapshotDir: string | undefined;

afterEach(async () => {
	delete process.env.NEXAI_TOKEN_SNAPSHOT_PATH;
	if (snapshotDir) {
		await rm(snapshotDir, { recursive: true, force: true });
		snapshotDir = undefined;
	}
});

describe("GET /api/dashboard/llm-limit-status", () => {
	it("returns the current runtime token snapshot instead of a build-time constant", async () => {
		snapshotDir = await mkdtemp(path.join(tmpdir(), "llm-limit-status-"));
		const snapshotPath = path.join(snapshotDir, "token_snapshot.json");
		process.env.NEXAI_TOKEN_SNAPSHOT_PATH = snapshotPath;

		await writeFile(
			snapshotPath,
			JSON.stringify({
				five_hour_utilization: 3,
				seven_day_utilization: 57,
				sonnet_pct: 4,
				gpt_five_hour_pct: 6,
				gpt_seven_day_pct: 8,
				weekly_progress_pct: 10,
				updated_at: "2026-06-16T00:00:00.000Z",
			}),
		);

		const { GET } = await import("./route");
		const response = await GET();
		const data = await response.json();

		expect(data).toMatchObject({
			five_hour_pct: 3,
			seven_day_pct: 57,
			sonnet_pct: 4,
			gpt_five_hour_pct: 6,
			gpt_seven_day_pct: 8,
			weekly_progress_pct: 10,
			updated_at: "2026-06-16T00:00:00.000Z",
		});
	});
});
