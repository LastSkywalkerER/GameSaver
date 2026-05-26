import { useEffect, useState } from "react";
import { api, formatBytes, formatDate, type DeepScanResult } from "../api";

export function DeepScanDialog({
  gameId,
  onClose,
  onChanged,
}: {
  gameId: string;
  onClose: () => void;
  onChanged: () => void;
}) {
  const [loading, setLoading] = useState(true);
  const [results, setResults] = useState<DeepScanResult[]>([]);
  const [busy, setBusy] = useState<string | null>(null);

  useEffect(() => {
    api.DeepScanGame(gameId)
      .then((r) => setResults((r as any) ?? []))
      .finally(() => setLoading(false));
  }, [gameId]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  async function attach(path: string) {
    setBusy(path);
    try {
      await api.AddManualSaveLocation(gameId, path);
      onChanged();
    } catch (e) {
      api.Toast("error", "Не удалось добавить: " + String(e));
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm" onClick={onClose}>
      <div
        className="card flex h-[70vh] w-[760px] max-w-[95vw] flex-col overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 border-b border-border p-3">
          <h3 className="flex-1 text-sm font-semibold uppercase tracking-wide text-muted">
            Глубокий скан сейвов
          </h3>
          <button className="btn btn-ghost" onClick={onClose}>✕</button>
        </div>
        <div className="flex-1 overflow-y-auto">
          {loading && <div className="p-4 text-sm text-muted">Сканирую Saved Games, My Games, AppData…</div>}
          {!loading && results.length === 0 && (
            <div className="p-4 text-sm text-muted">Кандидатов не найдено. Попробуй «Перевыбрать игру».</div>
          )}
          <ul>
            {results.map((r) => (
              <li key={r.path} className="border-b border-border px-4 py-2 hover:bg-card">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm text-gray-100" title={r.path}>{r.path}</div>
                    <div className="mt-0.5 text-xs text-muted">
                      {r.fileCount} files · {formatBytes(r.sizeBytes)} · {formatDate(r.mtime)} · score {r.score} · {r.reason}
                    </div>
                  </div>
                  <button
                    className="btn btn-primary"
                    disabled={busy === r.path}
                    onClick={() => attach(r.path)}
                  >
                    ➕ Добавить
                  </button>
                </div>
              </li>
            ))}
          </ul>
        </div>
        <div className="flex justify-end border-t border-border p-3">
          <button className="btn" onClick={onClose}>Закрыть</button>
        </div>
      </div>
    </div>
  );
}
