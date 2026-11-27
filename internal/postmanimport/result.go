package postmanimport

import "github.com/crazy-vedic/quark/internal/postman"

// Outcome says what an import did with one Postman collection file.
type Outcome string

const (
	// OutcomeCreated means a new root collection was created with no name conflict.
	OutcomeCreated Outcome = "created"
	// OutcomeDuplicated means a new root collection was created under a
	// suffixed name because the original name was taken.
	OutcomeDuplicated Outcome = "duplicated"
	// OutcomeReplaced means the existing root collection was deleted and recreated.
	OutcomeReplaced Outcome = "replaced"
	// OutcomeMerged means new requests were added to the existing root collection.
	OutcomeMerged Outcome = "merged"
	// OutcomeSkipped means nothing was written because the name was taken.
	OutcomeSkipped Outcome = "skipped"
)

// CollectionResult describes the import of one Postman collection file.
type CollectionResult struct {
	// FilePath is the collection file that was read.
	FilePath string
	// Name is the root collection name that was written (or skipped).
	Name string
	// ID is the root collection that received the requests. It is empty when
	// the collection was skipped or the import failed.
	ID string
	// Outcome is what happened to the collection. It is empty when Err is set.
	Outcome Outcome
	// Imported is the number of requests written.
	Imported int
	// Total is the number of requests in the Postman file.
	Total int
	// Warnings lists redacted, sorted diagnostics for this collection.
	Warnings []string
	// Security is the credential classification of the Postman file.
	Security postman.SecurityLevel
	// Err is set when this collection could not be imported. In a bulk import
	// other collections are still imported.
	Err error
}

// EnvironmentResult describes the Postman environment files of a bulk export.
type EnvironmentResult struct {
	// Imported counts environment files written (globals count once each,
	// named environments once per root collection they were copied to).
	Imported int
	// Warnings lists redacted, sorted diagnostics.
	Warnings []string
	// Errors lists environment files or writes that failed, sorted.
	Errors []string
}

// Result is the structured outcome of one Import call.
type Result struct {
	// Bulk is true when the path was a Postman bulk export directory.
	Bulk bool
	// Collections has one entry per collection file, in import order.
	Collections []CollectionResult
	// Environments is only populated by bulk imports.
	Environments EnvironmentResult
}

// Summary totals a Result.
type Summary struct {
	Created    int
	Duplicated int
	Replaced   int
	Merged     int
	Skipped    int
	// Failed counts collection files that could not be imported.
	Failed int
	// RequestsImported is the number of requests written.
	RequestsImported int
	// RequestsSkipped is the number of requests in the source files that were
	// not written (skipped collections, or requests already present on merge).
	RequestsSkipped int
	// Warnings counts collection and environment warnings.
	Warnings int
	// Errors counts failed collection files and environment errors.
	Errors int
	// Environments is the number of environments imported.
	Environments int
}

// Summary returns totals across every collection and environment in r.
func (r Result) Summary() Summary {
	var s Summary
	for _, c := range r.Collections {
		if c.Err != nil {
			s.Failed++
			s.Errors++
			continue
		}
		switch c.Outcome {
		case OutcomeCreated:
			s.Created++
		case OutcomeDuplicated:
			s.Duplicated++
		case OutcomeReplaced:
			s.Replaced++
		case OutcomeMerged:
			s.Merged++
		case OutcomeSkipped:
			s.Skipped++
		}
		s.RequestsImported += c.Imported
		s.RequestsSkipped += c.Total - c.Imported
		s.Warnings += len(c.Warnings)
	}
	s.Warnings += len(r.Environments.Warnings)
	s.Errors += len(r.Environments.Errors)
	s.Environments = r.Environments.Imported
	return s
}
