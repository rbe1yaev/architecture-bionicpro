// Package report собирает отчёт пользователя из готовой витрины за период, уже обработанный ETL.
package report

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const dateLayout = "2006-01-02"

// ErrInvalidPeriod — некорректный запрошенный период (HTTP 400).
var ErrInvalidPeriod = errors.New("invalid period")

type Status string

const (
	// StatusComplete — весь запрошенный период обработан.
	StatusComplete Status = "complete"
	// StatusPartial — конец периода позже processed_until, отчёт обрезан.
	StatusPartial Status = "partial"
	// StatusNotProcessed — за запрошенный период данных в витрине ещё нет.
	StatusNotProcessed Status = "not_processed"
)

// DailyRow — строка витрины reports_mart.
type DailyRow struct {
	Date             time.Time
	ProsthesisID     uint32
	Model            string
	SerialNumber     string
	EventsCnt        uint32
	AvgResponseMs    float32
	P95ResponseMs    float32
	MaxResponseMs    uint16
	AvgSignalQuality float32
	MinBatteryPct    uint8
	ErrorsCnt        uint32
	ActiveHours      uint8
}

type Period struct {
	From, To time.Time
}

type Store interface {
	// ProcessedUntil возвращает границу обработанного периода; ok=false, если ETL ещё не отработал.
	ProcessedUntil(ctx context.Context) (day time.Time, ok bool, err error)
	DailyRows(ctx context.Context, userID string, p Period) ([]DailyRow, error)
}

// Request — запрошенный период; nil означает «по умолчанию».
type Request struct {
	From, To *time.Time
}

type DateRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Summary struct {
	Days             int     `json:"days"`
	EventsCnt        uint64  `json:"events_cnt"`
	AvgResponseMs    float64 `json:"avg_response_ms"`
	MaxResponseMs    uint16  `json:"max_response_ms"`
	AvgSignalQuality float64 `json:"avg_signal_quality"`
	MinBatteryPct    uint8   `json:"min_battery_pct"`
	ErrorsCnt        uint64  `json:"errors_cnt"`
}

type Day struct {
	Date             string  `json:"date"`
	EventsCnt        uint32  `json:"events_cnt"`
	AvgResponseMs    float32 `json:"avg_response_ms"`
	P95ResponseMs    float32 `json:"p95_response_ms"`
	MaxResponseMs    uint16  `json:"max_response_ms"`
	AvgSignalQuality float32 `json:"avg_signal_quality"`
	MinBatteryPct    uint8   `json:"min_battery_pct"`
	ErrorsCnt        uint32  `json:"errors_cnt"`
	ActiveHours      uint8   `json:"active_hours"`
}

type Prosthesis struct {
	ProsthesisID uint32  `json:"prosthesis_id"`
	Model        string  `json:"model"`
	SerialNumber string  `json:"serial_number"`
	Summary      Summary `json:"summary"`
	Daily        []Day   `json:"daily"`
}

type Report struct {
	UserID         string       `json:"user_id"`
	Requested      DateRange    `json:"requested"`
	Period         *DateRange   `json:"period"`
	ProcessedUntil *string      `json:"processed_until"`
	Status         Status       `json:"status"`
	Prostheses     []Prosthesis `json:"prostheses"`
}

type Service struct {
	store       Store
	now         func() time.Time
	defaultDays int
	maxDays     int
}

func NewService(store Store, defaultDays, maxDays int) *Service {
	return &Service{store: store, now: time.Now, defaultDays: defaultDays, maxDays: maxDays}
}

