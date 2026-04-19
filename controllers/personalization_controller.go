package controllers

import (
	"api/config"
	"api/database"
	"api/models"
	"api/services"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type FeedbackRequest struct {
	FileID            uint           `json:"file_id"`
	Source            string         `json:"source"`
	Outcome           string         `json:"outcome"`
	ReasonCode        string         `json:"reason_code"`
	SuggestedFolder   string         `json:"suggested_folder"`
	FinalFolder       string         `json:"final_folder"`
	SuggestedFileName string         `json:"suggested_file_name"`
	FinalFileName     string         `json:"final_file_name"`
	ConfidenceScore   int            `json:"confidence_score"`
	Metadata          map[string]any `json:"metadata"`
}

type FolderProfileView struct {
	FolderName     string  `json:"folder_name"`
	AcceptCount    int     `json:"accept_count"`
	RejectCount    int     `json:"reject_count"`
	AcceptRate     float64 `json:"accept_rate"`
	Description    string  `json:"description"`
	LastUsedAt     int64   `json:"last_used_at"`
	FileCount      int     `json:"file_count"`
	SubFolderCount int     `json:"subfolder_count"`
}

func SubmitPersonalizationFeedback(c *fiber.Ctx) error {
	userID := GetUserID(c)
	var req FeedbackRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid body"})
	}

	if strings.TrimSpace(req.Outcome) == "" {
		req.Outcome = "accepted"
	}
	if strings.TrimSpace(req.Source) == "" {
		req.Source = "manual"
	}

	if err := services.RecordDecisionEvent(services.DecisionEventInput{
		UserID:            userID,
		FileID:            req.FileID,
		Source:            req.Source,
		Outcome:           req.Outcome,
		ReasonCode:        req.ReasonCode,
		SuggestedFolder:   req.SuggestedFolder,
		FinalFolder:       req.FinalFolder,
		SuggestedFileName: req.SuggestedFileName,
		FinalFileName:     req.FinalFileName,
		ConfidenceScore:   req.ConfidenceScore,
		Metadata:          req.Metadata,
	}); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to persist feedback"})
	}

	// NEW: Notify AI Agent of feedback for semantic learning
	go NotifyAgentOfFeedback(userID, req.FileID, req.Outcome, req.FinalFolder)

	// NEW: Update original AIActionLog status if log_id provided in metadata
	if logIDRaw, ok := req.Metadata["log_id"]; ok {
		var logID uint
		switch v := logIDRaw.(type) {
		case float64:
			logID = uint(v)
		case int:
			logID = uint(v)
		case string:
			fmt.Sscanf(v, "%d", &logID)
		}
		if logID > 0 {
			database.DB.Model(&models.AIActionLog{}).Where("log_id = ? AND user_id = ?", logID, userID).Update("status", req.Outcome)
		}
	}

	return c.JSON(fiber.Map{"message": "feedback saved"})
}

