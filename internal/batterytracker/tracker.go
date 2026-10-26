package batterytracker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/MateEke/picture-frame/internal/hostmetrics"
)

const (
	defaultSampleInterval = 30 * time.Second
	maxHistoryDuration    = 7 * 24 * time.Hour
	maxGapDuration        = 3 * time.Minute  // Gaps longer than this (e.g. system sleep/suspension) are attributed to deep sleep or ignored if too long
	maxSuspendGapDuration = 48 * time.Hour   // Suspensions longer than 48 hours are ignored
	minObservedDuration   = 10 * time.Minute // Minimum discharging observation needed for reliable estimation
)

type Sample struct {
	Timestamp time.Time `json:"timestamp"`
	Pct       int       `json:"pct"`
	ScreenOn  bool      `json:"screen_on"`
	Charging  bool      `json:"charging"`
}

type ConditionStats struct {
	ObservedDurationSec int64   `json:"observed_duration_sec"`
	DischargeRatePctHr  float64 `json:"discharge_rate_pct_hr"`
	EstimatedFullHours  float64 `json:"estimated_full_hours"`
	EstimatedRemHours   float64 `json:"estimated_rem_hours"`
	Confidence          string  `json:"confidence"` // "low", "medium", "high", "insufficient"
	SufficientData      bool    `json:"sufficient_data"`
}

type DaySummary struct {
	Date                  string  `json:"date"` // YYYY-MM-DD
	ScreenOnDischargeHr   float64 `json:"screen_on_discharge_hr"`
	ScreenOffDischargeHr  float64 `json:"screen_off_discharge_hr"`
	DeepSleepDischargeHr  float64 `json:"deep_sleep_discharge_hr"`
	AvgRateOnPctHr        float64 `json:"avg_rate_on_pct_hr"`
	AvgRateOffPctHr       float64 `json:"avg_rate_off_pct_hr"`
	AvgRateDeepSleepPctHr float64 `json:"avg_rate_deep_sleep_pct_hr"`
}

type StatsSummary struct {
	CurrentPct int            `json:"current_pct"`
	Charging   bool           `json:"charging"`
	ScreenOn   ConditionStats `json:"screen_on"`
	ScreenOff  ConditionStats `json:"screen_off"`
	DeepSleep  ConditionStats `json:"deep_sleep"`
	Comparison string         `json:"comparison"` // Summary text e.g. "Screen on consumes 1.8x more battery"
	Daily      []DaySummary   `json:"daily"`
	History    []Sample       `json:"history"`
}

type MetricsProvider interface {
	Read(ctx context.Context) hostmetrics.Metrics
}

type DisplayStateProvider interface {
	State() bool
}

type Tracker struct {
	log       *slog.Logger
	metrics   MetricsProvider
	display   DisplayStateProvider
	storePath string

	mu      sync.RWMutex
	samples []Sample
}

func New(log *slog.Logger, metrics MetricsProvider, display DisplayStateProvider, storePath string) *Tracker {
	t := &Tracker{
		log:       log,
		metrics:   metrics,
		display:   display,
		storePath: storePath,
	}
	t.load()
	return t
}

func (t *Tracker) load() {
	if t.storePath == "" {
		return
	}
	data, err := os.ReadFile(t.storePath)
	if err != nil {
		if !os.IsNotExist(err) {
			t.log.Warn("batterytracker: failed to read store file", "err", err)
		}
		return
	}
	var loaded []Sample
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.log.Warn("batterytracker: failed to unmarshal store file", "err", err)
		return
	}
	cutoff := time.Now().Add(-maxHistoryDuration)
	filtered := make([]Sample, 0, len(loaded))
	for _, s := range loaded {
		if s.Timestamp.After(cutoff) {
			filtered = append(filtered, s)
		}
	}
	t.samples = filtered
}

func (t *Tracker) save() {
	if t.storePath == "" {
		return
	}
	t.mu.RLock()
	samplesCopy := make([]Sample, len(t.samples))
	copy(samplesCopy, t.samples)
	t.mu.RUnlock()

	data, err := json.Marshal(samplesCopy)
	if err != nil {
		t.log.Warn("batterytracker: failed to marshal samples", "err", err)
		return
	}

	dir := filepath.Dir(t.storePath)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	tmp := t.storePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		t.log.Warn("batterytracker: failed to write tmp store file", "err", err)
		return
	}
	_ = os.Rename(tmp, t.storePath)
}

