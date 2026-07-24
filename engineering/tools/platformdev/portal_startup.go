package main

import (
	"errors"
	"fmt"
	"os"
)

// appendPublishedAPIExposureCatalog keeps the minimal Portal host independent
// from an API Exposure business publication. Once a catalog has been published,
// passing its exact path preserves the host's strict validation and fail-closed
// behavior for corrupt, unsafe, or subsequently removed files.
func appendPublishedAPIExposureCatalog(arguments []string, filename string) ([]string, error) {
	if _, err := os.Stat(filename); errors.Is(err, os.ErrNotExist) {
		return arguments, nil
	} else if err != nil {
		return nil, fmt.Errorf("检查已发布 API Exposure Catalog: %w", err)
	}
	return append(arguments, "--api-exposure-catalog", filename), nil
}
