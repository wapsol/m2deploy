package retry

import (
	"errors"
	"testing"
	"time"
)

func TestWithRetry_Success(t *testing.T) {
	attempts := 0
	operation := func() error {
		attempts++
		if attempts < 2 {
			return errors.New("temporary error")
		}
		return nil
	}

	opts := &Options{
		MaxRetries: 3,
		Delay:      10 * time.Millisecond,
	}

	err := WithRetry(operation, opts)
	if err != nil {
		t.Errorf("WithRetry() unexpected error: %v", err)
	}
	if attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", attempts)
	}
}

func TestWithRetry_Failure(t *testing.T) {
	attempts := 0
	operation := func() error {
		attempts++
		return errors.New("persistent error")
	}

	opts := &Options{
		MaxRetries: 3,
		Delay:      10 * time.Millisecond,
	}

	err := WithRetry(operation, opts)
	if err == nil {
		t.Error("WithRetry() expected error, got nil")
	}
	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

func TestWithRetry_ImmediateSuccess(t *testing.T) {
	attempts := 0
	operation := func() error {
		attempts++
		return nil
	}

	opts := &Options{
		MaxRetries: 3,
		Delay:      10 * time.Millisecond,
	}

	err := WithRetry(operation, opts)
	if err != nil {
		t.Errorf("WithRetry() unexpected error: %v", err)
	}
	if attempts != 1 {
		t.Errorf("Expected 1 attempt, got %d", attempts)
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()
	if opts.MaxRetries != 3 {
		t.Errorf("DefaultOptions().MaxRetries = %d, want 3", opts.MaxRetries)
	}
	if opts.Delay != 2*time.Second {
		t.Errorf("DefaultOptions().Delay = %v, want 2s", opts.Delay)
	}
}
