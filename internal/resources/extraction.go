package resourcecatalog

import (
	"fmt"
	"time"
)

func Concentration(star, object int) int { return 35 + (star*17+object*11)%61 }

func Quality(star, object int) string { return fmt.Sprintf("Q%d", 1+(star*13+object*7)%5) }

func ExtractorAccepts(e, res string) bool {
	if e == "none" {
		return false
	}
	if e == "universal" {
		return true
	}
	if e == "ice" {
		return res == "water" || res == "azoth"
	}
	if e == "gas" {
		return res == "agni" || res == "carbon"
	}
	return res != "water" && res != "azoth" && res != "agni" && res != "manna" && res != "ambrosia"
}

func ExtractorRange(e string) (int, int) {
	if e == "universal" {
		return 3, 3
	}
	return 4, 6
}

func ExtractorYield(e string, star, obj int) int {
	lo, hi := ExtractorRange(e)
	if lo == hi {
		return lo
	}
	grade := 1 + (star*13+obj*7)%5
	return lo + min(hi-lo, (Concentration(star, obj)/20+grade)/4)
}

type Quota struct {
	Capacity  int       `json:"capacity"`
	Remaining int       `json:"remaining"`
	ResetAt   time.Time `json:"reset_at"`
	Start     time.Time `json:"-"`
}
