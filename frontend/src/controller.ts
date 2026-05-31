// Xbox controller helpers. The backend (internal/controller) polls XInput
// at 50 Hz and emits three Wails events; this module turns those into
// React hooks the UI can subscribe to.
//
//   controller:state  {connected: bool}
//   controller:button {button: "a|b|x|y|start|back|lb|rb"}
//   controller:nav    {dir: "up|down|left|right"}            (auto-repeat)
//   controller:scroll {dx: number, dy: number}               (right stick, 50 Hz)

import { useEffect, useState } from "react";
import { api, EventsOn } from "./api";

export type NavDir = "up" | "down" | "left" | "right";
export type Button = "a" | "b" | "x" | "y" | "start" | "back" | "lb" | "rb";
export type ScrollDelta = { dx: number; dy: number };

// Single global state holder. Re-broadcast to subscribers so multiple
// components can show the chip / react to buttons without duplicating
// the Wails subscription.
let connected = false;
const stateSubs = new Set<(c: boolean) => void>();
const navSubs = new Set<(d: NavDir) => void>();
const buttonSubs = new Set<(b: Button) => void>();
const scrollSubs = new Set<(d: ScrollDelta) => void>();

let wired = false;
function ensureWired() {
  if (wired) return;
  wired = true;
  EventsOn("controller:state", (p: any) => {
    connected = !!p?.connected;
    stateSubs.forEach((fn) => fn(connected));
  });
  EventsOn("controller:nav", (p: any) => {
    const dir = p?.dir as NavDir;
    if (dir) navSubs.forEach((fn) => fn(dir));
  });
  EventsOn("controller:button", (p: any) => {
    const btn = p?.button as Button;
    if (btn) buttonSubs.forEach((fn) => fn(btn));
  });
  EventsOn("controller:scroll", (p: any) => {
    const d: ScrollDelta = { dx: Number(p?.dx) || 0, dy: Number(p?.dy) || 0 };
    if (d.dx === 0 && d.dy === 0) return;
    scrollSubs.forEach((fn) => fn(d));
  });
}

/** Returns true when an Xbox-compatible controller is plugged in. */
export function useControllerConnected(): boolean {
  ensureWired();
  const [c, setC] = useState(connected);
  useEffect(() => {
    const fn = (v: boolean) => setC(v);
    stateSubs.add(fn);
    // Pull the current state from the backend on mount. The XInput poller
    // emits "controller:state" only on connect/disconnect *transitions*,
    // and the very first event (the one that says "yes, pad is connected
    // right now") fires immediately at app startup — typically before any
    // frontend code has subscribed via EventsOn. Without this call the
    // chip stays stuck on "no pad" until the user replugs the controller.
    (api as any).IsControllerConnected?.()
      .then((v: boolean) => {
        if (v !== connected) {
          connected = v;
          stateSubs.forEach((s) => s(v));
        } else {
          setC(v);
        }
      })
      .catch(() => {});
    return () => { stateSubs.delete(fn); };
  }, []);
  return c;
}

/** Fires `handler` on every d-pad / left-stick direction (with auto-repeat). */
export function useControllerNav(handler: (dir: NavDir) => void) {
  ensureWired();
  useEffect(() => {
    navSubs.add(handler);
    return () => { navSubs.delete(handler); };
  }, [handler]);
}

/** Fires `handler` on every action-button press (no repeat). */
export function useControllerButton(handler: (b: Button) => void) {
  ensureWired();
  useEffect(() => {
    buttonSubs.add(handler);
    return () => { buttonSubs.delete(handler); };
  }, [handler]);
}

/** Fires `handler` ~50 Hz while the right stick is outside its deadzone.
 *  dx / dy are normalised in [-1.0..+1.0]. dy is already flipped to
 *  "scroll-down positive" so callers can pass it straight to
 *  `el.scrollBy({ top: dy * speed })`. */
export function useControllerScroll(handler: (d: ScrollDelta) => void) {
  ensureWired();
  useEffect(() => {
    scrollSubs.add(handler);
    return () => { scrollSubs.delete(handler); };
  }, [handler]);
}
