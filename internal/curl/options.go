package curl

// options for the Importer. Empty for now; fields added when first option is needed.
type options struct{}

// Option configures an Importer. Use the With* functions in this package.
// This interface is sealed: only types in this package can implement it.
type Option interface{ apply(*options) }

func applyOpts(opts []Option) options {
	o := options{}
	for _, opt := range opts {
		opt.apply(&o)
	}
	return o
}
