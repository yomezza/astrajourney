package naming

import "testing"

func TestUnicodeNames(t *testing.T) {
	a, key, err := Normalize("  Ａstra   Straße  ", 40)
	if err != nil || a != "Astra Straße" || key != "astra strasse" {
		t.Fatal(a, key, err)
	}
	if Key("Е\u0308лка") != Key("ЁЛКА") {
		t.Fatal("canonical Unicode mismatch")
	}
	for _, v := range []string{"aa", "☀☀☀", "https://site", "<img>", "Новая\u202eЗаря", "Новая\x00Заря", "www.game.ru", "administrator"} {
		if _, _, e := Normalize(v, 40); e == nil {
			t.Errorf("accepted %q", v)
		}
	}
}
