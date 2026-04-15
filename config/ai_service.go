package config

import (
	"os"
)

// AIServiceConfig holds configuration for the Python AI service
type AIServiceConfig struct {
	BaseURL string
	APIKey  string
}

// GetAIServiceConfig returns the AI service configuration
// It reads from environment variables or uses defaults
func GetAIServiceConfig() AIServiceConfig {
	baseURL := os.Getenv("AI_SERVICE_BASE_URL")
	if baseURL == "" {
		baseURL = "http://192.168.56.1:8000"
	}

	apiKey := os.Getenv("AI_SERVICE_API_KEY")
	if apiKey == "" {
		apiKey = "your-super-secret-api-key-12345" // Default, should be overridden
	}

	return AIServiceConfig{
		BaseURL: baseURL,
		APIKey:  apiKey,
	}
}

// Endpoint returns the full endpoint URL for a given path
func (a *AIServiceConfig) Endpoint(path string) string {
	return a.BaseURL + path
}
