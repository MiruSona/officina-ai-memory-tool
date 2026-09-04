package store

import (
	"bytes"
	"errors"
	"io"
	"unicode/utf8"

	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/transform"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// Encoding says how a file was read, so lint can count the ones we had to convert.
type Encoding string

const (
	EncodingUTF8    Encoding = "utf-8"
	EncodingUTF8BOM Encoding = "utf-8-bom"
	EncodingCP949   Encoding = "cp949"
)

var byteOrderMark = []byte{0xEF, 0xBB, 0xBF}

// decode is the three step fallback: BOM, plain UTF-8, then cp949.
// Files other tools wrote are the reason the last step exists.
func decode(data []byte, path string) ([]byte, Encoding, error) {
	if bytes.HasPrefix(data, byteOrderMark) {
		return data[len(byteOrderMark):], EncodingUTF8BOM, nil
	}
	if utf8.Valid(data) {
		return data, EncodingUTF8, nil
	}
	decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(data), korean.EUCKR.NewDecoder()))
	if err != nil {
		return nil, EncodingCP949, errors.New(i18n.T(i18n.CannotDecode, path))
	}
	return decoded, EncodingCP949, nil
}
