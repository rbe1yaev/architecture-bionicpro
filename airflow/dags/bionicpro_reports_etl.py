"""ETL витрины отчётов BionicPRO: CRM + телеметрия → ClickHouse (reports.reports_mart).

Каждый запуск обрабатывает закрытые сутки [data_interval_start, data_interval_end),
то есть «вчера» относительно времени запуска. После успешной сборки и проверок
сдвигается etl_watermark — Reports API отдаёт отчёт только до этой даты.
"""

from __future__ import annotations

import logging
from datetime import timedelta

import clickhouse_connect
import pendulum
from airflow.decorators import dag, task
from airflow.exceptions import AirflowFailException
from airflow.hooks.base import BaseHook
from airflow.providers.postgres.hooks.postgres import PostgresHook

log = logging.getLogger(__name__)

CRM_CONN_ID = "crm_db"
TELEMETRY_CONN_ID = "telemetry_db"
CLICKHOUSE_CONN_ID = "clickhouse_reports"
PIPELINE = "reports_mart"
BATCH_SIZE = 50_000

TELEMETRY_COLUMNS = [
    "event_id", "prosthesis_id", "ts", "movement_type",
    "response_ms", "signal_quality", "battery_pct", "error_code",
]


def clickhouse_client():
    conn = BaseHook.get_connection(CLICKHOUSE_CONN_ID)
    return clickhouse_connect.get_client(
        host=conn.host,
        port=conn.port or 8123,
        username=conn.login,
        password=conn.password or "",
        database=conn.schema or "reports",
    )


