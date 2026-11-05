package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/xuri/excelize/v2"

	"meters-reader/internal/analyzer"
	"meters-reader/internal/client"
	"meters-reader/internal/config"
	"meters-reader/internal/parser"
	"meters-reader/internal/server"
	"meters-reader/internal/visualizer"
)

func main() {
	filePathFlag := flag.String("file", "", "Path to local .xlsx file to analyze (offline mode)")
	daysFlag := flag.Int("days", 0, "Number of past days to fetch (defaults to .env DAYS_BACK or 7)")
	topFlag := flag.Int("top", 0, "Number of peak intervals to display (defaults to 10)")
	spFlag := flag.String("sp", "", "Service Point ID override")
	loopFlag := flag.Bool("loop", false, "Run periodically in daemon loop based on FETCH_INTERVAL")
	htmlFlag := flag.Bool("html", true, "Generate interactive HTML report in addition to console output")
	mockFlag := flag.Bool("mock", false, "Generate realistic sample dataset for demonstration and testing")
	serverFlag := flag.Bool("server", false, "Run web server mode with UI and proxy API")
	portFlag := flag.String("port", "", "HTTP port for web server (defaults to $PORT or 8080)")
	flag.Parse()

	// If -server is explicitly requested or running inside a cloud environment with $PORT (e.g. Koyeb)
	if *serverFlag || os.Getenv("PORT") != "" {
		srv := server.NewServer(*portFlag, "")
		if err := srv.Start(); err != nil {
			log.Fatalf("Server stopped: %v", err)
		}
		return
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	if *daysFlag > 0 {
		cfg.DaysBack = *daysFlag
	}
	if *topFlag > 0 {
		cfg.TopIntervals = *topFlag
	}
	if *spFlag != "" {
		cfg.SPID = *spFlag
	}
	if *htmlFlag {
		cfg.GenerateHTML = true
	}

	// Handle offline file mode
	if *filePathFlag != "" {
		fmt.Printf("📂 Analyzing local file: %s\n", *filePathFlag)
		if err := processFile(*filePathFlag, cfg); err != nil {
			log.Fatalf("Error: %v", err)
		}
		return
	}

	// Handle mock data mode
	if *mockFlag {
		fmt.Println("🧪 Generating synthetic sample meter readings for demonstration...")
		mockFile, err := generateMockXLSX(cfg.DataDir, cfg.DaysBack)
		if err != nil {
			log.Fatalf("Failed to generate mock data: %v", err)
		}
		fmt.Printf("✅ Mock file created: %s\n", mockFile)
		if err := processFile(mockFile, cfg); err != nil {
			log.Fatalf("Error: %v", err)
		}
		return
	}

	// Live mode with EAC DSO portal
	if err := cfg.ValidateCredentials(); err != nil {
		fmt.Printf("⚠️  Configuration error: %v\n", err)
		fmt.Println("💡 Tip: Copy .env.example to .env and fill in your EAC credentials:")
		fmt.Println("   cp .env.example .env")
		fmt.Println("   Or run with -mock to test immediately:")
		fmt.Println("   go run ./cmd/meters-reader -mock")
		os.Exit(1)
	}

	if *loopFlag {
		runPeriodicLoop(cfg)
	} else {
		if err := runOnce(cfg); err != nil {
			log.Fatalf("Execution failed: %v", err)
		}
	}
}

func runOnce(cfg *config.Config) error {
	eac := client.NewEACClient(cfg.BaseURL)

	fmt.Printf("🔑 Logging in to EAC Portal as %s...\n", cfg.Email)
	if err := eac.Login(cfg.Email, cfg.Password); err != nil {
		return fmt.Errorf("authentication error: %w", err)
	}
	fmt.Println("✅ Successfully authenticated.")

	spID, err := resolveServicePoint(eac, cfg.SPID)
	if err != nil {
		return fmt.Errorf("resolve service point: %w", err)
	}

	to := time.Now()
	from := to.AddDate(0, 0, -cfg.DaysBack)

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("readings_%s_%s.xlsx", spID, timestamp)
	destPath := filepath.Join(cfg.DataDir, filename)

	fmt.Printf("📥 Downloading interval readings for SP %s (%s to %s)...\n",
		spID, from.Format("2006-01-02"), to.Format("2006-01-02"))

	if err := eac.DownloadScalarIntervalXLSX(spID, from, to, destPath); err != nil {
		fmt.Printf("⚠️  Scalar interval download failed (%v), trying consumption endpoint...\n", err)
		devices, devErr := eac.GetDevices(spID)
		serial := ""
		if devErr == nil && len(devices) > 0 {
			serial = devices[0].SerialNumber
		}
		if err2 := eac.DownloadConsumptionXLSX(spID, serial, from, to, destPath); err2 != nil {
			return fmt.Errorf("both report endpoints failed: %w; fallback: %v", err, err2)
		}
	}

	fmt.Printf("💾 Saved report to %s\n", destPath)
	return processFile(destPath, cfg)
}

func runPeriodicLoop(cfg *config.Config) {
	fmt.Printf("🔄 Starting periodic daemon (interval: %v). Press Ctrl+C to stop.\n", cfg.FetchInterval)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Run first iteration immediately
	if err := runOnce(cfg); err != nil {
		log.Printf("⚠️  Iteration error: %v", err)
	}

	ticker := time.NewTicker(cfg.FetchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sigChan:
			fmt.Println("\n🛑 Stopping daemon gracefully...")
			return
		case <-ticker.C:
			fmt.Printf("\n⏰ [%s] Starting scheduled fetch...\n", time.Now().Format("2006-01-02 15:04:05"))
			if err := runOnce(cfg); err != nil {
				log.Printf("⚠️  Iteration error: %v", err)
			}
		}
	}
}

