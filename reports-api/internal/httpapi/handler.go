// Package httpapi — HTTP-слой сервиса отчётов: маршруты, проверка токена, CORS, JSON-ответы.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"bionicpro/reports-api/internal/auth"
	"bionicpro/reports-api/internal/report"
)

type Verifier interface {
	Verify(raw string) (auth.Identity, error)
}

type ReportBuilder interface {
	Build(ctx context.Context, userID string, req report.Request) (report.Report, error)
}

type Pinger interface {
	Ping(ctx context.Context) error
}

type API struct {
	reports     ReportBuilder
	verifier    Verifier
	db          Pinger
	corsOrigins []string
	log         *slog.Logger
}

func New(reports ReportBuilder, verifier Verifier, db Pinger, corsOrigins []string, log *slog.Logger) *API {
	return &API{reports: reports, verifier: verifier, db: db, corsOrigins: corsOrigins, log: log}
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /reports", a.authenticate(http.HandlerFunc(a.getReport)))
	mux.HandleFunc("GET /healthz", a.healthz)
	return a.cors(a.accessLog(mux))
}

// getReport отдаёт отчёт только пользователю из токена; параметра user_id у эндпоинта нет.
func (a *API) getReport(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.FromContext(r.Context())

	req, err := parseRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	rep, err := a.reports.Build(r.Context(), id.Username, req)
	switch {
	case errors.Is(err, report.ErrInvalidPeriod):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	case err != nil:
		a.log.ErrorContext(r.Context(), "построение отчёта", "user", id.Username, "error", err)
		writeError(w, http.StatusServiceUnavailable, "unavailable", "хранилище отчётов недоступно")
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (a *API) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.db.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "clickhouse недоступен")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || raw == "" {
			w.Header().Set("WWW-Authenticate", `Bearer`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "требуется Bearer-токен")
			return
		}
		id, err := a.verifier.Verify(raw)
		switch {
		case errors.Is(err, auth.ErrForbidden):
			a.log.InfoContext(r.Context(), "доступ запрещён", "user", id.Username, "reason", err)
			writeError(w, http.StatusForbidden, "forbidden", "недостаточно прав для получения отчёта")
			return
		case err != nil:
			a.log.InfoContext(r.Context(), "токен отклонён", "reason", err)
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "токен невалиден")
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), id)))
	})
}

func (a *API) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && slices.Contains(a.corsOrigins, origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization")
			h.Set("Access-Control-Max-Age", "600")
		}
		// Preflight браузера не несёт токен — отвечаем до проверки авторизации
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		a.log.InfoContext(r.Context(), "http", "method", r.Method, "path", r.URL.Path,
			"status", rec.status, "duration_ms", time.Since(start).Milliseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func parseRequest(r *http.Request) (report.Request, error) {
	var req report.Request
	q := r.URL.Query()
	for name, dst := range map[string]**time.Time{"from": &req.From, "to": &req.To} {
		v := q.Get(name)
		if v == "" {
			continue
		}
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return report.Request{}, errors.New(name + ": ожидается дата в формате YYYY-MM-DD")
		}
		*dst = &t
	}
	return req, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}
