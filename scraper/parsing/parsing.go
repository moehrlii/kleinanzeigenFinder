// Package parsing forwards the original parsing API to the Kleinanzeigen provider.
package parsing

import "github.com/Kamuyin/kleinanzeigenFinder/providers/kleinanzeigen/parsing"

func ExtractIDFromURL(raw string) string    { return parsing.ExtractIDFromURL(raw) }
func ParsePriceAmount(text string) *float64 { return parsing.ParsePriceAmount(text) }
func ParseRooms(text string) *float64       { return parsing.ParseRooms(text) }
func ParseSizeM2(text string) *float64      { return parsing.ParseSizeM2(text) }
