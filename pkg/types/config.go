package types

// Config represents the application configuration
type Config struct {
	Port    string        `yaml:"port"`
	Repos   []RepoConfig  `yaml:"repos"`
	Storage StorageConfig `yaml:"storage"`
}

// RepoConfig represents a Helm repository configuration
type RepoConfig struct {
	URL    string `yaml:"url"`
	Prefix string `yaml:"prefix"`
}

// StorageConfig represents storage configuration
type StorageConfig struct {
	Type     string `yaml:"type"`     // "s3" or "gcs"
	Endpoint string `yaml:"endpoint"` // Custom endpoint URL
	Bucket   string `yaml:"bucket"`   // Bucket name
	Region   string `yaml:"region"`   // Region (for S3)
}
