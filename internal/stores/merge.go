package stores

import (
	"log/slog"

	"GameSaver/internal/domain"
	"GameSaver/internal/match"
)

// mergeAccount links each owned title to an INSTALLED game (by Steam AppID or
// canonical name) so the aggregated card shows "installed". Owned-but-not-
// installed titles are left UNLINKED (game_id NULL) — they never get a stub
// games row (which would flood the dashboard + the scan/match/backup passes)
// and are grouped synthetically in GetAggregatedLibrary instead.
//
// Re-runs also UNLINK titles whose game is no longer installed (or that were
// linked to a now-dead stub from an earlier version), so the dashboard stays
// clean after the fix.
func (s *Service) mergeAccount(accountID string) error {
	owned, err := s.db.ListOwnedTitles(accountID)
	if err != nil {
		return err
	}
	games, err := s.db.ListGames()
	if err != nil {
		return err
	}
	// A game counts as "real" for linking only if it's installed on this PC.
	allInsts, _ := s.db.ListAllInstallations()
	installed := make(map[string]bool, len(allInsts))
	for _, i := range allInsts {
		installed[i.GameID] = true
	}
	byNameInstalled := map[string]*domain.Game{}
	for _, g := range games {
		if installed[g.ID] {
			byNameInstalled[match.NormalizedNameKey(g.Name)] = g
		}
	}

	for _, ot := range owned {
		gid := ""
		if ot.SteamAppID != 0 {
			if g, err := s.db.FindGameBySteamAppID(ot.SteamAppID); err == nil && g != nil && installed[g.ID] {
				gid = g.ID
			}
		}
		if gid == "" {
			if g, ok := byNameInstalled[match.NormalizedNameKey(ot.Title)]; ok {
				gid = g.ID
			}
		}
		// Only write when the link actually changes — including clearing a stale
		// link (gid "" → NULL) so dashboard-polluting stubs get orphaned.
		if gid != ot.GameID {
			if err := s.db.SetOwnedGameID(ot.ID, gid); err != nil {
				slog.Warn("stores: link owned title", "id", ot.ID, "err", err)
			}
		}
	}
	return nil
}
