# NEX-606 QA/Validator PASS 코멘트 최소 증거 예시

## QA PASS 통과 예시

```markdown
## QA PASS

변경 대상: PR https://github.com/karlkim1004/multica/pull/123 또는 commit abc123
실행 테스트: go test ./internal/handler -run TestUpdateIssueDone -count=1 PASS
사용자 시나리오: done 전환 전 실패 run/증거 누락/완전 증거 시나리오별 PASS
라이브 증거: HTTP 409 차단 응답 body, HTTP 200 응답 본문, DOM/hash/log 중 실제 증거
실패 run scan: failed/cancelled/api_invalid_request/unsupported model 없음
결론: PASS
```

## Validator PASS 통과 예시

```markdown
## Validator PASS

| Surface | 변경 여부 | 영향 범위 | 검증 증거 | 회귀 위험 | 판정 |
|---|---|---|---|---|---|
| Issue status transition | changed | done/in_review side effect | test/log | High | PASS |
| Run failure scan | changed | failed/cancelled 검증 run | fixture/log | High | PASS |
| Comment PASS parser | changed | QA/validator 코멘트 | parser test | Medium | PASS |
| No-op handling | unchanged | 변경 없음 케이스 | fixture | High | PASS |
| Parent/child issue workflow | unchanged | child done wake | integration test | Medium | PASS |
| PR close intent workflow | unchanged | merged PR auto-close | webhook test | Medium | PASS |

결론: PASS
```

## 차단 예시

- `CONDITIONAL_PASS`는 최종 PASS로 인정하지 않는다.
- `confidence: 0.92` 같은 confidence 점수는 최종 PASS 근거로 인정하지 않는다.
- `라이브 증거: HTTP 200` 단독은 최종 PASS 근거로 인정하지 않는다.
- QA PASS에서 `변경 대상`, `실행 테스트`, `사용자 시나리오`, `라이브 증거`, `실패 run scan`, `결론` 중 하나라도 빠지면 차단한다.
- Validator PASS에서 `Surface`, `변경 여부`, `영향 범위`, `검증 증거`, `회귀 위험`, `판정` 및 6개 영향 범위 row가 빠지면 차단한다.
