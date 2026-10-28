package api

type Option func(*handlerConfig)

type handlerConfig struct {
	publicURL func() string
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
