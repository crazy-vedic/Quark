package search

// options for the Searcher. Empty for now; fields added when first option is needed.
type options struct{}

// Option configures a Searcher. Use the With* functions in this package.
// This interface is sealed: only types in this package can implement it.
type Option interface{ apply(*options) }
