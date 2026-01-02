package config

type Config struct {
	Env string

	// Server
	Port       string
	ContentDir string

	// Database
	DatabaseDSN string

	// Auth
	GoogleClientID     string
	GoogleClientSecret string
	BaseURL            string

	// External Services
	NavalPlanAgentURL string
}
