// Package usagehistory persists daily client-key/account usage without credentials.
// CUSTOM (cpamc-custom): client-key to subscription accounting; see CUSTOM_FEATURES.md.
package usagehistory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

const RetentionDays = 365
const DateLayout = "2006-01-02"

// Per-minute rows are folded into hourly rows after minuteDetailDays; only the last
// residentDays stay in memory, older days are read from disk on demand.
const (
	minuteDetailDays = 7
	residentDays     = 31
	flushInterval    = 10 * time.Second
)

// Row is one UTC day's consumption by a client key on an upstream account.
type Row struct {
	At               time.Time `json:"at,omitempty"`
	Model            string    `json:"model,omitempty"`
	Price            *Rates    `json:"price,omitempty"`
	CostUSD          *float64  `json:"cost_usd"`
	UnpricedRequests int64     `json:"unpriced_requests"`
	Date             string    `json:"date"`
	KeyID            string    `json:"key_id"`
	AccountID        string    `json:"account_id"`
	Provider         string    `json:"provider"`
	AuthType         string    `json:"auth_type"`
	Requests         int64     `json:"requests"`
	Failed           int64     `json:"failed"`
	InputTokens      int64     `json:"input_tokens"`
	OutputTokens     int64     `json:"output_tokens"`
	CachedTokens     int64     `json:"cached_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
}

// Store has one writer per gateway instance; do not share its directory across replicas.
type Store struct {
	mu         sync.Mutex
	flushMu    sync.Mutex
	dir        string
	now        func() time.Time
	days       map[string]map[string]Row
	dirty      map[string]bool
	foldedOn   string
	writeErr   error
	settings   Settings
	keyNames   map[string]string
	keyKinds   map[string]string
	cycles     map[string]Cycle
	quotaErr   error
	quotaDirty map[string]bool
	stop       chan struct{}
	stopped    chan struct{}
	closeOnce  sync.Once
}

func Fingerprint(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func Open(dir string, now func() time.Time) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create usage history: %w", err)
	}
	s := &Store{dir: dir, now: now, days: make(map[string]map[string]Row), dirty: map[string]bool{}, settings: emptySettings(), cycles: map[string]Cycle{}, quotaDirty: map[string]bool{}, stop: make(chan struct{}), stopped: make(chan struct{})}
	for name, target := range map[string]any{"settings.json": &s.settings} {
		data, errRead := os.ReadFile(filepath.Join(dir, name))
		if os.IsNotExist(errRead) {
			continue
		}
		if errRead != nil {
			return nil, fmt.Errorf("read usage metadata: %w", errRead)
		}
		if errDecode := json.Unmarshal(data, target); errDecode != nil {
			return nil, fmt.Errorf("decode usage metadata: %w", errDecode)
		}
		if s.settings.Names == nil || s.settings.Plans == nil || s.settings.Prices == nil {
			return nil, fmt.Errorf("invalid usage metadata in %s", name)
		}
	}
	if errLoad := s.loadKeyNames(); errLoad != nil {
		return nil, errLoad
	}
	if errLoad := s.loadCycles(); errLoad != nil {
		return nil, errLoad
	}
	if errPrune := s.pruneCycles(); errPrune != nil {
		return nil, errPrune
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list usage history: %w", err)
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		date := strings.TrimSuffix(file.Name(), ".json")
		if _, errDate := time.Parse(DateLayout, date); errDate != nil {
			continue
		}
		if date < s.cutoff() {
			if errRemove := os.Remove(filepath.Join(dir, file.Name())); errRemove != nil {
				return nil, fmt.Errorf("expire usage history: %w", errRemove)
			}
			continue
		}
		day, errRead := s.readDay(date)
		if errRead != nil {
			return nil, errRead
		}
		folded := false
		if date < s.foldCutoff() {
			day, folded = foldHourly(day)
		}
		if date >= s.residentCutoff() {
			s.days[date] = day
			if folded {
				s.dirty[date] = true
			}
			continue
		}
		if folded {
			if errPersist := persistRows(s.dir, date, day); errPersist != nil {
				log.WithError(errPersist).Warn("Fold usage history to hourly rows")
			}
		}
	}
	s.foldedOn = s.today()
	go s.flushLoop()
	return s, nil
}

func (s *Store) cutoff() string {
	return s.now().UTC().AddDate(0, 0, -(RetentionDays - 1)).Format(DateLayout)
}

func (s *Store) residentCutoff() string {
	return s.now().UTC().AddDate(0, 0, -(residentDays - 1)).Format(DateLayout)
}

func (s *Store) foldCutoff() string {
	return s.now().UTC().AddDate(0, 0, -(minuteDetailDays - 1)).Format(DateLayout)
}

func (s *Store) today() string { return s.now().UTC().Format(DateLayout) }

// readDay validates one day file; a missing file is an empty day.
func (s *Store) readDay(date string) (map[string]Row, error) {
	data, errRead := os.ReadFile(filepath.Join(s.dir, date+".json"))
	if os.IsNotExist(errRead) {
		return make(map[string]Row), nil
	}
	if errRead != nil {
		return nil, fmt.Errorf("read usage history: %w", errRead)
	}
	var rows []Row
	if errDecode := json.Unmarshal(data, &rows); errDecode != nil {
		return nil, fmt.Errorf("decode usage history %s: %w", date, errDecode)
	}
	if rows == nil {
		return nil, fmt.Errorf("invalid usage history in %s", date)
	}
	day := make(map[string]Row, len(rows))
	for _, row := range rows {
		if row.Date != date || row.KeyID == "" || row.AccountID == "" || row.Requests < 0 || row.Failed < 0 || row.Failed > row.Requests || row.InputTokens < 0 || row.OutputTokens < 0 || row.CachedTokens < 0 || row.TotalTokens < 0 {
			return nil, fmt.Errorf("invalid usage history row in %s", date)
		}
		if !row.At.IsZero() && row.At.UTC().Format(DateLayout) != date || row.UnpricedRequests < 0 || row.UnpricedRequests > row.Requests || row.CostUSD != nil && (*row.CostUSD < 0 || math.IsNaN(*row.CostUSD) || math.IsInf(*row.CostUSD, 0)) || row.Price != nil && !validRates(*row.Price) {
			return nil, fmt.Errorf("invalid usage pricing in %s", date)
		}
		if _, duplicate := day[rowKey(row)]; duplicate {
			return nil, fmt.Errorf("duplicate usage history row in %s", date)
		}
		day[rowKey(row)] = row
	}
	return day, nil
}

// foldHourly merges per-minute rows into hourly rows; totals are unchanged.
func foldHourly(day map[string]Row) (map[string]Row, bool) {
	changed := false
	folded := make(map[string]Row, len(day))
	for _, row := range day {
		if hour := row.At.Truncate(time.Hour); !row.At.Equal(hour) {
			row.At = hour
			changed = true
		}
		key := rowKey(row)
		if previous, ok := folded[key]; ok {
			row = addRow(previous, row)
			changed = true
		}
		folded[key] = row
	}
	return folded, changed
}

func addRow(total, row Row) Row {
	total.Requests += row.Requests
	total.Failed += row.Failed
	total.UnpricedRequests += row.UnpricedRequests
	total.InputTokens += row.InputTokens
	total.OutputTokens += row.OutputTokens
	total.CachedTokens += row.CachedTokens
	total.TotalTokens += row.TotalTokens
	if row.CostUSD != nil {
		cost := *row.CostUSD
		if total.CostUSD != nil {
			cost += *total.CostUSD
		}
		total.CostUSD = &cost
	}
	return total
}
func rowKey(row Row) string {
	price, _ := json.Marshal(row.Price)
	return row.KeyID + "|" + row.AccountID + "|" + row.Provider + "|" + row.AuthType + "|" + row.At.Format(time.RFC3339) + "|" + row.Model + "|" + string(price)
}

// HandleUsage runs on the usage dispatcher, not the inference request goroutine.
func (s *Store) HandleUsage(ctx context.Context, record usage.Record) {
	if err := s.Record(record); err != nil {
		log.WithError(err).Error("Persist client subscription usage")
	}
}

func (s *Store) Record(record usage.Record) error { return s.RecordPriced(record, nil) }

func (s *Store) RecordPriced(record usage.Record, rate *Rates) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	at := record.RequestedAt
	if at.IsZero() {
		at = s.now()
	}
	date := at.UTC().Format(DateLayout)
	if date < s.cutoff() {
		return nil
	}
	account := record.AuthID
	if account == "" {
		account = record.AuthIndex
	}
	model := record.ResponseModel
	if model == "" {
		model = record.Model
	}
	minute := at.UTC().Truncate(time.Minute)
	if date < s.foldCutoff() {
		minute = minute.Truncate(time.Hour)
	}
	row := Row{At: minute, Model: model, Price: rate, Date: date, KeyID: Fingerprint(record.APIKey), AccountID: Fingerprint(account), Provider: record.Provider, AuthType: record.AuthType}
	if s.days[date] == nil && date < s.residentCutoff() {
		// A late record for a day outside the resident window merges into its file at the next flush.
		day, errRead := s.readDay(date)
		if errRead != nil {
			return errRead
		}
		s.days[date] = day
	}
	if s.days[date] == nil {
		s.days[date] = make(map[string]Row)
	}
	key := rowKey(row)
	if previous, ok := s.days[date][key]; ok {
		row = previous
	}
	row.Requests++
	if record.Failed {
		row.Failed++
	}
	detail := usage.EnsureTokenBreakdownForProvider(record.Detail, record.Provider, record.ExecutorType)
	row.InputTokens += max(0, detail.InputTokens)
	row.OutputTokens += max(0, detail.OutputTokens)
	row.CachedTokens += max(0, detail.CachedTokens, detail.CacheReadTokens)
	row.TotalTokens += max(0, detail.TotalTokens)
	if cost := EstimateCost(detail, rate); cost != nil {
		total := *cost
		if row.CostUSD != nil {
			total += *row.CostUSD
		}
		row.CostUSD = &total
	} else {
		row.UnpricedRequests++
	}
	s.days[date][key] = row
	s.dirty[date] = true
	return nil
}

// Flush persists every pending day, folds days that aged past minuteDetailDays and
// releases days that left the resident window. It also runs every flushInterval.
// Files are written without holding the store lock so recording never waits on disk.
func (s *Store) Flush() error {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	s.mu.Lock()
	expire, err := s.maintainLocked()
	if err != nil {
		s.writeErr = err
		s.mu.Unlock()
		return err
	}
	pending := make(map[string][]byte, len(s.dirty))
	for date := range s.dirty {
		data, errEncode := encodeDay(s.days[date])
		if errEncode != nil {
			s.writeErr = errEncode
			s.mu.Unlock()
			return errEncode
		}
		pending[date] = data
		delete(s.dirty, date)
	}
	s.mu.Unlock()

	if expire {
		err = s.expireFiles()
	}
	for date, data := range pending {
		if err != nil {
			break
		}
		if errWrite := atomicJSON(filepath.Join(s.dir, date+".json"), data); errWrite != nil {
			err = errWrite
		} else {
			delete(pending, date)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		for date := range pending {
			if s.days[date] != nil {
				s.dirty[date] = true
			}
		}
		if expire {
			s.foldedOn = ""
		}
		s.writeErr = err
		return err
	}
	s.writeErr = nil
	for date := range s.days {
		if date < s.residentCutoff() && !s.dirty[date] {
			delete(s.days, date)
		}
	}
	return nil
}

// Close stops the background writer and flushes pending usage.
func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		close(s.stop)
		<-s.stopped
	})
	return s.Flush()
}

func (s *Store) flushLoop() {
	defer close(s.stopped)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	var failing bool
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			err := s.Flush()
			if err != nil && !failing {
				log.WithError(err).Error("Persist client usage history")
			}
			failing = err != nil
		}
	}
}

// maintainLocked prunes expired days and, on the first flush of a UTC day, folds days
// that aged past minuteDetailDays. It reports whether expired files must be removed.
func (s *Store) maintainLocked() (bool, error) {
	if err := s.prune(); err != nil {
		return false, err
	}
	today := s.today()
	if s.foldedOn == today {
		return false, nil
	}
	for date, day := range s.days {
		if date >= s.foldCutoff() {
			continue
		}
		if folded, changed := foldHourly(day); changed {
			s.days[date] = folded
			s.dirty[date] = true
		}
	}
	s.foldedOn = today
	return true, nil
}

func (s *Store) expireFiles() error {
	files, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("list usage history: %w", err)
	}
	for _, file := range files {
		date := strings.TrimSuffix(file.Name(), ".json")
		if file.IsDir() || date == file.Name() || date >= s.cutoff() {
			continue
		}
		if _, errDate := time.Parse(DateLayout, date); errDate != nil {
			continue
		}
		if errRemove := os.Remove(filepath.Join(s.dir, file.Name())); errRemove != nil && !os.IsNotExist(errRemove) {
			return fmt.Errorf("expire usage history: %w", errRemove)
		}
	}
	return nil
}

func (s *Store) prune() error {
	for date := range s.days {
		if date >= s.cutoff() {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, date+".json")); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("expire usage history: %w", err)
		}
		delete(s.days, date)
		delete(s.dirty, date)
	}
	return nil
}

func encodeDay(day map[string]Row) ([]byte, error) {
	rows := make([]Row, 0, len(day))
	for _, row := range day {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rowKey(rows[i]) < rowKey(rows[j]) })
	data, err := json.Marshal(rows)
	if err != nil {
		return nil, fmt.Errorf("encode usage history: %w", err)
	}
	return data, nil
}

func persistRows(dir, date string, day map[string]Row) error {
	data, err := encodeDay(day)
	if err != nil {
		return err
	}
	return atomicJSON(filepath.Join(dir, date+".json"), data)
}

func (s *Store) Snapshot(from, to string) ([]Row, error) {
	s.mu.Lock()
	if err := s.prune(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if s.writeErr != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("usage history persistence unavailable: %w", s.writeErr)
	}
	rows := make([]Row, 0)
	for date, day := range s.days {
		if date < from || date > to {
			continue
		}
		for _, row := range day {
			rows = append(rows, row)
		}
	}
	// Days outside the resident window are read without holding the lock or caching them.
	var onDisk []string
	resident := s.residentCutoff()
	for at, errAt := time.Parse(DateLayout, max(from, s.cutoff())); errAt == nil; at = at.AddDate(0, 0, 1) {
		date := at.Format(DateLayout)
		if date > to || date >= resident {
			break
		}
		if s.days[date] == nil {
			onDisk = append(onDisk, date)
		}
	}
	s.mu.Unlock()
	for _, date := range onDisk {
		day, err := s.readDay(date)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		if loaded := s.days[date]; loaded != nil {
			day = loaded
		}
		for _, row := range day {
			rows = append(rows, row)
		}
		s.mu.Unlock()
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Date != rows[j].Date {
			return rows[i].Date < rows[j].Date
		}
		return rowKey(rows[i]) < rowKey(rows[j])
	})
	return rows, nil
}
