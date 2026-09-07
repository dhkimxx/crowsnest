# Crowsnest 설정

## 요구사항

- Go 1.24 이상 또는 Docker
- GitLab Self-Managed 17.6
- GitLab Administrator 권한의 API Token: Hook Reconciler를 사용할 때만 필요
- Feishu Self-Built App Bot: 실제 발송 모드에서 필요
- GitLab 서버가 Crowsnest의 Webhook 주소로 연결 가능한 네트워크 경로

## 로컬 실행

```bash
cp .env.example .env
go run ./cmd/crowsnest serve
```

기본 HTTP 주소는 `:8080`이다.

## Docker Compose 실행

Compose는 n8n과 별도 서비스로 Crowsnest를 호스트의 `5680` 포트에 노출한다.

```bash
docker compose --env-file .env -f deploy/compose.yaml up --build
```

운영 설정은 저장소 밖의 Secret 관리 방식 또는 로컬 `.env`로 주입한다. `.env`는 커밋하지 않는다.

## 환경변수

필수 운영 설정의 이름만 아래에 설명한다. 값은 이 문서나 Git에 기록하지 않는다.

| 변수 | 설명 |
| --- | --- |
| `CROWSNEST_HTTP_ADDR` | 컨테이너 내부 HTTP 주소. 기본 `:8080` |
| `CROWSNEST_DB_PATH` | SQLite 파일 경로. 기본 `data/crowsnest.sqlite3` |
| `CROWSNEST_DRY_RUN` | `true`이면 Feishu 대신 dry-run Messenger 사용 |
| `CROWSNEST_RECONCILE_DRY_RUN` | `true`이면 Hook Reconciler가 GitLab에 쓰지 않음 |
| `CROWSNEST_IDENTITY_SYNC_ENABLED` | GitLab 사용자와 Feishu 사용자 매핑 동기화 Worker 활성화 |
| `CROWSNEST_IDENTITY_SYNC_DRY_RUN` | `true`이면 사용자 매핑 DB를 변경하지 않음 |
| `CROWSNEST_IDENTITY_SYNC_INTERVAL` | 사용자 매핑 동기화 주기. 기본 `1h` |
| `CROWSNEST_RECIPIENT_ALLOWLIST` | 비어 있으면 전체 발송, 값이 있으면 쉼표로 구분한 이메일에만 발송 |
| `CROWSNEST_WEBHOOK_SECRET` | 수신 Webhook의 `X-Gitlab-Token` 검증값 |
| `CROWSNEST_GITLAB_BASE_URL` | GitLab 기본 URL |
| `CROWSNEST_GITLAB_API_TOKEN` | 전체 프로젝트 Hook을 관리하는 API Token |
| `CROWSNEST_GITLAB_WEBHOOK_URL` | GitLab Hook이 호출할 Crowsnest URL |
| `CROWSNEST_GITLAB_WEBHOOK_TOKEN` | 자동 생성 Hook에 넣을 Secret Token |
| `CROWSNEST_GITLAB_HOOK_TOKEN` | 위 값과 분리할 때 사용하는 관리용 Hook Token |
| `CROWSNEST_GITLAB_HOOK_NAME` | Crowsnest가 소유하는 Hook 이름 |
| `CROWSNEST_GITLAB_SSL_VERIFY` | GitLab outgoing HTTPS 검증 여부 |
| `CROWSNEST_ALLOWED_EMAIL_DOMAINS` | 허용할 이메일 도메인 목록 |
| `CROWSNEST_FEISHU_APP_ID` | Feishu App ID |
| `CROWSNEST_FEISHU_APP_SECRET` | Feishu App Secret |

수신 Secret과 자동 생성 Hook Token은 기본적으로 같은 값을 쓸 수 있지만, 운영에서는 분리할 수 있다.

## 사용자 자동 동기화

