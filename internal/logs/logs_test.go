package logs_test

import (
	"log"
	"log/slog"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"

	"github.com/meshcloud/meshstack-cli/internal/logs"
)

func TestCaptureFiltersByLevel(t *testing.T) {
	captured := logs.Capture(t)

	slog.DebugContext(t.Context(), "debug")
	slog.InfoContext(t.Context(), "info", "count", 1)
	slog.With("scope", "test").WithGroup("group").WarnContext(t.Context(), "warn", "key", "value")
	slog.ErrorContext(t.Context(), "error")

	assert.Equal(t, []string{"warn", "error"}, messages(captured.Records(slog.LevelWarn)))
	assert.Equal(t, []string{"error"}, messages(captured.Records(slog.LevelError)))
	assert.Equal(t, []string{
		"level=INFO msg=info count=1",
		"level=WARN msg=warn scope=test group.key=value",
		"level=ERROR msg=error",
	}, captured.Lines(slog.LevelInfo))
	assert.Equal(t, "level=DEBUG msg=debug\n"+
		"level=INFO msg=info count=1\n"+
		"level=WARN msg=warn scope=test group.key=value\n"+
		"level=ERROR msg=error\n", captured.String())
}

func TestCaptureForbidsAParallelTest(t *testing.T) {
	logs.Capture(t)

	assert.Panics(t, t.Parallel)
}

func TestCaptureRestoresTheLoggerOnceTheTestEnds(t *testing.T) {
	outer := logs.Capture(t)
	var inner *logs.Captured
	t.Run("inner", func(t *testing.T) {
		inner = logs.Capture(t)
		slog.InfoContext(t.Context(), "to the inner capture")
	})

	slog.InfoContext(t.Context(), "to the outer capture")
	log.Print("through log")

	assert.Equal(t, []string{"to the inner capture"}, messages(inner.Records(slog.LevelDebug)))
	assert.Equal(t, []string{"to the outer capture", "through log"}, messages(outer.Records(slog.LevelDebug)))
}

func TestARestoreOutOfOrderPanics(t *testing.T) {
	logs.Capture(t)
	captureLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(captureLogger) })
	held := logs.Hold(func(logs.Warned) {})
	slog.SetDefault(slog.New(slog.DiscardHandler))

	assert.PanicsWithValue(t, "logs: slog's default logger was replaced while the log was held, and not restored",
		func() { held.Release(t.Context()) })
}

func TestHoldWritesTheHeldRecordsInOrderOnRelease(t *testing.T) {
	captured := logs.Capture(t)
	held := logs.Hold(func(logs.Warned) {})
	logger := slog.With("scope", "prompt").WithGroup("group")

	slog.InfoContext(t.Context(), "first", "count", 1)
	logger.WarnContext(t.Context(), "second", "key", "value")
	slog.DebugContext(t.Context(), "third")
	assert.Empty(t, captured.Records(slog.LevelDebug), "nothing is written while the log is held")
	held.Release(t.Context())
	logger.InfoContext(t.Context(), "after the release")

	assert.Equal(t, []string{
		"level=INFO msg=first count=1",
		"level=WARN msg=second scope=prompt group.key=value",
		"level=DEBUG msg=third",
		"level=INFO msg=\"after the release\" scope=prompt",
	}, captured.Lines(slog.LevelDebug))
}

func TestHoldNotifiesAfterAWarningAndCountsTheOnesThatComeWhileNotifyBlocks(t *testing.T) {
	logs.Capture(t)
	synctest.Test(t, func(t *testing.T) {
		notified, unblock := make(chan logs.Warned, 2), make(chan struct{})
		held := logs.Hold(func(warned logs.Warned) {
			notified <- warned
			<-unblock
		})

		slog.InfoContext(t.Context(), "no warning")
		synctest.Wait()
		assert.Empty(t, notified)

		slog.WarnContext(t.Context(), "first")
		synctest.Wait()
		first := <-notified
		assert.Equal(t, logs.Warned{Warnings: 1, Latest: first.Latest}, first)
		assert.Equal(t, "first", first.Latest.Message)
		slog.WarnContext(t.Context(), "second", "while", "notify blocks")
		slog.ErrorContext(t.Context(), "third")
		synctest.Wait()
		assert.Empty(t, notified)

		unblock <- struct{}{}
		synctest.Wait()
		second := <-notified
		assert.Equal(t, logs.Warned{Warnings: 2, Errors: 1, Latest: second.Latest}, second,
			"one notify counts both records that came while the first blocked")
		assert.Equal(t, "third", second.Latest.Message)

		close(unblock)
		held.Release(t.Context())
	})
}

func TestWarnedNamesTheCountAndTheLatestMessage(t *testing.T) {
	latest := slog.Record{Message: "Cannot read the credential."}
	assert.Empty(t, logs.Warned{}.String())
	assert.Equal(t, "1 warning was logged, read it once you quit: Cannot read the credential.",
		logs.Warned{Warnings: 1, Latest: latest}.String())
	assert.Equal(t, "2 errors were logged, read them once you quit. The latest: Cannot read the credential.",
		logs.Warned{Errors: 2, Latest: latest}.String())
	assert.Equal(t, "1 error and 3 warnings were logged, read them once you quit. The latest: Cannot read the credential.",
		logs.Warned{Warnings: 3, Errors: 1, Latest: latest}.String())
}

func messages(records []slog.Record) (messages []string) {
	for _, record := range records {
		messages = append(messages, record.Message)
	}
	return messages
}
