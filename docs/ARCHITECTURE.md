# Crowsnest 아키텍처

상태: 초기 기준 확정, 세부 구현은 진행 중

## 목표

Crowsnest는 GitLab·GitHub·Forgejo 같은 Git 서비스의 이벤트를 공통 내부 이벤트로 변환하고, 결정론적인 수신자 판정과 영속 Outbox를 거쳐 Feishu 개인 메시지로 전달하는 self-hosted 서비스다.

이번 구현은 GitLab 17.6과 Feishu Self-Built App Bot을 기준으로 한다. 다른 Provider와 LLM은 핵심 로직을 다시 작성하지 않고 Adapter를 추가할 수 있도록 경계를 둔다.

## 핵심 결정

- 마이크로서비스가 아닌 Ports & Adapters 구조의 모듈형 단일 서비스
- 하나의 Go 프로세스 안에서 HTTP 서버와 내부 Worker를 실행
- Webhook 이벤트와 수신자별 delivery를 먼저 SQLite에 기록한 뒤 성공 응답
- Feishu 전송은 영속 Outbox Worker에서 비동기로 수행
- GitLab Payload와 버전별 차이는 Provider Adapter의 Decoder에서 흡수
- 수신자·인증·중복 제거·전달 성공 여부는 결정론적으로 처리
- LLM은 나중에 추가되는 비동기 Enricher이며 기본 알림을 차단하지 않음
- Feishu는 공식 HTTP API를 직접 사용하고 Custom Bot SDK에 의존하지 않음

## 런타임 구성

```text
                    ┌─────────────────────────┐
GitLab System Hook ─▶                         │
GitLab Project Hook ─▶  HTTP Ingress :5680     │
                    │  인증·Provider Decoder   │
                    └────────────┬────────────┘
                                 │ transaction
                                 ▼
                    ┌─────────────────────────┐
                    │ SQLite                  │
                    │ events / deliveries     │
                    │ identities / preferences│
                    └────────────┬────────────┘
                                 │ claim
                    ┌────────────▼────────────┐
                    │ Internal Workers        │
                    │ delivery / recovery     │
                    │ hook reconcile scheduler│
                    │ identity sync scheduler│
                    └────────────┬────────────┘
                                 ▼
                    ┌─────────────────────────┐
                    │ Feishu Official API     │
                    │ email personal message  │
                    └─────────────────────────┘
```

단일 프로세스라는 것은 단일 실행 파일을 의미하며, 내부적으로는 역할별 goroutine과 bounded worker pool을 사용한다.

## 요청 처리 흐름

1. HTTP Ingress가 `POST /webhook/gitlab`을 수신한다.
2. `X-Gitlab-Token`을 검증한다.
3. Provider Registry가 Webhook header와 body에 맞는 Decoder를 선택한다.
4. Decoder가 GitLab Payload를 Canonical Event로 변환한다.
5. 지원하지 않는 이벤트는 `ignored` 결과로 기록한다.
6. Application Router가 수신자 후보와 알림 이유를 계산한다.
7. 이벤트와 수신자별 delivery를 하나의 DB transaction으로 저장한다.
8. 저장 성공 후 GitLab에 2xx를 반환한다.
9. Delivery Worker가 pending delivery를 claim해 Feishu로 전송한다.
10. 성공은 `delivered`, 재시도 가능한 실패는 `failed` 또는 다음 시도 상태로 기록한다.

Feishu 장애가 GitLab Webhook 응답을 실패시키지 않으며, 프로세스 재시작 후에도 delivery 상태를 복구할 수 있다.

## 계층과 의존성 방향

```text
domain
  ▲
application ─── ports
  ▲              ▲
adapters ────────┘
platform
```

### Domain

외부 API 구조를 모르는 공통 업무 모델이다.

- `CanonicalEvent`
- `Identity`
- `ProjectRef`
- `ResourceRef`
- `Notification`
- `RecipientAddress`
- `Delivery`

GitLab의 `object_attributes`나 Feishu의 `receive_id`는 Domain 모델에 노출하지 않는다.

### Application

업무 흐름을 조합한다.

- Webhook 수신
- 이벤트 정규화
- 수신자 계산
- 수신자 allowlist 정책 적용
- preference 적용
- delivery 생성
- Outbox 전송
- 실패 복구
- Hook 재조정
- 사용자 매핑 동기화

### Ports

사용처가 필요한 최소 인터페이스를 정의한다. 하나의 거대한 Provider 또는 Repository 인터페이스를 만들지 않는다.

- `WebhookDecoder`
- `UserDirectory`
- `HookController`
- `Messenger`
- `EmailDirectory`
- `EventStore`
- `OutboxStore`
- `IdentityStore`
- `PreferenceStore`
- `PipelineStateStore`

### Adapters

- `git/gitlab/v17_6`: GitLab 17.6 Webhook Decoder와 GitLab API Hook Controller
- `messenger/feishu`: tenant token과 개인 메시지 전송
- `database/sqlite`: 초기 영속성 구현
- 향후 `git/github`, `git/forgejo`, `database/postgres`, `messenger/slack` 추가

## Canonical Event Model

Provider가 제공하는 모든 필드를 일반화하지 않는다. 현재 라우팅 규칙과 향후 공통 기능에 필요한 안정적인 의미만 담는다.