func GetPersonalizationProfile(c *fiber.Ctx) error {
	userID := GetUserID(c)

	// 1. Get Destination Path from Config to see all current folders
	var userAIConfig models.UserAIConfig
	database.DB.Where("user_id = ?", userID).First(&userAIConfig)

	diskFolders := make(map[string]bool)
	root := ""
	if userAIConfig.DestinationPath != "" {
		// Clean the path to handle potential mixed slashes
		root = filepath.Clean(userAIConfig.DestinationPath)

		// Attempt to resolve the correct Linux path using the user's DB info
		if len(root) > 1 && root[1] == ':' {
			// It's a Windows drive mapping (e.g. Z:\Files)
			var share models.Share
			if err := database.DB.Where("owner_id = ? AND type = ?", userID, "Private").First(&share).Error; err == nil && share.Path != "" {
				// Split into Drive "Z:" and Path "\Files"
				parts := strings.SplitN(root, ":", 2)
				if len(parts) == 2 {
					subPath := strings.ReplaceAll(parts[1], "\\", "/") // explicitly convert backslashes to forward slashes
					subPath = strings.TrimPrefix(subPath, "/")
					root = share.Path + "/" + subPath // Build canonical Linux path
					fmt.Printf("[Path Translator DB] Mapped DestinationPath: %s -> %s\n", userAIConfig.DestinationPath, root)
				}
			}
		} else {
			// Fallback: translate UNC or normal path
			if translator := services.NewPathTranslator(); translator != nil {
				if translated, err := translator.TranslatePath(root); err == nil {
					root = translated
				}
			}
		}

		// If it's still a Windows-style path (e.g., Z:\Files, C:\Users)
		// after attempted translation, these are UNC mappings that don't exist on Linux backend
		if strings.Contains(root, "\\") || (len(root) > 1 && root[1] == ':') {
			fmt.Printf("Skipping Windows path %s (UNC mapping on client side only)\n", root)
		} else if strings.HasPrefix(root, "/") {
			// Only process Linux-style absolute paths
			err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					// Log but continue to find other accessible folders
					fmt.Printf("Error accessing path %s: %v\n", path, err)
					return nil
				}
				if d.IsDir() && path != root {
					rel, _ := filepath.Rel(root, path)
					// Normalize to forward slashes for cross-platform depth counting
					normalizedRel := filepath.ToSlash(rel)

					// Limit to 2 levels deep to match the AI scanner's context depth
					if strings.Count(normalizedRel, "/") < 2 {
						diskFolders[rel] = true
					}
				}
				return nil
			})
			if err != nil {
				fmt.Printf("Directory walk error for root %s: %v\n", root, err)
			}
		} else {
			fmt.Printf("Skipping relative or invalid path: %s\n", root)
		}
	}

	// 2. Fetch existing DB Profiles
	var profiles []models.UserFolderProfile
	database.DB.Where("user_id = ?", userID).Find(&profiles)

	profileMap := make(map[string]models.UserFolderProfile)
	for _, p := range profiles {
		profileMap[p.FolderName] = p
	}

	// 3. Merge Disk Folders with DB Profiles
	folderViews := make([]FolderProfileView, 0)
	processedFolders := make(map[string]bool)

	// Prioritize folders found on disk
	for folder := range diskFolders {
		p, exists := profileMap[folder]
		rate := 0.0
		if exists {
			total := p.AcceptCount + p.RejectCount
			if total > 0 {
				rate = float64(p.AcceptCount) / float64(total)
			}
		}

		// Get stats for this folder
		fileCount := 0
		subFolderCount := 0
		folderPath := filepath.Join(root, folder)
		entries, _ := os.ReadDir(folderPath)
		for _, e := range entries {
			if e.IsDir() {
				subFolderCount++
			} else {
				fileCount++
			}
		}

		folderViews = append(folderViews, FolderProfileView{
			FolderName:     folder,
			AcceptCount:    p.AcceptCount,
			RejectCount:    p.RejectCount,
			AcceptRate:     rate,
			Description:    p.Description,
			LastUsedAt:     p.LastUsedAt,
			FileCount:      fileCount,
			SubFolderCount: subFolderCount,
		})
		processedFolders[folder] = true
	}

	// Add historical folders from DB that might no longer exist on disk
	for _, p := range profiles {
		if !processedFolders[p.FolderName] {
			total := p.AcceptCount + p.RejectCount
			rate := 0.0
			if total > 0 {
				rate = float64(p.AcceptCount) / float64(total)
			}
			folderViews = append(folderViews, FolderProfileView{
				FolderName:     p.FolderName,
				AcceptCount:    p.AcceptCount,
				RejectCount:    p.RejectCount,
				AcceptRate:     rate,
				Description:    p.Description,
				LastUsedAt:     p.LastUsedAt,
				FileCount:      0,
				SubFolderCount: 0,
			})
		}
	}

	// 4. Sort by AI activity (most accepted first), then alphabetical fallback
	sort.Slice(folderViews, func(i, j int) bool {
		if folderViews[i].AcceptCount != folderViews[j].AcceptCount {
			return folderViews[i].AcceptCount > folderViews[j].AcceptCount
		}
		// Alphabetical fallback for folders with same activity level
		return strings.ToLower(folderViews[i].FolderName) < strings.ToLower(folderViews[j].FolderName)
	})

	var naming models.UserNamingProfile
	var namingPayload any = nil
	if err := database.DB.Where("user_id = ?", userID).Take(&naming).Error; err == nil {
		namingPayload = naming
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load naming profile"})
	}

	var events []models.DecisionEvent
	database.DB.Where("user_id = ?", userID).Order("created_at desc").Limit(200).Find(&events)

	now := time.Now().Unix()
	sevenDays := int64(7 * 24 * 3600)
	last7Accepted, last7Total := 0, 0
	prev7Accepted, prev7Total := 0, 0

	for _, e := range events {
		if e.CreatedAt >= now-sevenDays {
			last7Total++
			if e.Outcome == "accepted" || e.Outcome == "auto_moved" {
				last7Accepted++
			}
		} else if e.CreatedAt >= now-(2*sevenDays) {
			prev7Total++
			if e.Outcome == "accepted" || e.Outcome == "auto_moved" {
				prev7Accepted++
			}
		}
	}

	last7Rate := ratio(last7Accepted, last7Total)
	prev7Rate := ratio(prev7Accepted, prev7Total)

	// 5. Get AI Service Config for Agent Discovery
	aiSvcConfig := config.GetAIServiceConfig()

	return c.JSON(fiber.Map{
		"folder_profiles": folderViews,
		"naming_profile":  namingPayload,
		"ai_config": fiber.Map{
			"agent_url":     aiSvcConfig.BaseURL,
			"agent_api_key": aiSvcConfig.APIKey,
		},
		"guardrails": fiber.Map{
			"last_7d_accept_rate":     last7Rate,
			"previous_7d_accept_rate": prev7Rate,
			"delta":                   last7Rate - prev7Rate,
			"sample_size_last_7d":     last7Total,
		},
	})
}

