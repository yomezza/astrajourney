package knowledge

import "testing"

func TestSafeMarkdown(t *testing.T) {
	for _, v := range []string{"<script>alert(1)</script>", "[x](javascript:alert)", "![x](https://site/image.png)", "[x](file:/etc/passwd)", "![x](data:svg)"} {
		if _, e := Parse(v); e == nil {
			t.Errorf("accepted %q", v)
		}
	}
	blocks, e := Parse("## Heading\nText **bold** and *emphasis* `literal`.\n- [Iron](game:resource:iron)")
	if e != nil || len(blocks) != 3 || blocks[0].Type != "heading" {
		t.Fatal(blocks, e)
	}
}
