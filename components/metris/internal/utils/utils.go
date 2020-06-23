package utils

import (
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TrackTime simply write to logger the time it took to run a task.
func TrackTime(name string, start time.Time, logger *zap.SugaredLogger) {
	elapsed := time.Since(start)
	logger.Debugf("%s took %s", name, elapsed)
}

// SetupSignalHandler registers for SIGTERM and SIGINT, a context is returned
// which is cancel when a signals is caught.
func SetupSignalHandler(shutdown func()) {
	sigbuf := 2
	signals := []os.Signal{os.Interrupt, syscall.SIGTERM}
	c := make(chan os.Signal, sigbuf)
	signal.Notify(c, signals...)

	go func() {
		<-c
		shutdown()
		<-c
		os.Exit(1)
	}()
}

// LogLevelDecoder is a mapper function used to decode the cli argument into the field.
func LogLevelDecoder() kong.MapperFunc {
	return func(ctx *kong.DecodeContext, target reflect.Value) error {
		var (
			level    string
			loglevel zapcore.Level
		)

		if err := ctx.Scan.PopValueInto("level", &level); err != nil {
			return err
		}

		err := loglevel.UnmarshalText([]byte(level))
		if err != nil {
			return fmt.Errorf("expected log level but got %q: %s", level, err)
		}

		target.Set(reflect.ValueOf(zap.NewAtomicLevelAt(loglevel)))

		return nil
	}
}
