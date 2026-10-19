-- Пользователь сервиса отчётов: только чтение витрины и границы обработанного периода.
CREATE USER IF NOT EXISTS reports_api IDENTIFIED WITH sha256_password BY 'reports_api_password'
    DEFAULT DATABASE reports;

GRANT SELECT ON reports.reports_mart TO reports_api;
GRANT SELECT ON reports.etl_watermark TO reports_api;
