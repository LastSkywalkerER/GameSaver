// Xbox controller helpers. The backend (internal/controller) polls XInput
// at 50 Hz and emits three Wails events; this module turns those into
// React hooks the UI can subscribe to.
//
//   controller:state  {connected: bool}
//   controller:button {button: "a|b|x|y|start|back|lb|rb"}
//   controller:nav    {dir: "up|down|left|right"}            (auto-repeat)

import { useEffect, useState } from "react";
import { EventsOn } from "./api";

export type NavDir = "up" | "down" | "left" | "right";
export type Button = "a" | "b" | "x" | "y" | "start" | "back" | "lb" | "rb";

// Single global state holder. Re-broadcast to subscribers so multiple
// components can show the chip / react to buttons without duplicating
// the Wails subscription.
let connected = false;
const stateSubs = new Set<(c: boolean) => void>();
const navSubs = new Set<(d: NavDir) => void>();
const buttonSubs = new Set<(b: Button) => void>();

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
}

/** Returns true when an Xbox-compatible controller is plugged in. */
export function useControllerConnected(): boolean {
  ensureWired();
  const [c, setC] = useState(connected);
  useEffect(() => {
    const fn = (v: boolean) => setC(v);
    stateSubs.add(fn);
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
