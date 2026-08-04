package store

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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

// HalfVector binds a []float32 against a pgvector halfvec column using its
// text input format, e.g. "[0.1,0.2,0.3]". Passing a raw []float32 through
// gorm without this encodes it as a Postgres composite record, which halfvec
// cannot accept.
type HalfVector []float32

// Value uses a value receiver, unlike Scan below: the ChunkEmbedding.Embedding
// field is a bare HalfVector (not *HalfVector), and gorm's AddVar type-switches
// on the field's own value, so a pointer-receiver Value here would never match
// driver.Valuer and the field would fall back to the driver's raw encoding.
func (v HalfVector) Value() (driver.Value, error) {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = strconv.FormatFloat(float64(f), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}

func (v *HalfVector) Scan(value any) error {
	if value == nil {
		*v = nil
		return nil
	}
	var raw string
	switch val := value.(type) {
	case string:
		raw = val
	case []byte:
		raw = string(val)
	default:
		return fmt.Errorf("cannot scan %T into HalfVector", value)
	}
	raw = strings.Trim(raw, "[]")
	if raw == "" {
		*v = HalfVector{}
		return nil
	}
	fields := strings.Split(raw, ",")
	out := make(HalfVector, len(fields))
	for i, f := range fields {
		parsed, err := strconv.ParseFloat(f, 32)
		if err != nil {
			return fmt.Errorf("cannot parse HalfVector element %q: %w", f, err)
		}
		out[i] = float32(parsed)
	}
	*v = out
	return nil
}

func (HalfVector) GormDataType() string { return "halfvec(1024)" }
