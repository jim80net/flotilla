package voice

import "testing"

func TestNormalizeTranscript(t *testing.T) {
	cases := map[string]string{
		`"Floatilla Voice - Check one, two, three.`: "Floatilla Voice - Check one, two, three.", // leading-quote artifact (the live probe)
		"  hello  ":     "hello",        // surrounding whitespace
		`"quoted both"`: `quoted both"`, // ONLY the leading quote stripped (trailing preserved)
		`"hi" there`:    `hi" there`,    // not over-stripped as a matched pair
		"no quotes":     "no quotes",    // untouched
		`he said "hi"`:  `he said "hi"`, // interior quotes preserved
		"":              "",             // empty
		`"`:             "",             // a lone stray quote
	}
	for in, want := range cases {
		if got := normalizeTranscript(in); got != want {
			t.Errorf("normalizeTranscript(%q) = %q, want %q", in, got, want)
		}
	}
}
