package visualizer

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/guptarohit/asciigraph"
	"meters-reader/internal/analyzer"
	"meters-reader/internal/parser"
)

// PrintReport renders a formatted summary, peak intervals table, and graphs to the console.
func PrintReport(rep *analyzer.ConsumptionReport) {
	printHeader(rep)
	printTopIntervals(rep)
	printHourlyProfile(rep)
	printDailySummary(rep)
	printTimelineGraph(rep)
}

func printHeader(rep *analyzer.ConsumptionReport) {
	fmt.Println()
	fmt.Println(strings.Repeat("═", 78))
	fmt.Println("                   EAC METER READING CONSUMPTION REPORT                    ")
	fmt.Println(strings.Repeat("═", 78))
	fmt.Printf(" Period:               %s  -->  %s\n",
		rep.StartTime.Format("2006-01-02 15:04"),
		rep.EndTime.Format("2006-01-02 15:04"),
	)
	fmt.Printf(" Total Consumption:    %.3f kWh  (%d interval records)\n", rep.TotalConsumption, rep.IntervalCount)
	fmt.Printf(" Daily Average:        %.3f kWh/day\n", rep.DailyAverage)
	fmt.Printf(" Interval Average:     %.3f kWh/interval\n", rep.AverageInterval)
	fmt.Printf(" Absolute Peak:        %.3f kWh at %s (~%.2f kW power)\n",
		rep.MaxReading.Consumption,
		rep.MaxReading.StartTime.Format("2006-01-02 15:04"),
		rep.MaxReading.PowerKW,
	)
	fmt.Printf(" Base Load (Min):      %.3f kWh at %s (~%.2f kW power)\n",
		rep.MinReading.Consumption,
		rep.MinReading.StartTime.Format("2006-01-02 15:04"),
		rep.MinReading.PowerKW,
	)
	fmt.Println(strings.Repeat("─", 78))
}

func printTopIntervals(rep *analyzer.ConsumptionReport) {
	fmt.Println("\n 🔥 TOP MAXIMUM CONSUMPTION INTERVALS:")
	fmt.Printf(" %-4s | %-16s | %-13s | %-10s | %s\n", "Rank", "Timestamp", "Energy (kWh)", "Power (kW)", "Intensity")
	fmt.Println(strings.Repeat("─", 78))

	maxVal := 0.001
	if len(rep.TopIntervals) > 0 {
		maxVal = rep.TopIntervals[0].Consumption
	}

	for i, r := range rep.TopIntervals {
		kW := r.PowerKW
		barLen := int((r.Consumption / maxVal) * 22)
		if barLen < 1 && r.Consumption > 0 {
			barLen = 1
		}
		bar := strings.Repeat("█", barLen)

		fmt.Printf(" #%-3d | %s | %10.3f   | ~%7.2f   | %s\n",
			i+1,
			r.StartTime.Format("2006-01-02 15:04"),
			r.Consumption,
			kW,
			bar,
		)
	}
	fmt.Println(strings.Repeat("─", 78))
}

func printHourlyProfile(rep *analyzer.ConsumptionReport) {
	fmt.Println("\n 🕒 HOURLY CONSUMPTION PROFILE (Average kWh by hour of day):")
	fmt.Printf(" %-5s | %-10s | %s\n", "Hour", "Avg (kWh)", "Distribution")
	fmt.Println(strings.Repeat("─", 50))

	maxAvg := 0.0001
	for _, avg := range rep.HourlyAverages {
		if avg > maxAvg {
			maxAvg = avg
		}
	}

	for h := 0; h < 24; h++ {
		avg := rep.HourlyAverages[h]
		barLen := int((avg / maxAvg) * 25)
		bar := strings.Repeat("■", barLen)

		// Color hint or icon for peak hours
		icon := " "
		if avg >= maxAvg*0.8 && avg > 0.1 {
			icon = "⚡"
		}

		fmt.Printf(" %02d:00 | %8.3f   | %s %s\n", h, avg, bar, icon)
	}
	fmt.Println(strings.Repeat("─", 50))
}

func printDailySummary(rep *analyzer.ConsumptionReport) {
	if len(rep.DailyTotals) <= 1 {
		return
	}

	fmt.Println("\n 📅 DAILY CONSUMPTION BREAKDOWN:")
	days := make([]string, 0, len(rep.DailyTotals))
	for d := range rep.DailyTotals {
		days = append(days, d)
	}
	sort.Strings(days)

	maxDay := 0.001
	for _, d := range days {
		if rep.DailyTotals[d] > maxDay {
			maxDay = rep.DailyTotals[d]
		}
	}

	for _, d := range days {
		val := rep.DailyTotals[d]
		barLen := int((val / maxDay) * 25)
		bar := strings.Repeat("█", barLen)
		fmt.Printf(" %s | %8.3f kWh | %s\n", d, val, bar)
	}
	fmt.Println(strings.Repeat("─", 50))
}

func printTimelineGraph(rep *analyzer.ConsumptionReport) {
	if len(rep.Readings) == 0 {
		return
	}

	fmt.Println("\n 📈 CONSUMPTION TIMELINE GRAPH (Terminal ASCII):")

	// Sample readings to ~70 points so graph fits cleanly on screen
	points := downsample(rep.Readings, 70)

	graph := asciigraph.Plot(points,
		asciigraph.Height(10),
		asciigraph.Width(70),
		asciigraph.Caption(fmt.Sprintf("Interval Consumption (kWh) from %s to %s",
			rep.StartTime.Format("01-02"),
			rep.EndTime.Format("01-02"),
		)),
	)

	fmt.Println(graph)
	fmt.Println()
}

func downsample(readings []parser.IntervalReading, targetCount int) []float64 {
	if len(readings) <= targetCount {
		res := make([]float64, len(readings))
		for i, r := range readings {
			res[i] = r.Consumption
		}
		return res
	}

	res := make([]float64, targetCount)
	bucketSize := float64(len(readings)) / float64(targetCount)

	for i := 0; i < targetCount; i++ {
		start := int(math.Floor(float64(i) * bucketSize))
		end := int(math.Floor(float64(i+1) * bucketSize))
		if end > len(readings) {
			end = len(readings)
		}

		maxVal := 0.0
		for j := start; j < end; j++ {
			if readings[j].Consumption > maxVal {
				maxVal = readings[j].Consumption
			}
		}
		res[i] = maxVal
	}

	return res
}
