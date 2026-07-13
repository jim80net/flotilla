package surface

// UsageReport is one authoritative provider-usage observation. Percent is the
// remaining allowance (0 exhausted, 100 untouched); Window identifies the
// provider-defined quota window.
type UsageReport struct {
	RemainingPercent int
	Window           string
	Scope            RateLimitScope
}

// UsageProbe is an OPTIONAL Driver capability: read an authoritative usage
// observation from the surface's live chrome. Absence or an unreadable value is
// reported as ok=false; callers must never synthesize coverage.
type UsageProbe interface {
	Usage(pane string) (report UsageReport, ok bool)
}

// UsageSupport type-asserts the optional UsageProbe capability.
func UsageSupport(d Driver) (UsageProbe, bool) {
	p, ok := d.(UsageProbe)
	return p, ok
}
