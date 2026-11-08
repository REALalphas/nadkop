package core

import (
	"encoding/json"
	"strings"
	"unicode"
)

// ServerLocation contains display metadata supplied by the subscription. It is
// persisted for the picker but excluded from Xray and connection fingerprints.
// No address lookup or guessed GeoIP location is performed.
type ServerLocation struct {
	CountryCode string `json:"countryCode,omitempty"`
	Country     string `json:"country,omitempty"`
	City        string `json:"city,omitempty"`
	Region      string `json:"region,omitempty"`
	Label       string `json:"label,omitempty"`
}

var locationKeys = []string{"location", "countryCode", "country_code", "country", "countryName", "country_name", "city", "region"}

func locationString(fields map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		var value string
		if json.Unmarshal(fields[key], &value) == nil {
			value = strings.TrimSpace(strings.Map(func(r rune) rune {
				if unicode.IsControl(r) {
					return -1
				}
				return r
			}, value))
			runes := []rune(value)
			if len(runes) > 128 {
				value = string(runes[:128])
			}
			if value != "" {
				return value
			}
		}
	}
	return ""
}

func countryCode(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 2 && value[0] < 128 && value[1] < 128 {
		value = strings.ToUpper(value)
		if value[0] >= 'A' && value[0] <= 'Z' && value[1] >= 'A' && value[1] <= 'Z' {
			if value == "UK" {
				return "GB"
			}
			return value
		}
	}
	return flagCountryCode(value)
}

func flagCountryCode(value string) string {
	var first rune
	for _, r := range value {
		if r >= 0x1f1e6 && r <= 0x1f1ff {
			if first != 0 {
				return string([]rune{'A' + first - 0x1f1e6, 'A' + r - 0x1f1e6})
			}
			first = r
		} else {
			first = 0
		}
	}
	return ""
}

func locationFromFields(fields map[string]json.RawMessage) *ServerLocation {
	return locationFromFieldsDepth(fields, 0)
}

func locationFromFieldsDepth(fields map[string]json.RawMessage, depth int) *ServerLocation {
	if depth > 2 {
		return nil
	}
	loc := &ServerLocation{CountryCode: countryCode(locationString(fields, "countryCode", "country_code")), Country: locationString(fields, "countryName", "country_name", "country"), City: locationString(fields, "city"), Region: locationString(fields, "region")}
	var nested map[string]json.RawMessage
	if json.Unmarshal(fields["location"], &nested) == nil && nested != nil {
		// Direct fields take precedence over a nested location object.
		inner := locationFromFieldsDepth(nested, depth+1)
		if inner != nil {
			loc = mergeLocation(loc, inner)
		}
		if loc.Label == "" {
			loc.Label = locationString(nested, "label", "name")
		}
	} else {
		loc.Label = locationString(fields, "location")
	}
	if loc.CountryCode == "" {
		loc.CountryCode = countryCode(loc.Country)
	}
	if loc.CountryCode == "" {
		loc.CountryCode = countryCode(loc.Label)
	}
	if loc.Country == loc.CountryCode || countryCode(loc.Country) != "" {
		loc.Country = ""
	}
	if loc.CountryCode == "" && loc.Country == "" && loc.City == "" && loc.Region == "" && loc.Label == "" {
		return nil
	}
	return loc
}

func mergeLocation(primary, fallback *ServerLocation) *ServerLocation {
	if primary == nil {
		if fallback == nil {
			return nil
		}
		copy := *fallback
		return &copy
	}
	result := *primary
	if fallback != nil {
		if result.CountryCode == "" && result.Country == "" {
			result.CountryCode, result.Country = fallback.CountryCode, fallback.Country
		}
		if result.City == "" {
			result.City = fallback.City
		}
		if result.Region == "" {
			result.Region = fallback.Region
		}
		if result.Label == "" {
			result.Label = fallback.Label
		}
	}
	return &result
}
