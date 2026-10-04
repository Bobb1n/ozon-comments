.DEFAULT_GOAL := help

GO ?= go
COMPOSE := docker compose -f docker-compose.yml
ENV_EXAMPLE := config/env.example
MOCKERY_CONFIG := config/mockery.yml
GQLGEN_CONFIG := config/gqlgen.yml
COVERAGE_FILE := coverage.out
COVER_PACKAGES = $(shell $(GO) list ./internal/... ./pkg/... | sed '/\/generated$$/d; /\/model$$/d' | paste -sd, -)

.PHONY: help env run mock gengraph test unit integration coverage test-db lint fmt migrate-up migrate-down start docker monitoring

help:
	@printf '%s\n' \
	  'env          — создать .env со случайными JWT-секретом и паролем Grafana; существующий не менять' \
	  'run          — запустить сервер локально с настройками из .env' \
	  'mock         — перегенерировать моки в корневой папке mocks' \
	  'gengraph     — перегенерировать GraphQL-код по схеме' \
	  'unit         — перегенерировать моки и запустить unit-тесты с проверкой гонок' \
	  'test-db      — поднять тестовую PostgreSQL из настроек .env и дождаться готовности' \
	  'integration  — поднять тестовую БД и запустить интеграционные тесты' \
	  'test         — перегенерировать моки, поднять тестовую БД и запустить все тесты' \
	  'coverage     — все тесты и покрытие internal/pkg без сгенерированных файлов' \
	  'lint         — проверить код линтером и форматирование' \
	  'fmt          — отформатировать Go-код и импорты' \
	  'migrate-up   — накатить миграции в БД из POSTGRES_* или DATABASE_URL' \
	  'migrate-down — откатить одну последнюю миграцию; может удалить данные' \
	  'docker       — собрать и запустить Compose: БД, миграции, приложение' \
	  'start        — создать .env и вызвать docker' \
	  'monitoring   — запустить приложение, Prometheus и Grafana с готовым дашбордом'

env:
	@if [ -e .env ]; then \
		printf '%s\n' '.env уже существует, оставлен без изменений'; \
	else \
		umask 077; \
		jwt_secret=$$(openssl rand -hex 32) || exit 1; \
		grafana_password=$$(openssl rand -hex 16) || exit 1; \
		set -C; \
		sed -e "s/^AUTH_JWT_SECRET=$$/AUTH_JWT_SECRET=$$jwt_secret/" \
		    -e "s/^GRAFANA_ADMIN_PASSWORD=$$/GRAFANA_ADMIN_PASSWORD=$$grafana_password/" $(ENV_EXAMPLE) > .env || exit 1; \
		chmod 600 .env; \
	fi

run:
	$(GO) run ./cmd/server

mock:
	$(GO) tool mockery --config $(MOCKERY_CONFIG)

gengraph:
	$(GO) tool gqlgen generate --config $(GQLGEN_CONFIG)

unit: mock
	$(GO) test -race -count=1 ./internal/... ./pkg/... ./cmd/...

test-db: env
	$(COMPOSE) --profile test up -d --wait test-db

integration: test-db
	$(GO) test -race -count=1 ./tests/integration/...

test: mock test-db
	$(GO) test -race -count=1 ./...

coverage: mock test-db
	$(GO) test -race -count=1 -coverpkg=$(COVER_PACKAGES) -coverprofile=$(COVERAGE_FILE) ./...
	$(GO) tool cover -func=$(COVERAGE_FILE)

lint: mock
	$(GO) tool golangci-lint run
	$(GO) tool golangci-lint fmt --diff

fmt:
	$(GO) tool golangci-lint fmt

migrate-up:
	$(GO) run ./cmd/migrate up

migrate-down:
	$(GO) run ./cmd/migrate down

start: docker

docker: env
	$(COMPOSE) up -d --build

monitoring: env
	METRICS_ENABLED=true $(COMPOSE) --profile monitoring up -d --build
