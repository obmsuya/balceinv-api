package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type DatedFileWriter struct {
	directory   string
	mutex       sync.Mutex
	currentDate string
	currentFile *os.File
}

func Setup(logDirectory string) (*DatedFileWriter, error) {
	createDirectoryError := os.MkdirAll(logDirectory, 0o750)
	if createDirectoryError != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", createDirectoryError)
	}

	fileWriter := &DatedFileWriter{
		directory: logDirectory,
	}

	combinedWriter := io.MultiWriter(fileWriter, os.Stdout)

	handlerOptions := &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: formatTimeOfDay,
	}

	textHandler := slog.NewTextHandler(combinedWriter, handlerOptions)
	slog.SetDefault(slog.New(textHandler))

	return fileWriter, nil
}

func (writer *DatedFileWriter) Write(logBytes []byte) (int, error) {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()

	rotateError := writer.rotateIfNewDay()
	if rotateError != nil {
		return 0, rotateError
	}

	return writer.currentFile.Write(logBytes)
}

func (writer *DatedFileWriter) WriteSeparator() {
	writer.Write([]byte("\n"))
}

func (writer *DatedFileWriter) Close() error {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()

	hasOpenFile := writer.currentFile != nil
	if !hasOpenFile {
		return nil
	}

	return writer.currentFile.Close()
}

func (writer *DatedFileWriter) rotateIfNewDay() error {
	todayDate := time.Now().UTC().Format("2006-01-02")
	isSameDay := todayDate == writer.currentDate && writer.currentFile != nil
	if isSameDay {
		return nil
	}

	hasPreviousFile := writer.currentFile != nil
	if hasPreviousFile {
		writer.currentFile.Close()
	}

	logPath := filepath.Join(writer.directory, todayDate+".log")
	openedFile, openError := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if openError != nil {
		return fmt.Errorf("failed to open log file %s: %w", logPath, openError)
	}

	writer.currentFile = openedFile
	writer.currentDate = todayDate

	return nil
}

func formatTimeOfDay(groups []string, attribute slog.Attr) slog.Attr {
	isTopLevelTime := len(groups) == 0 && attribute.Key == slog.TimeKey
	if !isTopLevelTime {
		return attribute
	}

	loggedAt := attribute.Value.Time().UTC()
	return slog.String(slog.TimeKey, loggedAt.Format("15:04:05.000"))
}
