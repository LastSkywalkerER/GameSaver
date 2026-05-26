package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"GameSaver/internal/config"
)

// Setup wires slog to write JSON to a file. Stderr is attempted as a best-effort
// secondary sink so dev runs from a console still see logs, but a failed stderr
// write (no console on Wails GUI app) does not break file logging.
func Setup(cfg *config.Config) error {
	if err := os.MkdirAll(cfg.LogsDir(), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(cfg.LogsDir(), "gamesaver.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w := &forgivingWriter{primary: f, secondary: os.Stderr}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(h))
	return nil
}

// forgivingWriter writes to primary and always reports success. Errors from
// secondary (e.g. stderr on a Windows GUI app) are silently ignored.
type forgivingWriter struct {
	primary   io.Writer
	secondary io.Writer
}

func (w *forgivingWriter) Write(p []byte) (int, error) {
	if w.secondary != nil {
		_, _ = w.secondary.Write(p)
	}
	n, err := w.primary.Write(p)
	if err != nil {
		return n, err
	}
	return len(p), nil
}
