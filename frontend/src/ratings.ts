// Per-game star ratings for the store library, persisted in localStorage keyed
// by the library card id (a real game id, or a synthetic "owned:…" key). Kept
// client-side so it survives a DB cache rebuild and needs no backend round-trip.

const KEY = "gs:libraryRatings";

export function getRatings(): Record<string, number> {
  try {
    return JSON.parse(localStorage.getItem(KEY) || "{}") || {};
  } catch {
    return {};
  }
}

// setRating writes (or clears, when stars<=0) the rating and returns the new map.
export function setRating(id: string, stars: number): Record<string, number> {
  const r = getRatings();
  if (stars <= 0) delete r[id];
  else r[id] = stars;
  try {
    localStorage.setItem(KEY, JSON.stringify(r));
  } catch {}
  return { ...r };
}
