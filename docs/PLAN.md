# Crowsnest 구현 계획

상태: 초기 구현 진행 중

## 목표

GitLab 인스턴스의 이벤트를 개인별 실행 알림으로 변환해 Feishu Self-Built App Bot 개인 DM으로 전달한다. n8n과 독립된 Go 서비스로 운영하며, 향후 LLM 분석을 추가할 수 있는 경계를 둔다.

## 합의된 기본값

- 언어: Go
- 배포: n8n과 분리된 Docker Compose
- Crowsnest 외부 포트: `5680`
- 저장소: 초기 SQLite
- 입력 URL: 하나의 `/webhook/gitlab` 엔드포인트
- GitLab 입력: 글로벌 System Hook + API로 자동 관리하는 Project Hook
- Feishu: 공식 HTTP API, `receive_id_type=email`, 개인 DM
- 초기 실행: dry-run 우선
- LLM: 이번 버전은 호출하지 않고 확장 인터페이스만 준비
- GitLab 변경: 알림 경로에서 승인·댓글·라벨 변경 등을 수행하지 않음
- 기존 n8n 워크플로우: 수정하지 않음

## 구현 범위

### 1. Go 서비스 기반

- Go module
- `net/http` 기반 HTTP 서버
- `log/slog` 기반 구조화 로그
- `/healthz`, `/readyz`
- 환경변수 및 Secret 파일 설정
- Dockerfile과 별도 Compose 스택
- SQLite 연결 및 migration 구조

### 2. GitLab 이벤트 수신

- `X-Gitlab-Token` 검증
- System Hook, Merge Request Hook, Pipeline Hook, Note Hook, Issue Hook 수신
- header와 body를 공통 내부 이벤트 모델로 정규화
- 지원하지 않는 이벤트는 `ignored` 상태로 처리
- 전체 원문 payload와 인증 헤더를 로그에 남기지 않음

### 3. Hook Reconciler

- 글로벌 System Hook 확인·생성·수정
- 전체 프로젝트 pagination 조회
- Crowsnest가 관리하는 Project Hook만 생성·수정
- Pipeline, Note/Comment, Issue 이벤트만 Project Hook에서 활성화
- 신규 프로젝트 자동 감지
- dry-run 및 once 실행 지원
- 주기 실행, rate limit, retry, race condition 대응
- 다른 시스템의 Hook은 삭제하거나 수정하지 않음

### 4. 결정론적 개인화 라우팅

- Pipeline 실패 및 명확한 복구 알림
- MR Reviewer·Assignee 지정
- MR의 의미 있는 변경
- 다른 사용자의 MR 댓글
- Issue 댓글 및 담당자 변경
- `@username` 멘션
- GitLab user ID/username에서 사내 이메일로 매핑
- GitLab Administrator API와 Feishu Contact API를 조합한 사용자 매핑 동기화
- 자기 알림 억제
- 여러 수신 이유를 한 DM으로 병합
- 사용자별 알림 설정 적용

### 5. Feishu 전송

- tenant access token 발급 및 만료 안전 여유를 둔 cache
- 이메일 기반 개인 DM
- interactive card
- HTTP status와 Feishu application code 동시 검사
- 인증 오류, 권한 오류, 수신자 오류, rate limit, 일시적 오류 구분
- 제한적 재시도와 token 만료 1회 재시도
- Secret과 token을 로그·fixture·문서에 저장하지 않음

### 6. 전달 상태와 복구

- `pending`, `delivered`, `failed` 상태
- 이벤트 단위와 수신자 단위 전달 상태 분리
- 수신자별 delivery key
- 성공 수신자 재전송 방지
- 실패 수신자만 재시도
- 서버 재시작 후 pending/failed 복구
- SQLite 기반 실용적 at-least-once 전달

### 7. 테스트·운영 문서

- GitLab 17.6 비밀 없는 fixture
- Pipeline, MR, Note, Issue, 멘션, 중복, 복구, 오류 시나리오
- Feishu mock server 기반 dry-run 테스트
- GitLab 사용자 목록·Feishu 이메일 조회·매핑 동기화 테스트
- 실제 메시지 발송 전 설정 검증
- Compose 운영 문서, Secret 주입 방법, SQLite 백업 방법
- GitLab Hook 등록·재조정·네트워크 접근 검증 절차

## 이번 버전 제외

- LLM 요약·위험도·권장 조치 실행
- Feishu 카드 양방향 버튼과 승인 액션
- GitLab MR 승인·댓글·라벨 변경
- 사용자 설정 UI와 slash command
- 모든 GitLab 이벤트 지원
- Confidential 이벤트 기본 전송
- 기존 n8n 이벤트 경로 편입
- 운영 환경의 실제 Hook 활성화 및 외부 시스템 변경

## 향후 LLM 확장 경계

정규화된 이벤트를 입력으로 받아 요약·영향·권장 조치·근거 링크를 구조화해 반환하는 `AIEnricher` 경계를 준비한다. LLM은 수신자 판정, Webhook 인증, 중복 제거, 전달 성공 여부를 결정하지 않는다.

## 아키텍처 결정 상태

다음 방향을 합의했다. 세부 구조와 필드는 추가 논의 후 확정한다.

- Canonical Event Model은 GitLab·GitHub·Forgejo에 공통인 업무 의미만 담는 목적 중심 수준으로 일반화한다.
- Provider별 Payload 경로와 버전 차이는 각 Adapter의 Decoder 안에서 처리한다.
- GitLab `v17.6` Adapter부터 구현하고, 이후 버전은 별도 Decoder 또는 호환 계층으로 확장한다.
- Crowsnest는 단일 프로세스로 실행하며 Delivery Worker, Recovery Worker, Hook Reconcile Scheduler를 내부 Worker로 둔다.
- `reconcile --once`, `reconcile --dry-run`은 수동 운영 명령으로 제공하되 상시 별도 프로세스로 운영하지 않는다.
- Webhook 이벤트와 수신자별 delivery는 SQLite에 저장한 뒤 성공 응답한다.
- Feishu 전송은 영속 Outbox Worker가 담당한다.

- GitLab API Token의 보관 위치와 권한 범위
- 사설 IP 접근 및 HTTPS/reverse proxy 구성
- SQLite driver와 migration 도구 선택
- Canonical Event의 최종 필드와 provider-specific metadata 범위
