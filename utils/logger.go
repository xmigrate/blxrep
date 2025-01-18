package utils

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

var (
	logFile *os.File
	logger  *log.Logger
	logMu   sync.Mutex
)

func InitLogging(logDir string) error {
	logMu.Lock()
	defer logMu.Unlock()

	if logger != nil {
		return nil // Already initialized
	}

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %v", err)
	}

	logPath := filepath.Join(logDir, "blxrep.log")
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %v", err)
	}

	logFile = file
	logger = log.New(file, "", log.LstdFlags)
	return nil
}

func LogDebug(message string) {
	logMu.Lock()
	defer logMu.Unlock()

	if logger != nil {
		logger.Printf("[DEBUG] %s", message)
	}
}

func LogError(message string) {
	logMu.Lock()
	defer logMu.Unlock()

	if logger != nil {
		logger.Printf("[ERROR] %s", message)
	}
}

func CloseLogFile() {
	logMu.Lock()
	defer logMu.Unlock()

	if logFile != nil {
		logFile.Close()
	}
}
