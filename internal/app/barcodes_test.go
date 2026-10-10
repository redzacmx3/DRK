package app

import (
	"strings"
	"testing"
)

func TestBarcodeFormatsAndChecksums(t *testing.T) {
	cases := []struct{ text, format, kind, value string }{
		{"4006381333931", "auto", "gtin", "04006381333931"}, {"036000291452", "gtin", "gtin", "00036000291452"}, {"96385074", "gtin", "gtin", "00000096385074"},
		{"PZN:01234562", "auto", "pzn", "01234562"}, {"-1234562", "auto", "pzn", "01234562"}, {"]A0-01234562", "auto", "pzn", "01234562"}, {"01234562", "pzn", "pzn", "01234562"}, {"00001175", "auto", "ambiguous", ""}, {"00001175", "pzn", "pzn", "00001175"}, {"00001175", "gtin", "gtin", "00000000001175"},
		{"]E04006381333931\r\n", "auto", "gtin", "04006381333931"}, {"REF-1/A", "ref", "ref", "REF-1/A"}, {"https://example.invalid/unsafe", "auto", "unknown", ""},
	}
	for _, c := range cases {
		t.Run(c.text+":"+c.format, func(t *testing.T) {
			p, err := parseBarcode(c.text, c.format)
			if err != nil || p.Kind != c.kind || p.Value != c.value {
				t.Fatalf("%+v / %v", p, err)
			}
		})
	}
	for _, c := range []struct{ text, format string }{{"4006381333932", "gtin"}, {"01234563", "pzn"}, {"", "auto"}, {strings.Repeat("x", 1025), "auto"}, {"12\x00", "auto"}, {"GTIN", "other"}, {"]E04006381333931", "pzn"}, {"PZN:01234562", "gtin"}, {"drk:v2:location:" + warehouse, "auto"}, {"drk:v1:user:" + warehouse, "auto"}, {"drk:v1:location:https://evil.invalid", "auto"}} {
		if _, err := parseBarcode(c.text, c.format); err == nil {
			t.Fatalf("accepted %q / %s", c.text, c.format)
		}
	}
	code, err := internalCode("location", warehouse)
	if err != nil {
		t.Fatal(err)
	}
	p, err := parseBarcode("]Q1"+code, "auto")
	if err != nil || p.ObjectKind != "location" || p.ObjectID != warehouse {
		t.Fatal(p, err)
	}
}
func TestGS1FieldsSeparatorsAndDates(t *testing.T) {
	for _, text := range []string{"(01)04006381333931(10)LOT-42(17)281231(21)SER-7", "(17)281231(10)LOT-42(01)04006381333931(21)SER-7", "]d2010400638133393110LOT-42\x1d1728123121SER-7", "01040063813339311728123110LOT-42\x1d21SER-7"} {
		p, err := parseBarcode(text, "auto")
		if err != nil || p.Kind != "gs1" || p.GTIN != "04006381333931" || p.Lot != "LOT-42" || p.ExpiryRaw != "281231" || p.Serial != "SER-7" {
			t.Fatalf("%q: %+v / %v", text, p, err)
		}
	}
	p, err := parseBarcode("0104006381333931", "auto")
	if err != nil || p.Kind != "gs1" {
		t.Fatal(p, err)
	}
	p, err = parseBarcode("(01)04006381333931(17)280200", "auto")
	if err != nil || p.ExpiryPrecision != "month" || len(p.Warnings) == 0 {
		t.Fatal(p, err)
	}
	p, err = parseBarcode("010400638133393110LOT17281231", "auto")
	if err != nil || p.Lot != "LOT17281231" || p.ExpiryRaw != "" || len(p.Warnings) == 0 {
		t.Fatal("missing separator fields were guessed", p, err)
	}
	for _, text := range []string{"(01)04006381333932", "(01)4006381333931", "(01)04006381333931(17)230229", "(01)04006381333931(17)281332", "(01)04006381333931(17)280431", "(01)04006381333931(10)", "(01)04006381333931(10)A(10)B", "(01)04006381333931(15)281231", "(10)LOT", "010400638133393110" + strings.Repeat("a", 21), "]d2010400638133393110LOT\x1d\x1d17281231"} {
		if _, err := parseBarcode(text, "gs1"); err == nil {
			t.Fatalf("accepted malformed GS1 %q", text)
		}
	}
}
func FuzzParseBarcode(f *testing.F) {
	for _, text := range []string{"4006381333931", "(01)04006381333931(10)LOT(17)281231", "drk:v1:location:" + warehouse, "https://example.invalid", "\x1d", "00001175"} {
		f.Add(text, "auto")
	}
	f.Fuzz(func(t *testing.T, text, format string) {
		p, err := parseBarcode(text, format)
		if err == nil {
			if len(text) > 1024 {
				t.Fatal("unbounded input")
			}
			if p.Kind == "gtin" {
				if _, err = canonicalGTIN(p.Value); err != nil {
					t.Fatal(err)
				}
			}
			if p.Kind == "pzn" {
				if _, err = canonicalPZN(p.Value); err != nil {
					t.Fatal(err)
				}
			}
		}
	})
}
