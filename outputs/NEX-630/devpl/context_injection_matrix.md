# NEX-630 GPT/Codex vs Claude 컨텍스트 주입 경로 실측

## 결론

GPT/Codex와 Claude는 같은 `TaskContextForEnv` 데이터를 출발점으로 쓰지만, 실제 md 파일명과 skill/MCP 주입 위치가 다르다. 공통으로 주입되는 것은 issue prompt, `.agent_context/issue_context.md`, agent instructions, workspace/project context, project resources 요약, mention 규칙이다. 차이는 런타임 config 파일명과 provider-native skill/MCP 경로다.

## 코드 근거

| 항목 | Claude 경로 | GPT/Codex 경로 | 같음/다름 | 코드 근거 |
|---|---|---|---|---|
| 최종 실행 prompt | `daemon.BuildPrompt(task, "claude")`가 issue ID, handoff note, comment history 명령을 생성 | `daemon.BuildPrompt(task, "codex")`가 같은 prompt 생성 | 같음 | `server/internal/daemon/prompt.go` |
| 런타임 md 파일 | `{workDir}/CLAUDE.md` | `{workDir}/AGENTS.md` | 다름 | `server/internal/daemon/execenv/runtime_config.go: InjectRuntimeConfig`, `runtimeConfigPath` |
| agent instructions | `TaskContextForEnv.AgentInstructions`가 runtime brief에 렌더됨 | 같은 필드가 AGENTS.md brief에 렌더됨 | 같음 | `server/internal/daemon/execenv/execenv.go`, `runtime_config.go` |
| issue sidecar | `{workDir}/.agent_context/issue_context.md` | 동일 | 같음 | `server/internal/daemon/execenv/context.go: writeContextFiles`, `renderIssueContext` |
| workspace context | `TaskContextForEnv.WorkspaceContext`가 brief에 렌더됨 | 동일 | 같음 | `server/internal/daemon/execenv/execenv.go`, `runtime_config.go` |
| project resources summary | runtime brief에 project/resource 요약 렌더 | 동일 | 같음 | `server/internal/daemon/execenv/runtime_config.go: formatProjectResource` |
| project resources raw JSON | `{workDir}/.multica/project/resources.json` | 동일 | 같음 | `server/internal/daemon/execenv/context.go: writeProjectResources` |
| skills 목록 | `CLAUDE.md`에 skill 목록 및 native discovery 안내 | `AGENTS.md`에 skill 목록 및 native discovery 안내 | 파일명만 다름 | `server/internal/daemon/execenv/runtime_config.go: buildMetaSkillContent` |
| skill 파일 위치 | `{workDir}/.claude/skills/{name}/SKILL.md` | per-task `CODEX_HOME` 쪽 Codex home 준비 단계에서 처리 | 다름 | `server/internal/daemon/execenv/context.go: skillsDirPath`, `writeContextFiles`; Codex 관련 `codex_home.go`/`codex_skill_strip.go` |
| MCP config | 임시 파일을 만들고 `claude --mcp-config <path>`로 전달 | `$CODEX_HOME/config.toml`의 daemon-managed `[mcp_servers.*]` 블록으로 전달 | 다름 | `server/pkg/agent/claude.go`; `server/pkg/agent/codex.go: ensureCodexMcpConfig` |
| memory/wiki | 별도 wiki/mem 전용 런타임 필드는 확인되지 않음. runtime brief, sidecar, skills, project resources로 전달되는 구조 | 동일. Codex는 `CODEX_HOME` 관리 블록과 사용자 skill 충돌 처리 로직이 추가됨 | 전용 mem/wiki 주입은 같음: 없음 | `server/internal/daemon/execenv/execenv.go`, `server/internal/daemon/execenv/context.go`, `server/pkg/agent/codex.go` |

## 해석

- “Claude와 GPT/Codex가 바라보는 md 파일이 같은가?”: 아니다. Claude는 `CLAUDE.md`, Codex는 `AGENTS.md`를 받는다.
- “Mem/wiki 보는 법이 같은가?”: 코드 기준으로 별도 `mem/wiki` 전용 주입 경로는 확인되지 않았다. 둘 다 runtime brief, issue sidecar, skills, project resource 파일을 통해 문맥을 받는다.
- “skills 보는 법이 같은가?”: 이름/설명은 runtime brief에 공통으로 들어가지만, 실제 skill 파일 배치 경로는 provider별 native discovery에 맞춰 다르다.
- “MCP 보는 법이 같은가?”: 아니다. Claude는 `--mcp-config`, Codex는 `$CODEX_HOME/config.toml`이다.