func ResetPersonalization(c *fiber.Ctx) error {
	userID := GetUserID(c)
	database.DB.Where("user_id = ?", userID).Delete(&models.UserFolderProfile{})
	database.DB.Where("user_id = ?", userID).Delete(&models.UserNamingProfile{})
	database.DB.Where("user_id = ?", userID).Delete(&models.DecisionEvent{})

	return c.JSON(fiber.Map{"message": "personalization profile reset"})
}

func UpdateFolderProfileDescription(c *fiber.Ctx) error {
	userID := GetUserID(c)
	var req struct {
		FolderName  string `json:"folder_name"`
		Description string `json:"description"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid body"})
	}

	if strings.TrimSpace(req.FolderName) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "folder_name is required"})
	}

	var profile models.UserFolderProfile
	result := database.DB.Where("user_id = ? AND folder_name = ?", userID, req.FolderName).First(&profile)

	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "database error"})
	}

	if profile.ID == 0 {
		// Create new profile record if it doesn't exist
		profile = models.UserFolderProfile{
			UserID:      userID,
			FolderName:  req.FolderName,
			Description: req.Description,
		}
		if err := database.DB.Create(&profile).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create folder profile"})
		}
	} else {
		// Update existing
		profile.Description = req.Description
		if err := database.DB.Save(&profile).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to update folder profile"})
		}
	}

	return c.JSON(fiber.Map{"message": "folder description updated"})
}

func SuggestPersonalizedFilename(c *fiber.Ctx) error {
	userID := GetUserID(c)
	var req struct {
		FileName string   `json:"file_name"`
		Tags     []string `json:"tags"`
		Style    string   `json:"style"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid body"})
	}
	if strings.TrimSpace(req.FileName) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "file_name is required"})
	}

	suggested := services.SuggestPersonalizedFileName(userID, req.FileName, req.Tags, req.Style)
	return c.JSON(fiber.Map{
		"suggested_file_name": suggested,
		"folder_hint":         filepath.Ext(suggested),
	})
}

