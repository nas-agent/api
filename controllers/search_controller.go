package controllers

import (
	"api/database"
	"api/models"
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/gofiber/fiber/v2"
)

type SemanticSearchRequest struct {
	Query string `json:"query"`
}

type PythonSearchResponse struct {
	SemanticIntent string    `json:"semantic_intent"`
	FileType       *string   `json:"file_type"`
	Tags           []string  `json:"tags"`
	StartDate      *string   `json:"start_date"`
	EndDate        *string   `json:"end_date"`
	SearchVector   []float64 `json:"search_vector"`
}

type SearchResult struct {
	File            models.FileMetadata `json:"file"`
	SimilarityScore float64             `json:"similarity_score"`
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
	pythonReqBody, _ := json.Marshal(map[string]string{"query": req.Query})
	resp, err := http.Post("http://localhost:8000/api/search/parse_query", "application/json", bytes.NewBuffer(pythonReqBody))
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

	// 2. Fetch all files from DB with their Embeddings and Tags
	userID := GetUserID(c)
	var allFiles []models.FileMetadata
	// Filter by Date and User directly in the DB query
	database.DB.Where("user_id = ?", userID).Preload("Embeddings").Preload("Tags").Find(&allFiles)

	var searchResults []SearchResult

	// 3. Iterate, filter, and calculate cosine similarity
	for _, file := range allFiles {
		// Hard Filters: Date Constraints
		if pythonResp.StartDate != nil {
			startTime, err := time.Parse(time.RFC3339, *pythonResp.StartDate)
			if err == nil && file.CreatedAt < startTime.Unix() {
				continue
			}
		}
		if pythonResp.EndDate != nil {
			endTime, err := time.Parse(time.RFC3339, *pythonResp.EndDate)
			if err == nil && file.CreatedAt > endTime.Unix() {
				continue
			}
		}

		// Ensure file has vector embeddings to compare against
		if len(file.Embeddings) == 0 {
			continue // Legacy files or unsupported types
		}

		var fileVector []float64
		err := json.Unmarshal([]byte(file.Embeddings[0].EmbeddingVector), &fileVector)
		if err != nil || len(fileVector) == 0 {
			continue
		}

		// Math: Cosine Similarity
		score := cosineSimilarity(pythonResp.SearchVector, fileVector)

		// Hard Filter: Only include somewhat related files (>0.1 similarity)
		if score > 0.10 {
			searchResults = append(searchResults, SearchResult{
				File:            file,
				SimilarityScore: score * 100, // Make it a percentile
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

	return c.JSON(fiber.Map{
		"intent":  pythonResp.SemanticIntent,
		"filters": pythonResp,
		"results": searchResults[:limit],
	})
}