func (t *Tracker) Sample(ctx context.Context) {
	if t.metrics == nil {
		return
	}
	m := t.metrics.Read(ctx)
	if !m.HasBattery {
		return
	}
	screenOn := false
	if t.display != nil {
		screenOn = t.display.State()
	}

	sample := Sample{
		Timestamp: time.Now(),
		Pct:       m.BatteryPct,
		ScreenOn:  screenOn,
		Charging:  m.BatteryCharging,
	}

	t.mu.Lock()
	cutoff := time.Now().Add(-maxHistoryDuration)
	n := 0
	for _, s := range t.samples {
		if s.Timestamp.After(cutoff) {
			t.samples[n] = s
			n++
		}
	}
	t.samples = t.samples[:n]
	t.samples = append(t.samples, sample)
	t.mu.Unlock()

	t.save()
}

func (t *Tracker) Run(ctx context.Context) {
	ticker := time.NewTicker(defaultSampleInterval)
	defer ticker.Stop()

	t.Sample(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.Sample(ctx)
		}
	}
}

func (t *Tracker) GetStats() StatsSummary {
	t.mu.RLock()
	samples := make([]Sample, len(t.samples))
	copy(samples, t.samples)
	t.mu.RUnlock()

	currentPct := 0
	charging := false
	if len(samples) > 0 {
		last := samples[len(samples)-1]
		currentPct = last.Pct
		charging = last.Charging
	}

	screenOnStats := computeConditionStats(samples, true, currentPct)
	screenOffStats := computeConditionStats(samples, false, currentPct)
	deepSleepStats := computeDeepSleepStats(samples, currentPct)

	comparison := "Dati insufficienti per confrontare il consumo tra schermo acceso e spento."
	if screenOnStats.SufficientData && screenOffStats.SufficientData {
		if screenOffStats.DischargeRatePctHr > 0 {
			ratio := screenOnStats.DischargeRatePctHr / screenOffStats.DischargeRatePctHr
			comparison = fmt.Sprintf("Lo schermo acceso consuma circa %.1fx rispetto allo schermo spento.", ratio)
		} else if screenOnStats.DischargeRatePctHr > 0 {
			comparison = "Lo schermo acceso causa scarica, mentre lo schermo spento ha consumo trascurabile."
		} else {
			comparison = "I consumi nelle due condizioni sono equivalenti o trascurabili."
		}
	}
	if deepSleepStats.SufficientData && deepSleepStats.DischargeRatePctHr > 0 {
		comparison += fmt.Sprintf(" In deep sleep il consumo scende a %.1f%%/ora.", deepSleepStats.DischargeRatePctHr)
	}

	daily := computeDailySummaries(samples)

	return StatsSummary{
		CurrentPct: currentPct,
		Charging:   charging,
		ScreenOn:   screenOnStats,
		ScreenOff:  screenOffStats,
		DeepSleep:  deepSleepStats,
		Comparison: comparison,
		Daily:      daily,
		History:    samples,
	}
}

func buildConditionStats(totalObservedSec int64, totalDischargePct float64, currentPct int) ConditionStats {
	if totalObservedSec < int64(minObservedDuration.Seconds()) {
		return ConditionStats{
			ObservedDurationSec: totalObservedSec,
			Confidence:          "insufficient",
			SufficientData:      false,
		}
	}

	ratePctHr := 0.0
	obsHours := float64(totalObservedSec) / 3600.0
	if obsHours > 0 {
		ratePctHr = totalDischargePct / obsHours
	}

	if ratePctHr <= 0.05 {
		return ConditionStats{
			ObservedDurationSec: totalObservedSec,
			DischargeRatePctHr:  0,
			EstimatedFullHours:  999,
			EstimatedRemHours:   999,
			Confidence:          "low",
			SufficientData:      true,
		}
	}

	estFullHr := 100.0 / ratePctHr
	estRemHr := float64(currentPct) / ratePctHr

	confidence := "low"
	if obsHours >= 6.0 {
		confidence = "high"
	} else if obsHours >= 1.0 {
		confidence = "medium"
	}

	return ConditionStats{
		ObservedDurationSec: totalObservedSec,
		DischargeRatePctHr:  ratePctHr,
		EstimatedFullHours:  estFullHr,
		EstimatedRemHours:   estRemHr,
		Confidence:          confidence,
		SufficientData:      true,
	}
}

