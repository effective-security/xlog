package xlog

const (
	DefaultMaxLogMessageLength = 2 * 1024
)

// FormatterOption is an option for formatter behavior.
type FormatterOption func(*Config)

// FormatWithCaller allows to configure if the caller shall be logged
func FormatWithCaller(val bool) FormatterOption {
	return func(o *Config) {
		o.WithCaller = val
	}
}

// FormatSkipTime allows to configure skipping the time log
func FormatSkipTime(val bool) FormatterOption {
	return func(o *Config) {
		o.SkipTime = val
	}
}

// FormatSkipLevel allows to configure skipping the level log
func FormatSkipLevel(val bool) FormatterOption {
	return func(o *Config) {
		o.SkipLevel = val
	}
}

// FormatWithLocation allows to configure printing the file:line for each log
func FormatWithLocation(val bool) FormatterOption {
	return func(o *Config) {
		o.WithLocation = val
	}
}

// FormatWithColor allows to configure printing color logs
func FormatWithColor(val bool) FormatterOption {
	return func(o *Config) {
		o.WithColor = val
	}
}

// FormatPrintEmpty allows to configure printing empty values
func FormatPrintEmpty(val bool) FormatterOption {
	return func(o *Config) {
		o.PrintEmpty = val
	}
}

func FormatMaxLogLength(val int) FormatterOption {
	return func(o *Config) {
		o.MaxLogLength = val
	}
}

type Config struct {
	PrintEmpty   bool
	SkipLevel    bool
	SkipTime     bool
	WithCaller   bool
	WithColor    bool
	WithLocation bool
	MaxLogLength int
}

// Apply applies the options to the Config.
func (c *Config) Apply(opts ...FormatterOption) {
	for _, opt := range opts {
		opt(c)
	}

	if c.MaxLogLength == 0 {
		c.MaxLogLength = DefaultMaxLogMessageLength
	}
}
