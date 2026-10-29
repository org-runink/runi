// SPDX-License-Identifier: BSD-3-Clause

package toml

import (
	"fmt"
	"strings"
	"time"
)

// LocalDate is a TOML local date, such as 1979-05-27: a day on the calendar
// with no time and no time zone.
type LocalDate struct {
	Year  int
	Month time.Month
	Day   int
}

// String writes the date as TOML does, YYYY-MM-DD.
func (d LocalDate) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

// MarshalText returns the date as String does, so encoding/json and other
// text encoders write 1979-05-27 rather than the struct's fields.
func (d LocalDate) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// In returns midnight at the start of the date in loc.
func (d LocalDate) In(loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, loc)
}

// LocalTime is a TOML local time, such as 07:32:00.999: a time of day with
// no date and no time zone. Precision finer than a nanosecond is truncated.
type LocalTime struct {
	Hour, Minute, Second, Nanosecond int
}

// String writes the time as HH:MM:SS, followed by the fraction of a second
// without trailing zeros when there is one.
func (t LocalTime) String() string {
	s := fmt.Sprintf("%02d:%02d:%02d", t.Hour, t.Minute, t.Second)
	if t.Nanosecond == 0 {
		return s
	}
	return s + "." + strings.TrimRight(fmt.Sprintf("%09d", t.Nanosecond), "0")
}

// MarshalText returns the time as String does.
func (t LocalTime) MarshalText() ([]byte, error) { return []byte(t.String()), nil }

// LocalDateTime is a TOML local date-time, such as 1979-05-27T07:32:00: a
// moment on a wall clock, in no particular time zone.
type LocalDateTime struct {
	Date LocalDate
	Time LocalTime
}

// String writes the date-time as TOML does, with a T between the parts.
func (dt LocalDateTime) String() string {
	return dt.Date.String() + "T" + dt.Time.String()
}

// MarshalText returns the date-time as String does.
func (dt LocalDateTime) MarshalText() ([]byte, error) { return []byte(dt.String()), nil }

// In returns the moment the wall-clock time names in loc.
func (dt LocalDateTime) In(loc *time.Location) time.Time {
	d, t := dt.Date, dt.Time
	return time.Date(d.Year, d.Month, d.Day, t.Hour, t.Minute, t.Second, t.Nanosecond, loc)
}
