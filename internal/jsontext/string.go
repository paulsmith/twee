// Package jsontext contains strict checks that encoding/json does not perform.
package jsontext

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ValidateObjectStringField rejects unpaired surrogate escapes in one string
// field while leaving all other object fields untouched.
func ValidateObjectStringField(raw []byte, name string) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return nil
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil
		}
		keyString, ok := key.(string)
		if !ok {
			return nil
		}
		if strings.EqualFold(keyString, name) {
			if err := ValidateStringSurrogates(value); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateStringSurrogates rejects unpaired UTF-16 surrogate escapes in a raw
// JSON string. encoding/json otherwise decodes them as the replacement rune.
func ValidateStringSurrogates(raw []byte) error {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return nil
	}
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw)-1 || raw[i] != 'u' {
			continue
		}
		value, ok := hex4(raw[i+1:])
		if !ok {
			continue
		}
		i += 4
		switch {
		case value >= 0xd800 && value <= 0xdbff:
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return fmt.Errorf("unpaired high surrogate \\u%04X", value)
			}
			low, ok := hex4(raw[i+3:])
			if !ok || low < 0xdc00 || low > 0xdfff {
				return fmt.Errorf("unpaired high surrogate \\u%04X", value)
			}
			i += 6
		case value >= 0xdc00 && value <= 0xdfff:
			return fmt.Errorf("unpaired low surrogate \\u%04X", value)
		}
	}
	return nil
}

func hex4(raw []byte) (uint16, bool) {
	if len(raw) < 4 {
		return 0, false
	}
	var value uint16
	for _, b := range raw[:4] {
		value <<= 4
		switch {
		case b >= '0' && b <= '9':
			value |= uint16(b - '0')
		case b >= 'a' && b <= 'f':
			value |= uint16(b-'a') + 10
		case b >= 'A' && b <= 'F':
			value |= uint16(b-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}
