import { useEffect, useState } from "react";
import { api, type ManifestSearchResult } from "../api";

export function ManifestPickerDialog({
  initialQuery,
  onClose,
  onPicked,
}: {
  initialQuery: string;
  onClose: () => void;
  onPicked: (entry: ManifestSearchResult | null) => void;
}) {
  const [query, setQuery] = useState(initialQuery);
  const [results, setResults] = useState<ManifestSearchResult[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    const h = setTimeout(async () => {
      setLoading(true);
      try {
        const r = await api.SearchManifest(query, 40);
        setResults((r as any) ?? []);
      } finally {
        setLoading(false);
      }
    }, 200);
    return () => clearTimeout(h);
  }, [query]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm" onClick={onClose}>
      <div
        className="card flex h-[70vh] w-[640px] max-w-[95vw] flex-col overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="border-b border-border p-3">
          <div className="mb-2 flex items-center gap-2">
            <h3 className="flex-1 text-sm font-semibold uppercase tracking-wide text-muted">
              Перевыбрать игру (Ludusavi)
            </h3>
            <button className="btn btn-ghost" onClick={onClose}>✕</button>
          </div>
          <input
            autoFocus
            className="input w-full"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Поиск по названию игры…"
          />
        </div>
        <div className="flex-1 overflow-y-auto">
          {loading && <div className="p-4 text-sm text-muted">Ищу…</div>}
          {!loading && results.length === 0 && (
            <div className="p-4 text-sm text-muted">Ничего не найдено.</div>
          )}
          <ul>
            {results.map((r) => (
              <li
                key={r.key}
                className="cursor-pointer border-b border-border px-4 py-2 hover:bg-card"
                onClick={() => onPicked(r)}
              >
                <div className="text-sm font-medium text-gray-100">{r.name}</div>
                <div className="mt-0.5 text-xs text-muted">
                  {r.steamAppId ? `Steam AppID ${r.steamAppId} · ` : ""}
                  {r.fileCount} путей сейвов
                </div>
              </li>
            ))}
          </ul>
        </div>
        <div className="flex items-center justify-between gap-2 border-t border-border p-3">
          <button className="btn" onClick={() => onPicked(null)}>
            Сбросить override
          </button>
          <button className="btn btn-ghost" onClick={onClose}>Отмена</button>
        </div>
      </div>
    </div>
  );
}
