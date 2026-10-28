package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

var (
	appFile   *os.File
	errorFile *os.File
)

// InitLogger initializes file loggers in the specified directory.
func InitLogger(logDir string) error {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log dir: %w", err)
	}

	var err error
	appLogPath := filepath.Join(logDir, "app.log")
	appFile, err = os.OpenFile(appLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("failed to open app.log: %w", err)
	}

	errorLogPath := filepath.Join(logDir, "error.log")
	errorFile, err = os.OpenFile(errorLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("failed to open error.log: %w", err)
	}

	// Mirror standard log output to both stdout and app.log
	multiWriter := io.MultiWriter(os.Stdout, appFile)
	log.SetOutput(multiWriter)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	Info("Logger initialized. Logs are stored in %s", logDir)
	return nil
}

// Info logs informational messages.
func Info(format string, v ...any) {
	msg := fmt.Sprintf("[%s] [INFO] %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, v...))
	fmt.Print(msg)
	if appFile != nil {
		_, _ = appFile.WriteString(msg)
	}
}

// Warn logs warning messages.
func Warn(format string, v ...any) {
	msg := fmt.Sprintf("[%s] [WARN] %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, v...))
	fmt.Print(msg)
	if appFile != nil {
		_, _ = appFile.WriteString(msg)
	}
}

// Error logs error messages to both app.log and error.log.
func Error(format string, v ...any) {
	msg := fmt.Sprintf("[%s] [ERROR] %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, v...))
	fmt.Print(msg)
	if appFile != nil {
		_, _ = appFile.WriteString(msg)
	}
	if errorFile != nil {
		_, _ = errorFile.WriteString(msg)
	}
}

// Close closes open log file descriptors.
func Close() {
	if appFile != nil {
		_ = appFile.Close()
	}
	if errorFile != nil {
		_ = errorFile.Close()
	}
}
