import React, { useState } from 'react';
import { useKeycloak } from '@react-keycloak/web';
import { fetchReport, Report, ReportApiError } from '../api/reports';
import ReportView from './ReportView';

const ReportPage: React.FC = () => {
  const { keycloak, initialized } = useKeycloak();
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [report, setReport] = useState<Report | null>(null);

  const loadReport = async () => {
    try {
      setLoading(true);
      setError(null);
      setReport(null);

      // Обновляем access token, если он истекает в ближайшие 30 секунд
      try {
        await keycloak.updateToken(30);
      } catch {
        keycloak.login();
        return;
      }

      setReport(await fetchReport(keycloak.token!, { from, to }));
    } catch (err) {
      if (err instanceof ReportApiError && err.status === 401) {
        keycloak.login();
        return;
      }
      setError(err instanceof Error ? err.message : 'Не удалось получить отчёт');
    } finally {
      setLoading(false);
    }
  };

  if (!initialized) {
    return <div>Загрузка...</div>;
  }

  if (!keycloak.authenticated) {
    return (
      <div className="flex flex-col items-center justify-center min-h-screen bg-gray-100">
        <button
          onClick={() => keycloak.login()}
          className="px-4 py-2 bg-blue-500 text-white rounded hover:bg-blue-600"
        >
          Войти
        </button>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gray-100 py-8 px-4">
      <div className="max-w-5xl mx-auto p-8 bg-white rounded-lg shadow-md">
        <div className="flex items-center justify-between mb-6">
          <h1 className="text-2xl font-bold">Отчёт о работе протеза</h1>
          <div className="text-sm text-gray-600">
            {keycloak.tokenParsed?.preferred_username}
            <button onClick={() => keycloak.logout()} className="ml-3 text-blue-600 hover:underline">
              Выйти
            </button>
          </div>
        </div>

        <div className="flex flex-wrap items-end gap-4 mb-6">
          <label className="text-sm">
            <span className="block text-gray-600 mb-1">С</span>
            <input type="date" value={from} onChange={(e) => setFrom(e.target.value)} className="border rounded px-2 py-1" />
          </label>
          <label className="text-sm">
            <span className="block text-gray-600 mb-1">По</span>
            <input type="date" value={to} onChange={(e) => setTo(e.target.value)} className="border rounded px-2 py-1" />
          </label>
          <button
            onClick={loadReport}
            disabled={loading}
            className={`px-4 py-2 bg-blue-500 text-white rounded hover:bg-blue-600 ${
              loading ? 'opacity-50 cursor-not-allowed' : ''
            }`}
          >
            {loading ? 'Формируем отчёт...' : 'Получить отчёт'}
          </button>
          <span className="text-sm text-gray-500">Без дат — последние 30 обработанных дней</span>
        </div>

        {error && <div className="mb-4 p-4 bg-red-100 text-red-700 rounded">{error}</div>}

        {report && <ReportView report={report} />}
      </div>
    </div>
  );
};

export default ReportPage;
