// Package jalali converts between the Gregorian and the Persian (Solar Hijri)
// calendars, with the astronomical leap-year table jalaali-js uses (years 1 to 3177).
package jalali

import "time"

var breaks = [...]int{-61, 9, 38, 199, 426, 686, 756, 818, 1111, 1181, 1210, 1635, 2060, 2097, 2192, 2262, 2324, 2394, 2456, 3178}

// cal returns, for a Persian year, how far it is from a leap year (0 is leap),
// the Gregorian year it starts in, and the March day its first day falls on.
func cal(jy int) (leap, gy, march int) {
	gy = jy + 621
	leapJ, jp := -14, breaks[0]
	jump := 0
	for i := 1; i < len(breaks); i++ {
		jm := breaks[i]
		jump = jm - jp
		if jy < jm {
			break
		}
		leapJ += jump/33*8 + jump%33/4
		jp = jm
	}
	n := jy - jp
	leapJ += n/33*8 + (n%33+3)/4
	if jump%33 == 4 && jump-n == 4 {
		leapJ++
	}
	leapG := gy/4 - (gy/100+1)*3/4 - 150
	march = 20 + leapJ - leapG
	if jump-n < 6 {
		n = n - jump + (jump+4)/33*33
	}
	leap = ((n+1)%33 - 1) % 4
	if leap == -1 {
		leap = 4
	}
	return leap, gy, march
}

func g2d(gy, gm, gd int) int {
	d := (gy+(gm-8)/6+100100)*1461/4 + (153*((gm+9)%12)+2)/5 + gd - 34840408
	return d - (gy+100100+(gm-8)/6)/100*3/4 + 752
}

func d2g(jdn int) (gy, gm, gd int) {
	j := 4*jdn + 139361631
	j += (4*jdn+183187720)/146097*3/4*4 - 3908
	i := j%1461/4*5 + 308
	gd = i%153/5 + 1
	gm = i/153%12 + 1
	gy = j/1461 - 100100 + (8-gm)/6
	return gy, gm, gd
}

// ToJalali converts a Gregorian date.
func ToJalali(gy, gm, gd int) (jy, jm, jd int) {
	jdn := g2d(gy, gm, gd)
	gy, _, _ = d2g(jdn)
	jy = gy - 621
	leap, _, march := cal(jy)
	k := jdn - g2d(gy, 3, march)
	if k >= 0 {
		if k <= 185 {
			return jy, 1 + k/31, k%31 + 1
		}
		k -= 186
	} else {
		jy--
		k += 179
		if leap == 1 {
			k++
		}
	}
	return jy, 7 + k/30, k%30 + 1
}

// ToGregorian converts a Persian date.
func ToGregorian(jy, jm, jd int) (gy, gm, gd int) {
	_, gyStart, march := cal(jy)
	jdn := g2d(gyStart, 3, march) + (jm-1)*31 - jm/7*(jm-7) + jd - 1
	return d2g(jdn)
}

// IsLeap reports whether the Persian year has 366 days.
func IsLeap(jy int) bool {
	leap, _, _ := cal(jy)
	return leap == 0
}

// MonthLength is the number of days in a Persian month.
func MonthLength(jy, jm int) int {
	switch {
	case jm <= 6:
		return 31
	case jm <= 11:
		return 30
	case IsLeap(jy):
		return 30
	default:
		return 29
	}
}

// Date is the midnight in loc that starts a Persian date.
func Date(jy, jm, jd int, loc *time.Location) time.Time {
	gy, gm, gd := ToGregorian(jy, jm, jd)
	return time.Date(gy, time.Month(gm), gd, 0, 0, 0, 0, loc)
}

// Of returns the Persian date of t in its own location.
func Of(t time.Time) (jy, jm, jd int) {
	return ToJalali(t.Year(), int(t.Month()), t.Day())
}
