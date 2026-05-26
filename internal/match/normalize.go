package match

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// digitCommaRe matches a digit-comma-digit (e.g. "40,000") so we can join them
// into a single number token before regular split. Without this, manifest
// "Warhammer 40,000: Space Marine 2" tokenizes as [40, 000] and never matches
// the user's "Warhammer 40000 Space Marine 2".
var digitCommaRe = regexp.MustCompile(`(\d),(\d)`)

// trailingVerOrYearRe strips an obvious trailing version or year suffix
// (" v1.3.214", " (2023)", " [2017]") so repack folder names like
// "Stray v1.3.214" canonicalize to "Stray". Conservative: only at end and
// only pure version-or-year tokens.
var trailingVerOrYearRe = regexp.MustCompile(`(?i)\s+(v\d+(\.\d+)*|\(\d{4}\)|\[\d{4}\]|build\s*\d+|patch\s*\d+(\.\d+)*)\s*$`)

// stripAccentsTransformer removes combining marks (e.g. é -> e, ö -> o).
var stripAccentsTransformer = transform.Chain(norm.NFD,
	runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// tokenize returns lowercased alphanumeric tokens of s, with diacritics stripped,
// "&" replaced by "and", and digits split out of CamelCase clumps.
// E.g. "Marvel's Spider-Man Remastered" -> [marvel, s, spider, man, remastered]
//      "Ratchet & Clank: Rift Apart"    -> [ratchet, and, clank, rift, apart]
//      "Crysis2Remastered"              -> [crysis, 2, remastered]
//      "STAR WARS Jedi: Survivor"       -> [star, wars, jedi, survivor]
func tokenize(s string) []string {
	if s == "" {
		return nil
	}
	t, _, _ := transform.String(stripAccentsTransformer, s)
	// Strip trailing version-or-year noise so "Stray v1.3.214" → "Stray".
	t = trailingVerOrYearRe.ReplaceAllString(t, "")
	// Collapse "40,000"-style separators inside digit runs.
	for i := 0; i < 3; i++ {
		newT := digitCommaRe.ReplaceAllString(t, "$1$2")
		if newT == t {
			break
		}
		t = newT
	}
	// IMPORTANT: do CamelCase splitting BEFORE lowercasing, otherwise
	// "DeathStranding" → "deathstranding" loses the word boundary.
	t = splitCamelCase(t)
	t = strings.ToLower(t)
	// Strip apostrophes BEFORE splitting so "Marvel's" → "marvels", not
	// ["marvel","s"]. Covers straight, curly, and modifier-letter apostrophes.
	for _, ap := range []string{"'", "’", "ʼ", "`", "´"} {
		t = strings.ReplaceAll(t, ap, "")
	}
	t = strings.ReplaceAll(t, "&", " and ")
	// Drop trademark/copyright + ignored generic noise.
	for _, junk := range []string{"™", "®", "©", "(tm)", "(r)", "(c)"} {
		t = strings.ReplaceAll(t, junk, " ")
	}
	// Split on anything non-alphanumeric.
	fields := strings.FieldsFunc(t, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	// Drop trivial words that hurt comparison.
	ignore := map[string]bool{
		"the": true, "a": true, "an": true, "of": true,
		"edition": true, "remaster": true, "remastered": true,
		"goty": true, "complete": true, "definitive": true,
		"enhanced": true, "directors": true, "cut": true, "deluxe": true,
		"ultimate": true, "gold": true, "premium": true, "anniversary": true,
		"hd": true, "version": true,
	}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if !ignore[f] {
			out = append(out, f)
		}
	}
	return out
}

// splitCamelCase inserts a space at every case/digit boundary, so that
// "DeathStranding" → "Death Stranding", "Crysis2Remastered" → "Crysis 2 Remastered",
// "TOWSpacersChoice" → "TOW Spacers Choice". The classic two-rule split:
//   - lower → Upper  (deathS → death S)
//   - UPPER → Upperlower (TOWS → TOW S, when next-next is lowercase)
//   - letter ↔ digit
func splitCamelCase(s string) string {
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i, r := range runes {
		if i > 0 {
			prev := runes[i-1]
			// digit/letter boundary
			if isLetterRune(prev) != isLetterRune(r) && (isLetterRune(r) || isDigitRune(r)) && (isLetterRune(prev) || isDigitRune(prev)) {
				b.WriteRune(' ')
			} else if unicode.IsLower(prev) && unicode.IsUpper(r) {
				// lower → Upper
				b.WriteRune(' ')
			} else if i+1 < len(runes) && unicode.IsUpper(prev) && unicode.IsUpper(r) && unicode.IsLower(runes[i+1]) {
				// UPPER → Upperlower (preserve acronym boundary)
				b.WriteRune(' ')
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isLetterRune(r rune) bool { return unicode.IsLetter(r) }
func isDigitRune(r rune) bool  { return unicode.IsDigit(r) }

// nameKey returns a stable comparable key from tokens (sorted joined).
func nameKey(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	sorted := append([]string(nil), tokens...)
	// simple sort
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	return strings.Join(sorted, "|")
}

// tokenSetMatch returns true if every token in a is present in b (a ⊆ b),
// using rough plural folding ("saves" ≈ "save").
func tokenSetMatch(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	bset := map[string]bool{}
	for _, t := range b {
		bset[t] = true
		bset[strings.TrimSuffix(t, "s")] = true
	}
	for _, t := range a {
		if !bset[t] && !bset[strings.TrimSuffix(t, "s")] {
			return false
		}
	}
	return true
}
