package prosody

import (
	"bytes"
	"encoding/json"
)

// decodeJSONList unmarshals a JSON array. Empty Lua tables encode as {} so
// that shape is treated as an empty list.
func decodeJSONList[T any](data json.RawMessage) ([]T, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("{}")) {
		return nil, nil
	}
	var out []T
	if err := json.Unmarshal(trimmed, &out); err != nil {
		return nil, err
	}
	return out, nil
}
