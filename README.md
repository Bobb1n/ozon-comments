# Посты и комментарии

Сервис на Go с GraphQL API. Можно создавать посты, оставлять комментарии и отвечать на них. Автор поста может отключить новые комментарии. Максимальная длина комментария — 2000 символов.

## Запуск

Нужны Docker с Compose. Для локального запуска и тестов — Go 1.27.1.

С PostgreSQL в Docker:

```sh
make start
```

Команда создаст `.env`, поднимет БД, применит миграции и запустит сервер. Существующий `.env` не перезаписывается.

Локально, с хранением данных в памяти:

```sh
make env
make run
```

В этом режиме данные теряются после остановки сервера.

Откройте [GraphQL Playground](http://localhost:8080/). API находится на `/graphql`, проверка доступности — `/healthz`.

## Настройки

Все настройки есть в [config/env.example](config/env.example). Менять их нужно в `.env`.

`STORAGE=inmemory` включает хранение в памяти, `STORAGE=postgres` — PostgreSQL. Подключение собирается из `POSTGRES_*` в `.env`; `DATABASE_URL` позволяет указать его целиком. Перед локальным запуском выполните `make migrate-up`. В Docker подключение к БД уже настроено.

Логи пишутся в консоль в JSON. Уровень задаётся через `LOG_LEVEL`.

## Метрики

```sh
make monitoring
```

Поднимет приложение, Prometheus и Grafana. В [Grafana](http://localhost:3000) войдите с `GRAFANA_ADMIN_USER` и `GRAFANA_ADMIN_PASSWORD` из `.env`. Дашборд «Посты и комментарии» появится в папке «Сервис» автоматически.

На дашборде: запросы и время ответа, HTTP- и GraphQL-ошибки, подписки, пул PostgreSQL и память Go. Ошибки GraphQL считаются отдельно: они могут приходить с HTTP 200. Длительность WebSocket-запроса — время всего соединения.

Метрики доступны на `/metrics`, Prometheus — на [localhost:9090](http://localhost:9090). Без мониторинга endpoint выключен. Для локального запуска включите `METRICS_ENABLED=true` в `.env`. Endpoint не требует авторизации; вне локального запуска доступ к нему нужно ограничить сетью.

Настройки лежат в `monitoring/`, код метрик — в `pkg/metrics`. Дашборд можно менять в Grafana. Чтобы передать изменения вместе с проектом, экспортируйте JSON без режима экспорта для внешнего использования и сохраните в `monitoring/grafana/dashboards/service.json`. Изменения только в интерфейсе не попадут в этот файл.

Если меняете порт приложения внутри Docker, обновите также `targets` в `monitoring/prometheus/prometheus.yml`.

## Регистрация и вход

Создайте пользователя:

```sh
curl http://localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"login":"my-user","password":"my-test-password"}'
```

Пароль должен содержать минимум 12 символов и занимать не больше 72 байт. Логин должен быть уникальным. Эти примеры не создаются автоматически при запуске.

Получите токены:

```sh
curl http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"login":"my-user","password":"my-test-password"}'
```

В ответе: `token`, `expiresAt`, `refreshToken`, `refreshExpiresAt`. Скопируйте `token` в HTTP HEADERS Playground:

```json
{"Authorization":"Bearer ВАШ_ТОКЕН"}
```

Для создания постов и комментариев нужен вход. Читать данные и подписываться на комментарии можно без него.

Access JWT действует 15 минут. Чтобы получить новую пару токенов:

```sh
curl http://localhost:8080/auth/refresh \
  -H 'Content-Type: application/json' \
  -d '{"refreshToken":"ВАШ_REFRESH_ТОКЕН"}'
```

Сохраните новый refresh: предыдущий больше не подходит. Повторное использование старого токена отзывает весь этот вход. Не отправляйте несколько refresh-запросов одновременно.

Refresh-сессия действует 7 дней с момента входа; обновление токена не продлевает этот срок. Сроки задаются через `AUTH_TOKEN_TTL` и `AUTH_REFRESH_TTL`.

Выход:

```sh
curl http://localhost:8080/auth/logout \
  -H 'Content-Type: application/json' \
  -d '{"refreshToken":"ВАШ_ТЕКУЩИЙ_REFRESH_ТОКЕН"}'
```

Logout отзывает refresh-сессию. Выданный access JWT продолжит работать до истечения срока.

Готовых аккаунтов нет. `make env` создаёт случайный JWT-секрет через OpenSSL. Не публикуйте `.env`; вне локального запуска используйте HTTPS. При хранении в памяти после остановки теряются и пользователи, и refresh-сессии.

## Запросы

Создать пост:

```graphql
mutation {
  createPost(input: {title: "Первый пост", content: "Текст поста"}) {
    id
    authorId
    commentsEnabled
  }
}
```

Создать комментарий, подставив ID поста:

```graphql
mutation {
  createComment(input: {postId: "POST_ID", text: "Комментарий"}) {
    id
    parentId
    text
  }
}
```

Для ответа на комментарий добавьте во входные данные `parentId: "COMMENT_ID"`.

Прочитать пост и его корневые комментарии:

```graphql
query {
  post(id: "POST_ID") { id title content }
  comments(postId: "POST_ID", first: 10) {
    items { id text authorId }
    endCursor
    hasNextPage
  }
}
```

Чтобы прочитать ответы, передайте `parentId` в `comments`. Ветки загружаются отдельно, всё дерево сразу не возвращается.

Следующая страница запрашивается с `after: "КУРСОР"`, где курсор — `endCursor` предыдущего ответа. Продолжение есть, если `hasNextPage=true`. Курсор нельзя переносить между постами или ветками.

Список постов: `posts(first: 10)`. У него такие же поля страницы: `items`, `endCursor`, `hasNextPage`. Посты и комментарии идут от старых к новым.

Отключить комментарии можно только с токеном автора поста:

```graphql
mutation {
  setCommentsEnabled(postId: "POST_ID", enabled: false) {
    commentsEnabled
  }
}
```

Существующие комментарии останутся доступными. Чтобы разрешить новые, передайте `enabled: true`.

## Подписки

В отдельной вкладке Playground:

```graphql
subscription {
  commentAdded(postId: "POST_ID") {
    id
    parentId
    text
  }
}
```

Создайте комментарий в другой вкладке — он появится в подписке. Подписки работают через WebSocket на `/graphql`, внутри одного экземпляра сервиса. Пропущенные события не сохраняются.

## Тесты и команды

```sh
make unit         # unit-тесты
make integration  # тесты с PostgreSQL
make test         # все тесты
make coverage     # все тесты и отчёт о покрытии
make lint         # линтер и проверка форматирования
```

Тесты запускаются с проверкой гонок. Для PostgreSQL автоматически поднимается отдельная тестовая БД; её креды берутся из `.env`. Рабочая БД не используется.

Без настроек тестовой БД PostgreSQL-проверки пропускаются. Если настройки заданы, но БД недоступна, тесты завершаются с ошибкой.

Unit-тесты проверяют сервисы, авторизацию, пагинацию и ошибки. Интеграционные — работу репозиториев, вложенные комментарии, параллельные записи и миграции. Есть проверки GraphQL и WebSocket.

`make mock` генерирует моки в `mocks/`; `make unit`, `make test` и `make coverage` обновляют их перед запуском.

Моки не хранятся в Git: перед обычным `go test ./...` на свежем клоне выполните `make mock`.

`make gengraph` обновляет код по GraphQL-схеме, `make fmt` форматирует Go-код.

Для БД из `POSTGRES_*` или `DATABASE_URL`: `make migrate-up` применяет миграции, `make migrate-down` откатывает одну последнюю. Откат может удалить данные.

Все команды: `make help`.
