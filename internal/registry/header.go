package registry

import (
	"fmt"
	"net/http"
	"net/url"
)

const (
	RegistryHeader = "X-Spegel-Registry"
	MirrorHeader   = "X-Spegel-Mirror"
)

// getRemoteRegistry returns the target registry passed in the header.
func getRemoteRegistry(header http.Header) (string, error) {
	registry := header.Get(RegistryHeader)
	if registry == "" {
		return "", fmt.Errorf("registry header cannot be empty")
	}
	registryUrl, err := url.Parse(registry)
	if err != nil {
		return "", fmt.Errorf("could not parse registry value: %w", err)
	}
	return registryUrl.Host, nil
}

// isMirrorRequest returns true if mirror header is present.
func isMirrorRequest(header http.Header) bool {
	mirror := header.Get(MirrorHeader)
	if mirror == "true" {
		return true
	}
	return false
}