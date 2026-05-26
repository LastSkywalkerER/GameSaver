import { useState } from "react";
import { api, type UpdateInfo } from "../api";

export function UpdateBanner({
  info,
  onDismiss,
}: {
  info: UpdateInfo;
  onDismiss: () => void;
}) {
  const [busy, setBusy] = useState<null | "downloading" | "applied">(null);
  const [err, setErr] = useState<string | null>(null);

  async function doApply() {
    setBusy("downloading");
    setErr(null);
    try {
      await api.ApplyUpdate();
      setBusy("applied");
      api.Toast("success", `Установлена ${info.latestVersion}. Перезапуск через 2 сек…`);
      setTimeout(() => api.RestartApp().catch(() => {}), 2000);
    } catch (e) {
      setBusy(null);
      setErr(String(e));
      api.Toast("error", "Не удалось обновить: " + String(e));
    }
  }

  async function doSkipThis() {
    try {
      await api.SkipUpdate(info.latestVersion);
    } finally {
      onDismiss();
    }
  }

  const mb = info.assetSize ? (info.assetSize / 1024 / 1024).toFixed(1) : "?";

  return (
    <div className="relative border-b border-accent/40 bg-gradient-to-r from-accent/20 via-accent/10 to-transparent px-5 py-2.5 text-sm">
      <div className="flex items-center gap-3">
        <span className="text-base">⬆️</span>
        <div className="min-w-0 flex-1">
          <div className="font-semibold text-gray-100">
            Доступно обновление: <span className="text-accent">{info.latestVersion}</span>
            <span className="ml-2 text-xs text-muted">(текущая {info.currentVersion}, ~{mb} MB)</span>
          </div>
          {info.releaseNotes && (
            <div
              className="mt-0.5 max-h-16 overflow-y-auto truncate whitespace-pre-line text-xs text-muted"
              title={info.releaseNotes}
            >
              {info.releaseNotes.slice(0, 240)}{info.releaseNotes.length > 240 ? "…" : ""}
            </div>
          )}
          {err && <div className="mt-1 text-xs text-red-400">{err}</div>}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {busy === "downloading" && <span className="text-xs text-muted">Качаю…</span>}
          <button
            className="btn btn-primary"
            disabled={busy !== null}
            onClick={doApply}
            title="Скачать и применить — после установки прила перезапустится"
          >
            {busy === "applied" ? "✓ Применено" : busy === "downloading" ? "↻ Качаю…" : "⬇ Обновить"}
          </button>
          <button
            className="btn"
            disabled={busy !== null}
            onClick={onDismiss}
            title="Сейчас не обновлять (напомню при следующем запуске)"
          >
            Позже
          </button>
          <button
            className="btn"
            disabled={busy !== null}
            onClick={doSkipThis}
            title="Не показывать эту версию больше"
          >
            Пропустить версию
          </button>
          {info.releaseUrl && (
            <a
              href={info.releaseUrl}
              target="_blank"
              rel="noreferrer"
              className="btn btn-ghost text-xs"
              onClick={(e) => { e.preventDefault(); window.open(info.releaseUrl, "_blank"); }}
            >
              GitHub
            </a>
          )}
        </div>
      </div>
    </div>
  );
}