GitLab Administrator API로 사용자를 읽고, 허용된 이메일 도메인의 사용자만 Feishu Open Platform의 `batch_get_id` API로 확인한다. Feishu에서 확인된 활성 사용자만 SQLite 매핑을 활성화한다. Feishu Contact API 권한 오류나 네트워크 오류가 발생하면 해당 실행에서는 기존 매핑을 변경하지 않는다.

수동 실행은 기본적으로 dry-run이다.

```bash
go run ./cmd/crowsnest sync-users --dry-run
go run ./cmd/crowsnest sync-users --apply
```

상시 동기화는 다음 설정으로 켠다.

```text
CROWSNEST_IDENTITY_SYNC_ENABLED=true
CROWSNEST_IDENTITY_SYNC_DRY_RUN=false
CROWSNEST_IDENTITY_SYNC_INTERVAL=1h
```

`serve`는 시작 직후 한 번 동기화하고 이후 설정된 주기로 반복한다. GitLab 사용자 API와 Feishu Contact API의 Secret·응답 원문은 로그에 남기지 않는다.

## 발송 대상 제한

`CROWSNEST_RECIPIENT_ALLOWLIST`가 비어 있으면 기존처럼 모든 결정된 수신자에게 발송한다. 하나 이상의 이메일이 설정되면 라우팅 단계와 Outbox Worker 양쪽에서 해당 이메일과 일치하는 수신자만 허용한다. 따라서 설정을 켠 뒤 이미 대기 중인 다른 수신자의 delivery도 발송되지 않는다.

예를 들어 단일 사용자에게만 시험 발송하려면 다음처럼 설정한다.

```text
CROWSNEST_RECIPIENT_ALLOWLIST=carol@example.com
```

목록은 이메일을 소문자로 정규화하고 쉼표로 구분한다. 운영 범위를 전체로 되돌리려면 값을 비우고 서비스를 재시작한다.

## 알림 설정

현재는 SQLite의 기본 알림 정책을 사용한다. `issue_updated` 기본값은 `false`다. 사용자별 preference 관리는 향후 관리 API/UI로 추가한다.

## Hook Reconciler

먼저 dry-run으로 확인한다.

```bash
go run ./cmd/crowsnest reconcile --dry-run
```

실제 생성·수정은 명시적으로 `--apply`를 사용한다.

```bash
go run ./cmd/crowsnest reconcile --apply
```

Reconciler는 이름이 `CROWSNEST_GITLAB_HOOK_NAME`과 일치하는 Hook만 Crowsnest 소유로 취급한다. 다른 Hook은 삭제하거나 수정하지 않는다.

기본 관리 정책은 다음과 같다.

- System Hook: Merge Request 이벤트
- Project Hook: Pipeline, Note, Issue 이벤트
- Push, Tag, Job, Deployment, Wiki, Confidential 이벤트: 기본 비활성화

## GitLab 네트워크

기본 Compose 주소는 다음과 같다.

```text
http://<crowsnest-host>:5680/webhook/gitlab
```

`crowsnest.example.test`가 사설 주소이므로 GitLab 서버에서 이 주소로 라우팅할 수 있어야 한다. 불가능하면 HTTPS reverse proxy 또는 GitLab과 같은 네트워크에 있는 공개 주소가 필요하다.

## Feishu 권한

실제 발송 모드에서는 Feishu Self-Built App Bot 기능과 메시지 전송 권한을 활성화한다. Crowsnest는 이메일을 `receive_id`로 직접 사용한다. Feishu Contact API를 통한 email→open_id 변환은 기본 경로가 아니다.

## 보안 주의

- App Secret, GitLab API Token, Webhook Token을 `.env.example`, fixture, 로그, Git에 넣지 않는다.
- GitLab 사용자 이메일을 Feishu Contact API로 확인하려면 앱에 `contact:user.id:readonly` 권한을 추가하고 승인한다.
- 실제 운영 Payload를 `testdata`에 복사하지 않는다.
- 처음에는 `CROWSNEST_DRY_RUN=true`로 실행한다.
- 운영 Hook 활성화 전 GitLab의 Webhook test와 Crowsnest delivery 상태를 함께 확인한다.
