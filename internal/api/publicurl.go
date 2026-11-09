package api

import "github.com/cachebag/relay/internal/store"

type Option func(*handlerConfig)

type handlerConfig struct {
	publicURL func() string
	// bundles is where job files are kept. Nil means this scheduler
	// doesn't take them, and uploads get 503.
	bundles *store.Bundles
}

// WithBundles turns on job files, stored in b.
func WithBundles(b *store.Bundles) Option {
	return func(c *handlerConfig) {
		c.bundles = b
	}
}

func WithPublicURL(get func() string) Option {
	return func(c *handlerConfig) {
		c.publicURL = get
	}
}
func newHandlerConfig(opts []Option) handlerConfig {
	c := handlerConfig{}
	for _, opt := range opts {
		opt(&c)
	}
	if c.publicURL == nil {
		c.publicURL = func() string { return "" }
	}
	return c
}
