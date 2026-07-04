# Performance Report — NEX-672

## 1. Core Web Vitals 목표
| Metric | Good | Target | 영향 |
|---|---|---|---|
| LCP | < 2.5s | < 1.5s | UI bundle 변경 없음 |
| INP | < 200ms | < 100ms | UI event handler 변경 없음 |
| CLS | < 0.1 | < 0.05 | layout 변경 없음 |
| TTFB | < 600ms | < 200ms | proxy redirect 1회 추가 |

## 2. Lighthouse 점수
- 신규 Lighthouse 측정 없음. 변경은 request redirect layer에 한정.

## 3. Bundle 분석
- JS bundle 변경 없음.
- `pnpm --dir apps/web build`: PASS.

## 4. Optimization 적용
- 404 page render 대신 307 redirect로 정상 workspace route에 진입한다.

## 5. Network
- authenticated `/usage`, `/dashboard`, `/chat` legacy path는 307 후 정상 200.

## 6. Runtime Performance
- React runtime 변경 없음.

## 7. Mobile Performance
- UI 변경 없음.
