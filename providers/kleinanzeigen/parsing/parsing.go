package parsing

import (
	"github.com/Kamuyin/kleinanzeigenFinder/providers/parsing"
	"regexp"
)

var idRE = regexp.MustCompile(`/([0-9]+)-[0-9]+-[0-9]+(?:[/?#]|$)`)

func ExtractIDFromURL(url string) string {
	if url == "" {
		return ""
	}
	match := idRE.FindStringSubmatch(url)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func ParsePriceAmount(text string) *float64 { return parsing.ParsePriceAmount(text) }
func ParseRooms(text string) *float64       { return parsing.ParseRooms(text) }
func ParseSizeM2(text string) *float64      { return parsing.ParseSizeM2(text) }
