// Клиент API отчётов (reports-api, GET /reports).

export type ReportStatus = 'complete' | 'partial' | 'not_processed';

export interface DateRange {
  from: string;
  to: string;
}

export interface ReportSummary {
  days: number;
  events_cnt: number;
  avg_response_ms: number;
  max_response_ms: number;
  avg_signal_quality: number;
  min_battery_pct: number;
  errors_cnt: number;
}

export interface ReportDay {
  date: string;
  events_cnt: number;
  avg_response_ms: number;
  p95_response_ms: number;
  max_response_ms: number;
  avg_signal_quality: number;
  min_battery_pct: number;
  errors_cnt: number;
  active_hours: number;
}

export interface ReportProsthesis {
  prosthesis_id: number;
  model: string;
  serial_number: string;
  summary: ReportSummary;
  daily: ReportDay[];
}

export interface Report {
  user_id: string;
  requested: DateRange;
  period: DateRange | null;
  processed_until: string | null;
  status: ReportStatus;
  prostheses: ReportProsthesis[];
}

export class ReportApiError extends Error {
  constructor(public readonly status: number, message: string) {
    super(message);
    this.name = 'ReportApiError';
  }
}

export interface ReportQuery {
  from?: string;
  to?: string;
}

export async function fetchReport(token: string, query: ReportQuery): Promise<Report> {
  const params = new URLSearchParams();
  if (query.from) params.set('from', query.from);
  if (query.to) params.set('to', query.to);
  const qs = params.toString();

  let response: Response;
  try {
    response = await fetch(`${process.env.REACT_APP_API_URL}/reports${qs ? `?${qs}` : ''}`, {
      headers: { Authorization: `Bearer ${token}` },
    });
  } catch {
    throw new ReportApiError(0, 'Сервис отчётов недоступен. Проверьте подключение и повторите попытку.');
  }

  if (response.ok) {
    return (await response.json()) as Report;
  }

  let message = '';
  try {
    message = ((await response.json()) as { message?: string }).message ?? '';
  } catch {
    // тело ошибки не JSON — используем текст по коду ответа
  }
  throw new ReportApiError(response.status, errorText(response.status, message));
}

function errorText(status: number, message: string): string {
  switch (status) {
    case 400:
      return `Некорректный период: ${message || 'проверьте даты'}`;
    case 401:
      return 'Сессия истекла. Войдите снова.';
    case 403:
      return 'Нет доступа к отчётам: отчёт доступен только пользователям протезов.';
    default:
      return 'Сервис отчётов временно недоступен. Повторите попытку позже.';
  }
}
