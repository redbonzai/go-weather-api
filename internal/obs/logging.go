package obs

import (
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"
)

// Logger is a small wrapper around the stdlib logger that supports
// key=value structured lines and bound fields.
type Logger struct {
	baseLogger *log.Logger
	mutex      sync.Mutex
	fields     map[string]any
}

// NewLogger creates a new Logger writing to the provided writer.
// Use os.Stdout for normal service logging.
func NewLogger(writer io.Writer) *Logger {
	return &Logger{
		baseLogger: log.New(writer, "", 0), // we add our own timestamp
		fields:     make(map[string]any),
	}
}

// With returns a new Logger with additional bound fields.
func (logger *Logger) With(extraFields map[string]any) *Logger {
	logger.mutex.Lock()
	defer logger.mutex.Unlock()

	merged := make(map[string]any, len(logger.fields)+len(extraFields))
	for key, value := range logger.fields {
		merged[key] = value
	}
	for key, value := range extraFields {
		merged[key] = value
	}

	return &Logger{
		baseLogger: logger.baseLogger,
		fields:     merged,
	}
}

func (logger *Logger) Info(message string, fields map[string]any) {
	logger.write("INFO", message, fields)
}

func (logger *Logger) Error(message string, fields map[string]any) {
	logger.write("ERROR", message, fields)
}

func (logger *Logger) write(level string, message string, fields map[string]any) {
	mergedFields := logger.mergeFields(fields)

	// RFC3339Nano keeps ordering clear under concurrency.
	timestamp := time.Now().Format(time.RFC3339Nano)

	var builder strings.Builder
	builder.WriteString("ts=")
	builder.WriteString(timestamp)
	builder.WriteString(" level=")
	builder.WriteString(level)
	builder.WriteString(" msg=")
	builder.WriteString(fmt.Sprintf("%q", message))

	for key, value := range mergedFields {
		builder.WriteString(" ")
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(fmt.Sprintf("%v", value))
	}

	logger.baseLogger.Println(builder.String())
}

func (logger *Logger) mergeFields(fields map[string]any) map[string]any {
	logger.mutex.Lock()
	defer logger.mutex.Unlock()

	merged := make(map[string]any, len(logger.fields)+len(fields))
	for key, value := range logger.fields {
		merged[key] = value
	}
	for key, value := range fields {
		merged[key] = value
	}
	return merged
}