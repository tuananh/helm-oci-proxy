package helm

import (
	"io"
	"strings"
	"testing"
)

func TestDownloadChart(t *testing.T) {
	tests := []struct {
		name        string
		repoURL     string
		chartName   string
		chartVer    string
		wantErr     bool
		errContains string
	}{
		{
			name:      "Valid chart download",
			repoURL:   "https://charts.jetstack.io",
			chartName: "cert-manager",
			chartVer:  "v1.12.0",
			wantErr:   false,
		},
		{
			name:        "Invalid chart name",
			repoURL:     "https://charts.jetstack.io",
			chartName:   "non-existent-chart",
			chartVer:    "v1.0.0",
			wantErr:     true,
			errContains: "not found in index",
		},
		// Add more test cases as needed
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, err := DownloadChart(tt.repoURL, tt.chartName, tt.chartVer)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			defer reader.Close()

			// Read a few bytes to verify we got something
			buf := make([]byte, 100)
			n, err := reader.Read(buf)
			if err != nil && err != io.EOF {
				t.Errorf("failed to read from chart: %v", err)
			}
			if n == 0 {
				t.Error("got empty chart data")
			}
		})
	}
}