func computeConditionStats(samples []Sample, targetScreenOn bool, currentPct int) ConditionStats {
	var totalObservedSec int64
	var totalDischargePct float64

	for i := 1; i < len(samples); i++ {
		prev := samples[i-1]
		curr := samples[i]

		if prev.Charging || curr.Charging {
			continue
		}
		if prev.ScreenOn != targetScreenOn || curr.ScreenOn != targetScreenOn {
			continue
		}
		dt := curr.Timestamp.Sub(prev.Timestamp)
		if dt <= 0 || dt > maxGapDuration {
			continue
		}

		dPct := float64(prev.Pct - curr.Pct)
		if dPct < -1 {
			continue
		}
		if dPct < 0 {
			dPct = 0
		}

		totalObservedSec += int64(dt.Seconds())
		totalDischargePct += dPct
	}

	return buildConditionStats(totalObservedSec, totalDischargePct, currentPct)
}

func computeDeepSleepStats(samples []Sample, currentPct int) ConditionStats {
	var totalObservedSec int64
	var totalDischargePct float64

	for i := 1; i < len(samples); i++ {
		prev := samples[i-1]
		curr := samples[i]

		if prev.Charging || curr.Charging {
			continue
		}
		dt := curr.Timestamp.Sub(prev.Timestamp)
		if dt < maxGapDuration || dt > maxSuspendGapDuration {
			continue
		}

		dPct := float64(prev.Pct - curr.Pct)
		if dPct < -1 {
			continue
		}
		if dPct < 0 {
			dPct = 0
		}

		totalObservedSec += int64(dt.Seconds())
		totalDischargePct += dPct
	}

	return buildConditionStats(totalObservedSec, totalDischargePct, currentPct)
}

type dayAccumulator struct {
	onSec         int64
	onDischarge   float64
	offSec        int64
	offDischarge  float64
	deepSec       int64
	deepDischarge float64
}

func computeDailySummaries(samples []Sample) []DaySummary {
	accumByDate := make(map[string]*dayAccumulator)

	getAccum := func(dateStr string) *dayAccumulator {
		acc, ok := accumByDate[dateStr]
		if !ok {
			acc = &dayAccumulator{}
			accumByDate[dateStr] = acc
		}
		return acc
	}

	for i := 1; i < len(samples); i++ {
		prev := samples[i-1]
		curr := samples[i]

		if prev.Charging || curr.Charging {
			continue
		}

		dt := curr.Timestamp.Sub(prev.Timestamp)
		if dt <= 0 || dt > maxSuspendGapDuration {
			continue
		}

		dPct := float64(prev.Pct - curr.Pct)
		if dPct < -1 {
			continue
		}
		if dPct < 0 {
			dPct = 0
		}

		dateStr := curr.Timestamp.Format("2006-01-02")
		acc := getAccum(dateStr)

		if dt > maxGapDuration {
			acc.deepSec += int64(dt.Seconds())
			acc.deepDischarge += dPct
		} else if prev.ScreenOn == curr.ScreenOn {
			if prev.ScreenOn {
				acc.onSec += int64(dt.Seconds())
				acc.onDischarge += dPct
			} else {
				acc.offSec += int64(dt.Seconds())
				acc.offDischarge += dPct
			}
		}
	}

	out := make([]DaySummary, 0, 7)
	now := time.Now()
	for i := 6; i >= 0; i-- {
		dateStr := now.AddDate(0, 0, -i).Format("2006-01-02")
		acc := getAccum(dateStr)

		onStats := buildConditionStats(acc.onSec, acc.onDischarge, 0)
		offStats := buildConditionStats(acc.offSec, acc.offDischarge, 0)
		deepStats := buildConditionStats(acc.deepSec, acc.deepDischarge, 0)

		summary := DaySummary{
			Date: dateStr,
		}
		if onStats.SufficientData {
			summary.ScreenOnDischargeHr = float64(onStats.ObservedDurationSec) / 3600.0
			summary.AvgRateOnPctHr = onStats.DischargeRatePctHr
		}
		if offStats.SufficientData {
			summary.ScreenOffDischargeHr = float64(offStats.ObservedDurationSec) / 3600.0
			summary.AvgRateOffPctHr = offStats.DischargeRatePctHr
		}
		if deepStats.SufficientData {
			summary.DeepSleepDischargeHr = float64(deepStats.ObservedDurationSec) / 3600.0
			summary.AvgRateDeepSleepPctHr = deepStats.DischargeRatePctHr
		}
		out = append(out, summary)
	}
	return out
}
