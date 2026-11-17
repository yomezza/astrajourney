package namedictionary

import "testing"

func TestExhaustedVocabulary10000(t *testing.T) {
	v := Vocabulary{Bases: []string{"Kepler"}, Suffixes: []string{"Harbor"}}
	seen := map[string]bool{}
	for i := int64(0); i < 10000; i++ {
		name := v.Name(i)
		if seen[name] {
			t.Fatalf("duplicate at %d: %s", i, name)
		}
		seen[name] = true
	}
}
