package parser

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

var cyprusLocation = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Nicosia")
	if err != nil {
		return time.UTC
	}
	return loc
}()

type IntervalReading struct {
	StartTime   time.Time
	EndTime     time.Time
	Duration    time.Duration
	Consumption float64 // in kWh
	PowerKW     float64 // in kW = Consumption / (Duration in hours)
	RawValue    string
}

// ParseXLSX reads an EAC meter reading Excel file and extracts interval readings.
func ParseXLSX(filePath string) ([]IntervalReading, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("open excel file %s: %w", filePath, err)
	}
	defer f.Close()

	sheetList := f.GetSheetList()
	if len(sheetList) == 0 {
		return nil, fmt.Errorf("excel file has no sheets")
	}

	targetSheet := ""
	for _, sheet := range sheetList {
		lower := strings.ToLower(sheet)
		if strings.Contains(lower, "interval") || strings.Contains(lower, "load") {
			targetSheet = sheet
			break
		}
	}
	if targetSheet == "" {
		bestRows := 0
		for _, sheet := range sheetList {
			rows, err := f.GetRows(sheet)
			if err == nil && len(rows) > bestRows {
				bestRows = len(rows)
				targetSheet = sheet
			}
		}
	}
	if targetSheet == "" {
		targetSheet = sheetList[0]
	}

	rows, err := f.GetRows(targetSheet)
	if err != nil {
		return nil, fmt.Errorf("read rows from sheet %s: %w", targetSheet, err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("sheet %s has insufficient rows (found %d)", targetSheet, len(rows))
	}

	maxCols := 0
	for _, row := range rows {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}
	dateScores := make([]int, maxCols)
	for _, row := range rows {
		for cIdx, cell := range row {
			if t, err := parseDateTime(cell); err == nil && !t.IsZero() {
				dateScores[cIdx]++
			}
		}
	}
	dateCol, maxDateScore := -1, 0
	for cIdx, score := range dateScores {
		if score > maxDateScore {
			maxDateScore, dateCol = score, cIdx
		}
	}
	if dateCol == -1 || maxDateScore < 2 {
		return nil, fmt.Errorf("could not find timestamp column in sheet %s", targetSheet)
	}

	valCol := -1
	for rIdx := 0; rIdx < min(len(rows), 25); rIdx++ {
		for cIdx, cell := range rows[rIdx] {
			if cIdx == dateCol {
				continue
			}
			lower := strings.ToLower(strings.TrimSpace(cell))
			if strings.Contains(lower, "consumed") || strings.Contains(lower, "active energy consumed") {
				valCol = cIdx
				break
			}
		}
		if valCol != -1 {
			break
		}
	}
	if valCol == -1 {
		numScores := make([]int, maxCols)
		for _, row := range rows {
			if len(row) <= dateCol {
				continue
			}
			if t, err := parseDateTime(row[dateCol]); err != nil || t.IsZero() {
				continue
			}
			for cIdx, cell := range row {
				if cIdx != dateCol {
					if _, err := parseNumber(cell); err == nil {
						numScores[cIdx]++
					}
				}
			}
		}
		maxNumScore := 0
		for cIdx, score := range numScores {
			if score > maxNumScore {
				maxNumScore, valCol = score, cIdx
			}
		}
	}
	if valCol == -1 {
		return nil, fmt.Errorf("could not find consumption value column in sheet %s", targetSheet)
	}

	var rawReadings []IntervalReading
	for _, row := range rows {
		if len(row) <= dateCol || len(row) <= valCol {
			continue
		}
		timestamp, errDate := parseDateTime(row[dateCol])
		if errDate != nil || timestamp.IsZero() {
			continue
		}
		consumption, errVal := parseNumber(row[valCol])
		if errVal != nil {
			continue
		}
		rawReadings = append(rawReadings, IntervalReading{
			StartTime: timestamp, Consumption: math.Round(consumption*1000) / 1000, RawValue: row[valCol],
		})
	}
	if len(rawReadings) == 0 {
		return nil, fmt.Errorf("no valid interval reading rows found in sheet %s of %s", targetSheet, filePath)
	}
	sort.Slice(rawReadings, func(i, j int) bool { return rawReadings[i].StartTime.Before(rawReadings[j].StartTime) })

	duration := 30 * time.Minute
	if len(rawReadings) > 1 {
		diff := rawReadings[1].StartTime.Sub(rawReadings[0].StartTime)
		if diff > 0 && diff <= 2*time.Hour {
			duration = diff
		}
	}
	durHours := duration.Hours()
	if durHours <= 0 {
		durHours = 0.5
	}
	for i := range rawReadings {
		rawReadings[i].Duration = duration
		rawReadings[i].EndTime = rawReadings[i].StartTime.Add(duration)
		rawReadings[i].PowerKW = math.Round((rawReadings[i].Consumption/durHours)*1000) / 1000
	}
	return rawReadings, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func parseNumber(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}
	if strings.Count(s, ",") == 1 && !strings.Contains(s, ".") {
		s = strings.ReplaceAll(s, ",", ".")
	} else if strings.Count(s, ",") > 1 && strings.Contains(s, ".") {
		s = strings.ReplaceAll(s, ",", "")
	}
	return strconv.ParseFloat(s, 64)
}

func parseDateTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date string")
	}

	// Keep timestamps that explicitly contain a UTC offset/zone as absolute instants.
	zonedLayouts := []string{
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05.999999999Z07:00",
	}
	for _, layout := range zonedLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}

	// EAC's XLSX/text timestamps without an offset are Cyprus wall-clock times,
	// not UTC. ParseInLocation attaches the Cyprus zone without changing the clock.
	localLayouts := []string{
		"02.01.2006 15:04:05", "02.01.2006 15:04",
		"02/01/2006 15:04:05", "02/01/2006 15:04",
		"2006-01-02 15:04:05", "2006-01-02 15:04",
		"2006-01-02T15:04:05", "2006-01-02T15:04",
		"02-01-2006 15:04:05", "02-01-2006 15:04",
		"02.01.2006", "02/01/2006", "2006-01-02",
	}
	for _, layout := range localLayouts {
		if t, err := time.ParseInLocation(layout, s, cyprusLocation); err == nil {
			return t, nil
		}
	}

	if f, err := strconv.ParseFloat(s, 64); err == nil && f > 30000 && f < 70000 {
		return excelTimeToTime(f), nil
	}
	return time.Time{}, fmt.Errorf("unrecognized date/time format: %s", s)
}

func excelTimeToTime(excelDate float64) time.Time {
	// Excel serial dates are wall-clock calendar values, not UTC instants.
	// Build the calendar date directly in the EAC timezone instead of converting
	// a UTC epoch into Cyprus time (which shifts the displayed clock by +2/+3).
	dayCount := int64(excelDate)
	fraction := excelDate - float64(dayCount)
	nanos := int64(math.Round(fraction * float64(24*time.Hour)))
	return time.Date(1899, 12, 30, 0, 0, 0, 0, cyprusLocation).
		AddDate(0, 0, int(dayCount)).Add(time.Duration(nanos))
}
