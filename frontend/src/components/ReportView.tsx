import React from 'react';
import { Report, ReportProsthesis } from '../api/reports';

const formatDate = (iso: string) => new Date(`${iso}T00:00:00Z`).toLocaleDateString('ru-RU', { timeZone: 'UTC' });

const StatusBanner: React.FC<{ report: Report }> = ({ report }) => {
  const until = report.processed_until ? formatDate(report.processed_until) : null;

  if (report.status === 'not_processed') {
    return (
      <div className="p-4 bg-yellow-100 text-yellow-800 rounded">
        Данные за выбранный период ещё не обработаны.
        {until ? ` Отчёт доступен за период по ${until} включительно.` : ' Подготовка отчётов ещё не выполнялась.'}
      </div>
    );
  }
  if (report.status === 'partial') {
    return (
      <div className="p-4 bg-yellow-100 text-yellow-800 rounded">
        Отчёт сформирован частично: данные обработаны по {until} включительно, остаток периода появится после
        очередной обработки.
      </div>
    );
  }
  return null;
};

const ProsthesisCard: React.FC<{ prosthesis: ReportProsthesis }> = ({ prosthesis: p }) => (
  <div className="border rounded-lg p-4">
    <h3 className="text-lg font-semibold">
      {p.model} <span className="text-gray-500 font-normal">№ {p.serial_number}</span>
    </h3>

    <dl className="grid grid-cols-2 md:grid-cols-4 gap-3 my-4 text-sm">
      <Metric label="Дней с данными" value={p.summary.days} />
      <Metric label="Движений" value={p.summary.events_cnt.toLocaleString('ru-RU')} />
      <Metric label="Средний отклик" value={`${p.summary.avg_response_ms} мс`} />
      <Metric label="Макс. отклик" value={`${p.summary.max_response_ms} мс`} />
      <Metric label="Качество сигнала" value={`${Math.round(p.summary.avg_signal_quality * 100)}%`} />
      <Metric label="Мин. заряд" value={`${p.summary.min_battery_pct}%`} />
      <Metric label="Ошибок" value={p.summary.errors_cnt} />
    </dl>

    <div className="overflow-x-auto">
      <table className="min-w-full text-sm">
        <thead>
          <tr className="text-left text-gray-500 border-b">
            <th className="py-1 pr-4">Дата</th>
            <th className="py-1 pr-4">Движений</th>
            <th className="py-1 pr-4">Отклик, мс (сред. / p95 / макс.)</th>
            <th className="py-1 pr-4">Сигнал</th>
            <th className="py-1 pr-4">Мин. заряд</th>
            <th className="py-1 pr-4">Ошибок</th>
            <th className="py-1">Активных часов</th>
          </tr>
        </thead>
        <tbody>
          {p.daily.map((d) => (
            <tr key={d.date} className="border-b last:border-0">
              <td className="py-1 pr-4">{formatDate(d.date)}</td>
              <td className="py-1 pr-4">{d.events_cnt}</td>
              <td className="py-1 pr-4">
                {d.avg_response_ms} / {d.p95_response_ms} / {d.max_response_ms}
              </td>
              <td className="py-1 pr-4">{Math.round(d.avg_signal_quality * 100)}%</td>
              <td className="py-1 pr-4">{d.min_battery_pct}%</td>
              <td className="py-1 pr-4">{d.errors_cnt}</td>
              <td className="py-1">{d.active_hours}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  </div>
);

const Metric: React.FC<{ label: string; value: React.ReactNode }> = ({ label, value }) => (
  <div>
    <dt className="text-gray-500">{label}</dt>
    <dd className="font-semibold">{value}</dd>
  </div>
);

// CSV с дневными показателями по всем протезам (разделитель «;» — так файл корректно открывает Excel в ru-локали)
function downloadCsv(report: Report) {
  const header = [
    'Протез', 'Серийный номер', 'Дата', 'Движений', 'Средний отклик, мс', 'p95 отклика, мс',
    'Макс. отклик, мс', 'Качество сигнала', 'Мин. заряд, %', 'Ошибок', 'Активных часов',
  ];
  const rows = report.prostheses.flatMap((p) =>
    p.daily.map((d) => [
      p.model, p.serial_number, d.date, d.events_cnt, d.avg_response_ms, d.p95_response_ms,
      d.max_response_ms, d.avg_signal_quality, d.min_battery_pct, d.errors_cnt, d.active_hours,
    ]),
  );
  const csv = [header, ...rows].map((r) => r.join(';')).join('\r\n');
  const blob = new Blob(['﻿' + csv], { type: 'text/csv;charset=utf-8' });

  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `bionicpro-report-${report.period?.from}_${report.period?.to}.csv`;
  a.click();
  URL.revokeObjectURL(url);
}

const ReportView: React.FC<{ report: Report }> = ({ report }) => (
  <div className="space-y-4">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <div className="text-gray-700">
        {report.period
          ? `Период: ${formatDate(report.period.from)} — ${formatDate(report.period.to)}`
          : `Запрошен период: ${formatDate(report.requested.from)} — ${formatDate(report.requested.to)}`}
      </div>
      {report.prostheses.length > 0 && (
        <button
          onClick={() => downloadCsv(report)}
          className="px-3 py-1 border border-blue-500 text-blue-600 rounded hover:bg-blue-50"
        >
          Скачать CSV
        </button>
      )}
    </div>

    <StatusBanner report={report} />

    {report.status !== 'not_processed' && report.prostheses.length === 0 && (
      <div className="p-4 bg-gray-100 text-gray-700 rounded">За выбранный период данных о протезах нет.</div>
    )}

    {report.prostheses.map((p) => (
      <ProsthesisCard key={p.prosthesis_id} prosthesis={p} />
    ))}
  </div>
);

export default ReportView;
