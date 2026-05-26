import { useEffect, useState } from "react";
import { EventsOn } from "../api";

type Toast = { id: number; level: string; message: string };

let nextId = 1;

export function Toaster() {
  const [toasts, setToasts] = useState<Toast[]>([]);
  useEffect(() => {
    const off = EventsOn("toast", (payload: any) => {
      const t = { id: nextId++, level: payload?.level ?? "info", message: payload?.message ?? "" };
      setToasts((curr) => [...curr, t]);
      setTimeout(() => setToasts((curr) => curr.filter((x) => x.id !== t.id)), 4500);
    });
    return () => { try { (off as any)?.(); } catch {} };
  }, []);
  return (
    <div className="pointer-events-none fixed bottom-4 right-4 z-50 flex flex-col gap-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={
            "pointer-events-auto min-w-[220px] rounded-lg border px-3 py-2 text-sm shadow-soft " +
            (t.level === "error"
              ? "bg-red-900/80 text-red-100 border-red-700"
              : t.level === "success"
                ? "bg-emerald-900/80 text-emerald-100 border-emerald-700"
                : "bg-card text-gray-100 border-border")
          }
        >
          {t.message}
        </div>
      ))}
    </div>
  );
}
