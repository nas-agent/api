package controllers

import (
	"api/config"
	"api/database"
	"api/models"
	"api/services"
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

type SemanticSearchRequest struct {
	Query string `json:"query"`
}

type QueryEntities struct {
	SubjectNames      []string `json:"subject_names"`
	CourseNames       []string `json:"course_names"`
	OrganizationNames []string `json:"organization_names"`
	DatePhrases       []string `json:"date_phrases"`
	ExtraKeywords     []string `json:"extra_keywords"`
}

type PythonSearchResponse struct {
	SemanticIntent string         `json:"semantic_intent"`
	FileType       *string        `json:"file_type"`
	Tags           []string       `json:"tags"`
	StartDate      *string        `json:"start_date"`
	EndDate        *string        `json:"end_date"`
	Entities       QueryEntities  `json:"entities"`
	SearchVector   []float64      `json:"search_vector"`
	RAGCandidates  []RAGCandidate `json:"rag_candidates"`
}

type RAGCandidate struct {
	FileID          uint     `json:"file_id"`
	FileName        string   `json:"file_name"`
	SuggestedFolder string   `json:"suggested_folder"`
	Tags            []string `json:"tags"`
	Summary         string   `json:"summary"`
	Similarity      float64  `json:"similarity"`
}

type SearchResult struct {
	File            models.FileMetadata `json:"file"`
	SimilarityScore float64             `json:"similarity_score"`
}

var tokenPattern = regexp.MustCompile(`[\p{L}\p{N}]+`)

func normalizeTerm(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

func normalizeFileType(fileType string) string {
	v := strings.TrimPrefix(normalizeTerm(fileType), ".")
	switch v {
	case "jpg", "jpeg", "png", "gif", "webp", "bmp", "svg", "heic", "tiff":
		return "image"
	case "doc", "docx", "odt", "rtf", "document":
		return "document"
	case "xls", "xlsx", "csv", "ods", "spreadsheet":
		return "spreadsheet"
	case "ppt", "pptx", "odp", "presentation":
		return "presentation"
	case "py", "go", "js", "jsx", "ts", "tsx", "java", "c", "cpp", "cs", "rb", "php", "rs", "swift", "kt", "kts", "scala", "sql", "html", "css", "json", "yaml", "yml", "xml", "code":
		return "code"
	case "txt", "md", "log", "text":
		return "txt"
	case "pdf":
		return "pdf"
	default:
		return v
	}
}

func matchesFileType(file models.FileMetadata, requested *string) bool {
	if requested == nil || strings.TrimSpace(*requested) == "" {
		return true
	}

	req := normalizeFileType(*requested)
	if req == "" {
		return true
	}

	actual := normalizeFileType(file.FileType)
	if actual == "" {
		dotIndex := strings.LastIndex(file.FileName, ".")
		if dotIndex >= 0 && dotIndex+1 < len(file.FileName) {
			actual = normalizeFileType(file.FileName[dotIndex+1:])
		}
	}

	if actual == req {
		return true
	}

	// Consider document broad enough to include PDFs in user-facing queries.
	if req == "document" && (actual == "pdf" || actual == "document") {
		return true
	}

	return false
}

func buildFileCorpus(file models.FileMetadata, folderDesc string) string {
	parts := []string{file.FileName, file.NASPath, file.Summary, file.FileType, folderDesc}
	for _, tag := range file.Tags {
		parts = append(parts, tag.TagName)
	}
	return normalizeTerm(strings.Join(parts, " "))
}

func containsTerm(corpus, term string) bool {
	if strings.Contains(term, " | ") {
		parts := strings.Split(term, " | ")
		for _, p := range parts {
			norm := normalizeTerm(p)
			if norm != "" && strings.Contains(corpus, norm) {
				return true
			}
		}
		return false
	}

	normalized := normalizeTerm(term)
	if normalized == "" {
		return false
	}
	return strings.Contains(corpus, normalized)
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(values))
	for _, value := range values {
		n := normalizeTerm(value)
		if n == "" {
			continue
		}
		if _, exists := seen[n]; exists {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func tokenize(values ...string) []string {
	var tokens []string
	for _, value := range values {
		matches := tokenPattern.FindAllString(strings.ToLower(value), -1)
		for _, token := range matches {
			if len(token) >= 2 {
				tokens = append(tokens, token)
			}
		}
	}
	return uniqueNonEmpty(tokens)
}

func lexicalEntityScore(file models.FileMetadata, folderDesc string, parsed PythonSearchResponse) float64 {
	corpus := buildFileCorpus(file, folderDesc)
	score := 0.0

	if containsTerm(corpus, parsed.SemanticIntent) {
		score += 0.32
	}

	tags := uniqueNonEmpty(parsed.Tags)
	if len(tags) > 0 {
		matchedTags := 0
		for _, tag := range tags {
			if containsTerm(corpus, tag) {
				matchedTags++
			}
		}
		score += (float64(matchedTags) / float64(len(tags))) * 0.20
	}

	entities := uniqueNonEmpty(append(
		append(append(parsed.Entities.SubjectNames, parsed.Entities.CourseNames...), parsed.Entities.OrganizationNames...),
		parsed.Entities.ExtraKeywords...,
	))
	if len(entities) > 0 {
		matchedEntities := 0
		for _, entity := range entities {
			if containsTerm(corpus, entity) {
				matchedEntities++
			}
		}
		score += (float64(matchedEntities) / float64(len(entities))) * 0.30
	}

	queryTokens := tokenize(strings.Join(append(append([]string{parsed.SemanticIntent}, parsed.Tags...), parsed.Entities.ExtraKeywords...), " "))
	if len(queryTokens) > 0 {
		matchedTokens := 0
		for _, token := range queryTokens {
			if strings.Contains(corpus, token) {
				matchedTokens++
			}
		}
		score += (float64(matchedTokens) / float64(len(queryTokens))) * 0.25
	}

	if score > 1.0 {
		return 1.0
	}
	return score
}

// Calculate the Cosine Similarity between two float64 vectors
func cosineSimilarity(a []float64, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0.0
	}

	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

func SemanticSearch(c *fiber.Ctx) error {
	var req SemanticSearchRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Query == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Query cannot be empty"})
	}

	// 1. Send Query to Python NLP Agent
	aiConfig := config.GetAIServiceConfig()
	userID := GetUserID(c)
	if strings.TrimSpace(userID) == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	pythonReqBody, _ := json.Marshal(map[string]string{
		"query":   req.Query,
		"user_id": userID,
	})
	pythonReq, err := http.NewRequest("POST", aiConfig.Endpoint("/api/search/parse_query"), bytes.NewBuffer(pythonReqBody))
	if err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "AI Search Engine is currently offline"})
	}
	pythonReq.Header.Set("Content-Type", "application/json")
	pythonReq.Header.Set("X-API-Key", aiConfig.APIKey)

	client := &http.Client{}
	resp, err := client.Do(pythonReq)
	if err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "AI Search Engine is currently offline"})
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to read AI response"})
	}

	var pythonResp PythonSearchResponse
	if err := json.Unmarshal(bodyBytes, &pythonResp); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to parse AI response"})
	}

	var startUnix *int64
	if pythonResp.StartDate != nil {
		if startTime, err := time.Parse(time.RFC3339, *pythonResp.StartDate); err == nil {
			v := startTime.Unix()
			startUnix = &v
		}
	}

	var endUnix *int64
	if pythonResp.EndDate != nil {
		if endTime, err := time.Parse(time.RFC3339, *pythonResp.EndDate); err == nil {
			v := endTime.Unix()
			endUnix = &v
		}
	}

	// 2. Fetch all files from DB with their Embeddings and Tags
	var allFiles []models.FileMetadata
	// Filter by Date and User directly in the DB query
	database.DB.Where("owner_id = ?", userID).Preload("Embeddings").Preload("Tags").Find(&allFiles)

	var searchResults []SearchResult
	profiles, _ := services.LoadUserFolderProfiles(userID)
	queryTokens := tokenize(strings.Join(append(
		append([]string{pythonResp.SemanticIntent}, pythonResp.Tags...),
		append(append(pythonResp.Entities.SubjectNames, pythonResp.Entities.CourseNames...),
			append(pythonResp.Entities.OrganizationNames, pythonResp.Entities.ExtraKeywords...)...)...),
		" "))

	ragCandidateScores := map[uint]float64{}
	for _, cand := range pythonResp.RAGCandidates {
		score := cand.Similarity
		if score < 0 {
			score = 0
		}
		if score > 1 {
			score = 1
		}
		ragCandidateScores[cand.FileID] = score
	}

	// 3. Iterate, filter, and calculate cosine similarity
	for _, file := range allFiles {
		// Hard Filters: Date Constraints
		if startUnix != nil && file.CreatedAt < *startUnix {
			continue
		}
		if endUnix != nil && file.CreatedAt > *endUnix {
			continue
		}

		if !matchesFileType(file, pythonResp.FileType) {
			continue
		}

		semanticScore := 0.0
		if len(file.Embeddings) > 0 && len(pythonResp.SearchVector) > 0 {
			var fileVector []float64
			err := json.Unmarshal([]byte(file.Embeddings[0].EmbeddingVector), &fileVector)
			if err == nil && len(fileVector) > 0 {
				semanticScore = math.Max(cosineSimilarity(pythonResp.SearchVector, fileVector), 0)
			}
		}

		lexicalScore := lexicalEntityScore(file, "", pythonResp)
		folderName := filepath.Base(filepath.Dir(file.NASPath))
		if folderName == "." || strings.TrimSpace(folderName) == "" {
			folderName = filepath.Base(file.NASPath)
		}

		// Inject Folder Description context if available
		fProfile, hasProfile := profiles[strings.ToLower(strings.TrimSpace(folderName))]
		if hasProfile && fProfile.Description != "" {
			lexicalScore = lexicalEntityScore(file, fProfile.Description, pythonResp)
		}

		personalizationScore := services.PersonalizationScore(folderName, queryTokens, pythonResp.SearchVector, profiles)
		ragScore := 0.0
		if v, ok := ragCandidateScores[file.ID]; ok {
			ragScore = v
		}

		// Weighted blend of vector meaning and identity/entity matching.
		baseScore := (semanticScore * 0.72) + (lexicalScore * 0.28)
		finalScore := (baseScore * 0.85) + (personalizationScore * 0.15)
		if len(pythonResp.SearchVector) == 0 {
			finalScore = (lexicalScore * 0.80) + (personalizationScore * 0.20)
		}

		if len(ragCandidateScores) > 0 {
			// Blend in ANN retrieval evidence from Python-side vector memory.
			finalScore = (finalScore * 0.90) + (ragScore * 0.10)
			// If ANN has candidates and this file is not one of them, require stronger direct evidence.
			if ragScore == 0 && semanticScore < 0.20 && lexicalScore < 0.20 {
				continue
			}
		}

		if finalScore >= 0.10 || lexicalScore >= 0.24 {
			searchResults = append(searchResults, SearchResult{
				File:            file,
				SimilarityScore: finalScore * 100,
			})
		}
	}

	// 4. Sort results descending by score
	sort.SliceStable(searchResults, func(i, j int) bool {
		return searchResults[i].SimilarityScore > searchResults[j].SimilarityScore
	})

	// 5. Return top 20
	limit := 20
	if len(searchResults) < limit {
		limit = len(searchResults)
	}

	// 6. Record search in history asynchronously
	go func() {
		database.DB.Create(&models.AIActionLog{
			UserID:      userID,
			Action:      "semantic_search",
			Description: "Searched for: \"" + pythonResp.SemanticIntent + "\"",
			Filename:    pythonResp.SemanticIntent,
			Status:      "success",
		})
	}()

	translator := services.NewPathTranslator()
	finalResults := searchResults[:limit]
	for i := range finalResults {
		if finalResults[i].File.NASPath != "" {
			finalResults[i].File.NASPath = translator.ToWindowsPath(userID, finalResults[i].File.NASPath)
		}
	}

	return c.JSON(fiber.Map{
		"intent":              pythonResp.SemanticIntent,
		"filters":             pythonResp,
		"rag_candidate_count": len(pythonResp.RAGCandidates),
		"results":             finalResults,
	})
}