// Build читает только готовые строки витрины; период обрезается по processed_until.
func (s *Service) Build(ctx context.Context, userID string, req Request) (Report, error) {
	until, processed, err := s.store.ProcessedUntil(ctx)
	if err != nil {
		return Report{}, err
	}

	to := yesterday(s.now())
	if processed {
		to = until
	}
	if req.To != nil {
		to = *req.To
	}
	from := to.AddDate(0, 0, -(s.defaultDays - 1))
	if req.From != nil {
		from = *req.From
	}
	if from.After(to) {
		return Report{}, fmt.Errorf("%w: from позже to", ErrInvalidPeriod)
	}
	if days(from, to) > s.maxDays {
		return Report{}, fmt.Errorf("%w: период длиннее %d дней", ErrInvalidPeriod, s.maxDays)
	}

	rep := Report{
		UserID:     userID,
		Requested:  DateRange{From: from.Format(dateLayout), To: to.Format(dateLayout)},
		Status:     StatusNotProcessed,
		Prostheses: []Prosthesis{},
	}
	if !processed || from.After(until) {
		if processed {
			rep.ProcessedUntil = ptr(until.Format(dateLayout))
		}
		return rep, nil
	}

	rep.ProcessedUntil = ptr(until.Format(dateLayout))
	rep.Status = StatusComplete
	if to.After(until) {
		to = until
		rep.Status = StatusPartial
	}
	rep.Period = &DateRange{From: from.Format(dateLayout), To: to.Format(dateLayout)}

	rows, err := s.store.DailyRows(ctx, userID, Period{From: from, To: to})
	if err != nil {
		return Report{}, err
	}
	rep.Prostheses = group(rows)
	return rep, nil
}

// group раскладывает строки (отсортированные по протезу и дате) по протезам и считает сводку.
func group(rows []DailyRow) []Prosthesis {
	out := []Prosthesis{}
	for _, r := range rows {
		if len(out) == 0 || out[len(out)-1].ProsthesisID != r.ProsthesisID {
			out = append(out, Prosthesis{
				ProsthesisID: r.ProsthesisID,
				Model:        r.Model,
				SerialNumber: r.SerialNumber,
				Summary:      Summary{MinBatteryPct: r.MinBatteryPct},
			})
		}
		p := &out[len(out)-1]
		p.Daily = append(p.Daily, Day{
			Date:             r.Date.Format(dateLayout),
			EventsCnt:        r.EventsCnt,
			AvgResponseMs:    r.AvgResponseMs,
			P95ResponseMs:    r.P95ResponseMs,
			MaxResponseMs:    r.MaxResponseMs,
			AvgSignalQuality: r.AvgSignalQuality,
			MinBatteryPct:    r.MinBatteryPct,
			ErrorsCnt:        r.ErrorsCnt,
			ActiveHours:      r.ActiveHours,
		})

		sm := &p.Summary
		sm.Days++
		// Средние за период — взвешенные по числу событий дня
		sm.AvgResponseMs += float64(r.AvgResponseMs) * float64(r.EventsCnt)
		sm.AvgSignalQuality += float64(r.AvgSignalQuality) * float64(r.EventsCnt)
		sm.EventsCnt += uint64(r.EventsCnt)
		sm.ErrorsCnt += uint64(r.ErrorsCnt)
		sm.MaxResponseMs = max(sm.MaxResponseMs, r.MaxResponseMs)
		sm.MinBatteryPct = min(sm.MinBatteryPct, r.MinBatteryPct)
	}
	for i := range out {
		sm := &out[i].Summary
		if sm.EventsCnt > 0 {
			sm.AvgResponseMs = round(sm.AvgResponseMs/float64(sm.EventsCnt), 1)
			sm.AvgSignalQuality = round(sm.AvgSignalQuality/float64(sm.EventsCnt), 3)
		}
	}
	return out
}

func yesterday(now time.Time) time.Time {
	y, m, d := now.UTC().AddDate(0, 0, -1).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func days(from, to time.Time) int {
	return int(to.Sub(from).Hours()/24) + 1
}

func round(v float64, digits int) float64 {
	p := 1.0
	for range digits {
		p *= 10
	}
	return float64(int64(v*p+0.5)) / p
}

func ptr[T any](v T) *T { return &v }
