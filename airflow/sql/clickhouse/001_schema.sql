-- OLAP-хранилище отчётов BionicPRO.
-- Выполняется при первом старте контейнера clickhouse (docker-entrypoint-initdb.d).

CREATE DATABASE IF NOT EXISTS reports;

-- Staging: снимок справочников CRM. Повторная загрузка заменяет строку по ключу (версия loaded_at).
CREATE TABLE IF NOT EXISTS reports.crm_clients
(
    client_id  UInt32,
    user_id    String,
    full_name  String,
    email      String,
    country    LowCardinality(String),
    loaded_at  DateTime
)
ENGINE = ReplacingMergeTree(loaded_at)
ORDER BY client_id;

CREATE TABLE IF NOT EXISTS reports.crm_prostheses
(
    prosthesis_id  UInt32,
    client_id      UInt32,
    model          LowCardinality(String),
    serial_number  String,
    installed_at   Date,
    loaded_at      DateTime
)
ENGINE = ReplacingMergeTree(loaded_at)
ORDER BY prosthesis_id;

-- Staging: сырая телеметрия по дням. Партиция = сутки, DAG перезаливает её целиком (DROP PARTITION + INSERT).
CREATE TABLE IF NOT EXISTS reports.telemetry_raw
(
    event_id        UInt64,
    prosthesis_id   UInt32,
    ts              DateTime('UTC'),
    movement_type   LowCardinality(String),
    response_ms     UInt16,
    signal_quality  Float32,
    battery_pct     UInt8,
    error_code      LowCardinality(String)
)
ENGINE = MergeTree
PARTITION BY toDate(ts)
ORDER BY (prosthesis_id, ts)
TTL toDate(ts) + INTERVAL 90 DAY DELETE;

-- Витрина для Reports API: 1 строка = пользователь × протез × день.
-- ORDER BY начинается с user_id: отчёт пользователя читается одним диапазоном первичного ключа.
-- Повторная сборка дня заменяет строки (версия loaded_at), API читает с FINAL.
CREATE TABLE IF NOT EXISTS reports.reports_mart
(
    user_id             String,
    report_date         Date,
    prosthesis_id       UInt32,
    model               LowCardinality(String),
    serial_number       String,
    client_name         String,
    country             LowCardinality(String),
    events_cnt          UInt32,
    avg_response_ms     Float32,
    p95_response_ms     Float32,
    max_response_ms     UInt16,
    avg_signal_quality  Float32,
    min_battery_pct     UInt8,
    errors_cnt          UInt32,
    active_hours        UInt8,
    loaded_at           DateTime
)
ENGINE = ReplacingMergeTree(loaded_at)
PARTITION BY toYYYYMM(report_date)
ORDER BY (user_id, prosthesis_id, report_date);

-- Граница обработанного периода: Reports API не отдаёт данные позже processed_until.
CREATE TABLE IF NOT EXISTS reports.etl_watermark
(
    pipeline         LowCardinality(String),
    processed_until  Date,
    updated_at       DateTime
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY pipeline;
