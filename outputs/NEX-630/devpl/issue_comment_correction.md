[정정] 직전 댓글은 셸 백틱 치환으로 기술 표현 일부가 누락되어, 아래 내용으로 정정합니다.

구현/PR 생성 완료했습니다.

## 변경 사항
- PR: https://github.com/karlkim1004/multica/pull/15
- 서버 `done` Evidence Gate에 다음 실행 주체 호출 검사를 추가했습니다.
- 최종 PASS 코멘트가 `다음 담당자`, `다음봇`, `다음 1수`, `handoff` 계열 문구를 포함하면 `mention://agent/<UUID>` 또는 같은 update 요청의 agent assignee 변경이 없을 때 `409 Conflict`로 차단합니다.
- 단일 `UpdateIssue`, batch update, GitHub close-intent auto-done 경로가 같은 게이트를 탑니다.
- GPT/Codex vs Claude 컨텍스트 주입 경로 표를 `outputs/NEX-630/devpl/context_injection_matrix.md`에 남겼습니다.

## 검증
- 통과: `git diff --check`
- 차단: `go test ./internal/handler -run 'TestUpdateIssueDone(RejectsFinalPassHandoffWithoutAgentMention|AllowsFinalPassHandoffWithAgentMention)' -count=1` 실행 시 현재 런타임에 `go`가 없어 `/bin/bash: line 1: go: command not found`로 중단됐습니다.
- 따라서 이 이슈는 제가 `done` 처리하지 않습니다. 서버 Go toolchain이 있는 런타임에서 테스트와 실제 CLI/API `409` 차단 실측이 필요합니다.

[@쵸단(qa)](mention://agent/e21f5e4a-4c4b-417d-aa50-b81fe4d4ad6f) PR #15 기준으로 서버 테스트와 실제 `done` 전환 `409/200` 경로 검증을 이어받아 주세요.
