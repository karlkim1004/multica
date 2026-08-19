# NEX-630 검증 로그

## 추가한 테스트 케이스

1. `TestUpdateIssueDoneRejectsFinalPassHandoffWithoutAgentMention`
   - 조건: QA PASS 템플릿이 `다음 담당자`와 `다음 1수`를 명시하지만 `mention://agent/<UUID>`가 없음.
   - 기대: `PUT /api/issues/{id}` status=`done` 전환이 `409 Conflict`로 차단되고 DB status는 `in_progress` 유지.

2. `TestUpdateIssueDoneAllowsFinalPassHandoffWithAgentMention`
   - 조건: 같은 QA PASS 템플릿에 `mention://agent/<UUID>`가 포함됨.
   - 기대: `done` 전환 허용.

## 실행 시도

```text
명령: go test ./internal/handler -run 'TestUpdateIssueDone(RejectsFinalPassHandoffWithoutAgentMention|AllowsFinalPassHandoffWithAgentMention)' -count=1
작업 디렉터리: server
결과: /bin/bash: line 1: go: command not found
```

현재 런타임 셸에는 Go toolchain이 없어 서버 테스트를 실행하지 못했다. `which go`, `/usr/local/go/bin/go`, `/home/iaas/sdk/go/bin/go`, `/home/iaas/.local/bin/go` 확인 결과 모두 없음.

## 정적 확인

- `checkDoneEvidenceGate` 호출부 전체 확인: `rg -n "checkDoneEvidenceGate\(" server/internal/handler`
- 연결된 경로:
  - CLI/API 단일 update: `server/internal/handler/issue.go`
  - CLI/API batch update: `server/internal/handler/issue.go`
  - GitHub close intent auto done: `server/internal/handler/github.go`
- 차단 메시지: `Evidence Gate 차단: 다음 실행 주체가 필요한 PASS/done handoff인데 assignee 변경 또는 mention://agent/<UUID> 호출이 없습니다.`
