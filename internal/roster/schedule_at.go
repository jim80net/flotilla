package roster

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseDailyAt parses a daily wall-clock time with an explicit timezone, e.g.
// "12:07Z", "03:07+00:00", or "09:30-05:00". The timezone suffix is required —
// a bare "12:07" is rejected so fleet-local ambiguity cannot slip through.
func ParseDailyAt(s string) (hour, minute int, loc *time.Location, err error) {
	s = strings.TrimSpace(s)
	if len(s) < 6 {
		return 0, 0, nil, fmt.Errorf("invalid daily at %q: want HH:MMZ or HH:MM±HH:MM", s)
	}
	if s[2] != ':' {
		return 0, 0, nil, fmt.Errorf("invalid daily at %q: want HH:MM…", s)
	}
	h, err := strconv.Atoi(s[0:2])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, nil, fmt.Errorf("invalid daily at %q: hour out of range", s)
	}
	m, err := strconv.Atoi(s[3:5])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, nil, fmt.Errorf("invalid daily at %q: minute out of range", s)
	}
	tz := s[5:]
	switch {
	case tz == "Z":
		return h, m, time.UTC, nil
	case len(tz) == 6 && (tz[0] == '+' || tz[0] == '-') && tz[3] == ':':
		sign := 1
		if tz[0] == '-' {
			sign = -1
		}
		th, err := strconv.Atoi(tz[1:3])
		if err != nil || th < 0 || th > 23 {
			return 0, 0, nil, fmt.Errorf("invalid daily at %q: offset hour out of range", s)
		}
		tm, err := strconv.Atoi(tz[4:6])
		if err != nil || tm < 0 || tm > 59 {
			return 0, 0, nil, fmt.Errorf("invalid daily at %q: offset minute out of range", s)
		}
		offset := sign * ((th * 60) + tm) * 60
		name := fmt.Sprintf("UTC%s", tz)
		return h, m, time.FixedZone(name, offset), nil
	default:
		return 0, 0, nil, fmt.Errorf("invalid daily at %q: timezone must be Z or ±HH:MM", s)
	}
}
