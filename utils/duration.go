package utils

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// ParseDuration parses a duration string that supports h,m,s,d,w,y units
func ParseDuration(duration string) (time.Duration, error) {
	// Regular expression to match number followed by unit
	re := regexp.MustCompile(`^(\d+)([hmsdwy])$`)
	matches := re.FindStringSubmatch(duration)

	if matches == nil {
		return 0, fmt.Errorf("invalid duration format: %s. Expected format: number followed by h,m,s,d,w, or y", duration)
	}

	value, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, fmt.Errorf("invalid number in duration: %v", err)
	}

	unit := matches[2]

	switch unit {
	case "h":
		return time.Duration(value) * time.Hour, nil
	case "m":
		return time.Duration(value) * time.Minute, nil
	case "s":
		return time.Duration(value) * time.Second, nil
	case "d":
		return time.Duration(value) * 24 * time.Hour, nil
	case "w":
		return time.Duration(value) * 7 * 24 * time.Hour, nil
	case "y":
		return time.Duration(value) * 365 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported duration unit: %s", unit)
	}
}
