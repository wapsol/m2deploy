package retry

import (
	"fmt"
	"time"

	"github.com/wapsol/m2deploy/pkg/config"
)

// Options configures retry behavior
type Options struct {
	MaxRetries int
	Delay      time.Duration
	Logger     *config.Logger
}

// DefaultOptions returns sensible defaults for retry behavior
func DefaultOptions() *Options {
	return &Options{
		MaxRetries: 3,
		Delay:      2 * time.Second,
	}
}

// WithRetry executes the given operation with retries
func WithRetry(operation func() error, opts *Options) error {
	if opts == nil {
		opts = DefaultOptions()
	}

	var lastErr error
	for attempt := 1; attempt <= opts.MaxRetries; attempt++ {
		lastErr = operation()
		if lastErr == nil {
			return nil
		}

		if opts.Logger != nil {
			opts.Logger.Warning("Attempt %d/%d failed: %v", attempt, opts.MaxRetries, lastErr)
		}

		// Don't sleep after the last attempt
		if attempt < opts.MaxRetries {
			if opts.Logger != nil {
				opts.Logger.Debug("Retrying in %v...", opts.Delay)
			}
			time.Sleep(opts.Delay)
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", opts.MaxRetries, lastErr)
}

// WithRetryFunc is a convenience function that takes a logger
func WithRetryFunc(operation func() error, logger *config.Logger) error {
	return WithRetry(operation, &Options{
		MaxRetries: 3,
		Delay:      2 * time.Second,
		Logger:     logger,
	})
}
