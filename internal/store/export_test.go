package store

// ParsedOptions applies opts to the default options and returns the resolved result.
// Only available to tests in package store_test. Never exported to production code.
func ParsedOptions(opts ...Option) options {
	o := defaultOptions()
	for _, opt := range opts {
		opt.apply(&o)
	}
	return o
}
