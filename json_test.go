package simplejsonext

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnicode(t *testing.T) {
	tests := []struct {
		in  string
		out any
	}{
		{`"\u2023"`, "\u2023"},
		// lowest two-character utf16 rune
		{`"\uD800\uDC00"`, "\U00010000"},
		// two replacement characters for low, high surrogate
		{`"\uDC00\uD800"`, "\ufffd\ufffd"},
		// two replacement characters for two high surrogates
		{`"\ud83d\ud83d"`, "\ufffd\ufffd"},
		// two replacement characters for two low surrogates
		{`"\udca5\udca5"`, "\ufffd\ufffd"},
		// replacement character for lone high surrogate
		{`"\uD800"`, "\ufffd"},
		// replacement character for lone low surrogate
		{`"\uDC01"`, "\ufffd"},
		// replacement character for unpaired low surrogate
		{`"foo\uDC01bar"`, "foo\ufffdbar"},
		// correctly encoded explosion emoji
		{`"\ud83d\udca5"`, "\U0001f4a5"},
		// unpaired high surrogates should not stomp on non-surrogates
		{`"\ud83d\u2023"`, "\ufffd\u2023"},
		// unpaired low surrogates shouldn't do that either for whatever reason
		{`"\udca5\u2023"`, "\ufffd\u2023"},
		// replacement character for an unpaired high surrogate and some literal hex
		{`"\ud83ddca5"`, "\ufffddca5"},
		// another case for an unpaired high surrogate
		{`"\ud83d         more text"`, "\ufffd         more text"},
		// non-hex in a unicode escape
		{`"\uasdf"`, errors.New("simple json: expected a hexadecimal unicode code point but found \"asdf\"")},
		// non-hex in a low surrogate escape
		{`"\ud83d\uasdf"`, errors.New("simple json: expected a hexadecimal unicode code point but found \"asdf\"")},
		// very-truncated case
		{`"\u12"`, errors.New("simple json: expected a unicode hexadecimal codepoint but json is truncated")},
		// invalid escapes are forbidden
		{`"\w"`, errors.New("simple json: invalid escape w")},
	}

	for _, test := range tests {
		t.Run(test.in, func(t *testing.T) {
			res, err := UnmarshalString(test.in)
			if err != nil {
				res = err
			}

			switch v := test.out.(type) {
			case string:
				if res != test.out {
					t.Errorf("expected %#v but got %#v", test.out, res)
				}
			case error:
				if v.Error() != res.(error).Error() {
					t.Errorf("expected error %#v but got %#v", test.out, res)
				}
			default:
				panic("oops")
			}
		})
	}
}

func TestParseImpossibleFloats(t *testing.T) {
	cases := []struct {
		raw   string
		value interface{}
	}{
		{raw: `NaN`, value: math.NaN()},
		{raw: `Inf`, value: math.Inf(+1)},
		{raw: `Infinity`, value: math.Inf(+1)},
		{raw: `-Inf`, value: math.Inf(-1)},
		{raw: `-Infinity`, value: math.Inf(-1)},
		{raw: `true`, value: true},
		{raw: `false`, value: false},
		{raw: `[Infinity]`, value: []interface{}{math.Inf(+1)}},
	}

	for _, test := range cases {
		test := test
		t.Run(fmt.Sprintf("%s is supported", test.raw), func(t *testing.T) {
			var parsed interface{}
			parsed, err := UnmarshalString(test.raw)
			require.NoError(t, err)
			if !reflect.DeepEqual(parsed, test.value) &&
				math.IsNaN(parsed.(float64)) != math.IsNaN(test.value.(float64)) {
				t.Errorf("%v != %v", parsed, test.value)
			}
		})
	}
}

