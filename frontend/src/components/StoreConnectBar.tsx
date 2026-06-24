// StoreConnectBar — the store-icons + connected-account controls shared by the
// desktop "Магазины" tab and the shell-mode library overlay. Renders the
// right-hand cluster (store icons → connect modal, 👤N → accounts dropdown) plus
// the connect/account modals. Mouse-driven (login uses browser redirects), so it
// works in shell mode too — the parent just gates its gamepad grid while a modal
// is open (onModalChange).

import { useEffect, useState, type ReactNode } from "react";
import { api, type SourceKind, type StoreAccount, type StoreClientStatus, type StoreLoginInfo } from "../api";
import { SourceBadge } from "./SourceBadge";

const STORES: { key: SourceKind; label: string }[] = [
  { key: "steam", label: "Steam" },
  { key: "gog", label: "GOG" },
  { key: "epic", label: "Epic" },
  { key: "ea", label: "EA" },
];

function Sheet({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  return (
    <div className="fixed inset-0 z-[2100] flex items-start justify-center bg-black/70 p-6 pt-20 backdrop-blur-sm" onClick={onClose}>
      <div className="w-full max-w-2xl rounded-xl2 border border-border bg-panel p-5 shadow-soft" onClick={(e) => e.stopPropagation()}>
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold text-gray-100">{title}</h2>
          <button className="btn" onClick={onClose}>✕</button>
        </div>
        {children}
      </div>
    </div>
  );
}

export function StoreConnectBar({
  accounts,
  clients,
  onChanged,
  onModalChange,
}: {
  accounts: StoreAccount[];
  clients: StoreClientStatus[];
  onChanged: () => void;
  onModalChange?: (open: boolean) => void;
}) {
  const [openStore, setOpenStore] = useState<SourceKind | null>(null);
  const [openAccount, setOpenAccount] = useState<StoreAccount | null>(null);
  const [login, setLogin] = useState<StoreLoginInfo | null>(null);
  const [steamKey, setSteamKey] = useState("");
  const [ref, setRef] = useState("");
  const [authCode, setAuthCode] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => { onModalChange?.(!!openStore || !!openAccount); }, [openStore, openAccount, onModalChange]);

  useEffect(() => {
    if (!openStore) return;
    (api as any).GetStoreLoginInfo?.(openStore).then((li: StoreLoginInfo) => setLogin(li)).catch(() => setLogin(null));
  }, [openStore]);

  async function connect() {
    if (!openStore) return;
    setBusy(true);
    try {
      await (api as any).AddStoreAccount(openStore, steamKey.trim(), ref.trim(), authCode.trim());
      api.Toast("success", "Аккаунт подключён — тяну библиотеку…");
      setRef(""); setAuthCode("");
      onChanged();
    } catch (e) { api.Toast("error", "Не удалось подключить: " + String(e)); }
    finally { setBusy(false); }
  }
  async function loginSteam() {
    setBusy(true);
    api.Toast("info", "Открываю браузер — войди в Steam…");
    try {
      await (api as any).AddSteamAccountViaLogin();
      api.Toast("success", "Steam подключён — тяну библиотеку…");
      onChanged();
    } catch (e) { api.Toast("error", "Вход Steam не удался: " + String(e)); }
    finally { setBusy(false); }
  }
  async function saveSteamKey() {
    setBusy(true);
    try {
      await (api as any).SetSteamWebAPIKey(steamKey.trim());
      (api as any).SyncStoreLibraries?.();
      api.Toast("success", "Ключ сохранён — обновляю библиотеки Steam…");
    } catch (e) { api.Toast("error", "Не удалось сохранить ключ: " + String(e)); }
    finally { setBusy(false); }
  }

  const storeClient = clients.find((x) => x.store === openStore);

  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        {STORES.map((st) => {
          const c = clients.find((x) => x.store === st.key);
          const has = accounts.some((a) => a.store === st.key);
          return (
            <button
              key={st.key}
              onClick={() => setOpenStore(st.key)}
              title={`Подключить / открыть ${st.label}${c?.installed ? " · клиент установлен" : ""}${has ? " · есть аккаунт" : ""}`}
              className="relative transition hover:scale-110"
            >
              <SourceBadge source={st.key} />
              {c?.installed && <span className="absolute -right-1 -top-1 h-2.5 w-2.5 rounded-full border border-panel bg-emerald-400" />}
              {has && <span className="absolute -left-1 -top-1 h-2.5 w-2.5 rounded-full border border-panel bg-accent" />}
            </button>
          );
        })}

        <div className="group relative">
          <button className="flex items-center gap-1 rounded-lg border border-white/10 px-2.5 py-1.5 text-xs text-gray-300 transition hover:bg-card">
            👤 {accounts.length}
            {accounts.some((a) => a.lastSyncError) && <span className="text-amber-400">⚠</span>}
          </button>
          <div className="invisible absolute right-0 top-full z-30 w-72 rounded-lg border border-border bg-panel p-2 opacity-0 shadow-soft transition group-hover:visible group-hover:opacity-100">
            <div className="mb-1 px-1 text-[11px] uppercase tracking-wide text-muted">Подключённые аккаунты</div>
            {accounts.length === 0 && <div className="px-2 py-1.5 text-xs text-muted">пока нет — нажми иконку магазина слева</div>}
            {accounts.map((a) => (
              <button
                key={a.id}
                onClick={() => setOpenAccount(a)}
                className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs transition hover:bg-card"
              >
                <SourceBadge source={a.store} />
                <span className="min-w-0 flex-1 truncate text-gray-300">{a.displayName || a.externalId}</span>
                {a.lastSyncError ? <span className="text-amber-400">⚠</span> : !a.enabled ? <span className="text-[10px] text-gray-500">выкл</span> : null}
              </button>
            ))}
          </div>
        </div>
      </div>

      {openStore && (
        <Sheet title={`Подключить — ${STORES.find((s) => s.key === openStore)?.label}`} onClose={() => setOpenStore(null)}>
          {storeClient && (
            <div className="mb-4 flex items-center gap-2 text-xs">
              {storeClient.installed ? (
                <>
                  <span className="rounded bg-emerald-900/50 px-2 py-1 text-emerald-200">● Клиент установлен на ПК</span>
                  <button className="btn" onClick={() => (api as any).OpenStoreClient?.(openStore)}>↗ Открыть</button>
                </>
              ) : (
                <>
                  <span className="rounded bg-white/5 px-2 py-1 text-gray-400">○ Клиент не установлен</span>
                  <button className="btn" onClick={() => (api as any).OpenURL?.(storeClient.downloadUrl)}>⤓ Скачать</button>
                </>
              )}
            </div>
          )}

          {openStore === "steam" && (
            <div className="flex flex-col gap-3">
              <div className="flex flex-wrap items-center gap-3">
                <button className="btn btn-primary" disabled={busy} onClick={loginSteam}>{busy ? "…" : "🎮 Вход через Steam"}</button>
                <span className="text-xs text-muted">Откроется браузер — войди в Steam, аккаунт подтянется сам. Для публичного профиля ключ не нужен.</span>
              </div>
              <details className="text-xs text-muted">
                <summary className="cursor-pointer select-none">или вручную (Web API key — самый надёжный способ)</summary>
                <div className="mt-2 leading-relaxed text-gray-400">
                  1. Открой{" "}
                  <button type="button" className="text-accent underline hover:text-accent/80"
                    onClick={() => (api as any).OpenURL?.("https://steamcommunity.com/dev/apikey")}>steamcommunity.com/dev/apikey</button>{" "}
                  · 2. в «Domain Name» впиши любой адрес (напр. <span className="text-gray-300">localhost</span>)
                  · 3. галочка соглашения → «Register» · 4. скопируй «Key» · 5. вставь ниже → «Сохранить ключ».
                </div>
                <div className="mt-2 flex flex-wrap items-end gap-3">
                  <label className="flex flex-col">
                    Steam Web API key
                    <input className="input mt-1 w-72" type="password" value={steamKey} placeholder="вставь ключ сюда" onChange={(e) => setSteamKey(e.target.value)} />
                  </label>
                  <label className="flex flex-col">
                    SteamID64 / vanity (если не входил через Steam)
                    <input className="input mt-1 w-72" value={ref} placeholder="76561198… или nickname" onChange={(e) => setRef(e.target.value)} />
                  </label>
                  <button className="btn" disabled={busy} onClick={connect}>{busy ? "…" : "Подключить"}</button>
                  <button className="btn" disabled={busy || !steamKey.trim()} onClick={saveSteamKey}>{busy ? "…" : "Сохранить ключ"}</button>
                </div>
              </details>
            </div>
          )}

          {(openStore === "gog" || openStore === "epic") && (
            <div className="flex flex-col gap-2">
              <div className="text-xs text-muted">{login?.hint}</div>
              <div className="flex flex-wrap items-end gap-3">
                <button className="btn" onClick={() => (api as any).OpenStoreLoginPage?.(openStore)}>1 · Открыть вход {openStore.toUpperCase()}</button>
                <label className="flex flex-col text-xs text-muted">
                  2 · Вставь код / URL
                  <input className="input mt-1 w-96" value={authCode} placeholder="authorizationCode / …code=…" onChange={(e) => setAuthCode(e.target.value)} />
                </label>
                <button className="btn btn-primary" disabled={busy || !authCode.trim()} onClick={connect}>{busy ? "…" : "Подключить"}</button>
              </div>
            </div>
          )}

          {openStore === "ea" && (
            <div className="text-xs leading-relaxed text-muted">
              Онлайн-библиотека EA недоступна: EA не отдаёт её сторонним приложениям без своего клиента —
              токен веб-входа отклоняется шлюзом EA («auth token not allowed for unknown client»).
              Показываются только <span className="text-gray-300">установленные</span> EA-игры — они подтянутся при сканировании.
            </div>
          )}
        </Sheet>
      )}

      {openAccount && (
        <Sheet title="Аккаунт магазина" onClose={() => setOpenAccount(null)}>
          <div className="flex items-center gap-3">
            <SourceBadge source={openAccount.store} />
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm text-gray-100">{openAccount.displayName || openAccount.externalId}</div>
              <div className="text-[11px] text-muted">
                {openAccount.lastSyncError
                  ? <span className="text-amber-300">⚠ {openAccount.lastSyncError}</span>
                  : openAccount.lastSyncAt ? "обновлено " + new Date(openAccount.lastSyncAt * 1000).toLocaleString() : "ещё не синхронизирован"}
              </div>
            </div>
          </div>
          <div className="mt-4 flex flex-wrap gap-2">
            <button className="btn" onClick={() => { (api as any).SyncStoreAccount?.(openAccount.id); api.Toast("info", "Обновляю…"); }}>↻ Обновить</button>
            <button className="btn" onClick={() => (api as any).SetStoreAccountEnabled?.(openAccount.id, !openAccount.enabled).then(onChanged).then(() => setOpenAccount(null))}>
              {openAccount.enabled ? "Выключить" : "Включить"}
            </button>
            <button className="btn" onClick={() => { (api as any).RemoveStoreAccount?.(openAccount.id); setOpenAccount(null); }}>✕ Удалить</button>
          </div>
        </Sheet>
      )}
    </>
  );
}
