package store

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// StringSlice is a JSON-serialized string slice compatible with any SQL driver.
type StringSlice []string

func (s *StringSlice) Value() (driver.Value, error) {
	if s == nil || *s == nil {
		return "[]", nil
	}
	b, err := json.Marshal([]string(*s))
	return string(b), err
}

func (s *StringSlice) Scan(value any) error {
	if value == nil {
		*s = nil
		return nil
	}
	var raw []byte
	switch v := value.(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		return fmt.Errorf("cannot scan %T into StringSlice", value)
	}
	if err := json.Unmarshal(raw, s); err == nil {
		return nil
	}
	// Legacy data stored participants as a JSON object keyed by Discord ID.
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		*s = keys
		return nil
	}
	*s = nil
	return nil
}

func (*StringSlice) GormDataType() string { return "text" }
