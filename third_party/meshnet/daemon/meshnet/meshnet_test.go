package meshnet

import (
	"bytes"
	"context"
	"testing"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	log "github.com/sirupsen/logrus"
)

func TestNewServerWithLogging(t *testing.T) {
	InitLogger()
	s := newServerWithLogging()
	if s == nil {
		t.Fatalf("expected non-nil grpc.Server")
	}
	s.Stop()
}

func TestInterceptorLogger(t *testing.T) {
	InitLogger()
	var buf bytes.Buffer
	testLogger := log.New()
	testLogger.SetOutput(&buf)
	testLogger.SetLevel(log.DebugLevel)
	testLogger.SetFormatter(&log.TextFormatter{DisableTimestamp: true})

	loggerAdapter := interceptorLogger(testLogger)
	ctx := context.Background()

	tests := []struct {
		name     string
		level    logging.Level
		msg      string
		fields   []any
		wantText string
	}{
		{
			name:     "debug",
			level:    logging.LevelDebug,
			msg:      "debug message",
			fields:   []any{"key1", "val1"},
			wantText: "debug message",
		},
		{
			name:     "info",
			level:    logging.LevelInfo,
			msg:      "info message",
			fields:   []any{"key2", "val2"},
			wantText: "info message",
		},
		{
			name:     "warn",
			level:    logging.LevelWarn,
			msg:      "warn message",
			fields:   []any{"key3", "val3"},
			wantText: "warn message",
		},
		{
			name:     "error",
			level:    logging.LevelError,
			msg:      "error message",
			fields:   []any{"key4", "val4"},
			wantText: "error message",
		},
		{
			name:     "default unknown level",
			level:    logging.Level(99),
			msg:      "unknown level message",
			fields:   []any{"key5", "val5"},
			wantText: "unknown level message",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf.Reset()
			loggerAdapter.Log(ctx, tc.level, tc.msg, tc.fields...)
			out := buf.String()
			if !bytes.Contains([]byte(out), []byte(tc.wantText)) {
				t.Errorf("expected log output to contain %q, got %q", tc.wantText, out)
			}
		})
	}
}

func TestReplaceGrpcLogger(t *testing.T) {
	entry := log.NewEntry(log.StandardLogger())
	replaceGrpcLogger(entry)
}
