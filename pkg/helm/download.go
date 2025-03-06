package helm

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"
)

type ChartIndex struct {
	Entries map[string][]ChartEntry `yaml:"entries"`
}

type ChartEntry struct {
	Name    string   `yaml:"name"`
	Version string   `yaml:"version"`
	URLs    []string `yaml:"urls"`
}

// DownloadChart downloads a Helm chart from a repository
func DownloadChart(repoURL, chartName, chartVersion string) (io.ReadCloser, error) {
	// Get the chart URL from the index
	chartURL, err := getChartURL(repoURL, chartName, chartVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to get chart URL: %w", err)
	}

	// Download the chart
	resp, err := http.Get(chartURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download chart: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("failed to download chart, status: %d", resp.StatusCode)
	}

	return resp.Body, nil
}

// getChartURL retrieves the download URL for a specific chart version
func getChartURL(repoURL, chartName, chartVersion string) (string, error) {
	// Download and parse the index.yaml file
	indexURL := fmt.Sprintf("%s/index.yaml", repoURL)

	resp, err := http.Get(indexURL)
	if err != nil {
		return "", fmt.Errorf("failed to download index.yaml: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download index.yaml, status: %d", resp.StatusCode)
	}

	indexData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read index.yaml: %w", err)
	}

	var index ChartIndex
	if err := yaml.Unmarshal(indexData, &index); err != nil {
		return "", fmt.Errorf("failed to parse index.yaml: %w", err)
	}

	// Find the chart URL
	entries, ok := index.Entries[chartName]
	if !ok {
		return "", fmt.Errorf("chart %s not found in index", chartName)
	}

	for _, entry := range entries {
		if entry.Version == chartVersion {
			if len(entry.URLs) == 0 {
				return "", fmt.Errorf("no URLs found for chart %s version %s", chartName, chartVersion)
			}
			chartURL := entry.URLs[0]

			// If the URL is relative, prepend the repo URL
			if !strings.HasPrefix(chartURL, "http") {
				chartURL = fmt.Sprintf("%s/%s", repoURL, chartURL)
			}
			return chartURL, nil
		}
	}

	return "", fmt.Errorf("version %s not found for chart %s", chartVersion, chartName)
}
