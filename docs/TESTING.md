# Crowsnest 검증

## 정적 검증

```bash
GOTOOLCHAIN=local go test ./...
GOTOOLCHAIN=local go vet ./...
git diff --check
```

## Docker 검증

```bash
docker compose -f deploy/compose.yaml config
docker compose -f deploy/compose.yaml build
```

컨테이너를 실행한 뒤 다음 endpoint를 확인한다.

```bash
curl http://127.0.0.1:5680/healthz
curl http://127.0.0.1:5680/readyz
```

## Fixture 검증

`testdata/gitlab/v17_6/`의 fixture는 GitLab 17.6 공식 Webhook 구조를 기준으로 하며 Secret과 실제 사용자 정보를 포함하지 않는다.

검증 대상:

- Pipeline 실패
- Merge Request 생성 및 Reviewer 변경
- MR Note
- Issue Assignee 변경
- System Hook의 지원하지 않는 lifecycle event
- 누락 이메일과 username 기반 매핑

사용자 동기화는 GitLab 사용자 pagination·비공개 이메일 fallback·Feishu `batch_get_id` 배치 조회·Contact API 권한 오류·비활성화된 stale mapping을 mock으로 검증한다.

## Feishu Mock 검증

Feishu Adapter 테스트는 `httptest` 서버를 사용한다. 다음을 확인한다.

- tenant access token 요청
- `receive_id_type=email`
- interactive card JSON
- Feishu application-level success code
- token 만료 후 1회 갱신
- 429 rate limit 분류

실제 Feishu 메시지는 자동 테스트에서 보내지 않는다.

## Outbox 검증

- 이벤트와 delivery가 함께 저장되는지 확인한다.
- 같은 delivery key가 중복 생성되지 않는지 확인한다.
- Worker가 pending delivery를 claim하는지 확인한다.
- 성공 delivery가 재전송되지 않는지 확인한다.
- retryable failure가 backoff 후 재처리되는지 확인한다.
- 최대 시도 횟수 이후 terminal failed가 되는지 확인한다.

## Hook Reconciler 검증

Mock GitLab API로 다음을 확인한다.

- dry-run은 POST/PUT를 수행하지 않는다.
- 없는 System/Project Hook을 생성한다.
- 설정이 다른 Crowsnest Hook을 수정한다.
- 다른 이름의 Hook은 건드리지 않는다.
- 프로젝트 pagination을 처리한다.
- API 오류가 발생해도 나머지 프로젝트를 계속 처리하고 실패 수를 보고한다.

실제 GitLab `--apply` 실행과 운영 Hook 활성화는 별도의 승인 단계다.
