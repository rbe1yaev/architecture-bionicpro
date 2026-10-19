package report

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const pipeline = "reports_mart"

// ClickHouseStore читает витрину reports_mart и etl_watermark (пользователь ClickHouse только с SELECT).
type ClickHouseStore struct {
	conn driver.Conn
}

func NewClickHouseStore(conn driver.Conn) *ClickHouseStore {
	return &ClickHouseStore{conn: conn}
}

func (s *ClickHouseStore) Ping(ctx context.Context) error {
	return s.conn.Ping(ctx)
}

func (s *ClickHouseStore) ProcessedUntil(ctx context.Context) (time.Time, bool, error) {
	ctx = clickhouse.Context(ctx, clickhouse.WithParameters(clickhouse.Parameters{"pipeline": pipeline}))
	var day time.Time
	err := s.conn.QueryRow(ctx,
		"SELECT max(processed_until) FROM etl_watermark FINAL WHERE pipeline = {pipeline:String}",
	).Scan(&day)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("чтение etl_watermark: %w", err)
	}
	// max по пустой таблице в ClickHouse даёт 1970-01-01: ETL ещё ни разу не отработал
	if day.Year() <= 1970 {
		return time.Time{}, false, nil
	}
	return day, true, nil
}

func (s *ClickHouseStore) DailyRows(ctx context.Context, userID string, p Period) ([]DailyRow, error) {
	// Серверные параметры: значения не подставляются в текст запроса
	ctx = clickhouse.Context(ctx, clickhouse.WithParameters(clickhouse.Parameters{
		"user_id": userID,
		"from":    p.From.Format(dateLayout),
		"to":      p.To.Format(dateLayout),
	}))
	rows, err := s.conn.Query(ctx, `
		SELECT report_date, prosthesis_id, model, serial_number, events_cnt, avg_response_ms,
		       p95_response_ms, max_response_ms, avg_signal_quality, min_battery_pct, errors_cnt, active_hours
		FROM reports_mart FINAL
		WHERE user_id = {user_id:String}
		  AND report_date BETWEEN {from:Date} AND {to:Date}
		ORDER BY prosthesis_id, report_date`)
	if err != nil {
		return nil, fmt.Errorf("чтение reports_mart: %w", err)
	}
	defer rows.Close()

	var out []DailyRow
	for rows.Next() {
		var r DailyRow
		if err := rows.Scan(&r.Date, &r.ProsthesisID, &r.Model, &r.SerialNumber, &r.EventsCnt, &r.AvgResponseMs,
			&r.P95ResponseMs, &r.MaxResponseMs, &r.AvgSignalQuality, &r.MinBatteryPct, &r.ErrorsCnt, &r.ActiveHours,
		); err != nil {
			return nil, fmt.Errorf("чтение строки reports_mart: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
