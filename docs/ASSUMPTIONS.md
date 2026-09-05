# Crowsnest 가정과 제한

- 현재 목표 GitLab 버전은 Self-Managed 17.6이다.
- System Hook은 Merge Request와 lifecycle 감지에 사용하고, Pipeline·Note·Issue는 Project Hook으로 보완한다.
- 전체 Project Hook 자동 관리는 GitLab Administrator API Token이 필요하다.
- System Hook과 Project Hook은 하나의 Crowsnest Webhook URL을 공유한다.
- Push·Tag·Job·Deployment·Wiki·Confidential 이벤트는 기본적으로 알림 대상이 아니다.
- Pipeline 실패 수신자는 우선 HEAD commit author email을 사용한다. Pipeline 실행 사용자와 commit author는 동일하다고 가정하지 않는다.
- 이메일이 `[REDACTED]`이거나 없으면 GitLab user ID/username 매핑을 사용한다.
- Feishu 수신자는 이메일 직접 전송을 우선한다. Contact API email→open_id 조회는 기본 경로가 아니다.
- 사용자 매핑과 preference가 없는 사용자는 안전하게 전송하지 않는다.
- Webhook 이벤트와 수신자별 delivery는 SQLite Outbox에 저장한 뒤 성공 응답한다.
- SQLite는 단일 호스트·단일 프로세스 운영을 기준으로 한다. 고가용성이나 다중 replica가 필요하면 PostgreSQL Adapter를 추가해야 한다.
- Feishu 전송 dry-run과 GitLab Hook Reconciler dry-run은 서로 독립된 설정으로 제어한다.
- LLM은 이번 버전에 포함하지 않으며, 이후 비동기 enrichment 기능으로만 추가한다.
- GitLab 승인·댓글·라벨 변경 등 외부 상태 변경은 Crowsnest 알림 경로에서 수행하지 않는다.
- GitLab의 실제 운영 Payload는 로컬 fixture와 다를 수 있으므로, 운영 전 Webhook test 결과를 별도로 검증해야 한다.
