package analyzer

import (
	"fmt"
	"math"
	"sort"
	"time"

	"meters-reader/internal/parser"
)

type ConsumptionReport struct {
	TotalConsumption float64
	IntervalCount    int
	StartTime        time.Time
	EndTime          time.Time
	AverageInterval  float64
	DailyAverage     float64
	MaxReading       parser.IntervalReading
	MinReading       parser.IntervalReading
	TopIntervals     []parser.IntervalReading
	HourlyTotals     [24]float64
	HourlyCounts     [24]int
	HourlyAverages   [24]float64
	DailyTotals      map[string]float64
	Readings         []parser.IntervalReading
}

// Analyze calculates consumption statistics, identifies peak intervals, and builds time profiles.
func Analyze(readings []parser.IntervalReading, topN int) (*ConsumptionReport, error) {
	if len(readings) == 0 {
		return nil, fmt.Errorf("no readings to analyze")
	}

	report := &ConsumptionReport{
		DailyTotals: make(map[string]float64),
		Readings:    readings,
	}

	total := 0.0
	minVal := math.MaxFloat64
	maxVal := -1.0
	var minReading, maxReading parser.IntervalReading

	firstTime := readings[0].StartTime
	lastTime := readings[0].StartTime

	for _, r := range readings {
		total += r.Consumption

		if r.StartTime.Before(firstTime) {
			firstTime = r.StartTime
		}
		if r.StartTime.After(lastTime) {
			lastTime = r.StartTime
		}

		if r.Consumption > maxVal {
			maxVal = r.Consumption
			maxReading = r
		}
		if r.Consumption < minVal {
			minVal = r.Consumption
			minReading = r
		}

		// Hourly profile
		hour := r.StartTime.Hour()
		report.HourlyTotals[hour] += r.Consumption
		report.HourlyCounts[hour]++

		// Daily totals
		dayKey := r.StartTime.Format("2006-01-02")
		report.DailyTotals[dayKey] += r.Consumption
	}

	report.TotalConsumption = round(total)
	report.IntervalCount = len(readings)
	report.StartTime = firstTime
	report.EndTime = lastTime
	report.MaxReading = maxReading
	report.MinReading = minReading

	if len(readings) > 0 {
		report.AverageInterval = round(total / float64(len(readings)))
	}

	daySpan := lastTime.Sub(firstTime).Hours() / 24.0
	if daySpan < 1.0 {
		daySpan = 1.0
	}
	report.DailyAverage = round(total / daySpan)

	// Calculate hourly averages
	for h := 0; h < 24; h++ {
		if report.HourlyCounts[h] > 0 {
			report.HourlyAverages[h] = round(report.HourlyTotals[h] / float64(report.HourlyCounts[h]))
		}
	}

	// Sort for top N intervals
	sorted := make([]parser.IntervalReading, len(readings))
	copy(sorted, readings)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Consumption > sorted[j].Consumption
	})

	if topN > len(sorted) {
		topN = len(sorted)
	}
	report.TopIntervals = sorted[:topN]

	return report, nil
}

func round(val float64) float64 {
	return math.Round(val*1000) / 1000
}
