package config

import (
	"os"
)

// AIServiceConfig holds configuration for the Python AI service
type AIServiceConfig struct {
	BaseURL string
	APIKey  string
}

// AIServiceURLFlag is populated from command-line flags in main.go
var AIServiceURLFlag string

// GetAIServiceConfig returns the AI service configuration
// It reads from environment variables, command-line flags, or uses defaults
func GetAIServiceConfig() AIServiceConfig {
	baseURL := os.Getenv("AI_SERVICE_BASE_URL")
	if baseURL == "" {
		if AIServiceURLFlag != "" {
			baseURL = AIServiceURLFlag
		} else {
			baseURL = "https://fx8gncn0-8000.asse.devtunnels.ms"
		}
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
