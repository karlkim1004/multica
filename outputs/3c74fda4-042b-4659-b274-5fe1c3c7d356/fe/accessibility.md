# Accessibility Compliance — NEX-672

## 1. WCAG 2.2 AA 매트릭스
- UI markup 변경 없음.
- 404로 막히던 route를 기존 접근성 검증 대상인 workspace dashboard shell로 redirect한다.

## 2. ARIA Patterns
- 변경 없음.

## 3. Keyboard Navigation
- 변경 없음.
- `/chat`는 기존 issues page에 mount된 `ChatFab`/`ChatWindow` 키보드 동작을 사용한다.

## 4. Screen Reader Test
- 신규 UI 없음. 기존 QA의 authenticated dashboard/chat 화면 검증 범위로 재검증 필요.

## 5. 자동화 검증
- `pnpm --dir apps/web exec vitest run proxy.test.ts`: PASS.
- axe/Lighthouse는 route-level proxy 변경이라 로컬에서 신규 측정하지 않음.

## 6. 색맹/저시력 대응
- 변경 없음.

## 7. 모션 감소
- 변경 없음.
