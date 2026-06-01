// ShellSettingsPage — full-screen settings view used in shell mode.
//
// v0.9.x rendered SettingsPage inside a <Modal>. That clipped the
// content into a small dark card and the gamepad couldn't drive any
// of the form controls. v0.10.0 makes Settings a real page on top of
// the animated blob background (matching the brief: "с нашим
// дефолтным анимированным фоном с фиолетовыми эффектами"), with
// roving focus driven by the d-pad so every control is reachable
// without a mouse.
//
// Focus model: we collect every focusable control inside the page on
// mount (and refresh when the DOM changes). d-pad ↑/↓ moves to the
// previous/next focusable in DOM order, calling .focus() so the
// browser's :focus-visible takes over (the gs-check / gs-radio styles
// and .input:focus ring already paint the active state in accent).
// A presses .click() on the focused element — for native buttons /
// labels-of-checkboxes / radios that's the same as activating them.
// B/Esc closes back to the carousel. Left/right are NOT remapped —
// inside a number input you still want to move the caret, inside a
// select you still want the browser's default. The d-pad up/down is
// the universal "move focus" channel.

import { useCallback, useEffect, useRef } from "react";
import { useControllerButton, useControllerNav, useControllerScroll } from "../../controller";
import { playBack, playMove, playSelect } from "../../sound";
import { SettingsPage } from "../../pages/SettingsPage";
import { ShellBackground } from "./ShellBackground";

