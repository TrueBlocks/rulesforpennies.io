package ratelimit

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Limiter struct {
	mu       sync.Mutex
	dataDir  string
	dailyCap float64
	sessions map[string]*sessionData
	spend    *dailySpend
}

type sessionData struct {
	RulingCount int
	LastReset   string // date string YYYY-MM-DD
}

type dailySpend struct {
	Date      string  `json:"date"`
	TotalCost float64 `json:"total_cost"`
}

type Status struct {
	SoftCapped bool
	HardCapped bool
	Remaining  int
}

const maxRulingsPerDay = 10

func New(dataDir string, dailyCap float64) *Limiter {
	l := &Limiter{
		dataDir:  dataDir,
		dailyCap: dailyCap,
		sessions: make(map[string]*sessionData),
		spend:    &dailySpend{},
	}
	l.loadSpend()
	return l
}

func (l *Limiter) Check(sessionToken string) Status {
	l.mu.Lock()
	defer l.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	l.resetIfNewDay(today)

	sess := l.getSession(sessionToken, today)

	softThreshold := l.dailyCap * 0.8
	return Status{
		SoftCapped: l.spend.TotalCost >= softThreshold,
		HardCapped: l.spend.TotalCost >= l.dailyCap || sess.RulingCount >= maxRulingsPerDay,
		Remaining:  maxRulingsPerDay - sess.RulingCount,
	}
}

func (l *Limiter) Record(sessionToken string, cost float64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	sess := l.getSession(sessionToken, today)
	sess.RulingCount++

	l.spend.TotalCost += cost
	l.saveSpend()
}

func (l *Limiter) getSession(token, today string) *sessionData {
	sess, ok := l.sessions[token]
	if !ok || sess.LastReset != today {
		sess = &sessionData{LastReset: today}
		l.sessions[token] = sess
	}
	return sess
}

func (l *Limiter) resetIfNewDay(today string) {
	if l.spend.Date != today {
		l.spend = &dailySpend{Date: today}
		l.sessions = make(map[string]*sessionData)
		l.saveSpend()
	}
}

func (l *Limiter) spendFile() string {
	return filepath.Join(l.dataDir, "daily_spend.json")
}

func (l *Limiter) loadSpend() {
	data, err := os.ReadFile(l.spendFile())
	if err != nil {
		return
	}
	var s dailySpend
	if err := json.Unmarshal(data, &s); err != nil {
		return
	}
	today := time.Now().Format("2006-01-02")
	if s.Date == today {
		l.spend = &s
	}
}

func (l *Limiter) saveSpend() {
	data, err := json.Marshal(l.spend)
	if err != nil {
		log.Printf("failed to marshal spend: %v", err)
		return
	}
	if err := os.WriteFile(l.spendFile(), data, 0644); err != nil {
		log.Printf("failed to save spend: %v", err)
	}
}
