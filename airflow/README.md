# Airflow: ETL витрины отчётов BionicPRO

DAG `bionicpro_reports_etl` объединяет данные CRM и телеметрию протезов и строит в ClickHouse
витрину `reports.reports_mart`, из которой Reports API отдаёт готовый отчёт пользователю.

## Состав

| Путь | Назначение |
|---|---|
| `dags/bionicpro_reports_etl.py` | DAG |
| `sql/clickhouse/001_schema.sql` | staging-таблицы, витрина, watermark (init ClickHouse) |
| `sql/crm/init.sql` | эмуляция CRM DB: клиенты и протезы |
| `sql/telemetry/init.sql` | эмуляция БД телеметрии: события за последние 35 дней |
| `Dockerfile`, `requirements.txt` | образ Airflow 2.10.5 + `clickhouse-connect` |

## Расписание и обработанный период

- `@daily`: каждый запуск обрабатывает закрытые сутки `[data_interval_start, data_interval_end)`, т.е. «вчера».
- `catchup=True` с `start_date` = сегодня − 30 дней: при первом включении DAG догружает историю.
- `max_active_runs=1`, `retries=2`.
- `etl_watermark.processed_until` сдвигается только после успешных проверок качества;
  у `update_watermark` стоит `depends_on_past=True`, поэтому граница не перескакивает через упавший день.
  Reports API не отдаёт данные позже `processed_until`.

## Задачи

```
extract_crm ─────────┐
                     ├─► build_mart ─► check_quality ─► update_watermark
extract_telemetry ───┘
```

1. `extract_crm`: полный снимок `crm.clients` и `crm.prostheses` в `crm_clients` / `crm_prostheses` (`ReplacingMergeTree`).
2. `extract_telemetry`: события за сутки серверным курсором пачками по 50 000 в `telemetry_raw`;
   партиция дня перед загрузкой удаляется (`DROP PARTITION`), поэтому перезапуск не задваивает данные.
3. `build_mart`: агрегация по `(user_id, prosthesis_id, день)` с `JOIN` на CRM: количество событий,
   среднее / p95 / максимум времени отклика, качество сигнала, минимальный заряд, ошибки, активные часы.
4. `check_quality`: число событий в staging равно выгруженному из источника; в витрине нет пустых `user_id`;
   события в витрине плюс события протезов без клиента в CRM («сироты», пишутся в лог) равны staging.
5. `update_watermark`: `processed_until = greatest(ds, текущее значение)`.

## Витрина

```sql
ENGINE = ReplacingMergeTree(loaded_at)
PARTITION BY toYYYYMM(report_date)
ORDER BY (user_id, prosthesis_id, report_date)
```

Одна строка — пользователь × протез × день. `user_id` = `preferred_username` пользователя Keycloak
(`prothetic1`…), по нему же Reports API фильтрует данные, беря значение из проверенного JWT.
Ключ сортировки начинается с `user_id`, поэтому отчёт пользователя читается одним диапазоном первичного ключа.
Повторная сборка дня заменяет строки по ключу; читать витрину нужно с `FINAL`.

## Запуск

```bash
docker compose up -d --build crm_db telemetry_db clickhouse airflow_db airflow-init airflow-webserver airflow-scheduler
```

- Airflow UI: http://localhost:8081 (`admin` / `admin`). DAG создаётся на паузе: включить его в UI или командой
  `docker compose exec airflow-scheduler airflow dags unpause bionicpro_reports_etl`.
- ClickHouse: `localhost:8123`, база `reports`, пользователь `airflow` / `airflow_password`.
- Источники: CRM DB `localhost:5434`, БД телеметрии `localhost:5435` (PostgreSQL 16.0).

## Проверка

```bash
docker compose exec clickhouse clickhouse-client -u airflow --password airflow_password -d reports -q "
  SELECT user_id, prosthesis_id, count() AS days, min(report_date), max(report_date), sum(events_cnt)
  FROM reports_mart FINAL GROUP BY 1, 2 ORDER BY 1, 2"

docker compose exec clickhouse clickhouse-client -u airflow --password airflow_password -d reports -q "
  SELECT * FROM etl_watermark FINAL"
```

Ожидается: по 30 дней для протезов 101, 102 (`prothetic1`), 201 (`prothetic2`), 301 (`prothetic3`);
протез 999 в витрину не попадает; `processed_until` = вчера.
