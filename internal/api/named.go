package api

import "fmt"

// mentionWords says in words how often the log mentions a person or a place. mentions and lastDay are the
// values of a row of the list as the template sees them: a JSON number and a day, or nothing.
func mentionWords(mentions, lastDay any) string {
	n, _ := mentions.(float64)
	if n < 1 {
		return "not mentioned yet"
	}
	words := fmt.Sprintf("mentioned on %d pages", int(n))
	if n == 1 {
		words = "mentioned on 1 page"
	}
	if day, _ := lastDay.(string); day != "" {
		words += ", last " + shortDayYear(day)
	}
	return words
}
