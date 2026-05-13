package generator

import "time"

// ContentProvider defines how to generate files for each pull.
type ContentProvider interface {
	// InitialFiles returns the files for the initial commit.
	InitialFiles() map[string][]byte
	// GenerateFiles returns files to create/update on each pull.
	// Existing files not in this map are preserved.
	GenerateFiles(count int64, now time.Time) map[string][]byte
	// CommitMessage returns the commit message for a pull.
	CommitMessage(count int64, now time.Time) string
}
