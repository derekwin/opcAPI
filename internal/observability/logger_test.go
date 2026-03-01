package observability

import (
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  slog.Level
	}{
		{name: "debug", input: "debug", want: slog.LevelDebug},
		{name: "warn", input: "warn", want: slog.LevelWarn},
		{name: "warning", input: "warning", want: slog.LevelWarn},
		{name: "error", input: "error", want: slog.LevelError},
		{name: "unknown defaults info", input: "x", want: slog.LevelInfo},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseLevel(tc.input); got != tc.want {
				t.Fatalf("parseLevel(%q)=%v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestNewLogger(t *testing.T) {
	logger := NewLogger("debug")
	if logger == nil {
		t.Fatalf("expected logger instance")
	}
}
