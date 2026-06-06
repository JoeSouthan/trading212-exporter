package gen

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// FlexibleDateTime accepts date-time values encoded either as ISO8601 strings
// or as Unix timestamps represented as JSON numbers.
type FlexibleDateTime struct {
	RawString *string
	RawNumber *float64
}

func (f *FlexibleDateTime) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		f.RawString = nil
		f.RawNumber = nil
		return nil
	}

	if len(trimmed) > 0 && trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		f.RawString = &s
		f.RawNumber = nil
		return nil
	}

	var n float64
	if err := json.Unmarshal(trimmed, &n); err == nil {
		f.RawNumber = &n
		f.RawString = nil
		return nil
	}

	return fmt.Errorf("unsupported date-time encoding: %s", string(trimmed))
}
