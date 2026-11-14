package parsing

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	euroRE  = regexp.MustCompile(`(?i)(\d{1,3}(?:[.\s]\d{3})*|\d+)(?:,(\d{1,2}))?\s*(?:EUR|€)`)
	roomsRE = regexp.MustCompile(`(?i)(\d+(?:[,.]\d+)?)\s*Zi\.?`)
	sizeRE  = regexp.MustCompile(`(?i)(\d+(?:[,.]\d+)?)\s*m[²2]`)
	freeRE  = regexp.MustCompile(`(?i)zu verschenken`)
)

func ParsePriceAmount(text string) *float64 {
	if text == "" {
		return nil
	}
	match := euroRE.FindStringSubmatch(text)
	if len(match) == 0 {
		if freeRE.MatchString(text) {
			return new(0.0)
		}
		return nil
	}

	whole := strings.ReplaceAll(match[1], ".", "")
	whole = strings.ReplaceAll(whole, " ", "")

	cents := match[2]
	if cents == "" {
		cents = "00"
	} else if len(cents) == 1 {
		cents = cents + "0"
	} else if len(cents) > 2 {
		cents = cents[:2]
	}

	val, err := strconv.ParseFloat(whole+"."+cents, 64)
	if err != nil {
		return nil
	}
	return &val
}

func ParseRooms(text string) *float64 {
	if text == "" {
		return nil
	}
	match := roomsRE.FindStringSubmatch(text)
	if len(match) > 1 {
		return new(parseGermanDecimal(match[1]))
	}
	return nil
}

func ParseSizeM2(text string) *float64 {
	if text == "" {
		return nil
	}
	match := sizeRE.FindStringSubmatch(text)
	if len(match) > 1 {
		return new(parseGermanDecimal(match[1]))
	}
	return nil
}

func parseGermanDecimal(val string) float64 {
	clean := strings.ReplaceAll(val, ",", ".")
	parsed, _ := strconv.ParseFloat(clean, 64)
	return parsed
}
