package simplejsonext_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/wandb/simplejsonext"
)

var (
	negativeZero = math.Copysign(0, -1)
	longNumber   = strings.Repeat("1234567890", 30)
	hugeNumber   = strings.Repeat("1234567890", 100)
	megaNumber   = strings.Repeat("1234567890", 1000)
	longFraction = "0." + longNumber + "e1"
	hugeFraction = "0." + hugeNumber + "e1"
	megaFraction = "0." + megaNumber + "e1"
)

type unmarshaler func([]byte, interface{}) error

type marshaler func(any) ([]byte, error)

type options struct {
	// If set, do not check the exact error messages.
	tolerateDifferentErrorMessages bool
	// If set, do not require numbers to have the exact same type when round-
	// tripping, only that they have equal values. This is here because some
	// JSON implementations will emit integral floats ambiguously formatted,
	// omitting the decimal point, which can cause them to round-trip to the
	// same value in an integer type rather than the original floating point.
	tolerateFloatToIntRoundTrip bool
	// If set, do not require empty map values to have the same null-ness
	tolerateMapNil bool
}

// Test equality between two values, including understanding NaNs and checking
// the sign bit of floating point zeros.
func assertEqual(t *testing.T, expected any, actual any, opt options) {
	t.Helper()
	if err := equalImpl(expected, actual, opt); err != nil {
		t.Error("values were not equal (", err, "):", expected, actual)
		assert.Exactly(t, expected, actual) // better diagnostics
	}
}

func floatIntEqual(f float64, i int64) bool {
	return int64(f) == i && f == float64(i)
}

