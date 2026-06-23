/**
 * Format an elapsed seconds value as `Ns` (under a minute) or `Nm Ms`
 * (over a minute). Drops the seconds part when the remainder is 0 to
 * keep round-minute readings short ("3m" rather than "3m 0s"). Shared
 * by the live StatusPill timer and the persistent assistant-message
 * timing line — keeping them in lockstep avoids visible drift between
 * "Working · 38s" mid-flight and a final "Replied in 39s" caption.
 */
export function formatElapsedSecs(secs: number): string {
  if (secs < 60) return `${secs}s`;
  const m = Math.floor(secs / 60);
  const s = secs % 60;
  return s ? `${m}m ${s}s` : `${m}m`;
}

/** Convenience: same formatting, but the input is milliseconds (server-stored elapsed_ms). */
export function formatElapsedMs(ms: number): string {
  return formatElapsedSecs(Math.max(0, Math.round(ms / 1000)));
}

const CHAT_TIMESTAMP_FORMATTER = new Intl.DateTimeFormat("en-CA", {
  timeZone: "Asia/Seoul",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
});

/**
 * Absolute chat message timestamp used by the live chat transcript.
 * KST is explicit so the value is stable regardless of the browser locale.
 */
export function formatChatTimestamp(
  timestamp: string | Date | null | undefined,
  fallback: Date = new Date(),
): string {
  const candidate =
    timestamp instanceof Date ? timestamp : timestamp ? new Date(timestamp) : fallback;
  const date = Number.isNaN(candidate.getTime()) ? fallback : candidate;
  const parts = Object.fromEntries(
    CHAT_TIMESTAMP_FORMATTER.formatToParts(date).map((part) => [part.type, part.value]),
  );
  return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}:${parts.second}`;
}
