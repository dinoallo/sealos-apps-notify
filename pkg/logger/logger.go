// Package logger provides logging functionality for sealos-notify
package logger

import (
	"os"

	log "github.com/sirupsen/logrus"
)

// Option is a functional option for configuring the logger
type Option func(*log.Logger)

// WithLevel sets the log level
func WithLevel(level string) Option {
	return func(l *log.Logger) {
		logLevel, err := log.ParseLevel(level)
		if err != nil {
			log.WithError(err).Warn("Invalid log level, using info")
			logLevel = log.InfoLevel
		}
		l.SetLevel(logLevel)
	}
}

// WithFormat sets the log format (json or text)
func WithFormat(format string) Option {
	return func(l *log.Logger) {
		switch format {
		case "json":
			l.SetReportCaller(false)
			l.SetFormatter(&log.JSONFormatter{
				TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
			})
		case "text":
			l.SetReportCaller(false)
			l.SetFormatter(&log.TextFormatter{
				FullTimestamp:   true,
				TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
			})
		case "debug":
			l.SetFormatter(&log.TextFormatter{
				DisableColors:    !isTerminal(),
				FullTimestamp:    true,
				TimestampFormat:  "2006-01-02T15:04:05.000Z07:00",
				ForceQuote:       true,
				PadLevelText:     true,
				CallerPrettyfier: nil,
			})
			l.SetReportCaller(true)
		default:
			log.WithField("format", format).Warn("Unknown log format, using json")
			l.SetReportCaller(false)
			l.SetFormatter(&log.JSONFormatter{
				TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
			})
		}
	}
}

func isTerminal() bool {
	fileInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}

// WithDebug enables debug mode
func WithDebug(debug bool) Option {
	return func(l *log.Logger) {
		if debug {
			l.SetLevel(log.DebugLevel)
		}
	}
}

// InitLog initializes the logger with the given options
// If logger is nil, creates a new logger; otherwise updates the existing one
func InitLog(logger *log.Logger, opts ...Option) *log.Logger {
	if logger == nil {
		logger = log.New()
	}

	// Apply options
	for _, opt := range opts {
		opt(logger)
	}

	return logger
}
