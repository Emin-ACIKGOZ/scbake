// Package manifest provides functions for reading and writing the scbake manifest file (scbake.toml).
package manifest

import (
	"os"
	"scbake/internal/types"

	"github.com/BurntSushi/toml"
)

// ManifestFileName is the name of the project manifest file.
const ManifestFileName = "scbake.toml"

// Load reads scbake.toml from disk or returns a new, empty manifest.
func Load() (*types.Manifest, error) {
	var m types.Manifest

	// Try to read the file
	data, err := os.ReadFile(ManifestFileName)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, return a new one
			m.SbakeVersion = "v0.0.1" // We can get this from the 'version' var later
			m.Projects = []types.Project{}
			m.Templates = []types.Template{}
			return &m, nil
		}
		// Some other error
		return nil, err
	}

	// File exists, unmarshal it
	if _, err := toml.Decode(string(data), &m); err != nil {
		return nil, err
	}

	return &m, nil
}

// Save atomically writes the manifest to scbake.toml
func Save(m *types.Manifest) error {
	f, err := os.Create(ManifestFileName)
	if err != nil {
		return err
	}

	// Capture the potential error from f.Close() and return it if writing was successful.
	// This is necessary because f.Close() can return errors related to disk sync or I/O.
	defer func() {
		if closeErr := f.Close(); err == nil { // Only capture close error if no prior error occurred
			err = closeErr
		}
	}()

	// Use a TOML encoder to write to the file
	encoder := toml.NewEncoder(f)
	if encodeErr := encoder.Encode(m); encodeErr != nil {
		return encodeErr // Return encoding error immediately
	}

	// The deferred function handles the close error.
	return err
}
