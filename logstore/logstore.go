package logstore

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/sirupsen/logrus"
)

const (
	DefaultMaxEntries = 10_000
	DefaultMaxBytes   = 50 * 1024 * 1024
	MinMaxEntries     = 500
	MaxMaxEntries     = 50_000
	MinMaxBytes       = 5 * 1024 * 1024
	MaxMaxBytes       = 200 * 1024 * 1024

	defaultQueryLimit = 500
	maxQueryLimit     = 2_000
	maxLogLineBytes   = 16 * 1024
	maxFieldValueLen  = 1024
	pruneEveryEntries = 500
)

type Settings struct {
	MaxEntries int   `json:"maxEntries"`
	MaxBytes   int64 `json:"maxBytes"`
}

type Entry struct {
	ID        uint64            `json:"id"`
	Timestamp string            `json:"ts"`
	Level     string            `json:"level"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
}

type QueryFilter struct {
	Level string
	Query string
	Limit int
}

type Store struct {
	mu          sync.RWMutex
	dir         string
	logPath     string
	settings    Settings
	subscribers map[chan Entry]struct{}
	nextID      atomic.Uint64
	writeCount  atomic.Uint64
	pruneMu     sync.Mutex
}

var defaultStore = New()

func Default() *Store {
	return defaultStore
}

func New() *Store {
	return &Store{
		settings:    DefaultSettings(),
		subscribers: make(map[chan Entry]struct{}),
	}
}

func DefaultSettings() Settings {
	return Settings{
		MaxEntries: DefaultMaxEntries,
		MaxBytes:   DefaultMaxBytes,
	}
}

func LevelName(level logrus.Level) string {
	if level == logrus.WarnLevel {
		return "warn"
	}
	return level.String()
}

func CanonicalLevelName(level string) string {
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "warning" {
		return "warn"
	}
	return level
}

func NormalizeSettings(settings Settings) Settings {
	if settings.MaxEntries == 0 {
		settings.MaxEntries = DefaultMaxEntries
	}
	if settings.MaxBytes == 0 {
		settings.MaxBytes = DefaultMaxBytes
	}
	if settings.MaxEntries < MinMaxEntries {
		settings.MaxEntries = MinMaxEntries
	}
	if settings.MaxEntries > MaxMaxEntries {
		settings.MaxEntries = MaxMaxEntries
	}
	if settings.MaxBytes < MinMaxBytes {
		settings.MaxBytes = MinMaxBytes
	}
	if settings.MaxBytes > MaxMaxBytes {
		settings.MaxBytes = MaxMaxBytes
	}
	return settings
}

func Init(cfgDir string) error {
	return defaultStore.Init(cfgDir)
}

func (s *Store) Init(cfgDir string) error {
	logDir := filepath.Join(cfgDir, "logs")
	if err := os.MkdirAll(logDir, 0750); err != nil {
		return fmt.Errorf("create log cache dir: %w", err)
	}
	settings, err := loadSettings()
	if err != nil {
		return fmt.Errorf("load log settings: %w", err)
	}

	s.mu.Lock()
	s.dir = logDir
	s.logPath = filepath.Join(logDir, "current.jsonl")
	s.settings = settings
	s.mu.Unlock()

	nextID, err := readLastLogID(s.logPath)
	if err != nil {
		return err
	}
	s.nextID.Store(nextID)
	return s.Prune()
}

func loadSettings() (Settings, error) {
	if !db.IsInitialized() {
		return DefaultSettings(), nil
	}
	settings, err := db.GetLogSetting(context.Background())
	if err != nil {
		return DefaultSettings(), err
	}
	if settings == nil {
		return DefaultSettings(), nil
	}
	return NormalizeSettings(Settings{
		MaxEntries: settings.MaxEntries,
		MaxBytes:   settings.MaxBytes,
	}), nil
}

func saveSettings(settings Settings) error {
	if !db.IsInitialized() {
		return nil
	}
	return db.SaveLogSetting(context.Background(), db.LogSetting{
		MaxEntries: settings.MaxEntries,
		MaxBytes:   settings.MaxBytes,
	})
}

func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

func (s *Store) SetSettings(settings Settings) (Settings, error) {
	settings = NormalizeSettings(settings)
	if err := saveSettings(settings); err != nil {
		return settings, err
	}

	s.mu.Lock()
	s.settings = settings
	s.mu.Unlock()

	return settings, s.Prune()
}

func (s *Store) Clear() error {
	s.mu.RLock()
	logPath := s.logPath
	s.mu.RUnlock()
	if logPath == "" {
		return nil
	}
	s.pruneMu.Lock()
	defer s.pruneMu.Unlock()
	return os.WriteFile(logPath, nil, 0600)
}

func (s *Store) Query(filter QueryFilter) ([]Entry, error) {
	filter.Level = strings.ToLower(strings.TrimSpace(filter.Level))
	filter.Query = strings.ToLower(strings.TrimSpace(filter.Query))
	if filter.Limit <= 0 {
		filter.Limit = defaultQueryLimit
	}
	if filter.Limit > maxQueryLimit {
		filter.Limit = maxQueryLimit
	}

	s.mu.RLock()
	logPath := s.logPath
	s.mu.RUnlock()
	if logPath == "" {
		return nil, nil
	}

	file, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	results := make([]Entry, 0, min(filter.Limit, 256))
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxLogLineBytes*2)
	for scanner.Scan() {
		var entry Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if !entryMatchesFilter(entry, filter) {
			continue
		}
		if len(results) == filter.Limit {
			copy(results, results[1:])
			results[len(results)-1] = entry
			continue
		}
		results = append(results, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *Store) Subscribe(buffer int) (<-chan Entry, func()) {
	if buffer <= 0 {
		buffer = 100
	}
	ch := make(chan Entry, buffer)
	s.mu.Lock()
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()

	return ch, func() {
		s.mu.Lock()
		delete(s.subscribers, ch)
		close(ch)
		s.mu.Unlock()
	}
}

func (s *Store) Fire(logEntry *logrus.Entry) error {
	if logEntry == nil {
		return nil
	}

	entry := Entry{
		ID:        s.nextID.Add(1),
		Timestamp: logTimestamp(logEntry).Format(time.RFC3339Nano),
		Level:     LevelName(logEntry.Level),
		Message:   trimString(logEntry.Message, maxLogLineBytes),
		Fields:    logFields(logEntry.Data),
	}
	line, err := encodeEntryLine(entry)
	if err != nil {
		return nil
	}

	s.mu.RLock()
	logPath := s.logPath
	settings := s.settings
	subscribers := make([]chan Entry, 0, len(s.subscribers))
	for subscriber := range s.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	s.mu.RUnlock()

	shouldPrune := false
	if logPath != "" {
		s.pruneMu.Lock()
		if err := appendLogLine(logPath, line); err == nil {
			writeCount := s.writeCount.Add(1)
			if writeCount%pruneEveryEntries == 0 || int64(len(line)) > settings.MaxBytes/10 {
				shouldPrune = true
			} else if info, statErr := os.Stat(logPath); statErr == nil && info.Size() > settings.MaxBytes {
				shouldPrune = true
			}
		}
		s.pruneMu.Unlock()
	}
	if shouldPrune {
		go func() { _ = s.Prune() }()
	}

	for _, subscriber := range subscribers {
		select {
		case subscriber <- entry:
		default:
		}
	}
	return nil
}

func (s *Store) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (s *Store) Prune() error {
	s.pruneMu.Lock()
	defer s.pruneMu.Unlock()

	s.mu.RLock()
	logPath := s.logPath
	settings := s.settings
	s.mu.RUnlock()
	if logPath == "" {
		return nil
	}

	data, err := readTailBytes(logPath, settings.MaxBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}

	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	if len(lines) > settings.MaxEntries {
		lines = lines[len(lines)-settings.MaxEntries:]
	}
	pruned := bytes.Join(lines, []byte("\n"))
	if len(pruned) > 0 {
		pruned = append(pruned, '\n')
	}
	tmpPath := logPath + ".tmp"
	if err := os.WriteFile(tmpPath, pruned, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, logPath)
}

func appendLogLine(path string, line []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(line)
	return err
}

func encodeEntryLine(entry Entry) ([]byte, error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	if len(data) > maxLogLineBytes {
		entry.Message = trimString(entry.Message, maxLogLineBytes/2)
		entry.Fields = trimFields(entry.Fields, 256)
		data, err = json.Marshal(entry)
		if err != nil {
			return nil, err
		}
	}
	data = append(data, '\n')
	return data, nil
}

func readTailBytes(path string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size <= 0 {
		return nil, nil
	}
	offset := int64(0)
	if size > maxBytes {
		offset = size - maxBytes
	}
	if _, err := file.Seek(offset, 0); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		if newline := bytes.IndexByte(data, '\n'); newline >= 0 && newline+1 < len(data) {
			data = data[newline+1:]
		}
	}
	return data, nil
}

func readLastLogID(path string) (uint64, error) {
	data, err := readTailBytes(path, 1024*1024)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		var entry Entry
		if err := json.Unmarshal(lines[i], &entry); err == nil {
			return entry.ID, nil
		}
	}
	return 0, nil
}

func logTimestamp(entry *logrus.Entry) time.Time {
	if entry.Time.IsZero() {
		return time.Now()
	}
	return entry.Time
}

func logFields(fields logrus.Fields) map[string]string {
	if len(fields) == 0 {
		return nil
	}
	result := make(map[string]string, len(fields))
	for key, value := range fields {
		result[key] = trimString(fmt.Sprint(value), maxFieldValueLen)
	}
	return result
}

func trimFields(fields map[string]string, maxValueLen int) map[string]string {
	if len(fields) == 0 {
		return nil
	}
	result := make(map[string]string, len(fields))
	for key, value := range fields {
		result[key] = trimString(value, maxValueLen)
	}
	return result
}

func trimString(value string, maxLen int) string {
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "..."
}

func entryMatchesFilter(entry Entry, filter QueryFilter) bool {
	if filter.Level != "" && filter.Level != "all" && CanonicalLevelName(entry.Level) != filter.Level {
		return false
	}
	if filter.Query == "" {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Message), filter.Query) {
		return true
	}
	for key, value := range entry.Fields {
		if strings.Contains(strings.ToLower(key), filter.Query) || strings.Contains(strings.ToLower(value), filter.Query) {
			return true
		}
	}
	return false
}
