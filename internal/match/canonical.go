package match

// NormalizedNameKey returns the canonical comparable key for a game name — the
// same tokenization + sorted-token key the matcher uses internally — so other
// packages (the store-library merger, #5) can dedupe titles the same way
// ("Cyberpunk 2077" on Steam and GOG collapse to one key; edition suffixes and
// punctuation are folded out).
func NormalizedNameKey(name string) string {
	return nameKey(tokenize(name))
}
