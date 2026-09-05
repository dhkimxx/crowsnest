.PHONY: fmt test vet check build compose-config docker-build

fmt:
	gofmt -w cmd internal

test:
	GOTOOLCHAIN=local go test ./...

vet:
	GOTOOLCHAIN=local go vet ./...

check: test vet
	git diff --check

build:
	GOTOOLCHAIN=local go build ./cmd/crowsnest

compose-config:
	docker compose --env-file .env -f deploy/compose.yaml config

docker-build:
	docker compose --env-file .env -f deploy/compose.yaml build