func TestEmitImpossibleFloats(t *testing.T) {
	cases := []struct {
		raw   string
		value interface{}
	}{
		{raw: `NaN`, value: math.NaN()},
		{raw: `Infinity`, value: math.Inf(+1)},
		{raw: `-Infinity`, value: math.Inf(-1)},
	}

	for _, test := range cases {
		test := test
		t.Run(fmt.Sprintf("%s is supported", test.raw), func(t *testing.T) {
			serialized, err := MarshalToString(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(serialized, test.raw) {
				t.Errorf("%s != %s", string(serialized), test.raw)
			}
		})
	}
}

func TestParseBigInts(t *testing.T) {
	cases := []struct {
		raw   string
		value interface{}
	}{
		{raw: `377656068437302000000`, value: float64(377656068437302000000)},
		{raw: `-377656068437302000000`, value: float64(-377656068437302000000)},
		{raw: `9223372036854775807`, value: int64(9223372036854775807)},
		{raw: `9223372036854775808`, value: float64(9223372036854775808)},
		{raw: `-9223372036854775808`, value: int64(-9223372036854775808)},
		{raw: `-9223372036854775809`, value: float64(-9223372036854775809)},
	}

	for _, test := range cases {
		test := test
		t.Run(fmt.Sprintf("%s is supported", test.raw), func(t *testing.T) {
			var actual interface{}
			actual, err := UnmarshalString(test.raw)
			require.NoError(t, err)
			if !reflect.DeepEqual(test.value, actual) {
				t.Errorf("%v != %v", test.value, actual)
			}
		})
	}
}

var _ json.Marshaler = &jsonextMarshaled{}

type jsonextMarshaled struct {
	value interface{}
}

func (jm jsonextMarshaled) MarshalJSON() ([]byte, error) {
	b, e := Marshal(jm.value)
	return b, e
}

func TestControlCharacters(t *testing.T) {
	var sb strings.Builder
	for r := rune(0); r <= '"'; r++ {
		sb.WriteRune(r)
	}
	testString := sb.String()
	marshaled, err := MarshalToString(testString)
	if err != nil {
		t.Fatal(err)
	}
	expected := `"\u0000\u0001\u0002\u0003\u0004\u0005\u0006\u0007` +
		`\b\t\n\u000b\f\r\u000e\u000f\u0010\u0011\u0012\u0013\u0014` +
		`\u0015\u0016\u0017\u0018\u0019\u001a\u001b\u001c\u001d\u001e\u001f` +
		` !\""`
	assert.Equal(t, expected, marshaled)

	unmarshaled, err := UnmarshalString(marshaled)
	require.NoError(t, err)
	assert.Equal(t, testString, unmarshaled)

	err = json.Unmarshal([]byte(marshaled), &unmarshaled)
	if err != nil {
		t.Fatal(err)
	}
	if unmarshaled != testString {
		t.Errorf("Control characters did not round trip with encoding/json: expected %#v, but got %#v",
			testString, unmarshaled)
	}

	marshaledViaInterface, err := json.Marshal(jsonextMarshaled{value: testString})
	if err != nil {
		t.Error("encoding/json did not validate the encoding", err)
	}
	if !bytes.Equal([]byte(marshaled), marshaledViaInterface) {
		t.Fatalf("sanity check: jsonextMarshaled didn't work: expected %#v, but got %#v",
			marshaled, marshaledViaInterface)
	}

	for ch := 0; ch < 32; ch++ {
		_, err := Unmarshal([]byte{'"', byte(ch), '"'})
		assert.ErrorContains(t, err, "simple json: control character, tab, or newline in string value")
	}
}

func TestParseObject(t *testing.T) {
	val, err := UnmarshalObjectString(`    {"a": 1  }  `)
	require.NoError(t, err)
	assert.Equal(t, val, map[string]any{"a": int64(1)})
}

func TestWhitespaceSkipping(t *testing.T) {
	val, err := UnmarshalString(` { "a" : 1 } `)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a": int64(1)}, val)

	val, err = UnmarshalString(` [ true , false ] `)
	require.NoError(t, err)
	assert.Equal(t, []any{true, false}, val)
}
