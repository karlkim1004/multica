# Self-Test Report — NEX-672

## 1. Visual Verification
- UI 변경 없음. 8 breakpoint screenshot은 route-level proxy fix에는 해당 없음.
- 기존 QA가 `/issues` desktop/mobile 및 chat overlay screenshot을 이미 첨부했으며, 배포 후 동일 화면 재검증 필요.

## 2. Functional Test
- RED 확인: 기존 `proxy.ts`에서 `/usage`, `/dashboard`, `/chat` 모두 200 pass-through로 테스트 실패.
- GREEN 확인: `pnpm --dir apps/web exec vitest run proxy.test.ts` → PASS, 4/4.

## 3. Accessibility Test
- 신규 UI 없음. route가 정상 workspace shell로 진입하는지 확인.

## 4. Performance Test
- `pnpm --dir apps/web build` → PASS.

## 5. Cross-browser Test
- HTTP redirect layer라 browser API 차이 없음.

## 6. i18n Test
- 신규 문구 없음.

## 7. Build & Lint
- `pnpm --dir apps/web exec vitest run proxy.test.ts`: PASS.
- `pnpm --dir apps/web lint`: PASS, 기존 `apps/web/app/(auth)/login/page.tsx:116` warning 1건.
- `pnpm --dir apps/web typecheck`: PASS.
- `pnpm --dir apps/web build`: PASS.

## 8. HTTP Proof
로컬 production 서버 `next start -p 3100` 기준:

```text
GET /usage?range=7d with Cookie: multica_logged_in=1; last_workspace_slug=nexai
→ 307 Location: /nexai/usage?range=7d
→ curl -L final: 200 http://localhost:3100/nexai/usage?range=7d

GET /dashboard with Cookie
→ 307 Location: /nexai/dashboard
→ curl -L final: 200 http://localhost:3100/nexai/dashboard

GET /chat with Cookie
→ 307 Location: /nexai/issues
→ curl -L final: 200 http://localhost:3100/nexai/issues

GET /chat without Cookie
→ 307 Location: /login
```

## 9. PR
- PR: https://github.com/karlkim1004/multica/pull/21