// Deep equality for our supported types, with nuances.
//
// The only types supported are: nil, bool, string, int64, float64, []any,
// map[string]any, and map[any]any.
//
// We optionally tolerate expected float64 values being found as int64, including
// in map keys.
//
// This function is structured to only return nil in explicit success cases.
func equalImpl(expected any, actual any, opt options) error {
	switch ev := expected.(type) {
	case nil:
		if actual == nil {
			return nil
		}
	case bool:
		if av, ok := actual.(bool); ok && ev == av {
			return nil
		}
	case string:
		if av, ok := actual.(string); ok && ev == av {
			return nil
		}
	case int64:
		if av, ok := actual.(int64); ok && ev == av {
			return nil
		}
	case float64:
		if av, ok := actual.(float64); ok {
			if math.IsNaN(ev) && math.IsNaN(av) {
				// NaN do not compare equal, but this is the correct outcome
				return nil
			} else if ev == 0.0 && av == 0.0 {
				// zeros must have matching signs
				if math.Signbit(ev) == math.Signbit(av) {
					return nil
				}
			} else if ev == av {
				return nil
			}
		} else if av, ok := actual.(int64); ok && opt.tolerateFloatToIntRoundTrip {
			if floatIntEqual(ev, av) {
				return nil
			}
		}
	case []any:
		if av, ok := actual.([]any); ok {
			if len(ev) != len(av) {
				return errors.New("arrays have different lengths")
			}
			for i := range ev {
				if err := equalImpl(ev[i], av[i], opt); err != nil {
					return fmt.Errorf("%s at index %d", err, i)
				}
			}
			return nil
		}
	case map[string]any:
		if av, ok := actual.(map[string]any); ok {
			if len(av) > len(ev) {
				return errors.New("more object keys than expected")
			}
			if opt.tolerateMapNil && (av == nil) != (ev == nil) {
				return errors.New("one object, but not both, are nil")
			}
			for k, wantV := range ev {
				haveV, found := av[k]
				if !found {
					return fmt.Errorf("did not find expected key %#v", k)
				}
				if err := equalImpl(wantV, haveV, opt); err != nil {
					return fmt.Errorf("%s at key %#v", err, k)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("expected %[1]T(%#[1]v) but got %[2]T(%#[2]v)", expected, actual)
}

// Test ummarshaling
func testUnmarshal[T any](t *testing.T, u unmarshaler, data string, expected interface{}, opt options) {
	t.Helper()
	var actual T
	err := u([]byte(data), &actual)
	if err != nil {
		if expectedErr, ok := expected.(error); ok {
			if !opt.tolerateDifferentErrorMessages {
				assert.Equal(t, expectedErr.Error(), err.Error(), "error messages should match")
			}
		} else {
			t.Errorf("expected %#v but got an error: %s", expected, err)
		}
	} else {
		// Never tolerate float-to-int conversion on first parse.
		assertEqual(t, expected, actual, options{tolerateFloatToIntRoundTrip: false})
	}
}

// Tests that an unmarshaler/marshaler pair can round-trip a value.
func testRoundTrip(t *testing.T, u unmarshaler, m marshaler, data string, opt options) {
	t.Helper()
	var v1 any
	err := u([]byte(data), &v1)
	if err != nil {
		t.Error(err)
		return
	}
	emitted, err := m(v1)
	if err != nil {
		t.Error(err)
		return
	}
	var v2 any
	err = u(emitted, &v2)
	if err != nil {
		t.Error(err)
		return
	}
	// Pass in the options here, so that we can tolerate float to int conversion
	// in round-trips.
	assertEqual(t, v1, v2, opt)
}

type jsonCase struct {
	s string
	v interface{}
}

var (
	allCasesTested      map[string]bool
	populateCasesTested sync.Once
)

func testBehavior(t *testing.T, u unmarshaler, m marshaler, opt options, cases ...[]jsonCase) {
	setOfCasesTested := make(map[string]bool)
	for _, cc := range cases {
		for _, c := range cc {
			testName := c.s
			if len(testName) > 100 {
				testName = testName[:100]
			}
			t.Run(testName, func(t *testing.T) {
				if setOfCasesTested[c.s] {
					t.Fatalf("Duplicated test case %s", c.s)
				}
				setOfCasesTested[c.s] = true
				testUnmarshal[any](t, u, c.s, c.v, opt)
				// Also check that valid data will round-trip
				if _, isErr := c.v.(error); !isErr {
					testRoundTrip(t, u, m, c.s, opt)
				}
			})
		}
	}
	// Ensure that each call to testBehavior runs against the same set
	// of inputs
	populateCasesTested.Do(func() {
		allCasesTested = setOfCasesTested
	})
	assert.Equal(t, allCasesTested, setOfCasesTested, "all test cases should be covered for all parsers")
}

func nestedArrayJSON(depth int) string {
	return strings.Repeat("[", depth) + "null" + strings.Repeat("]", depth)
}

func nestedArrayValue(depth int) (res any) {
	for i := 0; i < depth; i++ {
		res = []any{res}
	}
	return
}

var (
	// These are basic cases for standard JSON behavior, which should be met by
	// every parser
	standardCases = []jsonCase{
		{`1.0`, float64(1)},
		{`-1e+1`, float64(-10)},
		{`9223372036854775808`, float64(9223372036854775808)},
		{`-9223372036854775809`, float64(-9223372036854775809)},
		{`-0.0`, negativeZero},
		{`.1`, errors.New("simple json: expected token but found '.'")},
		{`"foo"`, "foo"},
		// UTF-16 escapes
		{`"\u0000\u0041"`, "\x00A"},
		// UTF-16 surrogate pair escapes
		{`"\uD800\uDC00"`, "\U00010000"},
		// JSON standard shorthand escapes
		{`"\b\f\n\r\t"`, "\b\f\n\r\t"},
		// Raw UTF-8
		{"\"\U0001f4a5\"", "\U0001f4a5"},
		// Structural values and sentinel keywords
		{`[]`, []any{}},
		{`[true, false, null]`, []any{true, false, nil}},
		// Numbers that are mis-parsed by the rapidjson fast mode library, just
		// in case we end up trying to use a library that's doing that
		{`[
				0.9984394609928131,
				0.9328378140926361,
				0.38277979195117956,
				0.9761228142189365,
				0.030161080385250442,
				0.20488051639705546,
				0.12961511899336461,
				0.9279897927636401
			]`,
			[]any{
				0.9984394609928131,
				0.9328378140926361,
				0.38277979195117956,
				0.9761228142189365,
				0.030161080385250442,
				0.20488051639705546,
				0.12961511899336461,
				0.9279897927636401,
			},
		},
		// Unpaired surrogates become replacement characters
		{`"\ud83ddca5"`, "\ufffddca5"},
		// Each unpaired surrogate gets its own replacement character without
		// stomping adjacent escapes
		{`"\udc00\ud83d\udca5\u0021"`, "\ufffd\U0001f4a5!"},
		// Two surrogates in the wrong order become two replacement characters
		{`"\uDC00\uD800"`, "\ufffd\ufffd"},
		// Invalid escapes are rejected
		{`"\w"`, errors.New("simple json: invalid escape w")},
		// Unpaired surrogates
		{`"\ud801 not an escape"`, "\ufffd not an escape"},
		// Long numbers
		{longNumber, 1.2345678901234568e+299},
		{longFraction, 1.2345678901234567},
		{hugeFraction, 1.2345678901234567},
		{megaFraction, 1.2345678901234567},
		// Leading unary + is not allowed
		{`+.1`, errors.New("simple json: expected token but found '+'")},
		{`+1.0`, errors.New("simple json: expected token but found '+'")},
		{`+Inf`, errors.New("simple json: expected token but found '+'")},
		{`+Infinity`, errors.New("simple json: expected token but found '+'")},
		{`+9223372036854775808`, errors.New("simple json: expected token but found '+'")},
		{`+1000000000000000000`, errors.New("simple json: expected token but found '+'")},
		{`+123`, errors.New("simple json: expected token but found '+'")},
		// Objects
		{`{}`, map[string]any(nil)},
		{`{1.0: "a", false: true, null: 1, -Infinity: []}`, errors.New("simple json: expected '\"' but found '1'")},
		{`{"x": true,`, io.EOF},
		{"\"\t\"", errors.New("simple json: control character, tab, or newline in string value")},
		{"\"a\nb\"", errors.New("simple json: control character, tab, or newline in string value")},
		{"\"\x00\"", errors.New("simple json: control character, tab, or newline in string value")},
		{nestedArrayJSON(5), nestedArrayValue(5)},
		{nestedArrayJSON(500), nestedArrayValue(500)},
	}
	// Corner cases currently handled only by the standard library
	standardOnlyCornerCases = []jsonCase{
		// Invalid UTF-8 becomes all replacement characters for every wonky byte
		// UTF-8 has certain bytes that never appear and cannot encode surrogates
		//    " U+dc00      U+d800    "
		{"\"\xed\xb0\x80\xed\xa0\x80\"", "\ufffd\ufffd\ufffd\ufffd\ufffd\ufffd"},
		// invalid UTF-8 byte
		{"\"\xff\"", "\ufffd"},
		// Overlong encoding is also detected (this is the 3-byte overlong
		// encoding of the nul character and the character '!')
		{"\"\xe0\x80\x80\xe0\x80\xa1\"", "\ufffd\ufffd\ufffd\ufffd\ufffd\ufffd"},
	}
	// Standard library behavior for cases that we do want the ext library to
	// handle differently
	standardBehaviorForExtCases = []jsonCase{
		// Numbers are always floating point
		{`1`, float64(1)},
		{`9223372036854775807`, float64(9223372036854775807)},
		{`-9223372036854775808`, float64(-9223372036854775808)},
		// Numbers too large to fit in float64 are errors
		{`9e999`, fmt.Errorf("strconv.ParseFloat: parsing \"9e999\": value out of range")},
		{`-9e999`, fmt.Errorf("strconv.ParseFloat: parsing \"-9e999\": value out of range")},
		{hugeNumber, fmt.Errorf("strconv.ParseFloat: parsing \"%s\": value out of range", hugeNumber)},
		{megaNumber, fmt.Errorf("strconv.ParseFloat: parsing \"%s\": value out of range", megaNumber)},
		// Many special values are not accepted
		{`NaN`, errors.New("not accepted")},
		{`Inf`, errors.New("not accepted")},
		{`Infinity`, errors.New("not accepted")},
		{`-Inf`, errors.New("not accepted")},
		{`-Infinity`, errors.New("not accepted")},
		{`-NaN`, errors.New("not accepted")},
		{`1.`, errors.New("not accepted")},
		{`1.e1`, errors.New("not accepted")},
		{`-.1`, errors.New("not accepted")},
		{`NaNvvvvv`, errors.New("not accepted")},
		{`NaNa`, errors.New("not accepted")},
		{`Infrared`, errors.New("not accepted")},
		{nestedArrayJSON(501), nestedArrayValue(501)},
	}
	standardBehaviorForExtCasesUnmarshaling = []jsonCase{
		// Leading zeros are always parsed as zero, and any following digits are
		// unexpected data.
		{`01`, errors.New("extra data")},
		{`02.3`, errors.New("extra data")},
		{`-01`, errors.New("extra data")},
		// Other trailing data cases
		{`1,2`, errors.New("extra data")},
		{`[1]2`, errors.New("extra data")},
		{`"foo"{}bar`, errors.New("extra data")},
		{`{}foobar`, errors.New("extra data")},
		{`123zfoo456bar`, errors.New("extra data")},
		{`5e1b892d`, errors.New("extra data")},
		{`0xff`, errors.New("extra data")},
		{`123foo`, errors.New("extra data")},
		{`5e1a892d`, errors.New("extra data")},
		{`5e1f892d`, errors.New("extra data")},
	}
	standardBehaviorForExtCasesStreaming = []jsonCase{
		// Leading zeros are always parsed as zero, and any following digits are
		// unexpected data.
		{`01`, float64(0)},
		{`02.3`, float64(0)},
		{`-01`, negativeZero},
		// The standard library parser ignored trailing data when streaming, but
		// parses some values somewhat differently
		{`1,2`, float64(1)},
		{`[1]2`, []any{float64(1)}},
		{`"foo"{}bar`, "foo"},
		{`{}foobar`, map[string]any{}},
		{`123zfoo456bar`, float64(123)},
		{`5e1b892d`, float64(50)},
		{`0xff`, float64(0)},
		{`123foo`, float64(123)},
		{`5e1a892d`, float64(50)},
		{`5e1f892d`, float64(50)},
	}

	simpleCases = []jsonCase{
		// Numbers should parse as int64 when possible
		{`1`, int64(1)},
		{`9223372036854775807`, int64(9223372036854775807)},
		{`-9223372036854775808`, int64(-9223372036854775808)},
		// Numbers too large to represent in float64 become infinity
		{`9e999`, math.Inf(1)},
		{`-9e999`, math.Inf(-1)},
		{hugeNumber, math.Inf(1)},
		{megaNumber, math.Inf(1)},
		// NaN and Inf[inity] special tokens are supported
		{`NaN`, math.NaN()},
		{`Inf`, math.Inf(1)},
		{`Infinity`, math.Inf(1)},
		{`-Inf`, math.Inf(-1)},
		{`-Infinity`, math.Inf(-1)},
		{`-NaN`, errors.New("strconv.ParseFloat: parsing \"-NaN\": invalid syntax")},
		{`01`, int64(1)},
		{`02.3`, float64(2.3)},
		{`-01`, int64(-1)},
		// Trailing decimal points are allowed
		{`1.`, float64(1)},
		{`1.e1`, float64(10)},
		// Leading decimal points are only allowed with a sign
		{`-.1`, float64(-.1)},
		// Sometimes the parser may be tricked into parsing a float where none
		// exists
		{`123foo`, errors.New("strconv.ParseFloat: parsing \"123f\": invalid syntax")},
		{`5e1a892d`, errors.New("strconv.ParseFloat: parsing \"5e1a892\": invalid syntax")},
		{`5e1f892d`, errors.New("strconv.ParseFloat: parsing \"5e1f892\": invalid syntax")},
		{`NaNa`, errors.New("strconv.ParseFloat: parsing \"NaNa\": invalid syntax")},
		// Invalid UTF-8 is not detected. Currently the ext parser does not
		// do any introspection of the validitiy of the UTF-8 text.
		// Understanding multi-byte codepoints is never required for parsing
		// valid data and adds additional cost.
		//   "  U+dc00      U+d800    "
		{"\"\xed\xb0\x80\xed\xa0\x80\"", "\xed\xb0\x80\xed\xa0\x80"},
		// invalid UTF-8 byte
		{"\"\xff\"", "\xff"},
		// Overlong encoding is also passed through (this is the 3-byte overlong
		// encoding of the nul character and the character '!')
		{"\"\xe0\x80\x80\xe0\x80\xa1\"", "\xe0\x80\x80\xe0\x80\xa1"},
		{nestedArrayJSON(501), errors.New("simple json: maximum nesting depth exceeded")},
	}
	simpleCasesUnmarshaling = []jsonCase{
		// Errors on all data after a top-level value has ended
		{`1,2`, errors.New("simple json: remainder of buffer not empty")},
		{`[1]2`, errors.New("simple json: remainder of buffer not empty")},
		{`"foo"{}bar`, errors.New("simple json: remainder of buffer not empty")},
		{`123zfoo456bar`, errors.New("simple json: remainder of buffer not empty")},
		{`5e1b892d`, errors.New("simple json: remainder of buffer not empty")},
		{`0xff`, errors.New("simple json: remainder of buffer not empty")},
		{`NaNvvvvv`, errors.New("simple json: remainder of buffer not empty")},
		{`Infrared`, errors.New("simple json: remainder of buffer not empty")},
		{`{}foobar`, errors.New("simple json: remainder of buffer not empty")},
	}
	simpleCasesStreaming = []jsonCase{
		// Ignores all data after a top-level value has ended
		{`1,2`, int64(1)},
		{`[1]2`, []any{int64(1)}},
		{`"foo"{}bar`, "foo"},
		// ...including in numbers
		{`123zfoo456bar`, int64(123)},
		{`5e1b892d`, float64(50)},
		{`0xff`, int64(0)},
		{`NaNvvvvv`, math.NaN()},
		{`Infrared`, math.Inf(1)},
		// Empty map with trailing data
		{`{}foobar`, map[string]any(nil)},
	}
)

// Demonstrate that the old and new parsers have the same behavior
func TestExtUnmarshalBehavior(t *testing.T) {
	t.Run("stdlib parser", func(t *testing.T) {
		testBehavior(t, json.Unmarshal, json.Marshal,
			options{
				tolerateDifferentErrorMessages: true,
				tolerateMapNil:                 true,
			},
			standardCases,
			standardBehaviorForExtCases,
			standardBehaviorForExtCasesUnmarshaling,
			standardOnlyCornerCases,
		)
	})
	t.Run("stdlib parser streaming", func(t *testing.T) {
		streamUnmarshal := func(data []byte, dest any) error {
			return json.NewDecoder(bytes.NewBuffer(data)).Decode(dest)
		}
		streamMarshal := func(v any) ([]byte, error) {
			var b bytes.Buffer
			err := json.NewEncoder(&b).Encode(v)
			return b.Bytes(), err
		}
		testBehavior(t, streamUnmarshal, streamMarshal,
			options{
				tolerateDifferentErrorMessages: true,
				tolerateMapNil:                 true,
			},
			standardCases,
			standardBehaviorForExtCases,
			standardBehaviorForExtCasesStreaming,
			standardOnlyCornerCases,
		)
	})
	t.Run("simple jsonext parser", func(t *testing.T) {
		unmarshalSimple := func(b []byte, dest interface{}) (err error) {
			*(dest.(*any)), err = simplejsonext.Unmarshal(b)
			return
		}
		testBehavior(t, unmarshalSimple, simplejsonext.Marshal,
			options{tolerateFloatToIntRoundTrip: true},
			standardCases,
			simpleCases,
			simpleCasesUnmarshaling,
		)
	})
	t.Run("simple jsonext parser streaming", func(t *testing.T) {
		streamUnmarshal := func(data []byte, dest any) (err error) {
			*(dest.(*any)), err = simplejsonext.NewParser(bytes.NewBuffer(data)).Parse()
			return
		}
		streamMarshal := func(v any) ([]byte, error) {
			var b bytes.Buffer
			err := simplejsonext.NewEmitter(&b).Emit(v)
			return b.Bytes(), err
		}
		testBehavior(t,
			streamUnmarshal,
			streamMarshal,
			options{tolerateFloatToIntRoundTrip: true},
			standardCases,
			simpleCases,
			simpleCasesStreaming,
		)
	})
}
