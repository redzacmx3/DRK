package app

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var opaqueID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var trailingExpiry = regexp.MustCompile(`17[0-9]{6}$`)

type barcodeData struct {
	Kind            string   `json:"kind"`
	Value           string   `json:"value,omitempty"`
	ObjectKind      string   `json:"object_kind,omitempty"`
	ObjectID        string   `json:"object_id,omitempty"`
	GTIN            string   `json:"gtin,omitempty"`
	Lot             string   `json:"lot,omitempty"`
	ExpiryRaw       string   `json:"expiry_raw,omitempty"`
	ExpiryPrecision string   `json:"expiry_precision,omitempty"`
	Serial          string   `json:"serial,omitempty"`
	Warnings        []string `json:"warnings"`
}

func asciiDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func canonicalGTIN(s string) (string, error) {
	if !asciiDigits(s) || len(s) != 8 && len(s) != 12 && len(s) != 13 && len(s) != 14 {
		return "", fmt.Errorf("GTIN benötigt 8, 12, 13 oder 14 Ziffern.")
	}
	sum, weight := 0, 3
	for i := len(s) - 2; i >= 0; i-- {
		sum += int(s[i]-'0') * weight
		weight = 4 - weight
	}
	if (10-sum%10)%10 != int(s[len(s)-1]-'0') {
		return "", fmt.Errorf("GTIN-Prüfziffer ungültig.")
	}
	return strings.Repeat("0", 14-len(s)) + s, nil
}
func canonicalPZN(s string) (string, error) {
	s = strings.TrimPrefix(s, "-")
	if len(s) == 7 {
		s = "0" + s
	}
	if len(s) != 8 || !asciiDigits(s) {
		return "", fmt.Errorf("PZN benötigt 7 oder 8 Ziffern.")
	}
	sum := 0
	for i := 0; i < 7; i++ {
		sum += int(s[i]-'0') * (i + 1)
	}
	if sum%11 == 10 || sum%11 != int(s[7]-'0') {
		return "", fmt.Errorf("PZN-Prüfziffer ungültig.")
	}
	return s, nil
}
func validGS1Text(s string) bool {
	if len(s) < 1 || len(s) > 20 {
		return false
	}
	for _, r := range s {
		if r < 32 || r > 126 {
			return false
		}
	}
	return true
}
func validateGS1Date(s string) (string, error) {
	if len(s) != 6 || !asciiDigits(s) {
		return "", fmt.Errorf("GS1-Verfall benötigt YYMMDD.")
	}
	month, day := int(s[2]-'0')*10+int(s[3]-'0'), int(s[4]-'0')*10+int(s[5]-'0')
	if month < 1 || month > 12 {
		return "", fmt.Errorf("GS1-Verfallsmonat ungültig.")
	}
	// YY cannot establish a century. Check calendar syntax without inventing one;
	// matching later uses the explicitly registered batch's full year.
	if day == 0 {
		return "month", nil
	}
	year := 2000 + int(s[0]-'0')*10 + int(s[1]-'0')
	d := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if d.Month() != time.Month(month) || d.Day() != day {
		return "", fmt.Errorf("GS1-Verfallsdatum ungültig.")
	}
	return "day", nil
}
func parseGS1(text string) (barcodeData, error) {
	data := barcodeData{Kind: "gs1", Warnings: []string{}}
	hri := strings.HasPrefix(text, "(")
	seen := map[string]bool{}
	for text != "" {
		if text[0] == 29 {
			return data, fmt.Errorf("Unerwarteter GS1-Feldtrenner.")
		}
		var ai, value string
		if hri {
			if len(text) < 4 || text[0] != '(' || text[3] != ')' {
				return data, fmt.Errorf("GS1-Feldkennung ungültig.")
			}
			ai = text[1:3]
			text = text[4:]
			next := strings.Index(text, "(")
			if next < 0 {
				value = text
				text = ""
			} else {
				value = text[:next]
				text = text[next:]
			}
			if strings.ContainsAny(value, "()\x1d") {
				return data, fmt.Errorf("Mehrdeutige GS1-Klammerdarstellung.")
			}
		} else {
			if len(text) < 2 {
				return data, fmt.Errorf("GS1-Feldkennung unvollständig.")
			}
			ai = text[:2]
			text = text[2:]
			length := 0
			if ai == "01" {
				length = 14
			} else if ai == "17" {
				length = 6
			}
			if length > 0 {
				if len(text) < length {
					return data, fmt.Errorf("GS1-Feld unvollständig.")
				}
				value = text[:length]
				text = text[length:]
				if strings.HasPrefix(text, "\x1d") {
					text = text[1:]
				}
			} else {
				next := strings.IndexByte(text, 29)
				if next < 0 {
					value = text
					text = ""
				} else {
					value = text[:next]
					text = text[next+1:]
				}
			}
		}
		if seen[ai] {
			return data, fmt.Errorf("Doppelte GS1-Feldkennung.")
		}
		seen[ai] = true
		switch ai {
		case "01":
			if len(value) != 14 {
				return data, fmt.Errorf("GS1-GTIN benötigt genau 14 Ziffern.")
			}
			gtin, err := canonicalGTIN(value)
			if err != nil {
				return data, err
			}
			data.GTIN = gtin
			data.Value = gtin
		case "10":
			if !validGS1Text(value) {
				return data, fmt.Errorf("GS1-Charge benötigt 1–20 druckbare ASCII-Zeichen.")
			}
			data.Lot = value
			if !hri && trailingExpiry.MatchString(value) {
				data.Warnings = append(data.Warnings, "Möglicherweise fehlender Feldtrenner zwischen Charge und Verfall; keine Felder geraten.")
			}
		case "17":
			precision, err := validateGS1Date(value)
			if err != nil {
				return data, err
			}
			data.ExpiryRaw = value
			data.ExpiryPrecision = precision
			if precision == "month" {
				data.Warnings = append(data.Warnings, "Monatsverfall (Tag 00) benötigt bestätigte Hersteller-/Produktregel; kein Grenztag automatisch vergeben.")
			}
		case "21":
			if !validGS1Text(value) {
				return data, fmt.Errorf("GS1-Seriennummer benötigt 1–20 druckbare ASCII-Zeichen.")
			}
			data.Serial = value
			data.Warnings = append(data.Warnings, "Seriennummer erkannt; Einzelgeräte-/Serienverfolgung ist noch nicht implementiert.")
		default:
			return data, fmt.Errorf("GS1-Feldkennung %q wird noch nicht unterstützt; keine Teilauflösung.", ai)
		}
	}
	if data.GTIN == "" {
		return data, fmt.Errorf("GS1-Auflösung benötigt eine GTIN (01).")
	}
	return data, nil
}
func parseBarcode(text, format string) (barcodeData, error) {
	data := barcodeData{Kind: "unknown", Warnings: []string{}}
	if !utf8.ValidString(text) || len(text) < 1 || len(text) > 1024 {
		return data, fmt.Errorf("Scannertext benötigt 1–1024 UTF-8-Bytes.")
	}
	text = strings.TrimRight(text, "\r\n")
	if format == "" {
		format = "auto"
	}
	if format != "auto" && format != "gtin" && format != "pzn" && format != "ref" && format != "gs1" && format != "internal" {
		return data, fmt.Errorf("Scannerformat ungültig.")
	}
	symbology := ""
	for _, prefix := range []string{"]d2", "]C1", "]Q3", "]E0", "]A0", "]Q1", "]Q2", "]d1", "]C0"} {
		if strings.HasPrefix(text, prefix) {
			symbology = prefix
			text = text[3:]
			break
		}
	}
	if symbology == "]d2" || symbology == "]C1" || symbology == "]Q3" || symbology == "]E0" {
		hint := "gs1"
		if symbology == "]E0" {
			hint = "gtin"
		}
		if format != "auto" && format != hint {
			return data, fmt.Errorf("Format widerspricht der Scanner-Symbologie.")
		}
		format = hint
	}
	if strings.HasPrefix(text, "drk:") || format == "internal" {
		parts := strings.Split(text, ":")
		if len(parts) != 4 || parts[0] != "drk" || parts[1] != "v1" || parts[2] != "location" && parts[2] != "product" && parts[2] != "batch" || !opaqueID.MatchString(strings.ToLower(parts[3])) {
			return data, fmt.Errorf("Interne Kennung oder Schema-Version ungültig.")
		}
		if format != "auto" && format != "internal" {
			return data, fmt.Errorf("Format widerspricht der internen Kennung.")
		}
		data.Kind = "internal"
		data.ObjectKind = parts[2]
		data.ObjectID = strings.ToLower(parts[3])
		return data, nil
	}
	if format == "gs1" || format == "auto" && ((strings.HasPrefix(text, "(01)") || strings.HasPrefix(text, "(10)") || strings.HasPrefix(text, "(17)") || strings.HasPrefix(text, "(21)")) || strings.ContainsRune(text, 29)) {
		return parseGS1(text)
	}
	// Raw GS1 without AIM prefix/FNC1 can also be recognized when its leading
	// fixed-width GTIN is valid; 14-digit GTINs are never consumed as GS1.
	if format == "auto" && strings.HasPrefix(text, "01") && len(text) >= 16 {
		if _, err := canonicalGTIN(text[2:16]); err == nil {
			return parseGS1(text)
		}
	}
	text = strings.TrimSpace(text)
	if strings.HasPrefix(strings.ToUpper(text), "PZN:") {
		if format != "auto" && format != "pzn" {
			return data, fmt.Errorf("Format widerspricht PZN-Kennung.")
		}
		format = "pzn"
		text = strings.TrimSpace(text[4:])
	}
	if format == "auto" && strings.HasPrefix(text, "-") {
		format = "pzn"
	}
	if format == "ref" {
		if len(text) > 100 || text == "" {
			return data, fmt.Errorf("REF benötigt 1–100 Zeichen.")
		}
		for _, r := range text {
			if r < 32 || r > 126 {
				return data, fmt.Errorf("REF benötigt druckbare ASCII-Zeichen.")
			}
		}
		data.Kind = "ref"
		data.Value = text
		return data, nil
	}
	if format == "gtin" {
		value, err := canonicalGTIN(text)
		data.Kind = "gtin"
		data.Value = value
		return data, err
	}
	if format == "pzn" {
		value, err := canonicalPZN(text)
		data.Kind = "pzn"
		data.Value = value
		return data, err
	}
	if asciiDigits(text) {
		gtin, gtinErr := canonicalGTIN(text)
		pzn, pznErr := canonicalPZN(text)
		if gtinErr == nil && pznErr == nil {
			data.Kind = "ambiguous"
			data.Warnings = append(data.Warnings, "Ziffernfolge ist sowohl als GTIN als auch PZN gültig; Format ausdrücklich auswählen.")
			return data, nil
		}
		if gtinErr == nil {
			data.Kind = "gtin"
			data.Value = gtin
			return data, nil
		}
		if pznErr == nil {
			data.Kind = "pzn"
			data.Value = pzn
			return data, nil
		}
		return data, fmt.Errorf("Länge oder Prüfziffer der Ziffernfolge ungültig.")
	}
	for _, r := range text {
		if r < 32 || r == 127 {
			return data, fmt.Errorf("Nicht unterstützte Steuerzeichen im Scannertext.")
		}
	}
	data.Warnings = append(data.Warnings, "Unbekanntes Format. Fremde URLs werden nicht geöffnet; keine automatische Zuordnung.")
	return data, nil
}
