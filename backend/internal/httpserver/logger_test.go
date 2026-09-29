package httpserver

import (
	"context"
	"log/slog"
)

// recordingHandler collects slog records as maps, for asserting on the
// request log without parsing text.
type recordingHandler struct {
	lines *[]map[string]any
}

func newRecordingLogger(lines *[]map[string]any) *slog.Logger {
	return slog.New(recordingHandler{lines: lines})
}

func (recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h recordingHandler) Handle(_ context.Context, r slog.Record) error {
	line := map[string]any{"msg": r.Message}
	r.Attrs(func(a slog.Attr) bool {
		line[a.Key] = a.Value.Any()
		return true
	})
	*h.lines = append(*h.lines, line)
	return nil
}

func (h recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h recordingHandler) WithGroup(string) slog.Handler      { return h }
