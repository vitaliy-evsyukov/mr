package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"meters-reader/internal/analyzer"
	"meters-reader/internal/cache"
	"meters-reader/internal/client"
	"meters-reader/internal/parser"
	"meters-reader/web"
)

type Server struct {
	cache   *cache.LRUCache
	baseURL string
	port    string
}

type fetchRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	DaysBack int    `json:"daysBack"`
	SPID     string `json:"spId"`
}

type fetchResponse struct {
	Report    *analyzer.ConsumptionReport `json:"report"`
	FromCache bool                        `json:"fromCache"`
	CachedAgo string                      `json:"cachedAgo,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewServer(port string, baseURL string) *Server {
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8080"
	}
	if baseURL == "" {
		baseURL = "https://meterreading-dso.eac.com.cy"
	}

	return &Server{
		cache:   cache.NewLRUCache(100),
		baseURL: baseURL,
		port:    port,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Static web interface
	mux.HandleFunc("/", s.handleIndex)

	// API endpoints
	mux.HandleFunc("/api/fetch", s.handleFetch)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/health", s.handleHealth)

	addr := fmt.Sprintf(":%s", s.port)
	log.Printf("🚀 EAC Meter Reader Web Service listening on http://0.0.0.0%s", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data, err := web.Content.ReadFile("index.html")
	if err != nil {
		http.Error(w, "Web UI not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleFetch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req fetchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	if req.Email == "" || req.Password == "" {
		writeJSONError(w, http.StatusBadRequest, "Email and Password are required")
		return
	}

	if req.DaysBack <= 0 {
		req.DaysBack = 7
	}
	if req.DaysBack > 90 {
		req.DaysBack = 90
	}

	// Check LRU Cache
	cacheKey := cache.GenerateKey(req.Email, req.SPID, req.DaysBack)
	if cachedReport, cachedAt, hit := s.cache.Get(cacheKey); hit {
		cachedAgo := formatDurationAgo(time.Since(cachedAt))
		writeJSON(w, http.StatusOK, fetchResponse{
			Report:    cachedReport,
			FromCache: true,
			CachedAgo: cachedAgo,
		})
		return
	}

	// Live fetch from EAC Portal (no credentials stored on server!)
	eac := client.NewEACClient(s.baseURL)

	if err := eac.Login(req.Email, req.Password); err != nil {
		log.Printf("Authentication failed for user %s: %v", req.Email, err)
		writeJSONError(w, http.StatusUnauthorized, fmt.Sprintf("Ошибка авторизации EAC: %v", err))
		return
	}

	spID := req.SPID
	if spID == "" {
		sps, err := eac.GetServicePoints()
		if err != nil || len(sps) == 0 {
			writeJSONError(w, http.StatusBadRequest, "Не удалось найти активные точки учета (Service Points) на аккаунте")
			return
		}
		spID = sps[0].ID.String()
	}

	to := time.Now()
	from := to.AddDate(0, 0, -req.DaysBack)

	// Create temporary file for download that is deleted immediately after parsing
	tempFile, err := os.CreateTemp("", "eac_readings_*.xlsx")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Ошибка создания временного файла")
		return
	}
	tempPath := tempFile.Name()
	_ = tempFile.Close()
	defer os.Remove(tempPath) // Clean up immediately from disk

	if err := eac.DownloadScalarIntervalXLSX(spID, from, to, tempPath); err != nil {
		log.Printf("Scalar download failed, trying consumption endpoint: %v", err)
		devices, _ := eac.GetDevices(spID)
		serial := ""
		if len(devices) > 0 {
			serial = devices[0].SerialNumber
		}
		if err2 := eac.DownloadConsumptionXLSX(spID, serial, from, to, tempPath); err2 != nil {
			writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("Ошибка скачивания отчета с портала: %v", err2))
			return
		}
	}

	readings, err := parser.ParseXLSX(tempPath)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка разбора файла Excel: %v", err))
		return
	}

	report, err := analyzer.Analyze(readings, 10)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка анализа показаний: %v", err))
		return
	}

	// Save to LRU cache (TTL: 2 hours)
	s.cache.Set(cacheKey, report, 2*time.Hour)

	writeJSON(w, http.StatusOK, fetchResponse{
		Report:    report,
		FromCache: false,
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, errorResponse{Error: msg})
}

func formatDurationAgo(d time.Duration) string {
	minutes := int(d.Minutes())
	if minutes < 1 {
		return "только что"
	}
	if minutes < 60 {
		return fmt.Sprintf("%d мин. назад", minutes)
	}
	hours := int(d.Hours())
	return fmt.Sprintf("%d ч. назад", hours)
}
