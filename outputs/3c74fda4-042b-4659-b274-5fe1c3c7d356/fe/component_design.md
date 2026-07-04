# Component Architecture — NEX-672

## 1. Atomic Design Hierarchy
- 변경 대상은 UI 컴포넌트가 아니라 `apps/web/proxy.ts`의 route canonicalization 계층이다.
- `ChatWindow`/`ChatFab`는 기존 dashboard layout에 유지되며 신규 컴포넌트 없음.

## 2. Component Inventory
| 컴포넌트 | 레벨 | 재사용도 | Props | 의존 |
|---|---|---|---|---|
| 없음 | N/A | N/A | N/A | N/A |

## 3. Composition Patterns
- 기존 `DashboardLayout` composition 유지.
- `/chat`는 독립 page가 아니라 `/{workspaceSlug}/issues`에서 dashboard overlay로 접근한다.

## 4. Component API Design
- 신규 public component API 없음.
- route proxy 입력: request pathname, `multica_logged_in`, `last_workspace_slug`.
- route proxy 출력: 307 redirect 또는 locale header pass-through.

## 5. State Boundary 정의
- Local/Global/Server state 변경 없음.
- URL state만 보정: `/usage`, `/dashboard`, `/chat` legacy 진입을 workspace-scoped route로 redirect.

## 6. Re-render 최적화 전략
- 렌더 트리 변경 없음.

## 7. Storybook Stories
- UI 컴포넌트 변경이 없어 Storybook 신규 작성 없음.
- route behavior는 `apps/web/proxy.test.ts`로 검증.
