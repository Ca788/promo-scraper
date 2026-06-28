package clock

import "time"

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now().UTC()
}

type FakeClock struct {
	T time.Time
}

func (f *FakeClock) Now() time.Time {
	return f.T
}