@dag(
    dag_id="bionicpro_reports_etl",
    description="CRM + телеметрия → витрина отчётов в ClickHouse",
    schedule="@daily",
    # Учебный стенд: seed телеметрии строится относительно текущей даты, поэтому
    # и catchup начинаем за 30 дней до сегодняшнего дня.
    start_date=pendulum.today("UTC").subtract(days=30),
    catchup=True,
    max_active_runs=1,
    default_args={"retries": 2, "retry_delay": timedelta(minutes=5)},
    tags=["bionicpro", "reports"],
)
def bionicpro_reports_etl():
    @task
    def extract_crm(data_interval_end=None) -> dict:
        """Полный снимок клиентов и протезов CRM (справочник небольшой)."""
        hook = PostgresHook(postgres_conn_id=CRM_CONN_ID)
        loaded_at = data_interval_end.replace(tzinfo=None)

        clients = hook.get_records(
            "SELECT client_id, user_id, full_name, email, country FROM crm.clients"
        )
        prostheses = hook.get_records(
            "SELECT prosthesis_id, client_id, model, serial_number, installed_at FROM crm.prostheses"
        )

        ch = clickhouse_client()
        ch.insert(
            "crm_clients",
            [(*row, loaded_at) for row in clients],
            column_names=["client_id", "user_id", "full_name", "email", "country", "loaded_at"],
        )
        ch.insert(
            "crm_prostheses",
            [(*row, loaded_at) for row in prostheses],
            column_names=["prosthesis_id", "client_id", "model", "serial_number", "installed_at", "loaded_at"],
        )
        log.info("CRM: клиентов %d, протезов %d", len(clients), len(prostheses))
        return {"clients": len(clients), "prostheses": len(prostheses)}

    @task
    def extract_telemetry(ds=None, data_interval_start=None, data_interval_end=None) -> int:
        """Инкрементальная выгрузка телеметрии за сутки в партицию telemetry_raw."""
        ch = clickhouse_client()
        # Перезапуск дня не должен задваивать данные: партиция дня перезаливается целиком
        ch.command(f"ALTER TABLE telemetry_raw DROP PARTITION '{ds}'")

        hook = PostgresHook(postgres_conn_id=TELEMETRY_CONN_ID)
        conn = hook.get_conn()
        total = 0
        try:
            # Именованный (серверный) курсор: день телеметрии не загружается в память целиком
            with conn.cursor(name="telemetry_day") as cur:
                cur.execute(
                    """
                    SELECT event_id, prosthesis_id, ts, movement_type, response_ms,
                           signal_quality, battery_pct, coalesce(error_code, '')
                    FROM telemetry.events
                    WHERE ts >= %s AND ts < %s
                    """,
                    (data_interval_start, data_interval_end),
                )
                while rows := cur.fetchmany(BATCH_SIZE):
                    ch.insert("telemetry_raw", rows, column_names=TELEMETRY_COLUMNS)
                    total += len(rows)
        finally:
            conn.close()

        log.info("Телеметрия за %s: выгружено %d событий", ds, total)
        return total

    @task
    def build_mart(ds=None) -> None:
        """Агрегация телеметрии в разрезе пользователя и протеза + данные клиента из CRM."""
        ch = clickhouse_client()
        ch.command(
            """
            INSERT INTO reports_mart
            SELECT
                c.user_id,
                toDate(t.ts)                              AS report_date,
                t.prosthesis_id,
                p.model,
                p.serial_number,
                c.full_name                               AS client_name,
                c.country,
                count()                                   AS events_cnt,
                round(avg(t.response_ms), 1)              AS avg_response_ms,
                round(quantile(0.95)(t.response_ms), 1)   AS p95_response_ms,
                max(t.response_ms)                        AS max_response_ms,
                round(avg(t.signal_quality), 3)           AS avg_signal_quality,
                min(t.battery_pct)                        AS min_battery_pct,
                countIf(t.error_code != '')               AS errors_cnt,
                uniqExact(toStartOfHour(t.ts))            AS active_hours,
                now()                                     AS loaded_at
            FROM telemetry_raw AS t
            INNER JOIN (SELECT * FROM crm_prostheses FINAL) AS p ON p.prosthesis_id = t.prosthesis_id
            INNER JOIN (SELECT * FROM crm_clients FINAL) AS c ON c.client_id = p.client_id
            WHERE toDate(t.ts) = {ds:Date}
            GROUP BY c.user_id, report_date, t.prosthesis_id, p.model, p.serial_number, c.full_name, c.country
            """,
            parameters={"ds": ds},
        )

    @task
    def check_quality(raw_events: int, ds=None) -> None:
        """Сверка: всё, что выгружено, либо попало в витрину, либо отброшено как «сирота»."""
        ch = clickhouse_client()
        staged = ch.command("SELECT count() FROM telemetry_raw WHERE toDate(ts) = {ds:Date}", parameters={"ds": ds})
        mart_events, empty_users = ch.query(
            """
            SELECT sum(events_cnt), countIf(user_id = '')
            FROM reports_mart FINAL
            WHERE report_date = {ds:Date}
            """,
            parameters={"ds": ds},
        ).result_rows[0]
        orphans = ch.query(
            """
            SELECT prosthesis_id, count()
            FROM telemetry_raw
            WHERE toDate(ts) = {ds:Date}
              AND prosthesis_id NOT IN (SELECT prosthesis_id FROM crm_prostheses FINAL)
            GROUP BY prosthesis_id
            """,
            parameters={"ds": ds},
        ).result_rows
        orphan_events = sum(cnt for _, cnt in orphans)

        if orphans:
            log.warning("Телеметрия протезов без клиента в CRM за %s: %s", ds, dict(orphans))
        if staged != raw_events:
            raise AirflowFailException(f"В staging {staged} событий, из источника выгружено {raw_events}")
        if empty_users:
            raise AirflowFailException(f"В витрине {empty_users} строк без user_id")
        if mart_events + orphan_events != staged:
            raise AirflowFailException(
                f"Витрина {mart_events} + сироты {orphan_events} != staging {staged}"
            )
        log.info("Проверки за %s пройдены: в витрине %d событий", ds, mart_events)

    # depends_on_past: watermark не перескакивает через день, который не удалось обработать
    @task(depends_on_past=True)
    def update_watermark(ds=None) -> None:
        ch = clickhouse_client()
        # greatest: перезапуск старого дня не откатывает watermark назад
        # (max по пустой таблице в ClickHouse даёт 1970-01-01, а не NULL)
        ch.command(
            """
            INSERT INTO etl_watermark
            SELECT {p:String}, greatest({ds:Date}, max(processed_until)), now()
            FROM etl_watermark FINAL
            WHERE pipeline = {p:String}
            """,
            parameters={"p": PIPELINE, "ds": ds},
        )
        processed_until = ch.command(
            "SELECT max(processed_until) FROM etl_watermark FINAL WHERE pipeline = {p:String}",
            parameters={"p": PIPELINE},
        )
        log.info("etl_watermark[%s] = %s", PIPELINE, processed_until)

    crm = extract_crm()
    raw_events = extract_telemetry()
    mart = build_mart()
    [crm, raw_events] >> mart
    mart >> check_quality(raw_events) >> update_watermark()


bionicpro_reports_etl()
