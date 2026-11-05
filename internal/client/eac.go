package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type EACClient struct {
	BaseURL    string
	HTTPClient *http.Client
	Token      string
}

// FlexibleID safely unmarshals an ID whether returned as JSON string or number.
type FlexibleID string

func (f *FlexibleID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = FlexibleID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err == nil {
		*f = FlexibleID(n.String())
		return nil
	}
	*f = FlexibleID(strings.Trim(string(b), "\""))
	return nil
}

func (f FlexibleID) String() string {
	return string(f)
}

type ServicePoint struct {
	ID                 FlexibleID `json:"id"`
	Alias              string     `json:"alias"`
	PremiseAddress     string     `json:"premiseAddress"`
	UsageStartDateTime string     `json:"usageStartDateTime,omitempty"`
	UsageEndDateTime   string     `json:"usageEndDateTime,omitempty"`
}

type Device struct {
	ID           FlexibleID `json:"id"`
	SerialNumber string     `json:"serialNumber"`
	DeviceType   string     `json:"deviceType"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	JWT     string `json:"jwt"`
	Message string `json:"message"`
}

type reportIntervalRequest struct {
	SPID     any    `json:"spId"`
	DateFrom string `json:"dateFrom"`
	DateTo   string `json:"dateTo"`
}

type reportConsumptionRequest struct {
	SPID         any    `json:"spId"`
	SerialNumber string `json:"serialNumber,omitempty"`
	DateFrom     string `json:"dateFrom"`
	DateTo       string `json:"dateTo"`
}

func NewEACClient(baseURL string) *EACClient {
	return &EACClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Login authenticates against the EAC portal and stores the JWT token.
func (c *EACClient) Login(email, password string) error {
	reqBody, err := json.Marshal(loginRequest{
		Email:    email,
		Password: password,
	})
	if err != nil {
		return fmt.Errorf("marshal login request: %w", err)
	}

	url := fmt.Sprintf("%s/api/portal/login", c.BaseURL)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute login request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read login response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var res loginResponse
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return fmt.Errorf("unmarshal login response: %w", err)
	}

	if res.JWT == "" {
		return fmt.Errorf("login response did not contain JWT token")
	}

	c.Token = res.JWT
	return nil
}

// GetServicePoints fetches the list of service points registered to the user account.
func (c *EACClient) GetServicePoints() ([]ServicePoint, error) {
	if c.Token == "" {
		return nil, fmt.Errorf("not authenticated: call Login first")
	}

	url := fmt.Sprintf("%s/api/portal/servicePoints", c.BaseURL)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create servicePoints request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) EAC-Meters-Reader/1.0")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute servicePoints request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read service points response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get service points failed (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var sps []ServicePoint
	if err := json.Unmarshal(bodyBytes, &sps); err != nil {
		return nil, fmt.Errorf("decode service points: %w (raw response: %s)", err, string(bodyBytes))
	}

	return sps, nil
}

// GetDevices fetches meter devices for a given service point.
func (c *EACClient) GetDevices(spID string) ([]Device, error) {
	if c.Token == "" {
		return nil, fmt.Errorf("not authenticated: call Login first")
	}

	url := fmt.Sprintf("%s/api/portal/servicePoints/%s", c.BaseURL, spID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create devices request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute devices request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get devices failed (status %d): %s", resp.StatusCode, string(body))
	}

	var devices []Device
	if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
		return nil, fmt.Errorf("decode devices: %w", err)
	}

	return devices, nil
}

// DownloadScalarIntervalXLSX downloads interval meter readings in XLSX format.
func (c *EACClient) DownloadScalarIntervalXLSX(spID string, from, to time.Time, destPath string) error {
	if c.Token == "" {
		return fmt.Errorf("not authenticated: call Login first")
	}

	// EAC frontend sets noon 12:00:00 UTC to avoid timezone drift
	dateFrom := time.Date(from.Year(), from.Month(), from.Day(), 12, 0, 0, 0, time.UTC).Format("2006-01-02T15:04:05.000Z")
	dateTo := time.Date(to.Year(), to.Month(), to.Day(), 12, 0, 0, 0, time.UTC).Format("2006-01-02T15:04:05.000Z")

	payload := reportIntervalRequest{
		SPID:     spID,
		DateFrom: dateFrom,
		DateTo:   dateTo,
	}

	reqBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal interval request: %w", err)
	}

	url := fmt.Sprintf("%s/api/portal/report/scalarinterval/xlsx", c.BaseURL)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(reqBytes))
	if err != nil {
		return fmt.Errorf("create report request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) EAC-Meters-Reader/1.0")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute report download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download scalarinterval/xlsx failed (status %d): %s", resp.StatusCode, string(body))
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	outFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file %s: %w", destPath, err)
	}
	defer outFile.Close()

	n, err := io.Copy(outFile, resp.Body)
	if err != nil {
		return fmt.Errorf("write response to file: %w", err)
	}

	if n == 0 {
		return fmt.Errorf("downloaded file is empty")
	}

	return nil
}

// DownloadConsumptionXLSX is an alternative report endpoint for consumption xlsx.
func (c *EACClient) DownloadConsumptionXLSX(spID string, serialNumber string, from, to time.Time, destPath string) error {
	if c.Token == "" {
		return fmt.Errorf("not authenticated: call Login first")
	}

	dateFrom := time.Date(from.Year(), from.Month(), from.Day(), 12, 0, 0, 0, time.UTC).Format("2006-01-02T15:04:05.000Z")
	dateTo := time.Date(to.Year(), to.Month(), to.Day(), 12, 0, 0, 0, time.UTC).Format("2006-01-02T15:04:05.000Z")

	payload := reportConsumptionRequest{
		SPID:         spID,
		SerialNumber: serialNumber,
		DateFrom:     dateFrom,
		DateTo:       dateTo,
	}

	reqBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal consumption request: %w", err)
	}

	url := fmt.Sprintf("%s/api/portal/report/consumption/xlsx", c.BaseURL)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(reqBytes))
	if err != nil {
		return fmt.Errorf("create report request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) EAC-Meters-Reader/1.0")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute consumption download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download consumption/xlsx failed (status %d): %s", resp.StatusCode, string(body))
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	outFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file %s: %w", destPath, err)
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return fmt.Errorf("write response to file: %w", err)
	}

	return nil
}