func ManualRelocateFeedback(c *fiber.Ctx) error {
	userID := GetUserID(c)
	var req struct {
		FileID            uint   `json:"file_id"`
		SuggestedFolder   string `json:"suggested_folder"`
		Strategy          string `json:"strategy"` // origin | custom
		DestinationFolder string         `json:"destination_folder"`
		Metadata          map[string]any `json:"metadata"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid body"})
	}
	if req.FileID == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "file_id is required"})
	}

	strategy := strings.ToLower(strings.TrimSpace(req.Strategy))
	if strategy != "origin" && strategy != "custom" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "strategy must be origin or custom"})
	}

	var file models.FileMetadata
	if err := database.DB.Where("id = ? AND owner_id = ?", req.FileID, userID).First(&file).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "file not found"})
	}

	var cfg models.UserAIConfig
	database.DB.Where("user_id = ?", userID).First(&cfg)

	targetDir := ""
	if strategy == "origin" {
		targetDir = strings.TrimSpace(cfg.OriginPath)
	} else {
		targetDir = strings.TrimSpace(req.DestinationFolder)
	}

	if targetDir == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "destination folder is required"})
	}

	if err := os.MkdirAll(targetDir, os.ModePerm); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to prepare destination"})
	}

	oldPath := file.NASPath
	oldName := file.FileName
	newName := services.EnsureUniqueName(targetDir, file.FileName)
	newPath := filepath.Join(targetDir, newName)

	if err := moveFileWithFallback(oldPath, newPath); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to move file"})
	}

	file.NASPath = newPath
	file.FileName = newName
	file.LastAccessed = time.Now().Unix()
	database.DB.Save(&file)

	finalFolder := filepath.Base(filepath.Dir(newPath))
	reason := "manual_move_custom"
	if strategy == "origin" {
		reason = "moved_back_origin"
	}

	if err := services.RecordDecisionEvent(services.DecisionEventInput{
		UserID:            userID,
		FileID:            file.ID,
		Source:            "history_ui",
		Outcome:           "accepted",
		ReasonCode:        reason,
		SuggestedFolder:   req.SuggestedFolder,
		FinalFolder:       finalFolder,
		SuggestedFileName: oldName,
		FinalFileName:     newName,
		ConfidenceScore:   0,
		Metadata: map[string]any{
			"from_path": oldPath,
			"to_path":   newPath,
			"strategy":  strategy,
		},
	}); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to record learning event"})
	}

	// NEW: Notify AI Agent of manual correction
	go NotifyAgentOfFeedback(userID, file.ID, "rejected", finalFolder)

	database.DB.Create(&models.AIActionLog{
		UserID:      userID,
		FileID:      file.ID,
		Action:      "manual_move",
		Description: fmt.Sprintf("User manually moved file to '%s'", finalFolder),
		Folder:      finalFolder,
		Filename:    newName,
		IsMove:      true,
	})

	// NEW: Update original AIActionLog status if log_id provided in metadata
	if req.Metadata != nil {
		if logIDRaw, ok := req.Metadata["log_id"]; ok {
			var logID uint
			switch v := logIDRaw.(type) {
			case float64:
				logID = uint(v)
			case int:
				logID = uint(v)
			case string:
				fmt.Sscanf(v, "%d", &logID)
			}
			if logID > 0 {
				database.DB.Model(&models.AIActionLog{}).Where("log_id = ? AND user_id = ?", logID, userID).Update("status", "rejected")
			}
		}
	}

	return c.JSON(fiber.Map{
		"message":      "moved",
		"final_path":   newPath,
		"final_folder": finalFolder,
		"file_name":    newName,
	})
}

func CaptureManualMoveFeedback(c *fiber.Ctx) error {
	userID := GetUserID(c)
	var req struct {
		FileID          uint           `json:"file_id"`
		SuggestedFolder string         `json:"suggested_folder"`
		FinalFileName   string         `json:"final_file_name"`
		Metadata        map[string]any `json:"metadata"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid body"})
	}
	if req.FileID == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "file_id is required"})
	}

	var file models.FileMetadata
	if err := database.DB.Where("id = ? AND owner_id = ?", req.FileID, userID).First(&file).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "file not found"})
	}

	targetName := strings.TrimSpace(req.FinalFileName)
	if targetName == "" {
		targetName = file.FileName
	}

	var cfg models.UserAIConfig
	database.DB.Where("user_id = ?", userID).First(&cfg)

	searchRoots := []string{}
	if strings.TrimSpace(cfg.DestinationPath) != "" {
		searchRoots = append(searchRoots, cfg.DestinationPath)
	}
	if strings.TrimSpace(cfg.OriginPath) != "" {
		searchRoots = append(searchRoots, cfg.OriginPath)
	}
	if strings.TrimSpace(file.NASPath) != "" {
		searchRoots = append(searchRoots, filepath.Dir(file.NASPath))
	}

	newPath := ""
	for _, root := range uniquePaths(searchRoots) {
		if strings.TrimSpace(root) == "" {
			continue
		}
		found := findFileByName(root, targetName)
		if found != "" {
			newPath = found
			break
		}
	}

	if newPath == "" && !strings.EqualFold(targetName, file.FileName) {
		for _, root := range uniquePaths(searchRoots) {
			if strings.TrimSpace(root) == "" {
				continue
			}
			found := findFileByName(root, file.FileName)
			if found != "" {
				newPath = found
				targetName = filepath.Base(found)
				break
			}
		}
	}

	if newPath == "" {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "moved file not found yet. move file first and try again"})
	}

	file.NASPath = newPath
	file.FileName = filepath.Base(newPath)
	file.LastAccessed = time.Now().Unix()
	database.DB.Save(&file)

	finalFolder := filepath.Base(filepath.Dir(newPath))
	
	// Notify AI Agent of the capture
	go NotifyAgentOfFeedback(userID, file.ID, "accepted", finalFolder)

	// NEW: Update original AIActionLog status if log_id provided in metadata
	if req.Metadata != nil {
		if logIDRaw, ok := req.Metadata["log_id"]; ok {
			var logID uint
			switch v := logIDRaw.(type) {
			case float64:
				logID = uint(v)
			case int:
				logID = uint(v)
			case string:
				fmt.Sscanf(v, "%d", &logID)
			}
			if logID > 0 {
				database.DB.Model(&models.AIActionLog{}).Where("log_id = ? AND user_id = ?", logID, userID).Update("status", "accepted")
			}
		}
	}

	return c.JSON(fiber.Map{
		"message":      "captured",
		"final_path":   newPath,
		"final_folder": finalFolder,
		"file_name":    file.FileName,
	})
}

