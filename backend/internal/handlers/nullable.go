package handlers

import (
	"bytes"
	"encoding/json"
	"time"
)

// jsonNullableTime distinguishes the three states a PATCH field can be in:
// absent (leave it alone), explicitly null (clear it), and a value (set it).
// A plain *time.Time collapses the first two into nil, which would make
// "remove the closing time" impossible to express.
type jsonNullableTime struct {
	Present bool
	Value   *time.Time
}

func (n *jsonNullableTime) UnmarshalJSON(data []byte) error {
	n.Present = true
	if bytes.Equal(data, []byte("null")) {
		n.Value = nil
		return nil
	}

	var parsed time.Time
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	n.Value = &parsed
	return nil
}
