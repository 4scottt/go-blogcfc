package i18n

import (
	"fmt"
	"strconv"
	"time"
)

// Dates and names, localized (PLAN §9 R06).
//
// BlogCFC had no tables of its own: org/hastings/locale/utils.cfc built a
// java.util.Locale and asked the JVM - DateFormatSymbols for the month
// and day names, GregorianCalendar.getFirstDayOfWeek() for the calendar
// pod's week start, DateFormat.getDateInstance(LONG|SHORT) and
// getTimeInstance(SHORT) for the formats. Go has no such data and we add
// no dependency for it, so the four locales BlogCFC ships with are
// tabulated here, reproducing what that JVM answered:
//
//   - week start: Sunday for en_US, Monday for de_DE, de_AT and de_CH,
//     which is Calendar's SUNDAY=1 / MONDAY=2 by locale;
//   - names: the JRE's own (pre-CLDR) German, the data BlogCFC 5.9.8 ran
//     against - "Mrz" and not "Mär", no trailing dots on the short days -
//     with de_AT's one override, "Jänner" for January;
//   - long date: "September 20, 2026" for en_US, "20. September 2026"
//     for the German three; short date: "9/20/26" and "20.09.26"; short
//     time: "12:03 AM" and "00:03".
//
// An unknown locale is answered with en_US, as New is.

// DateStyle picks a format. The names are the call sites, not Java's:
// StylePosted is the entry footer, StyleMonthYear the monthly archives
// pod and the calendar pod's title.
type DateStyle string

const (
	// StyleLong is Java's DateFormat.LONG date: "September 20, 2026".
	StyleLong DateStyle = "long"
	// StyleShort is Java's SHORT date: "9/20/26".
	StyleShort DateStyle = "short"
	// StyleTime is Java's SHORT time: "12:03 AM".
	StyleTime DateStyle = "time"
	// StylePosted is the entry footer's "September 20, 2026 at 12:03 AM".
	// BlogCFC wrote that line with CF's dateFormat/timeFormat masks and
	// a literal "at" in index.cfm, so the as-is footer is English in
	// every locale; here it follows the locale, and the German three say
	// "um" where en_US says "at".
	StylePosted DateStyle = "posted"
	// StyleMonthYear is "September 2026": the monthly archives pod and
	// the calendar pod's month title.
	StyleMonthYear DateStyle = "monthyear"
)

// dateTable is one locale's names and formats.
type dateTable struct {
	months      [12]string
	shortMonths [12]string
	days        [7]string // indexed by time.Weekday, Sunday first
	shortDays   [7]string
	firstDay    time.Weekday
	at          string // the word between date and time in StylePosted
	euro        bool   // day-first dates and a 24-hour clock
}

var (
	enUS = dateTable{
		months:      [12]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
		shortMonths: [12]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
		days:        [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
		shortDays:   [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
		firstDay:    time.Sunday,
		at:          "at",
	}
	deDE = dateTable{
		months:      [12]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"},
		shortMonths: [12]string{"Jan", "Feb", "Mrz", "Apr", "Mai", "Jun", "Jul", "Aug", "Sep", "Okt", "Nov", "Dez"},
		days:        [7]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"},
		shortDays:   [7]string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"},
		firstDay:    time.Monday,
		at:          "um",
		euro:        true,
	}
	// de_AT differs from de_DE in one name, as the JRE's own data did.
	deAT = func() dateTable {
		t := deDE
		t.months[0] = "Jänner"
		t.shortMonths[0] = "Jän"
		return t
	}()
	// de_CH took its names from de_DE; only numbers and currency differ,
	// and neither is a date.
	deCH = deDE

	dateTables = map[string]*dateTable{
		"en_US": &enUS,
		"de_DE": &deDE,
		"de_AT": &deAT,
		"de_CH": &deCH,
	}
)

// table is the locale's table, or en_US's.
func table(locale string) *dateTable {
	if t, ok := dateTables[Normalize(locale)]; ok {
		return t
	}
	return &enUS
}

// MonthName is the full month name, "September" or "Jänner".
func MonthName(locale string, m time.Month) string {
	if m < time.January || m > time.December {
		return ""
	}
	return table(locale).months[m-1]
}

// ShortMonth is the abbreviated month name, "Sep" or "Mrz".
func ShortMonth(locale string, m time.Month) string {
	if m < time.January || m > time.December {
		return ""
	}
	return table(locale).shortMonths[m-1]
}

// DayName is the full day name, "Sunday" or "Sonntag".
func DayName(locale string, d time.Weekday) string {
	if d < time.Sunday || d > time.Saturday {
		return ""
	}
	return table(locale).days[d]
}

// ShortDay is the abbreviated day name the calendar pod's header row
// uses, "Sun" or "So".
func ShortDay(locale string, d time.Weekday) string {
	if d < time.Sunday || d > time.Saturday {
		return ""
	}
	return table(locale).shortDays[d]
}

// FirstDayOfWeek is the day the calendar pod's week starts on: Sunday
// for en_US, Monday for the German three.
func FirstDayOfWeek(locale string) time.Weekday { return table(locale).firstDay }

// FormatDate renders a time in one of the styles above. The caller has
// already put the time in the blog's zone (PLAN §11, "Timezone").
func FormatDate(locale string, t time.Time, style DateStyle) string {
	tab := table(locale)
	switch style {
	case StyleShort:
		return tab.shortDate(t)
	case StyleTime:
		return tab.clock(t)
	case StylePosted:
		return tab.longDate(t) + " " + tab.at + " " + tab.clock(t)
	case StyleMonthYear:
		return tab.months[t.Month()-1] + " " + strconv.Itoa(t.Year())
	default:
		return tab.longDate(t)
	}
}

func (tab *dateTable) longDate(t time.Time) string {
	if tab.euro {
		return fmt.Sprintf("%d. %s %d", t.Day(), tab.months[t.Month()-1], t.Year())
	}
	return fmt.Sprintf("%s %d, %d", tab.months[t.Month()-1], t.Day(), t.Year())
}

func (tab *dateTable) shortDate(t time.Time) string {
	if tab.euro {
		return t.Format("02.01.06")
	}
	return t.Format("1/2/06")
}

func (tab *dateTable) clock(t time.Time) string {
	if tab.euro {
		return t.Format("15:04")
	}
	return t.Format("3:04 PM")
}