var errFileFound = errors.New("file-found")

func findFileByName(root, filename string) string {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(filename) == "" {
		return ""
	}
	if _, err := os.Stat(root); err != nil {
		return ""
	}

	found := ""
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), filename) {
			found = path
			return errFileFound
		}
		return nil
	})

	if walkErr != nil && !errors.Is(walkErr, errFileFound) {
		return ""
	}
	return found
}

func uniquePaths(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		clean := strings.TrimSpace(filepath.Clean(v))
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func moveFileWithFallback(src, dst string) error {
	if strings.TrimSpace(src) == "" || strings.TrimSpace(dst) == "" {
		return errors.New("invalid path")
	}

	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err := io.Copy(destination, source); err != nil {
		destination.Close()
		return err
	}
	if err := destination.Close(); err != nil {
		return err
	}

	if err := os.Remove(src); err != nil {
		return err
	}

	return nil
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}
func GenerateFolderDescription(c *fiber.Ctx) error {
	log.Printf("[AI GATEWAY] Processing description generation request...")
	userID := GetUserID(c)
	var req struct {
		FolderName string `json:"folder_name"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid body"})
	}

	if strings.TrimSpace(req.FolderName) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "folder_name is required"})
	}

	var userAIConfig models.UserAIConfig
	database.DB.Where("user_id = ?", userID).First(&userAIConfig)

	// Context gathering: find files in this folder
	var files []models.FileMetadata
	// We look for files where the path contains the folder name as the parent
	// This is a simple heuristic. Using SQL LIKE on the full path.
	searchPattern := "%" + filepath.FromSlash(req.FolderName) + "%"
	database.DB.Where("owner_id = ? AND nas_path LIKE ?", userID, searchPattern).Limit(15).Find(&files)

	fileContexts := make([]map[string]string, 0)
	for _, f := range files {
		// Only take files that are actually IN that folder (base dir matches)
		if filepath.Base(filepath.Dir(f.NASPath)) == req.FolderName {
			fileContexts = append(fileContexts, map[string]string{
				"name":    f.FileName,
				"summary": f.Summary,
			})
		}
	}

	// Trigger Python Agent
	aiConfig := config.GetAIServiceConfig()
	payload := map[string]any{
		"folder_name":    req.FolderName,
		"file_contexts":  fileContexts,
		"gemini_api_key": userAIConfig.GeminiAPIKey,
	}

	jsonData, _ := json.Marshal(payload)
	agentReq, err := http.NewRequest("POST", aiConfig.Endpoint("/api/analyze/folder"), bytes.NewBuffer(jsonData))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "agent unreachable"})
	}
	agentReq.Header.Set("Content-Type", "application/json")
	agentReq.Header.Set("X-API-Key", aiConfig.APIKey)

	client := &http.Client{}
	resp, err := client.Do(agentReq)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "agent unreachable"})
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": string(body)})
	}

	var result struct {
		Description string `json:"description"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	return c.JSON(fiber.Map{"description": result.Description})
}

// NotifyAgentOfFeedback informs the Python Agent of a user decision for semantic learning
func NotifyAgentOfFeedback(userID string, fileID uint, outcome string, finalFolder string) {
	if fileID == 0 || finalFolder == "" {
		return
	}

	aiConfig := config.GetAIServiceConfig()
	payload := map[string]any{
		"user_id":      userID,
		"file_id":      fileID,
		"outcome":      outcome,
		"final_folder": finalFolder,
	}

	jsonData, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", aiConfig.Endpoint("/api/analyze/feedback"), bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("[Feedback Bridge] Error creating request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", aiConfig.APIKey)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Feedback Bridge] Agent unavailable: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[Feedback Bridge] Agent error (%d): %s", resp.StatusCode, body)
	} else {
		log.Printf("[Feedback Bridge] Successfully reported feedback for file %d to Agent", fileID)
	}
}
