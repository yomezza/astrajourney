// Package knowledge owns the published guide, immutable revisions and private media.
package knowledge

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
)

type Inline struct {
	Type     string   `json:"type"`
	Text     string   `json:"text,omitempty"`
	Target   string   `json:"target,omitempty"`
	Children []Inline `json:"children,omitempty"`
}
type Block struct {
	Type    string   `json:"type"`
	Level   int      `json:"level,omitempty"`
	Content []Inline `json:"content"`
}

var markdownLink = regexp.MustCompile(`^(!?)\[([^\]\n]{0,300})\]\(([^)\s]{1,160})\)`)

// Parse produces a small allowlisted AST, never HTML. Public and admin preview
// share this parser; links are typed identifiers, never arbitrary URLs.
func Parse(source string) ([]Block, error) {
	if !utf8.ValidString(source) || len(source) > 262144 {
		return nil, errors.New("Слишком большой или некорректный Markdown")
	}
	if strings.ContainsAny(source, "<>\x00") {
		return nil, errors.New("HTML в статьях запрещён")
	}
	out := []Block{}
	code := false
	for _, line := range strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "```") {
			code = !code
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(out) >= 3000 {
			return nil, errors.New("Слишком много блоков статьи")
		}
		block := Block{Type: "paragraph"}
		if code {
			block.Type = "code"
			block.Content = []Inline{{Type: "text", Text: line}}
			out = append(out, block)
			continue
		}
		for i := 4; i >= 2; i-- {
			prefix := strings.Repeat("#", i) + " "
			if strings.HasPrefix(line, prefix) {
				block.Type = "heading"
				block.Level = i
				line = strings.TrimPrefix(line, prefix)
				break
			}
		}
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			block.Type = "list_item"
			line = line[2:]
		}
		content, err := parseInline(line, 0)
		if err != nil {
			return nil, err
		}
		block.Content = content
		out = append(out, block)
		if len(out) > 3000 {
			return nil, errors.New("Слишком много блоков статьи")
		}
	}
	if code {
		return nil, errors.New("Незакрытый блок кода")
	}
	return out, nil
}
func parseInline(s string, depth int) ([]Inline, error) {
	if depth > 4 {
		return nil, errors.New("Слишком глубокая разметка")
	}
	out := []Inline{}
	text := func(s string) {
		if s != "" {
			out = append(out, Inline{Type: "text", Text: s})
		}
	}
	for len(s) > 0 {
		if m := markdownLink.FindStringSubmatch(s); m != nil {
			kind := "link"
			if m[1] == "!" {
				kind = "image"
			}
			target := m[3]
			valid := false
			if strings.HasPrefix(target, "article:") {
				valid = kind == "link" && identifiers.ValidUUID(strings.TrimPrefix(target, "article:"))
			}
			if strings.HasPrefix(target, "game:") {
				valid = kind == "link" && regexp.MustCompile(`^game:(resource|ship|object|technical):[a-z0-9-]+$`).MatchString(target)
			}
			if strings.HasPrefix(target, "media:") {
				valid = kind == "image" && identifiers.ValidUUID(strings.TrimPrefix(target, "media:"))
			}
			if strings.HasPrefix(target, "builtin:") {
				valid = kind == "image" && regexp.MustCompile(`^builtin:[a-z0-9/_-]+$`).MatchString(target)
			}
			if !valid {
				return nil, errors.New("Используйте ссылки article:, game:, media: или builtin:")
			}
			out = append(out, Inline{Type: kind, Text: m[2], Target: target})
			s = s[len(m[0]):]
			continue
		}
		matched := false
		for _, delim := range []string{"**", "*", "`"} {
			if strings.HasPrefix(s, delim) {
				if end := strings.Index(s[len(delim):], delim); end >= 0 {
					body := s[len(delim) : len(delim)+end]
					kind := "emphasis"
					if delim == "**" {
						kind = "strong"
					}
					if delim == "`" {
						kind = "code"
					}
					children := []Inline{{Type: "text", Text: body}}
					var err error
					if delim != "`" {
						children, err = parseInline(body, depth+1)
					}
					if err != nil {
						return nil, err
					}
					out = append(out, Inline{Type: kind, Children: children})
					s = s[len(delim)*2+end:]
					matched = true
					break
				}
			}
		}
		if matched {
			continue
		}
		end := 1
		for end < len(s) && !strings.ContainsRune("[!*`", rune(s[end])) {
			end++
		}
		text(s[:end])
		s = s[end:]
	}
	return out, nil
}

func references(blocks []Block) []Inline {
	result := []Inline{}
	var visit func([]Inline)
	visit = func(items []Inline) {
		for _, v := range items {
			if v.Type == "image" || v.Type == "link" {
				result = append(result, v)
			}
			visit(v.Children)
		}
	}
	for _, b := range blocks {
		visit(b.Content)
	}
	return result
}
