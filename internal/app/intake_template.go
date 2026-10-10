package app

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"strconv"
	"strings"
)

const intakeTemplatePrefix = "D-TEMPLATE0000"
const intakeTemplateRows = 100
const code39Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ-. $/+%*"

var code39Patterns = []int{0x034, 0x121, 0x061, 0x160, 0x031, 0x130, 0x070, 0x025, 0x124, 0x064, 0x109, 0x049, 0x148, 0x019, 0x118, 0x058, 0x00d, 0x10c, 0x04c, 0x01c, 0x103, 0x043, 0x142, 0x013, 0x112, 0x052, 0x007, 0x106, 0x046, 0x016, 0x181, 0x0c1, 0x1c0, 0x091, 0x190, 0x0d0, 0x085, 0x184, 0x0c4, 0x0a8, 0x0a2, 0x08a, 0x02a, 0x094}

func code39PNG(value string) ([]byte, error) {
	bits := make([]bool, 10)
	for _, char := range "*" + value + "*" {
		index := strings.IndexRune(code39Alphabet, char)
		if index < 0 {
			return nil, fmt.Errorf("unsupported Code 39 character")
		}
		pattern := code39Patterns[index]
		for element := range 9 {
			width := 1
			if pattern&(1<<(8-element)) != 0 {
				width = 3
			}
			for range width {
				bits = append(bits, element%2 == 0)
			}
		}
		bits = append(bits, false)
	}
	bits = append(bits, make([]bool, 10)...)
	picture := image.NewGray(image.Rect(0, 0, len(bits)*2, 90))
	for y := range picture.Rect.Dy() {
		for x := range picture.Rect.Dx() {
			c := color.Gray{Y: 255}
			if bits[x/2] {
				c.Y = 0
			}
			picture.SetGray(x, y, c)
		}
	}
	var output bytes.Buffer
	err := png.Encode(&output, picture)
	return output.Bytes(), err
}

// IDs and embedded images are fixed within each file; a new file gets a fresh
// namespace. No font, macro, external image service or workbook formula creates IDs.
func NewDeviceIntakeTemplate() ([]byte, error) {
	var random [6]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	prefix := "D-" + strings.ToUpper(hex.EncodeToString(random[:]))
	skeleton, err := assets.ReadFile("templates/device-intake.xlsx")
	if err != nil {
		return nil, err
	}
	reader, err := zip.NewReader(bytes.NewReader(skeleton), int64(len(skeleton)))
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, file := range reader.File {
		source, err := file.Open()
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(source)
		source.Close()
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(file.Name, ".xml") {
			content = bytes.ReplaceAll(content, []byte(intakeTemplatePrefix), []byte(prefix))
		}
		if strings.HasPrefix(file.Name, "xl/media/image") && strings.HasSuffix(file.Name, ".png") {
			number, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(file.Name, "xl/media/image"), ".png"))
			if err != nil || number < 1 || number > intakeTemplateRows {
				return nil, fmt.Errorf("invalid template barcode image")
			}
			content, err = code39PNG(fmt.Sprintf("%s-%03d", prefix, number))
			if err != nil {
				return nil, err
			}
		}
		header := file.FileHeader
		header.Method = zip.Deflate
		destination, err := writer.CreateHeader(&header)
		if err != nil {
			return nil, err
		}
		if _, err = destination.Write(content); err != nil {
			return nil, err
		}
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
