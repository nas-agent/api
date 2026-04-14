package services

import (
	"api/database"
	"api/models"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type DecisionEventInput struct {
	UserID            uint
	FileID            uint
	Source            string
	Outcome           string
	ReasonCode        string
	SuggestedFolder   string
	FinalFolder       string
	SuggestedFileName string
	FinalFileName     string
	ConfidenceScore   int
	Metadata          map[string]any
}

type FolderProfileSnapshot struct {
	AcceptCount   int
	RejectCount   int
	Keyword       map[string]float64
	Centroid      []float64
	CentroidCount int
	Description   string
	LastUsedAt    int64
}

var tokenExtractor = regexp.MustCompile(`[\p{L}\p{N}]{2,}`)

func RecordDecisionEvent(input DecisionEventInput) error {
	if input.UserID == 0 {
		return nil
	}

	meta := "{}"
	if len(input.Metadata) > 0 {
		if b, err := json.Marshal(input.Metadata); err == nil {
			meta = string(b)
		}
	}

	event := models.DecisionEvent{
		UserID:            input.UserID,
		FileID:            input.FileID,
		Source:            defaultString(input.Source, "system"),
		Outcome:           defaultString(input.Outcome, "observed"),
		ReasonCode:        strings.TrimSpace(input.ReasonCode),
		SuggestedFolder:   strings.TrimSpace(input.SuggestedFolder),
		FinalFolder:       strings.TrimSpace(input.FinalFolder),
		SuggestedFileName: strings.TrimSpace(input.SuggestedFileName),
		FinalFileName:     strings.TrimSpace(input.FinalFileName),
		ConfidenceScore:   input.ConfidenceScore,
		MetadataJSON:      meta,
	}

	if err := database.DB.Create(&event).Error; err != nil {
		return err
	}

	if shouldCreateFeedback(input) {
		feedbackType := "folder_correction"
		original := input.SuggestedFolder
		corrected := input.FinalFolder
		if input.ReasonCode == "changed_name" {
			feedbackType = "filename_correction"
			original = input.SuggestedFileName
			corrected = input.FinalFileName
		}
		database.DB.Create(&models.FeedbackLog{
			UserID:         input.UserID,
			FileID:         input.FileID,
			FeedbackType:   feedbackType,
			OriginalValue:  original,
			CorrectedValue: corrected,
		})
	}

	if err := updateFolderProfile(input); err != nil {
		return err
	}

	if err := updateNamingProfile(input); err != nil {
		return err
	}

	return nil
}

func shouldCreateFeedback(input DecisionEventInput) bool {
	reason := strings.TrimSpace(strings.ToLower(input.ReasonCode))
	return reason == "changed_folder" || reason == "changed_name"
}

func LoadUserFolderProfiles(userID uint) (map[string]FolderProfileSnapshot, error) {
	out := map[string]FolderProfileSnapshot{}
	var rows []models.UserFolderProfile
	if err := database.DB.Where("user_id = ?", userID).Find(&rows).Error; err != nil {
		return out, err
	}

	for _, row := range rows {
		keyword := map[string]float64{}
		if strings.TrimSpace(row.KeywordWeights) != "" {
			_ = json.Unmarshal([]byte(row.KeywordWeights), &keyword)
		}
		centroid := parseFloatSlice(row.CentroidVector)
		out[strings.ToLower(strings.TrimSpace(row.FolderName))] = FolderProfileSnapshot{
			AcceptCount:   row.AcceptCount,
			RejectCount:   row.RejectCount,
			Keyword:       keyword,
			Centroid:      centroid,
			CentroidCount: row.CentroidCount,
			Description:   row.Description,
			LastUsedAt:    row.LastUsedAt,
		}
	}

	return out, nil
}

func PersonalizationScore(folderName string, queryTokens []string, searchVector []float64, profileMap map[string]FolderProfileSnapshot) float64 {
	folder := strings.ToLower(strings.TrimSpace(folderName))
	profile, ok := profileMap[folder]
	if !ok {
		return 0
	}

	total := profile.AcceptCount + profile.RejectCount
	acceptRatio := 0.5
	if total > 0 {
		acceptRatio = float64(profile.AcceptCount) / float64(total)
	}

	keywordScore := 0.0
	if len(queryTokens) > 0 && len(profile.Keyword) > 0 {
		sum := 0.0
		for _, token := range uniqueTokens(queryTokens) {
			if weight, exists := profile.Keyword[token]; exists {
				sum += math.Max(weight, 0)
			}
		}
		keywordScore = sum / float64(len(queryTokens))
		if keywordScore > 1 {
			keywordScore = 1
		}
	}

	centroidScore := 0.0
	if len(searchVector) > 0 && len(profile.Centroid) == len(searchVector) {
		centroidScore = math.Max(cosineSimilarity(searchVector, profile.Centroid), 0)
	}

	recency := 0.0
	if profile.LastUsedAt > 0 {
		hoursAgo := time.Since(time.Unix(profile.LastUsedAt, 0)).Hours()
		recency = math.Exp(-hoursAgo / 168.0) // weekly decay
	}

	descriptionScore := 0.0
	if profile.Description != "" && len(queryTokens) > 0 {
		descTokens := tokenize(profile.Description)
		matches := 0
		for _, q := range queryTokens {
			for _, d := range descTokens {
				if q == d {
					matches++
					break
				}
			}
		}
		if len(queryTokens) > 0 {
			descriptionScore = float64(matches) / float64(len(queryTokens))
		}
	}

	final := (0.45 * acceptRatio) + (0.15 * keywordScore) + (0.15 * centroidScore) + (0.20 * descriptionScore) + (0.05 * recency)
	if final > 1 {
		return 1
	}
	return final
}

func SuggestFolderForFile(userID uint, aiFolder string, existingFolders []string, fileName string, summary string, tags []string) (string, float64) {
	if !allowFolderOverride(userID) {
		return aiFolder, 0
	}

	profiles, err := LoadUserFolderProfiles(userID)
	if err != nil {
		return aiFolder, 0
	}
	if len(profiles) == 0 {
		return aiFolder, 0
	}

	tokens := tokenize(strings.Join(append(tags, fileName, summary), " "))
	bestFolder := aiFolder
	bestScore := PersonalizationScore(aiFolder, tokens, nil, profiles)

	for _, candidate := range existingFolders {
		score := PersonalizationScore(candidate, tokens, nil, profiles)
		if score > bestScore {
			bestScore = score
			bestFolder = candidate
		}
	}

	if strings.TrimSpace(bestFolder) == "" {
		return aiFolder, bestScore
	}

	// Guardrail: only override AI when profile evidence is clearly stronger.
	if !strings.EqualFold(bestFolder, aiFolder) && bestScore < 0.62 {
		return aiFolder, bestScore
	}

	return bestFolder, bestScore
}

func SuggestPersonalizedFileName(userID uint, originalFileName string, tags []string, fallbackStyle string) string {
	base := strings.TrimSuffix(originalFileName, filepath.Ext(originalFileName))
	ext := filepath.Ext(originalFileName)
	if strings.TrimSpace(base) == "" {
		return originalFileName
	}

	style := strings.TrimSpace(fallbackStyle)
	var profile models.UserNamingProfile
	database.DB.Where("user_id = ?", userID).Limit(1).Find(&profile)
	if profile.ID != 0 && strings.TrimSpace(profile.PreferredStyle) != "" {
		style = profile.PreferredStyle
	}
	if style == "" {
		style = "opt2"
	}

	parts := tokenize(strings.Join(append([]string{base}, tags...), " "))
	if len(parts) == 0 {
		parts = tokenize(base)
	}
	if len(parts) == 0 {
		return originalFileName
	}

	newBase := applyNamingStyle(parts, style)
	newBase = sanitizeFileName(newBase)
	if strings.TrimSpace(newBase) == "" {
		return originalFileName
	}
	return newBase + ext
}

func updateFolderProfile(input DecisionEventInput) error {
	folder := strings.TrimSpace(input.FinalFolder)
	if folder == "" {
		return nil
	}

	var profile models.UserFolderProfile
	database.DB.Where("user_id = ? AND folder_name = ?", input.UserID, folder).Limit(1).Find(&profile)
	if profile.ID == 0 {
		profile = models.UserFolderProfile{UserID: input.UserID, FolderName: folder}
	}

	accepted := input.Outcome == "accepted" || input.Outcome == "auto_moved"
	if accepted {
		profile.AcceptCount++
		profile.LastUsedAt = time.Now().Unix()
	} else if input.Outcome == "rejected" || input.ReasonCode == "changed_folder" {
		profile.RejectCount++
	}

	keywords := map[string]float64{}
	if strings.TrimSpace(profile.KeywordWeights) != "" {
		_ = json.Unmarshal([]byte(profile.KeywordWeights), &keywords)
	}

	terms := collectFileTerms(input.FileID)
	for _, token := range terms {
		if accepted {
			keywords[token] += 0.08
		} else {
			keywords[token] -= 0.03
		}
		keywords[token] = clamp(keywords[token], -1, 1)
	}

	if b, err := json.Marshal(keywords); err == nil {
		profile.KeywordWeights = string(b)
	}

	if accepted {
		vec := fetchFileVector(input.FileID)
		if len(vec) > 0 {
			existing := parseFloatSlice(profile.CentroidVector)
			if len(existing) == 0 {
				profile.CentroidVector = string(mustJSON(vec))
				profile.CentroidCount = 1
			} else if len(existing) == len(vec) {
				n := float64(profile.CentroidCount)
				for i := range existing {
					existing[i] = ((existing[i] * n) + vec[i]) / (n + 1)
				}
				profile.CentroidCount++
				profile.CentroidVector = string(mustJSON(existing))
			}
		}
	}

	if profile.ID == 0 {
		return database.DB.Create(&profile).Error
	}
	return database.DB.Save(&profile).Error
}

func updateNamingProfile(input DecisionEventInput) error {
	if input.UserID == 0 {
		return nil
	}
	finalName := strings.TrimSpace(input.FinalFileName)
	if finalName == "" {
		return nil
	}

	var profile models.UserNamingProfile
	database.DB.Where("user_id = ?", input.UserID).Limit(1).Find(&profile)
	if profile.ID == 0 {
		profile = models.UserNamingProfile{UserID: input.UserID, PreferredStyle: "opt2", Separator: "_", DateFormat: "2006-01-02", PreferredLanguage: "auto"}
	}

	patternScores := map[string]int{}
	if strings.TrimSpace(profile.PatternScores) != "" {
		_ = json.Unmarshal([]byte(profile.PatternScores), &patternScores)
	}

	style := detectStyle(finalName)
	if style != "" {
		profile.PreferredStyle = style
		patternScores[style] = patternScores[style] + 1
	}
	if strings.Contains(finalName, "_") {
		profile.Separator = "_"
	} else if strings.Contains(finalName, "-") {
		profile.Separator = "-"
	} else {
		profile.Separator = " "
	}

	if b, err := json.Marshal(patternScores); err == nil {
		profile.PatternScores = string(b)
	}

	if profile.ID == 0 {
		return database.DB.Create(&profile).Error
	}
	return database.DB.Save(&profile).Error
}

func collectFileTerms(fileID uint) []string {
	if fileID == 0 {
		return nil
	}
	var file models.FileMetadata
	if err := database.DB.Preload("Tags").First(&file, fileID).Error; err != nil {
		return nil
	}
	chunks := []string{file.FileName, file.Summary, file.FileType}
	for _, tag := range file.Tags {
		chunks = append(chunks, tag.TagName)
	}
	return tokenize(strings.Join(chunks, " "))
}

func fetchFileVector(fileID uint) []float64 {
	if fileID == 0 {
		return nil
	}
	var emb models.FileEmbedding
	if err := database.DB.Where("file_id = ?", fileID).Limit(1).Find(&emb).Error; err != nil || emb.ID == 0 {
		return nil
	}
	return parseFloatSlice(emb.EmbeddingVector)
}

func parseFloatSlice(raw string) []float64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var a []float64
	if err := json.Unmarshal([]byte(raw), &a); err == nil {
		return a
	}
	var b []float32
	if err := json.Unmarshal([]byte(raw), &b); err == nil {
		out := make([]float64, len(b))
		for i, v := range b {
			out[i] = float64(v)
		}
		return out
	}
	return nil
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	dot := 0.0
	normA := 0.0
	normB := 0.0
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func tokenize(text string) []string {
	matches := tokenExtractor.FindAllString(strings.ToLower(text), -1)
	return uniqueTokens(matches)
}

func uniqueTokens(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		t := strings.TrimSpace(strings.ToLower(v))
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func applyNamingStyle(parts []string, style string) string {
	switch style {
	case "opt1":
		for i, p := range parts {
			parts[i] = strings.Title(strings.ToLower(p))
		}
		return strings.Join(parts, " ")
	case "opt3":
		for i, p := range parts {
			parts[i] = strings.ToLower(p)
		}
		return strings.Join(parts, "-")
	case "opt4":
		for i, p := range parts {
			if i == 0 {
				parts[i] = strings.ToLower(p)
				continue
			}
			parts[i] = strings.Title(strings.ToLower(p))
		}
		return strings.Join(parts, "")
	case "opt5":
		for i, p := range parts {
			parts[i] = strings.Title(strings.ToLower(p))
		}
		return strings.Join(parts, "")
	case "opt6":
		for i, p := range parts {
			parts[i] = strings.ToUpper(p)
		}
		return strings.Join(parts, "_")
	default: // opt2 and fallback
		for i, p := range parts {
			parts[i] = strings.ToLower(p)
		}
		return strings.Join(parts, "_")
	}
}

func sanitizeFileName(name string) string {
	replacer := strings.NewReplacer(
		"/", " ",
		"\\", " ",
		":", " ",
		"*", " ",
		"?", " ",
		"\"", " ",
		"<", " ",
		">", " ",
		"|", " ",
	)
	clean := replacer.Replace(name)
	clean = strings.Join(strings.Fields(clean), " ")
	return strings.TrimSpace(clean)
}

func detectStyle(fileName string) string {
	base := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	if strings.Contains(base, "_") {
		if base == strings.ToUpper(base) {
			return "opt6"
		}
		return "opt2"
	}
	if strings.Contains(base, "-") {
		return "opt3"
	}
	if strings.Contains(base, " ") {
		if strings.Title(strings.ToLower(base)) == base {
			return "opt1"
		}
	}
	if len(base) > 0 && strings.ToUpper(base[:1]) == base[:1] {
		return "opt5"
	}
	if strings.IndexFunc(base, func(r rune) bool { return r >= 'A' && r <= 'Z' }) > 0 {
		return "opt4"
	}
	return "opt2"
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("[]")
	}
	return b
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func clamp(v, minV, maxV float64) float64 {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func allowFolderOverride(userID uint) bool {
	var events []models.DecisionEvent
	database.DB.Where("user_id = ?", userID).Order("created_at desc").Limit(400).Find(&events)
	if len(events) < 30 {
		return true
	}

	now := time.Now().Unix()
	window := int64(7 * 24 * 3600)
	last7Accepted, last7Total := 0, 0
	prev7Accepted, prev7Total := 0, 0

	for _, e := range events {
		if e.CreatedAt >= now-window {
			last7Total++
			if e.Outcome == "accepted" || e.Outcome == "auto_moved" {
				last7Accepted++
			}
		} else if e.CreatedAt >= now-(2*window) {
			prev7Total++
			if e.Outcome == "accepted" || e.Outcome == "auto_moved" {
				prev7Accepted++
			}
		}
	}

	if last7Total < 10 || prev7Total < 10 {
		return true
	}
	lastRate := float64(last7Accepted) / float64(last7Total)
	prevRate := float64(prev7Accepted) / float64(prev7Total)

	// Guardrail: disable aggressive folder overrides when quality drops sharply.
	return (lastRate - prevRate) > -0.15
}

func EnsureUniqueName(dirPath, fileName string) string {
	candidate := fileName
	ext := filepath.Ext(fileName)
	base := strings.TrimSuffix(fileName, ext)
	index := 1
	for {
		if _, err := os.Stat(filepath.Join(dirPath, candidate)); os.IsNotExist(err) {
			return candidate
		}
		candidate = base + "_" + strconv.Itoa(index) + ext
		index++
	}
}
