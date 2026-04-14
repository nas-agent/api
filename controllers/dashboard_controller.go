package controllers

import (
	"api/database"
	"api/models"
	"encoding/json"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

type SummaryCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type SummaryRecentFile struct {
	Name     string `json:"name"`
	Folder   string `json:"folder"`
	Type     string `json:"type"`
	Modified int64  `json:"modified"`
}

type DashboardSummaryResponse struct {
	TotalFiles            int                 `json:"total_files"`
	TotalFolders          int                 `json:"total_folders"`
	FilesProcessedToday   int                 `json:"files_processed_today"`
	FilesMovedToday       int                 `json:"files_moved_today"`
	PendingReview         int                 `json:"pending_review"`
	AverageConfidence     *float64            `json:"average_confidence"`
	SuccessRate           float64             `json:"success_rate"`
	TopFileTypes          []SummaryCount      `json:"top_file_types"`
	TopFolders            []SummaryCount      `json:"top_folders"`
	RecentFiles           []SummaryRecentFile `json:"recent_files"`
	OriginPath            string              `json:"origin_path"`
	DestinationPath       string              `json:"destination_path"`
	OriginConfigured      bool                `json:"origin_configured"`
	DestinationConfigured bool                `json:"destination_configured"`
	MonitoringActive      bool                `json:"monitoring_active"`
	AiOnline              bool                `json:"ai_online"`
	AiModelsLoaded        bool                `json:"ai_models_loaded"`
	LastAction            string              `json:"last_action"`
	LastActionAt          *int64              `json:"last_action_at"`
}

var confidencePattern = regexp.MustCompile(`Confidence:\s*(\d+(?:\.\d+)?)%`)

func normalizeFileTypeFromName(fileType, fileName string) string {
	value := strings.TrimSpace(strings.ToLower(fileType))
	if value == "" {
		value = strings.TrimSpace(strings.ToLower(filepath.Ext(fileName)))
	}
	value = strings.TrimPrefix(value, ".")
	if value == "" {
		return "no_extension"
	}
	switch value {
	case "jpg", "jpeg", "png", "gif", "webp", "bmp", "svg", "heic", "tiff":
		return "image"
	case "doc", "docx", "odt", "rtf":
		return "document"
	case "xls", "xlsx", "csv", "ods":
		return "spreadsheet"
	case "ppt", "pptx", "odp":
		return "presentation"
	case "py", "go", "js", "jsx", "ts", "tsx", "java", "c", "cpp", "cs", "rb", "php", "rs", "swift", "kt", "kts", "scala", "sql", "html", "css", "json", "yaml", "yml", "xml":
		return "code"
	case "txt", "md", "log":
		return "text"
	case "pdf":
		return "pdf"
	default:
		return value
	}
}

func parseConfidence(description string) *float64 {
	matches := confidencePattern.FindStringSubmatch(description)
	if len(matches) != 2 {
		return nil
	}
	value, err := strconvParseFloat(matches[1])
	if err != nil {
		return nil
	}
	return &value
}

func strconvParseFloat(value string) (float64, error) {
	return strconv.ParseFloat(value, 64)
}

func probeAIHealth() (bool, bool) {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:8000/health")
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, false
	}
	var payload struct {
		Status         string `json:"status"`
		ModelsLoaded   bool   `json:"models_loaded"`
		LLMService     string `json:"llm_service"`
		EmbeddingState string `json:"embedding_service"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false, false
	}
	return payload.Status == "online", payload.ModelsLoaded
}

func GetDashboardSummary(c *fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var configs []models.UserAIConfig
	database.DB.Where("user_id = ?", userID).Find(&configs)

	originSet := make(map[string]struct{})
	destinationConfigured := false
	monitoringActive := false
	originPath := ""
	destinationPath := ""
	for _, config := range configs {
		if config.OriginPath != "" {
			originSet[config.OriginPath] = struct{}{}
			if originPath == "" {
				originPath = config.OriginPath
			}
		}
		if config.DestinationPath != "" {
			destinationConfigured = true
			if destinationPath == "" {
				destinationPath = config.DestinationPath
			}
		}
		if config.Active {
			monitoringActive = true
		}
	}

	var history []models.AIActionLog
	database.DB.Where("user_id = ?", userID).Order("log_id desc").Limit(100).Find(&history)

	now := time.Now().Local()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	cutoff24h := now.Add(-24 * time.Hour)

	filesProcessedToday := 0
	filesMovedToday := 0
	pendingReview := 0
	var confidenceSum float64
	confidenceCount := 0
	lastAction := "Ready for your first task"
	var lastActionAt *int64
	if len(history) > 0 {
		lastAction = history[0].Description
		latest := history[0].CreatedAt
		lastActionAt = &latest
	}

	for _, entry := range history {
		if entry.CreatedAt >= startOfDay.Unix() {
			filesProcessedToday++
		}
		if entry.Action == "move_file" {
			filesMovedToday++
		}
		if entry.Action == "suggestion" {
			pendingReview++
		}
		if confidence := parseConfidence(entry.Description); confidence != nil {
			confidenceSum += *confidence
			confidenceCount++
		}
	}

	var files []models.FileMetadata
	database.DB.Where("owner_id = ?", userID).Find(&files)

	fileTypeCounts := make(map[string]int)
	folderCounts := make(map[string]int)
	recentFiles := make([]SummaryRecentFile, 0, 8)
	for _, file := range files {
		fileType := normalizeFileTypeFromName(file.FileType, file.FileName)
		fileTypeCounts[fileType]++

		folderName := filepath.Base(filepath.Dir(file.NASPath))
		if folderName == "." || folderName == "" {
			folderName = filepath.Base(file.NASPath)
		}
		folderCounts[folderName]++

		if file.CreatedAt >= cutoff24h.Unix() {
			recentFiles = append(recentFiles, SummaryRecentFile{
				Name:     file.FileName,
				Folder:   folderName,
				Type:     fileType,
				Modified: file.CreatedAt,
			})
		}
	}

	topFileTypes := make([]SummaryCount, 0, len(fileTypeCounts))
	for name, count := range fileTypeCounts {
		topFileTypes = append(topFileTypes, SummaryCount{Name: name, Count: count})
	}
	sort.Slice(topFileTypes, func(i, j int) bool { return topFileTypes[i].Count > topFileTypes[j].Count })

	topFolders := make([]SummaryCount, 0, len(folderCounts))
	for name, count := range folderCounts {
		topFolders = append(topFolders, SummaryCount{Name: name, Count: count})
	}
	sort.Slice(topFolders, func(i, j int) bool { return topFolders[i].Count > topFolders[j].Count })
	sort.Slice(recentFiles, func(i, j int) bool { return recentFiles[i].Modified > recentFiles[j].Modified })
	if len(recentFiles) > 8 {
		recentFiles = recentFiles[:8]
	}

	var avgConfidence *float64
	if confidenceCount > 0 {
		value := confidenceSum / float64(confidenceCount)
		avgConfidence = &value
	}

	successRate := 0.0
	if filesProcessedToday > 0 {
		successRate = (float64(filesMovedToday) / float64(filesProcessedToday)) * 100
	}

	aiOnline, aiModelsLoaded := probeAIHealth()

	response := DashboardSummaryResponse{
		TotalFiles:            len(files),
		TotalFolders:          len(originSet),
		FilesProcessedToday:   filesProcessedToday,
		FilesMovedToday:       filesMovedToday,
		PendingReview:         pendingReview,
		AverageConfidence:     avgConfidence,
		SuccessRate:           successRate,
		TopFileTypes:          topFileTypes,
		TopFolders:            topFolders,
		RecentFiles:           recentFiles,
		OriginPath:            originPath,
		DestinationPath:       destinationPath,
		OriginConfigured:      originPath != "",
		DestinationConfigured: destinationConfigured,
		MonitoringActive:      monitoringActive,
		AiOnline:              aiOnline,
		AiModelsLoaded:        aiModelsLoaded,
		LastAction:            lastAction,
		LastActionAt:          lastActionAt,
	}

	return c.JSON(response)
}
