# reports-api — сервис отчётов BionicPRO (Go)

`GET /reports` отдаёт аутентифицированному пользователю отчёт о работе **его** протезов из готовой
витрины ClickHouse `reports.reports_mart` (её строит Airflow DAG из `../airflow`). Сервис не считает
агрегаты по сырой телеметрии — только читает строки витрины по ключу пользователя.

## Доступ

| Проверка | Ответ при нарушении |
|---|---|
| Заголовок `Authorization: Bearer <access token>` | `401` |
| Подпись RS256 по JWKS Keycloak (другие алгоритмы, в т.ч. HS256 и `none`, отклоняются) | `401` |
| `iss` = `OIDC_ISSUER`, `exp`/`nbf` (допуск 30 с), `typ = Bearer` (не ID token) | `401` |
| `azp` ∈ `OIDC_ALLOWED_AZP` (токен выдан клиенту `reports-frontend`) | `401` |
| Роль `prothetic_user` в `realm_access.roles` | `403` |

Пользователь определяется **только** по `preferred_username` из проверенного токена; параметра `user_id`
у эндпоинта нет, лишние параметры игнорируются.

Для продакшена строже проверять `aud`: добавить на клиенте `reports-frontend` Audience mapper
с `reports-api` и проверять его вместо/вместе с `azp`.

## Контракт

`GET /reports?from=YYYY-MM-DD&to=YYYY-MM-DD` — оба параметра необязательны; по умолчанию последние
30 дней, заканчивая `processed_until`. Неверная дата, `from > to` или период длиннее 366 дней → `400`.

Отчёт формируется только за период, уже обработанный Airflow (`etl_watermark.processed_until`):

| `status` | Когда | Что в ответе |
|---|---|---|
| `complete` | `to ≤ processed_until` | весь период |
| `partial` | `from ≤ processed_until < to` | `period.to` обрезан до `processed_until` |
| `not_processed` | `from > processed_until` или ETL ещё не запускался | `period: null`, `prostheses: []`, витрина не читается |

```json
{
  "user_id": "prothetic1",
  "requested": {"from": "2026-10-15", "to": "2026-10-25"},
  "period":    {"from": "2026-10-15", "to": "2026-10-18"},
  "processed_until": "2026-10-18",
  "status": "partial",
  "prostheses": [{
    "prosthesis_id": 101, "model": "BionicPRO Hand X2", "serial_number": "BP-HX2-000101",
    "summary": {"days": 4, "events_cnt": 481, "avg_response_ms": 79.1, "max_response_ms": 119,
                "avg_signal_quality": 0.8, "min_battery_pct": 22, "errors_cnt": 6},
    "daily": [{"date": "2026-10-15", "events_cnt": 144, "avg_response_ms": 78.2, "p95_response_ms": 115,
               "max_response_ms": 119, "avg_signal_quality": 0.79, "min_battery_pct": 23,
               "errors_cnt": 1, "active_hours": 24}]
  }]
}
```

Ошибка ClickHouse → `503` (подробности только в логе). `GET /healthz` — без авторизации, пингует ClickHouse.
CORS: разрешены источники из `CORS_ALLOWED_ORIGINS`, preflight `OPTIONS` не требует токена.

## Конфигурация

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `ADDR` | `:8000` | адрес HTTP-сервера |
| `CLICKHOUSE_ADDR` | `localhost:9000` | ClickHouse (native) |
| `CLICKHOUSE_DATABASE` / `CLICKHOUSE_USER` / `CLICKHOUSE_PASSWORD` | `reports` / `reports_api` / — | пользователь только с `SELECT` на `reports_mart` и `etl_watermark` (`../airflow/sql/clickhouse/002_reports_api_user.sql`) |
| `OIDC_ISSUER` | — (обязательно) | ожидаемый `iss`: адрес Keycloak, как его видит браузер |
| `OIDC_JWKS_URL` | — (обязательно) | JWKS Keycloak (адрес внутри docker-сети) |
| `OIDC_ALLOWED_AZP` | `reports-frontend` | разрешённые клиенты, через запятую |
| `REQUIRED_ROLE` | `prothetic_user` | роль realm для доступа к отчёту |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:3000` | через запятую |
| `DEFAULT_PERIOD_DAYS` / `MAX_PERIOD_DAYS` | `30` / `366` | период по умолчанию и максимальный |

## Структура

```
cmd/reports-api/main.go        конфиг, подключения, graceful shutdown
internal/config                переменные окружения
internal/auth                  проверка JWT, Identity в context
internal/report/service.go     период, обрезка по processed_until, сводка
internal/report/clickhouse.go  запросы к витрине (серверные параметры, FINAL)
internal/httpapi               маршруты, аутентификация, CORS, JSON-ошибки
```

## Запуск и проверка

```bash
docker compose up -d --build keycloak_db keycloak clickhouse reports-api   # из корня репозитория

TOKEN=$(curl -s -X POST localhost:8080/realms/reports-realm/protocol/openid-connect/token \
  -d grant_type=password -d client_id=reports-frontend -d username=prothetic1 -d password=prothetic123 \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["access_token"])')

curl -i localhost:8000/reports                                          # 401
curl -s -H "Authorization: Bearer $TOKEN" 'localhost:8000/reports?from=2026-10-10'
```