```text
source
source_version
source_event
event_key
kind
action
occurred_at
project
actor
author
object
reviewers
assignees
mentions
changes
source_text
raw_status
```

Provider별 필드 경로는 Decoder에 격리한다. 필요한 경우 제한된 문자열 metadata만 보존하며, 원본 전체 Payload나 Secret은 Canonical Event에 저장하지 않는다.

GitLab 17.6을 먼저 구현하고 버전 차이가 커지면 `gitlab/v17_7` 같은 Decoder를 추가한다. 라우팅·Outbox·Messenger 코드는 Provider 버전 분기를 직접 알지 않는다.

## Identity 경계

Git 서비스의 사용자 식별과 메신저의 전송 주소를 분리한다.

```text
GitLab user ID / username / email
  → IdentityResolver
  → canonical company email
  → Feishu email recipient
```

Payload에 유효한 이메일이 있으면 우선 사용한다. 이메일이 없거나 `[REDACTED]`이면 `IdentityStore`의 ID/username 매핑을 사용한다. Feishu Contact API는 선택적 검증·변환 수단이지 GitLab username을 이메일로 추측하는 수단이 아니다.

Webhook에 이메일이 없는 경우에는 GitLab Adapter가 `/users/:id` 또는 username 검색 API로 사용자 정보를 보완한 뒤 Router에 전달한다. 보완된 이메일은 SQLite IdentityStore에 기억해 다음 이벤트에서 재사용한다. GitLab API 장애가 기본 Webhook 응답을 실패시키지는 않으며, 기존 매핑이 있으면 그것을 사용한다.

### Identity Sync

사용자 동기화는 GitLab API와 Feishu Open Platform API의 양쪽 디렉터리를 조합하는 별도 Application 흐름이다.

```text
GitLab Admin API /users 또는 /users/:id
  → GitLab user ID, username, state, email
  → 허용 도메인·활성 상태 필터
  → (선택) Feishu Contact API /contact/v3/users/batch_get_id
  → GitLab 이메일 매핑 활성화
  → IdentityStore upsert + stale mapping 비활성화
```

Feishu 조회 실패는 동기화 전체 실패로 처리하며, 부분 응답을 매핑에 반영하지 않는다. Feishu에서 사용자를 찾지 못한 정상 응답은 해당 매핑을 비활성화한다. `serve`의 Identity Sync Scheduler는 시작 직후 실행한 뒤 설정된 주기로 반복한다.

## Outbox와 전달 상태

이벤트 단위 상태와 수신자 단위 전달 상태를 분리한다.

```text
event: accepted / ignored / invalid
delivery: pending / processing / delivered / failed
```

`delivery_key`는 이벤트·정규화 이메일·이유 그룹의 조합으로 만든다. Worker는 lease와 재시도 횟수를 사용하고, 성공한 delivery는 다시 보내지 않는다.

초기 SQLite는 WAL과 명시적인 transaction을 사용한다. 영속성 Adapter를 바꾸면 Application 계층은 바뀌지 않도록 한다.

## Hook Reconciler

Hook Reconciler는 같은 프로세스의 Scheduler Worker로 실행한다.

- Crowsnest 소유 Hook만 식별한다.
- 없는 Hook은 생성한다.
- 설정이 다른 Crowsnest Hook은 수정한다.
- 다른 Hook은 삭제·수정하지 않는다.
- `dry-run`과 `once` 실행 경로를 제공한다.
- API rate limit과 신규 프로젝트 생성 직후의 race를 처리한다.

실시간 Webhook 처리와 Reconciler는 같은 Process 안에 있지만, 코드·권한·worker queue를 분리한다.

## Feishu 경계

Application은 Feishu 카드 JSON을 직접 만들지 않는다. 의미 중심 Notification을 만들고 Feishu Adapter가 공식 API 요청으로 렌더링한다.

```text
Notification
  → Feishu MessageRenderer
  → tenant_access_token
  → receive_id_type=email
  → application-level response check
```

Token·App Secret·원본 Payload는 로그에 남기지 않는다.

## LLM 확장

LLM은 `CanonicalEvent`와 결정론적 Notification을 읽는 비동기 `AIEnricher`로 둔다.

```text
CanonicalEvent
  ├─ deterministic router → base delivery
  └─ AIEnricher → AIAnnotation → optional card enrichment
```

LLM은 수신자 결정, 인증 우회, 중복 제거, 전송 성공 판정을 수행할 수 없다.

## Go 패키지 구조

```text
cmd/crowsnest/
internal/domain/
internal/application/
internal/ports/
internal/adapter/git/gitlab/v17_6/
internal/adapter/messenger/feishu/
internal/adapter/database/sqlite/
internal/platform/config/
internal/platform/httpserver/
testdata/gitlab/v17_6/
deploy/
docs/
```

## 아직 열어 둔 결정

- SQLite driver와 migration 도구
- Canonical Event의 세부 필드와 변화 표현 방식
- Hook Reconciler의 기본 주기
- GitLab Admin API Token의 운영 보관 방식
- `crowsnest.example.test:5680` 외부 노출 및 HTTPS/reverse proxy
- 실제 GitLab 17.6 인스턴스 fixture와 confidential event 정책
