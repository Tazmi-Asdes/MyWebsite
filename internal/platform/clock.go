package platform

import "time"

// Clock supplies the current time to code that needs to be testable.
type Clock interface {
	Now() time.Time
}

// RealClock reads the system clock and presents it in its configured location.
type RealClock struct {
	location *time.Location
}

// NewRealClock creates a real clock for location. A nil location uses UTC.
func NewRealClock(location *time.Location) RealClock {
	if location == nil {
		location = time.UTC
	}
	return RealClock{location: location}
}

// NewShanghaiClock creates the clock used by the public site.
func NewShanghaiClock() RealClock {
	return NewRealClock(ShanghaiLocation())
}

// Now returns the current time in the clock's configured location.
func (c RealClock) Now() time.Time {
	return time.Now().In(c.location)
}

// ShanghaiLocation returns the IANA Asia/Shanghai location. The fixed offset
// fallback keeps the application usable in minimal environments without tzdata.
func ShanghaiLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err == nil {
		return location
	}
	return time.FixedZone("Asia/Shanghai", 8*60*60)
}

// CurrentYear returns the current calendar year in Asia/Shanghai, regardless
// of the location attached to the time returned by clock.
func CurrentYear(clock Clock) int {
	if clock == nil {
		clock = NewShanghaiClock()
	}
	return clock.Now().In(ShanghaiLocation()).Year()
}