export function ShellSettingsPage({
  onClose,
  onOpenMonitorPicker,
  onOpenAudioPicker,
  onOpenBluetoothPicker,
}: {
  onClose: () => void;
  onOpenMonitorPicker: () => void;
  onOpenAudioPicker: () => void;
  onOpenBluetoothPicker: () => void;
}) {
  const rootRef = useRef<HTMLDivElement | null>(null);
  // The element our gamepad cursor is on right now. We can't rely on
  // document.activeElement for this — in WebView2 a programmatic
  // .focus() on a <button> sometimes leaves activeElement pointing at
  // <body>, so when the user presses A the click target is wrong.
  // Tracking the ref directly is unambiguous.
  const focusedRef = useRef<HTMLElement | null>(null);

  // Recollect focusables lazily — DOM shape inside SettingsPage rarely
  // changes after the first render, but new sections appear when async
  // GetConfig / GetSunshineStatus resolve. We snapshot on every focus
  // call to stay correct without a MutationObserver.
  const collect = useCallback((): HTMLElement[] => {
    const root = rootRef.current; if (!root) return [];
    const sel = [
      'button:not([disabled])',
      'input:not([disabled]):not([type="hidden"])',
      'select:not([disabled])',
      'textarea:not([disabled])',
      'a[href]',
      '[tabindex]:not([tabindex="-1"])',
    ].join(",");
    return Array.from(root.querySelectorAll<HTMLElement>(sel))
      // Drop offscreen / hidden controls so focus doesn't jump into a
      // collapsed section.
      .filter((el) => el.offsetParent !== null);
  }, []);

  // Clear the marker from anywhere it may still live, then set it on the
  // new target AND record it in our ref. WebView2 won't fire
  // :focus-visible on programmatic .focus() (style.css keys the ring
  // off data-gs-focused for the visual), and it ALSO sometimes leaves
  // document.activeElement pointing at <body> instead of the focused
  // element (which is why a separate ref tracks the cursor for the
  // A-button click handler).
  function setGamepadFocus(target: HTMLElement) {
    document.querySelectorAll<HTMLElement>('[data-gs-focused="true"]')
      .forEach((el) => el.removeAttribute("data-gs-focused"));
    target.setAttribute("data-gs-focused", "true");
    target.focus();
    focusedRef.current = target;
  }

  const moveFocus = useCallback((delta: number) => {
    const els = collect();
    if (!els.length) return;
    // Prefer our own ref over document.activeElement for the same
    // reason it matters in the A-click path — activeElement lies in
    // WebView2 after programmatic .focus().
    const cur = focusedRef.current ?? (document.activeElement as HTMLElement | null);
    const i = cur ? els.indexOf(cur) : -1;
    const next = Math.max(0, Math.min(els.length - 1, (i < 0 ? 0 : i) + delta));
    if (next === i) return;
    const target = els[next];
    setGamepadFocus(target);
    target.scrollIntoView({ block: "nearest", behavior: "smooth" });
    playMove();
  }, [collect]);

  // Autofocus the first control so a single ↓ press is immediate.
  useEffect(() => {
    const t = setTimeout(() => {
      const els = collect();
      if (els[0]) setGamepadFocus(els[0]);
    }, 50);
    return () => clearTimeout(t);
  }, [collect]);

  useControllerNav((dir) => {
    // d-pad / LS — walk focusable controls. ↑/↓ AND ←/→ both move the
    // focus cursor here: in a vertical settings page horizontal nav
    // would have nowhere to go anyway, so we map all four directions to
    // the same axis instead of leaving ←/→ silent.
    if (dir === "up" || dir === "left")    moveFocus(-1);
    else if (dir === "down" || dir === "right") moveFocus(+1);
  });
  // Right stick — pure scroll on the page body. Per-tick delta is in
  // [-1..+1] at 50 Hz, multiplied by a px speed factor. 18 px/tick × 50
  // Hz ≈ 900 px/s at full deflection, close to a continuous wheel
  // scroll. The handler runs against the inner scroll container ref
  // captured below.
  useControllerScroll(({ dy }) => {
    const el = rootRef.current; if (!el) return;
    el.scrollBy({ top: dy * 18, behavior: "auto" });
  });
  useControllerButton((btn) => {
    if (btn === "a") {
      const el = focusedRef.current;
      if (!el) return;
      const tag = el.tagName;
      if (tag === "TEXTAREA" || tag === "SELECT") return;
      // For text/number inputs A is meaningless (nothing to click) but
      // for CHECKBOX / RADIO inputs A SHOULD toggle them — calling
      // .click() on a checkbox flips its checked state, which is
      // exactly what the user wants on the gs-check / gs-radio
      // controls in Settings.
      if (tag === "INPUT") {
        const t = (el as HTMLInputElement).type;
        if (t !== "checkbox" && t !== "radio") return;
      }
      playSelect();
      el.click();
    } else if (btn === "b" || btn === "back" || btn === "y") {
      // Y toggles the page; pressing it again closes — symmetrical with
      // the Y-opens-settings binding in ShellApp.
      playBack();
      onClose();
    }
  });

  // Esc closes the page. Enter mirrors A so a keyboard-only user can
  // also activate the focused control even when the browser hasn't
  // moved real focus there (WebView2 quirk — see setGamepadFocus).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") { e.preventDefault(); playBack(); onClose(); }
      else if (e.key === "Enter") {
        const el = focusedRef.current; if (!el) return;
        const tag = el.tagName;
        if (tag === "TEXTAREA" || tag === "SELECT") return;
        if (tag === "INPUT") {
          const t = (el as HTMLInputElement).type;
          if (t !== "checkbox" && t !== "radio") return;
        }
        e.preventDefault();
        playSelect();
        el.click();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div className="fixed inset-0 overflow-hidden text-gray-100">
      {/* game={null} → ShellBackground draws the animated blob fallback
          (the v0.5 default look). No game art behind Settings — would
          be visual noise behind a wall of form controls. */}
      <ShellBackground game={null} />

      {/* Header strip — title + close hint. Sticky top so it stays
          visible while the body scrolls. */}
      <div className="relative z-10 flex items-center justify-between border-b border-white/10 bg-black/40 px-8 py-4 backdrop-blur-md">
        <h1 className="text-2xl font-semibold">Настройки</h1>
        <div className="flex items-center gap-3 text-xs text-muted">
          <span>↑ ↓ навигация · A — применить · B / Esc / Y — назад</span>
          <button className="btn" onClick={() => { playBack(); onClose(); }}>
            Закрыть
          </button>
        </div>
      </div>

      {/* The actual settings markup. We reuse the regular-desktop
          <SettingsPage> verbatim and just override its width via the
          outer wrapper, plus pass through the in-app picker handles so
          the Monitor / Audio buttons there delegate to ShellApp's
          picker host instead of falling back to system shortcuts. */}
      <div ref={rootRef} className="relative z-10 h-[calc(100vh-65px)] overflow-y-auto">
        <SettingsPage
          onOpenMonitorPicker={onOpenMonitorPicker}
          onOpenAudioPicker={onOpenAudioPicker}
          onOpenBluetoothPicker={onOpenBluetoothPicker}
        />
      </div>
    </div>
  );
}
