package goibus

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"time"
)

// DefaultRetryInterval is how often the wait helpers poll when the caller does
// not pick an interval.
const DefaultRetryInterval = time.Second

// WaitForAddress blocks until a usable ibus daemon address is available and
// returns it.
//
// It is meant for components that ibus-daemon spawns, which can start before
// the daemon has written its address file, and for callers that were started
// with a stale address left over from a previous session. Unlike libibus it
// polls instead of relying on an inotify watch, so it also works over network
// and container filesystems.
func WaitForAddress(ctx context.Context, interval time.Duration) (string, error) {
	var address string
	_, err := retry(ctx, interval, func() (bool, error) {
		addr, err := GetAddressE()
		if err != nil {
			return false, err
		}
		address = addr
		return true, nil
	}, "no ibus daemon became available")
	if err != nil {
		return "", err
	}
	return address, nil
}

// DialWhenAvailable blocks until it can connect to the ibus daemon and returns
// the connected bus. It resolves the address again on every attempt, so a
// daemon that starts late, or restarts with a new socket, is picked up.
func DialWhenAvailable(ctx context.Context, interval time.Duration) (*Bus, error) {
	var bus *Bus
	_, err := retry(ctx, interval, func() (bool, error) {
		b, err := NewBusE()
		if err != nil {
			return false, err
		}
		bus = b
		return true, nil
	}, "could not connect to the ibus daemon")
	if err != nil {
		return nil, err
	}
	return bus, nil
}

// retry calls f until it reports success, the interval elapses between
// attempts, or ctx is done. The last failure is reported, annotated with what
// was being waited for. It is wrapped with %w so callers can still match on
// errors.Is and errors.As.
func retry(ctx context.Context, interval time.Duration, f func() (bool, error), what string) (bool, error) {
	if interval <= 0 {
		interval = DefaultRetryInterval
	}
	var lastErr error
	for {
		ok, err := f()
		if err != nil {
			lastErr = err
		}
		if ok {
			return true, nil
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			if lastErr == nil {
				return false, fmt.Errorf("%s: %v", what, ctx.Err())
			}
			return false, fmt.Errorf("%s: %w", what, lastErr)
		case <-timer.C:
		}
	}
}

// WatchAddress calls onChange whenever the file recording the ibus daemon
// address is created, removed or rewritten, until ctx is done.
//
// This mirrors the g_file_monitor_file() watch that libibus installs on the
// address file, which is how an IBus component learns that the daemon it was
// disconnected from has come back. The callback runs on the watcher's
// goroutine and must not block for long.
func WatchAddress(ctx context.Context, path string, interval time.Duration, onChange func()) {
	if interval <= 0 {
		interval = DefaultRetryInterval
	}
	previous, previousExists := addressFileStamp(path)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current, currentExists := addressFileStamp(path)
			if current == previous && currentExists == previousExists {
				continue
			}
			previous, previousExists = current, currentExists
			onChange()
		}
	}
}

// addressFileStamp summarises the file so changes can be detected cheaply.
func addressFileStamp(path string) (string, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	// The address is short, and ibus-daemon rewrites the whole file, so reading
	// it catches content changes even when the timestamp resolution is coarse.
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("%d:%d", fi.ModTime().UnixNano(), fi.Size()), true
	}
	return fmt.Sprintf("%d:%s", fi.ModTime().UnixNano(), data), true
}
