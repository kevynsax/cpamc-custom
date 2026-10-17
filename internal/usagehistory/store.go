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
	dir        string
	now        func() time.Time
	days       map[string]map[string]Row
	writeErr   error
	settings   Settings
	cycles     map[string]Cycle
	quotaErr   error
	quotaDirty map[string]bool
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
	s := &Store{dir: dir, now: now, days: make(map[string]map[string]Row), settings: emptySettings(), cycles: map[string]Cycle{}, quotaDirty: map[string]bool{}}
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
		data, errRead := os.ReadFile(filepath.Join(dir, file.Name()))
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
		s.days[date] = make(map[string]Row)
		for _, row := range rows {
			if row.Date != date || row.KeyID == "" || row.AccountID == "" || row.Requests < 0 || row.Failed < 0 || row.Failed > row.Requests || row.InputTokens < 0 || row.OutputTokens < 0 || row.CachedTokens < 0 || row.TotalTokens < 0 {
				return nil, fmt.Errorf("invalid usage history row in %s", date)
			}
			if !row.At.IsZero() && row.At.UTC().Format(DateLayout) != date || row.UnpricedRequests < 0 || row.UnpricedRequests > row.Requests || row.CostUSD != nil && (*row.CostUSD < 0 || math.IsNaN(*row.CostUSD) || math.IsInf(*row.CostUSD, 0)) || row.Price != nil && !validRates(*row.Price) {
				return nil, fmt.Errorf("invalid usage pricing in %s", date)
			}
			if _, duplicate := s.days[date][rowKey(row)]; duplicate {
				return nil, fmt.Errorf("duplicate usage history row in %s", date)
			}
			s.days[date][rowKey(row)] = row
		}
	}
	return s, nil
}

func (s *Store) cutoff() string {
	return s.now().UTC().AddDate(0, 0, -(RetentionDays - 1)).Format(DateLayout)
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
	row := Row{At: at.UTC().Truncate(time.Minute), Model: model, Price: rate, Date: date, KeyID: Fingerprint(record.APIKey), AccountID: Fingerprint(account), Provider: record.Provider, AuthType: record.AuthType}
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
	if err := s.prune(); err != nil {
		s.writeErr = err
		return err
	}
	// Retry every dirty day after an earlier disk failure, not just the current day.
	for day := range s.days {
		if day != date && s.writeErr == nil {
			continue
		}
		if err := s.persist(day); err != nil {
			s.writeErr = err
			return err
		}
	}
	s.writeErr = nil
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
	}
	return nil
}

func (s *Store) persist(date string) error {
	rows := make([]Row, 0, len(s.days[date]))
	for _, row := range s.days[date] {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rowKey(rows[i]) < rowKey(rows[j]) })
	data, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("encode usage history: %w", err)
	}
	return atomicJSON(filepath.Join(s.dir, date+".json"), data)
}

func (s *Store) Snapshot(from, to string) ([]Row, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.prune(); err != nil {
		return nil, err
	}
	if s.writeErr != nil {
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
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Date != rows[j].Date {
			return rows[i].Date < rows[j].Date
		}
		return rowKey(rows[i]) < rowKey(rows[j])
	})
	return rows, nil
}
