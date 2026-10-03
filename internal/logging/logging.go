package logging

import (
	"fmt"
	"os"

	flog "github.com/jeffotoni/log"
)

type Logger struct {
	logger *flog.Logger
}

func New(format, serviceName string) *Logger {
	logFormat := flog.FormatJSON
	if format == "text" {
		logFormat = flog.FormatText
	}

	return &Logger{
		logger: flog.New(flog.Config{
			Format:      logFormat,
			Writer:      os.Stdout,
			Level:       flog.INFO,
			ServiceName: serviceName,
		}),
	}
}

func (l *Logger) Info(message string, args ...any) {
	l.send(l.logger.Info(), message, args...)
}

func (l *Logger) Warn(message string, args ...any) {
	l.send(l.logger.Warn(), message, args...)
}

func (l *Logger) Error(message string, args ...any) {
	l.send(l.logger.Error(), message, args...)
}

func (l *Logger) send(entry *flog.Entry, message string, args ...any) {
	for i := 0; i < len(args); i += 2 {
		key := fmt.Sprint(args[i])
		if key == "" {
			continue
		}
		var value any
		if i+1 < len(args) {
			value = args[i+1]
		}
		if err, ok := value.(error); ok {
			entry.Err(key, err)
			continue
		}
		entry.Any(key, value)
	}
	_ = entry.Msg(message).Send()
}
