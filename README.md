# Crowsnest

GitLab 이벤트를 개인별 실행 알림으로 변환해 Feishu로 전달하는 self-hosted integration service.

## 현재 방향

- GitLab 글로벌 System Hook을 기본 입력으로 사용한다.
- Pipeline, Note/Comment, Issue처럼 System Hook에서 제공하지 않는 이벤트는 GitLab API로 Project Hook을 자동 관리한다.
- Webhook 수신·인증·정규화·수신자 판정·중복 제거는 결정론적으로 처리한다.
- Feishu는 Custom Bot이 아닌 Self-Built App Bot의 공식 Open API를 사용한다.
- 개인 DM은 이메일 기반 수신을 우선 사용한다.
- 향후 LLM은 수신자 판정이나 보안 게이트가 아니라 요약·위험도·권장 조치 같은 비동기 보조 기능으로 추가한다.

## 로컬 실행

Go toolchain으로 실행:

```bash
go run ./cmd/crowsnest serve
```

Docker Compose로 실행:

```bash
docker compose --env-file .env -f deploy/compose.yaml up --build
```

기본 health endpoint는 `http://127.0.0.1:8080/healthz`이며, Compose는 호스트의 `5680` 포트로 노출한다.

사용자 매핑과 알림 설정은 CSV 템플릿을 채운 뒤 다음 명령으로 SQLite에 입력한다.

```bash
go run ./cmd/crowsnest import-users --file templates/gitlab_user_map.csv
go run ./cmd/crowsnest import-preferences --file templates/notification_preferences.csv
go run ./cmd/crowsnest sync-users --dry-run
```

`sync-users --apply` 또는 `CROWSNEST_IDENTITY_SYNC_ENABLED=true` 설정을 사용하면 GitLab 사용자와 Feishu 계정을 이메일 기준으로 자동 검증·동기화할 수 있다. Feishu Contact API 권한이 필요하다.

`CROWSNEST_RECIPIENT_ALLOWLIST`를 설정하면 해당 이메일에만 발송하고, 비워 두면 전체 수신자에게 발송한다.

자세한 구현 범위는 [docs/PLAN.md](docs/PLAN.md), 구조와 경계는 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)를 참고한다.

## 보안 원칙

- Secret, token, 실제 사용자 매핑은 Git에 커밋하지 않는다.
- 외부 GitLab·n8n·Feishu 환경 변경은 별도 승인을 받은 작업에서만 수행한다.
- Webhook 원문과 메시지 본문을 기본 로그에 남기지 않는다.
- LLM에 전달하는 코드·댓글·변경 내용은 프로젝트별 정책과 보존 기간을 따른다.
