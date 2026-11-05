package visualizer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"meters-reader/internal/analyzer"
)

// GenerateHTML creates an interactive Chart.js HTML report for in-depth visual analysis.
func GenerateHTML(rep *analyzer.ConsumptionReport, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	labels := make([]string, len(rep.Readings))
	values := make([]float64, len(rep.Readings))
	for i, r := range rep.Readings {
		labels[i] = r.StartTime.Format("01-02 15:04")
		values[i] = r.Consumption
	}

	labelsJSON, _ := json.Marshal(labels)
	valuesJSON, _ := json.Marshal(values)

	hourlyLabels := []string{
		"00:00", "01:00", "02:00", "03:00", "04:00", "05:00",
		"06:00", "07:00", "08:00", "09:00", "10:00", "11:00",
		"12:00", "13:00", "14:00", "15:00", "16:00", "17:00",
		"18:00", "19:00", "20:00", "21:00", "22:00", "23:00",
	}
	hourlyJSON, _ := json.Marshal(rep.HourlyAverages[:])

	days := make([]string, 0, len(rep.DailyTotals))
	dayVals := make([]float64, 0, len(rep.DailyTotals))
	for d := range rep.DailyTotals {
		days = append(days, d)
	}
	sort.Strings(days)
	for _, d := range days {
		dayVals = append(dayVals, rep.DailyTotals[d])
	}
	dayLabelsJSON, _ := json.Marshal(days)
	dayValsJSON, _ := json.Marshal(dayVals)

	topRowsHTML := ""
	for i, r := range rep.TopIntervals {
		topRowsHTML += fmt.Sprintf(`
			<tr>
				<td>#%d</td>
				<td>%s</td>
				<td><b>%.3f</b></td>
				<td>~%.2f</td>
			</tr>`,
			i+1,
			r.StartTime.Format("2006-01-02 15:04"),
			r.Consumption,
			r.PowerKW,
		)
	}

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>EAC Meter Consumption Report</title>
    <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
    <style>
        :root {
            --bg: #0f172a;
            --card-bg: #1e293b;
            --text: #f8fafc;
            --text-muted: #94a3b8;
            --accent: #38bdf8;
            --accent-warm: #f59e0b;
            --border: #334155;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: var(--bg); color: var(--text); padding: 24px; }
        .container { max-width: 1200px; margin: 0 auto; }
        header { margin-bottom: 24px; }
        h1 { font-size: 1.8rem; margin-bottom: 8px; color: var(--accent); }
        .period { color: var(--text-muted); font-size: 0.95rem; }
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 16px; margin-bottom: 24px; }
        .card { background: var(--card-bg); border: 1px solid var(--border); border-radius: 12px; padding: 20px; }
        .card-label { font-size: 0.85rem; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 6px; }
        .card-value { font-size: 1.6rem; font-weight: bold; color: var(--text); }
        .card-sub { font-size: 0.8rem; color: var(--text-muted); margin-top: 4px; }
        .chart-box { background: var(--card-bg); border: 1px solid var(--border); border-radius: 12px; padding: 20px; margin-bottom: 24px; }
        .chart-title { font-size: 1.1rem; margin-bottom: 16px; color: var(--text); }
        table { width: 100%%; border-collapse: collapse; margin-top: 12px; }
        th, td { padding: 10px 14px; text-align: left; border-bottom: 1px solid var(--border); font-size: 0.9rem; }
        th { color: var(--text-muted); font-weight: 600; }
        tr:hover { background: rgba(255, 255, 255, 0.03); }
        .two-cols { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; }
        @media(max-width: 800px) { .two-cols { grid-template-columns: 1fr; } }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>⚡ EAC Smart Meter Consumption Report</h1>
            <p class="period">Period: %s — %s &bull; %d records</p>
        </header>

        <div class="grid">
            <div class="card">
                <div class="card-label">Total Energy</div>
                <div class="card-value">%.2f <span style="font-size: 1rem; color: var(--text-muted);">kWh</span></div>
                <div class="card-sub">Avg %.2f kWh / day</div>
            </div>
            <div class="card">
                <div class="card-label">Max Peak Interval</div>
                <div class="card-value" style="color: var(--accent-warm);">%.3f <span style="font-size: 1rem; color: var(--text-muted);">kWh</span></div>
                <div class="card-sub">~%.2f kW at %s</div>
            </div>
            <div class="card">
                <div class="card-label">Interval Average</div>
                <div class="card-value">%.3f <span style="font-size: 1rem; color: var(--text-muted);">kWh</span></div>
                <div class="card-sub">~%.2f kW avg load</div>
            </div>
            <div class="card">
                <div class="card-label">Base Load (Min)</div>
                <div class="card-value">%.3f <span style="font-size: 1rem; color: var(--text-muted);">kWh</span></div>
                <div class="card-sub">~%.2f kW baseline</div>
            </div>
        </div>

        <div class="chart-box">
            <h2 class="chart-title">📈 Consumption Timeline (15-min Intervals)</h2>
            <canvas id="timelineChart" style="max-height: 380px;"></canvas>
        </div>

        <div class="two-cols">
            <div class="chart-box">
                <h2 class="chart-title">🕒 Average Hourly Profile (00:00 - 23:00)</h2>
                <canvas id="hourlyChart" style="max-height: 280px;"></canvas>
            </div>
            <div class="chart-box">
                <h2 class="chart-title">📅 Daily Consumption (kWh)</h2>
                <canvas id="dailyChart" style="max-height: 280px;"></canvas>
            </div>
        </div>

        <div class="chart-box">
            <h2 class="chart-title">🔥 Top Peak Intervals</h2>
            <table>
                <thead>
                    <tr>
                        <th>Rank</th>
                        <th>Timestamp</th>
                        <th>Energy (kWh)</th>
                        <th>Approx Power (kW)</th>
                    </tr>
                </thead>
                <tbody>
                    %s
                </tbody>
            </table>
        </div>
    </div>

    <script>
        const timelineData = {
            labels: %s,
            datasets: [{
                label: 'Consumption (kWh)',
                data: %s,
                borderColor: '#38bdf8',
                backgroundColor: 'rgba(56, 189, 248, 0.1)',
                borderWidth: 1.5,
                fill: true,
                tension: 0.1,
                pointRadius: 1
            }]
        };

        new Chart(document.getElementById('timelineChart'), {
            type: 'line',
            data: timelineData,
            options: {
                responsive: true,
                maintainAspectRatio: false,
                scales: {
                    x: { ticks: { color: '#94a3b8', maxTicksLimit: 12 }, grid: { color: '#334155' } },
                    y: { ticks: { color: '#94a3b8' }, grid: { color: '#334155' }, title: { display: true, text: 'kWh', color: '#94a3b8' } }
                },
                plugins: { legend: { labels: { color: '#f8fafc' } } }
            }
        });

        new Chart(document.getElementById('hourlyChart'), {
            type: 'bar',
            data: {
                labels: %s,
                datasets: [{
                    label: 'Avg kWh',
                    data: %s,
                    backgroundColor: '#f59e0b',
                    borderRadius: 4
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                scales: {
                    x: { ticks: { color: '#94a3b8' }, grid: { display: false } },
                    y: { ticks: { color: '#94a3b8' }, grid: { color: '#334155' } }
                },
                plugins: { legend: { display: false } }
            }
        });

        new Chart(document.getElementById('dailyChart'), {
            type: 'bar',
            data: {
                labels: %s,
                datasets: [{
                    label: 'Daily kWh',
                    data: %s,
                    backgroundColor: '#10b981',
                    borderRadius: 4
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                scales: {
                    x: { ticks: { color: '#94a3b8' }, grid: { display: false } },
                    y: { ticks: { color: '#94a3b8' }, grid: { color: '#334155' } }
                },
                plugins: { legend: { display: false } }
            }
        });
    </script>
</body>
</html>`,
		rep.StartTime.Format("2006-01-02"),
		rep.EndTime.Format("2006-01-02"),
		rep.IntervalCount,
		rep.TotalConsumption,
		rep.DailyAverage,
		rep.MaxReading.Consumption,
		rep.MaxReading.PowerKW,
		rep.MaxReading.StartTime.Format("2006-01-02 15:04"),
		rep.AverageInterval,
		rep.AverageInterval * (time.Hour.Hours() / rep.MaxReading.Duration.Hours()),
		rep.MinReading.Consumption,
		rep.MinReading.PowerKW,
		topRowsHTML,
		string(labelsJSON),
		string(valuesJSON),
		fmt.Sprintf("%v", hourlyLabels), // will replace below with json
		string(hourlyJSON),
		string(dayLabelsJSON),
		string(dayValsJSON),
	)

	// Fix hourly labels JSON
	hourlyLabelsJSON, _ := json.Marshal(hourlyLabels)
	htmlContent = stringsReplaceFirst(htmlContent, fmt.Sprintf("%v", hourlyLabels), string(hourlyLabelsJSON))

	return os.WriteFile(outputPath, []byte(htmlContent), 0644)
}

func stringsReplaceFirst(s, old, new string) string {
	idx := -1
	for i := 0; i+len(old) <= len(s); i++ {
		if s[i:i+len(old)] == old {
			idx = i
			break
		}
	}
	if idx == -1 {
		return s
	}
	return s[:idx] + new + s[idx+len(old):]
}
