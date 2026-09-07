// Matching a Nintendo Switch title to a game installed on this PC, plus the
// save-folder options that follow from it. Shared by the desktop Switch page
// and the shell-mode overlay so both filter destinations identically.
//
// A transfer only makes sense between the same game: pushing a Cyberpunk save
// into Biomutant's console slot could only corrupt it. So destination pickers
// are filtered by name.

import type { GameView } from "./api";

// Edition wording differs between a console release and its PC counterpart far
// more often than the game does ("The Witcher 3 Wild Hunt — Complete Edition"
// vs "The Witcher 3: Wild Hunt"), so these are stripped before comparing.
// Ordered longest-first so "completeedition" is consumed before "edition".
const EDITION_TOKENS = [
  "gameoftheyearedition", "completeedition", "definitiveedition", "specialedition",
  "eternalcollection", "platinumedition", "remastered", "collection", "deluxe",
  "goty", "edition", "remake", "redux",
];

/** normTitle folds a title down to a comparable key. */
export function normTitle(s: string): string {
  let n = s
    .toLowerCase()
    .replace(/[®™©]/g, "")
    .replace(/[^\p{L}\p{N}]+/gu, "");
  for (const t of EDITION_TOKENS) n = n.split(t).join("");
  return n;
}

export type LocOption = { id: string; label: string };

export function allLocations(games: GameView[]): LocOption[] {
  const out: LocOption[] = [];
  for (const g of games) {
    for (const l of g.saveLocations ?? []) out.push({ id: l.id, label: `${g.game.name} — ${l.path}` });
  }
  out.sort((a, b) => a.label.localeCompare(b.label));
  return out;
}

/**
 * pcLocationsFor returns save folders of PC games whose name matches the
 * console title. Empty means "this game isn't on this PC".
 *
 * Comparison is **exact** after normalisation, never substring: "hollowknight"
 * is a prefix of "hollowknightsilksong" and "doom" of "doometernal", but those
 * are different games.
 *
 * It cannot bridge localised names — the console lists "Ведьмак 3 Дикая Охота"
 * while the PC copy is "The Witcher 3" — which is why every caller also offers
 * a manual override.
 */
export function pcLocationsFor(games: GameView[], consoleTitle: string): LocOption[] {
  const want = normTitle(consoleTitle);
  if (!want) return [];
  const out: LocOption[] = [];
  for (const g of games) {
    if (normTitle(g.game.name) !== want) continue;
    for (const l of g.saveLocations ?? []) out.push({ id: l.id, label: `${g.game.name} — ${l.path}` });
  }
  out.sort((a, b) => a.label.localeCompare(b.label));
  return out;
}

/** pcTitleSet is the set of normalised names of PC games that have saves. */
export function pcTitleSet(games: GameView[]): Set<string> {
  const s = new Set<string>();
  for (const g of games) if ((g.saveLocations ?? []).length > 0) s.add(normTitle(g.game.name));
  return s;
}
