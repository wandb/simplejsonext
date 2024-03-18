# Simple extended JSON parser

This is prospective open-source code for a simple extended-JSON parser. It
understands (and emits) JSON containing `Infinity` and `NaN` numbers, and only
decodes simply typed values; it does not know how to decode JSON into structs
and their fields.
