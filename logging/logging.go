package logging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
)

func SetupLogging(settings config.LoggingSettings) {
	level, err := logrus.ParseLevel(settings.Level)
	if err != nil {
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)

	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
		DisableColors:   false,
		ForceColors:     true,
	})

	logrus.SetOutput(os.Stdout)

	if settings.File != "" {
		if err := os.MkdirAll(filepath.Dir(settings.File), 0755); err == nil {
			file, err := os.OpenFile(settings.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
			if err == nil {
				logrus.AddHook(newJSONFileHook(file))
			} else {
				logrus.Warnf("Failed to open log file %s, logging to stdout only: %v", settings.File, err)
			}
		}
	}

	logrus.Debugf("Logging configured: level=%s file=%s", settings.Level, settings.File)
}

type jsonFileHook struct {
	file *os.File
	mu   sync.Mutex
}

func newJSONFileHook(file *os.File) *jsonFileHook {
	return &jsonFileHook{file: file}
}

func (h *jsonFileHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *jsonFileHook) Fire(entry *logrus.Entry) error {
	payload := map[string]interface{}{
		"time":    entry.Time.UTC().Format("2006-01-02T15:04:05.000Z"),
		"level":   entry.Level.String(),
		"message": entry.Message,
	}

	if len(entry.Data) > 0 {
		fields := make(map[string]interface{}, len(entry.Data))
		for k, v := range entry.Data {
			fields[k] = v
		}
		payload["fields"] = fields
	}

	line, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	_, err = h.file.Write(append(line, '\n'))
	return err
}
