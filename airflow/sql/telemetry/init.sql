-- Эмуляция основной БД телеметрии протезов.
-- Генерирует события за последние 35 дней (каждые 10 минут на протез),
-- чтобы catchup DAG сразу находил данные.

CREATE SCHEMA IF NOT EXISTS telemetry;

CREATE TABLE telemetry.events (
    event_id        bigserial   PRIMARY KEY,
    prosthesis_id   integer     NOT NULL,
    ts              timestamptz NOT NULL,
    movement_type   text        NOT NULL,
    response_ms     integer     NOT NULL,
    signal_quality  real        NOT NULL,
    battery_pct     smallint    NOT NULL,
    error_code      text
);

CREATE INDEX events_ts_idx ON telemetry.events (ts);

SELECT setseed(0.42);

-- 999 — протез без клиента в CRM: в витрину не попадает, DAG логирует его как «сироту»
INSERT INTO telemetry.events (prosthesis_id, ts, movement_type, response_ms, signal_quality, battery_pct, error_code)
SELECT
    p.prosthesis_id,
    g.ts,
    (ARRAY['grip', 'pinch', 'release', 'rotate', 'point'])[1 + floor(random() * 5)::int],
    40 + floor(random() * 80)::int,
    round((0.6 + random() * 0.4)::numeric, 3)::real,
    (100 - extract(hour FROM g.ts) * 3 - floor(random() * 10))::smallint,
    CASE WHEN random() < 0.01 THEN (ARRAY['E_SENSOR_NOISE', 'E_ACTUATOR_STALL', 'E_LOW_BATTERY'])[1 + floor(random() * 3)::int] END
FROM (VALUES (101), (102), (201), (301), (999)) AS p (prosthesis_id)
CROSS JOIN generate_series(
    date_trunc('day', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' - interval '35 days',
    now(),
    interval '10 minutes'
) AS g (ts);
