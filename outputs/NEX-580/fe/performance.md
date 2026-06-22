# Performance Report — NEX-580

## Build
- `pnpm --filter @multica/web build`: PASS.

## Runtime Cost
- No new dependency.
- Web Speech APIs are browser-native.
- TTS effect scans only current message array and speaks the latest assistant message once per id.

## Bundle
- Bundle analyzer not run. Change adds lucide icons already in dependency graph.
