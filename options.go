package xlog

const (
	// DefaultMaxLogMessageLength is the byte limit installed by Config.Apply
	// when MaxLogLength is zero. Constructors do not call Apply automatically.
	DefaultMaxLogMessageLength = 2 * 1024
)

// FormatterOption is an option for formatter behavior.
type FormatterOption func(*Config)

// WithSink delivers rendered records through s instead of writing them from the
// logging call, moving destination latency off producers. The formatter still
// renders the complete record, so producer metadata and caller-owned values are
// captured before the logging call returns and no value is encoded twice.
//
// Apply it at construction. Applying it through Options rebinds the
// destination, which must not race with logging; flush the formatter first so
// records still queued in a previous sink cannot interleave with the new
// binding at one destination. The application closes the sink after removing
// the formatter and stopping producers.

func WithSink(s *Sink) FormatterOption {
	return func(o *Config) {
		o.sink = s
	}
}

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

// FormatMaxLogLength limits rendered text KV values or a JSON plain message in
// bytes. Zero selects the default on Apply; negative values disable these limits.
// It does not limit whole records, JSON KV values, or Stackdriver output.
func FormatMaxLogLength(val int) FormatterOption {
	return func(o *Config) {
		o.MaxLogLength = val
	}
}

// Config holds formatter options. Support differs by formatter; see
// Documentation/codemap.md for the behavior matrix.
type Config struct {
	// PrintEmpty includes empty values in text output; JSON always includes them.
	PrintEmpty bool
	// SkipLevel omits the level in text and JSON output.
	SkipLevel bool
	// SkipTime omits the timestamp.
	SkipTime bool
	// WithCaller requests the caller's function name.
	WithCaller bool
	// WithColor enables ANSI colors in PrettyFormatter.
	WithColor bool
	// WithLocation requests the caller's file and line.
	WithLocation bool
	// MaxLogLength limits selected values/messages, not total record size.
	MaxLogLength int
	// sink delivers rendered records asynchronously when set by WithSink.
	sink *Sink
}

// Sink returns the sink configured by WithSink, or nil when records are written
// inline. Formatters outside this package use it to bind their Output.
func (c *Config) Sink() *Sink {
	return c.sink
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
