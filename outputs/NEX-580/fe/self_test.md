# Self-Test Report — NEX-580

## TDD Cases
- STT mic button starts `SpeechRecognition` with `ko-KR`.
- Final transcript fills chat input draft.
- Unsupported STT renders disabled graceful UI.
- TTS ON speaks newly received assistant message with `ko-KR`.
- TTS OFF does not call `speechSynthesis.speak()`.

## Commands
- `git fetch origin`: PASS.
- Base: `origin/agent/fe/nex-575-restore-nex562-kst-copy` at `a1112581`.
- `pnpm --dir packages/views exec vitest run chat/components/chat-input.test.tsx chat/components/chat-message-list.test.tsx`: PASS, 17/17.
- `pnpm --dir packages/views exec vitest run chat/components/chat-input.test.tsx chat/components/chat-message-list.test.tsx chat/lib/format.test.ts chat/lib/copy-text.test.ts`: PASS, 30/30.
- `pnpm --filter @multica/views typecheck`: PASS.
- `pnpm --filter @multica/views lint`: PASS with existing warnings, 0 errors.
- `pnpm --filter @multica/web build`: PASS.

## Regression Checklist
- NEX-575 base branch preserved.
- Header refresh selector retained: `chat-messages-manual-refresh`.
- Copy selector retained: `chat-message-copy-button`.
- Timestamp selector retained: `chat-message-timestamp`.
- Token badge component not changed.
- Dark theme/global styles not changed.
- Pause/stop path not changed.

## Not Completed
- Authenticated live chat screenshot and 8-breakpoint screenshots were not captured in this runtime because no authenticated production/local session was available.
