package simplejsonext

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

const raw = `{"a": 1, "b": 1.2, "c": -1e-3, "d": Infinity, "e": -Infinity, "f": NaN, "g": "str", "h": "abc Infinity"}`

func TestDeNaN(t *testing.T) {
	var expected = map[string]interface{}{
		"a": int64(1),
		"b": 1.2,
		"c": -1e-3,
		"d": "Infinity",
		"e": "-Infinity",
		"f": "NaN",
		"g": "str",
		"h": "abc Infinity",
	}

	dirty, err := UnmarshalString(raw)
	require.NoError(t, err)
	cleaned, ok := WalkDeNaN(dirty).(map[string]any)
	require.True(t, ok)

	if !reflect.DeepEqual(cleaned, expected) {
		t.Errorf("<<< %+v", cleaned)
		t.Errorf(">>> %+v", expected)
	}
}
