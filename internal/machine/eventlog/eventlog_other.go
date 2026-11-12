//go:build !windows

package eventlog

import "time"

// EndingsBetween fails off Windows: there are no event logs.
func EndingsBetween(time.Time, time.Time) ([]Ending, error) { return nil, ErrNotWindows }
