# ADR-001: PASS/done handoff 하드게이트

## Status
Accepted

## Date
2026-06-26

## Context
- 최종 PASS 또는 `done` 전환 후 다음 봇 호출이 누락되면 작업 체인이 멈춘다.
- 기존 instructions/AGENTS.md 규칙은 런타임이 지키지 않으면 서버가 막지 못했다.
- 이미 `done` 전환은 Evidence Gate를 통과하므로, 같은 서버 게이트에 handoff 검사를 추가하는 것이 가장 작은 변경이다.

## Decision
- `done` 전환 Evidence Gate에서 최신 최종 PASS 코멘트가 `다음 담당자`, `다음봇`, `다음 1수`, `handoff` 계열 문구를 포함하면 다음 실행 주체가 필요하다고 판단한다.
- 이때 `mention://agent/<UUID>`가 코멘트에 없고, 같은 update 요청에서 assignee가 다른 agent로 변경되지 않으면 `409 Conflict`로 차단한다.
- GitHub close intent auto-done 경로도 같은 Evidence Gate를 호출하되 요청 assignee 변경이 없으므로, handoff 필요 PASS에는 agent mention이 있어야 통과한다.

## Considered Alternatives

### Option 1: Evidence Gate에 handoff 검사 추가
- 설명: 기존 `checkDoneEvidenceGate`에 최종 PASS handoff 조건을 넣는다.
- Pros: 단일/배치/API/GitHub done 경로를 한 지점에서 제어, 변경 작음, 기존 회귀 테스트 구조 재사용.
- Cons: “다음 실행 주체 필요” 판정은 PASS 코멘트 문구 기반이라 표현 누락 시 감지하지 못한다.
- 비용: TCO 1yr/5yr 외부비용 0. 유지보수는 기존 서버 테스트 범위 내.
- 리스크: handoff 문구 false positive.
- 선정 점수: 8/10

### Option 2: comment create 시점에서 PASS 코멘트 차단
- 설명: 댓글 작성 경로에서 PASS 템플릿을 검사하고 handoff 누락을 거부한다.
- Pros: 잘못된 PASS 코멘트 자체를 저장하지 않음.
- Cons: PASS가 상태 전환 없이 중간 보고로 쓰이는 경우를 과차단할 수 있음. done/batch/GitHub 경로와 별도 구현 필요.
- 비용: TCO 1yr/5yr 외부비용 0. 회귀 범위 큼.
- 리스크: 댓글 UX 회귀.
- 선정 점수: 6/10

### Option 3: chain-keeper/autopilot이 사후 감지
- 설명: done 이후 주기적으로 handoff 누락을 찾아 재호출한다.
- Pros: 기존 운영 자동화와 맞음.
- Cons: 완료 전 차단이 아니며 대표님 지적의 핵심인 false done을 허용한다.
- 비용: TCO 1yr/5yr 외부비용 0. 운영 복잡도 증가.
- 리스크: 감지 주기 사이 체인 정체.
- 선정 점수: 4/10

## Decision Matrix

| 기준 | 가중치 | Option 1 | Option 2 | Option 3 |
|---|---:|---:|---:|---:|
| 차단력 | 0.35 | 9 | 8 | 4 |
| 변경 범위 | 0.25 | 9 | 6 | 6 |
| 회귀 위험 | 0.20 | 7 | 5 | 6 |
| 운영 단순성 | 0.20 | 8 | 6 | 4 |
| **가중 합** |  | **8.35** | **6.55** | **4.90** |

## Consequences

### Positive
- 다음 실행 주체가 필요한 PASS/done 누락을 서버가 차단한다.
- CLI/API/GitHub close intent done 경로가 같은 판단을 공유한다.

### Negative
- handoff 필요 여부 판정이 코멘트 키워드 기반이다.

### Neutral
- 일반 leaf issue의 PASS/done은 기존 Evidence Gate 조건만 충족하면 계속 허용된다.

## Implementation Notes
- `server/internal/handler/issue_evidence_gate.go`에 `finalPassRequiresNextAgentCall`와 `containsAgentMention`를 추가했다.
- `issue.go` 단일/배치 update는 같은 요청의 agent assignee 변경을 handoff 호출로 인정한다.
- `github.go` auto-done은 assignee 변경이 없으므로 mention 기반으로만 통과한다.

## Validation
- 서버 테스트: `TestUpdateIssueDoneRejectsFinalPassHandoffWithoutAgentMention`, `TestUpdateIssueDoneAllowsFinalPassHandoffWithAgentMention`.
- 현재 런타임에는 `go` toolchain이 없어 실행은 차단됨.

## References
- `server/internal/handler/issue_evidence_gate.go`
- `server/internal/handler/issue.go`
- `server/internal/handler/github.go`
