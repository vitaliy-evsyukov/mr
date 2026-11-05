package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	BaseURL       string
	Email         string
	Password      string
	SPID          string
	FetchInterval time.Duration
	DaysBack      int
	DataDir       string
	TopIntervals  int
	GenerateHTML  bool
}

func LoadConfig() (*Config, error) {
	// Try loading .env if present (ignore error if file doesn't exist)
	_ = godotenv.Load()

	email := getEnv("EAC_EMAIL", "")
	if email == "" {
		email = getEnv("EAC_USERNAME", "")
	}
	password := getEnv("EAC_PASSWORD", "")

	baseURL := getEnv("EAC_BASE_URL", "https://meterreading-dso.eac.com.cy")
	spID := getEnv("EAC_SP_ID", "")
	dataDir := getEnv("DATA_DIR", "./data")

	intervalStr := getEnv("FETCH_INTERVAL", "12h")
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		interval = 12 * time.Hour
	}

	daysBackStr := getEnv("DAYS_BACK", "7")
	daysBack, err := strconv.Atoi(daysBackStr)
	if err != nil || daysBack <= 0 {
		daysBack = 7
	}

	topStr := getEnv("TOP_INTERVALS", "10")
	topIntervals, err := strconv.Atoi(topStr)
	if err != nil || topIntervals <= 0 {
		topIntervals = 10
	}

	genHTMLStr := getEnv("GENERATE_HTML", "true")
	genHTML := genHTMLStr == "true" || genHTMLStr == "1" || genHTMLStr == "yes"

	return &Config{
		BaseURL:       baseURL,
		Email:         email,
		Password:      password,
		SPID:          spID,
		FetchInterval: interval,
		DaysBack:      daysBack,
		DataDir:       dataDir,
		TopIntervals:  topIntervals,
		GenerateHTML:  genHTML,
	}, nil
}

func (c *Config) ValidateCredentials() error {
	if c.Email == "" {
		return fmt.Errorf("EAC_EMAIL is required (specify in .env or environment variable)")
	}
	if c.Password == "" {
		return fmt.Errorf("EAC_PASSWORD is required (specify in .env or environment variable)")
	}
	return nil
}

func getEnv(key, defaultVal string) string {
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		return defaultVal
	}
	return val
}