func resolveServicePoint(eac *client.EACClient, configuredSPID string) (string, error) {
	if configuredSPID != "" {
		return configuredSPID, nil
	}

	fmt.Println("🔍 Fetching service points list from EAC...")
	sps, err := eac.GetServicePoints()
	if err != nil {
		return "", fmt.Errorf("get service points: %w", err)
	}

	if len(sps) == 0 {
		return "", fmt.Errorf("no service points found on this EAC account")
	}

	fmt.Printf("📋 Found %d service point(s):\n", len(sps))
	for _, sp := range sps {
		fmt.Printf("   • ID: %s | Alias: %s | Address: %s\n", sp.ID, sp.Alias, sp.PremiseAddress)
	}

	selected := sps[0].ID.String()
	fmt.Printf("👉 Using Service Point ID: %s\n", selected)
	return selected, nil
}

func processFile(filePath string, cfg *config.Config) error {
	readings, err := parser.ParseXLSX(filePath)
	if err != nil {
		return fmt.Errorf("parse excel: %w", err)
	}

	rep, err := analyzer.Analyze(readings, cfg.TopIntervals)
	if err != nil {
		return fmt.Errorf("analyze readings: %w", err)
	}

	// Render terminal visualizer
	visualizer.PrintReport(rep)

	// Render HTML report if enabled
	if cfg.GenerateHTML {
		htmlPath := filepath.Join(cfg.DataDir, "consumption_report.html")
		if err := visualizer.GenerateHTML(rep, htmlPath); err != nil {
			log.Printf("⚠️  Warning: failed to write HTML report: %v", err)
		} else {
			fmt.Printf("🌐 Interactive HTML report generated: file://%s\n\n", htmlPath)
		}
	}

	return nil
}

// generateMockXLSX generates a realistic test XLSX file with 15-minute intervals.
func generateMockXLSX(dataDir string, days int) (string, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", err
	}

	f := excelize.NewFile()
	sheet := "IntervalReadings"
	f.SetSheetName("Sheet1", sheet)

	// Write header
	f.SetCellValue(sheet, "A1", "Date")
	f.SetCellValue(sheet, "B1", "Time Interval")
	f.SetCellValue(sheet, "C1", "Active Energy (kWh)")

	now := time.Now().Truncate(15 * time.Minute)
	startTime := now.AddDate(0, 0, -days)

	row := 2
	cur := startTime
	r := rand.New(rand.NewSource(42))

	for cur.Before(now) {
		hour := cur.Hour()
		// Base load 0.15 - 0.25 kWh (e.g. fridge, standby)
		base := 0.18 + r.Float64()*0.07

		// Daytime / Evening peaks (AC / Cooking / Water heater)
		var peak float64
		if hour >= 13 && hour <= 16 { // Afternoon heat / AC
			peak = 0.8 + r.Float64()*1.2
		} else if hour >= 19 && hour <= 22 { // Evening cooking / lighting / AC
			peak = 1.1 + r.Float64()*1.6
		} else if hour >= 7 && hour <= 9 { // Morning routine
			peak = 0.4 + r.Float64()*0.6
		} else {
			peak = r.Float64() * 0.1
		}

		consumption := math.Round((base+peak)*1000) / 1000

		next := cur.Add(15 * time.Minute)
		f.SetCellValue(sheet, fmt.Sprintf("A%d", row), cur.Format("2006-01-02"))
		f.SetCellValue(sheet, fmt.Sprintf("B%d", row), fmt.Sprintf("%s - %s", cur.Format("15:04"), next.Format("15:04")))
		f.SetCellValue(sheet, fmt.Sprintf("C%d", row), consumption)

		cur = next
		row++
	}

	mockPath := filepath.Join(dataDir, "sample_readings_demo.xlsx")
	if err := f.SaveAs(mockPath); err != nil {
		return "", err
	}

	return mockPath, nil
}
