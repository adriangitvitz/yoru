package stdlib

import (
	"strings"

	"github.com/adriangitvitz/yoru/interpreter"
)

// FuzzyProvider implements the Fuzzy effect namespace with a 4-level
// progressive matcher: exact, trim trailing, trim all, unicode-normalized.
// At trim-all and above, the replacement is re-indented to match the
// haystack's leading whitespace.
type FuzzyProvider struct{}

func (p *FuzzyProvider) EffectName() string { return "Fuzzy" }

func (p *FuzzyProvider) Methods() map[string]interpreter.Value {
	return map[string]interpreter.Value{
		"find_replace": builtin("Fuzzy.find_replace", p.findReplace),
	}
}

func (p *FuzzyProvider) findReplace(args []interpreter.Value) (interpreter.Value, error) {
	if len(args) != 4 {
		return fuzzyErr("fuzzy_bad_args", "Fuzzy.find_replace(content, old, new, count) takes 4 arguments"), nil
	}
	content, ok := args[0].(*interpreter.StringVal)
	if !ok {
		return fuzzyErr("fuzzy_bad_args", "content must be a String"), nil
	}
	oldText, ok := args[1].(*interpreter.StringVal)
	if !ok {
		return fuzzyErr("fuzzy_bad_args", "old must be a String"), nil
	}
	newText, ok := args[2].(*interpreter.StringVal)
	if !ok {
		return fuzzyErr("fuzzy_bad_args", "new must be a String"), nil
	}
	countV, ok := args[3].(*interpreter.IntVal)
	if !ok {
		return fuzzyErr("fuzzy_bad_args", "count must be an Int"), nil
	}

	if oldText.V == "" {
		return fuzzyErr("fuzzy_empty_needle", "search text is empty"), nil
	}

	max := int(countV.V)
	if max < 0 {
		max = 0
	}

	result, level, reps, ok := fuzzyReplaceLoop(content.V, oldText.V, newText.V, max)
	if !ok {
		return fuzzyErr("fuzzy_no_match", "could not find text to replace"), nil
	}

	return &interpreter.ObjectVal{
		TypeName: "FuzzyResult",
		Fields: map[string]interpreter.Value{
			"result":       &interpreter.StringVal{V: result},
			"match_level":  &interpreter.StringVal{V: level.String()},
			"replacements": &interpreter.IntVal{V: int64(reps)},
		},
	}, nil
}

type matchLevel int

const (
	levelExact             matchLevel = 1
	levelTrimTrailing      matchLevel = 2
	levelTrimAll           matchLevel = 3
	levelUnicodeNormalized matchLevel = 4
)

func (l matchLevel) String() string {
	switch l {
	case levelExact:
		return "exact"
	case levelTrimTrailing:
		return "trim_trailing"
	case levelTrimAll:
		return "trim_all"
	case levelUnicodeNormalized:
		return "unicode_normalized"
	}
	return "unknown"
}

// fuzzyReplaceLoop runs find/replace iteratively; count=0 means all matches.
// The worst match level across replacements is returned.
func fuzzyReplaceLoop(content, oldText, newText string, count int) (string, matchLevel, int, bool) {
	current := content
	trailingNL := strings.HasSuffix(content, "\n")
	worst := levelExact
	reps := 0
	needleLines := toLines(oldText)

	for {
		if count > 0 && reps >= count {
			break
		}
		hayLines := toLines(current)
		start, level, found := fuzzyFindLines(hayLines, needleLines)
		if !found {
			break
		}
		if level > worst {
			worst = level
		}

		var spliceIn []string
		if level >= levelTrimAll {
			base := detectIndent(hayLines[start])
			spliceIn = applyIndent(newText, base)
		} else {
			spliceIn = toLines(newText)
		}

		out := make([]string, 0, len(hayLines)-len(needleLines)+len(spliceIn))
		out = append(out, hayLines[:start]...)
		out = append(out, spliceIn...)
		out = append(out, hayLines[start+len(needleLines):]...)
		current = fromLines(out, trailingNL)
		reps++
	}
	if reps == 0 {
		return "", 0, 0, false
	}
	return current, worst, reps, true
}

func fuzzyFindLines(haystack, needle []string) (int, matchLevel, bool) {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return 0, 0, false
	}
	comparators := []struct {
		level matchLevel
		cmp   func(a, b string) bool
	}{
		{levelExact, func(a, b string) bool { return a == b }},
		{levelTrimTrailing, func(a, b string) bool {
			return strings.TrimRight(a, " \t") == strings.TrimRight(b, " \t")
		}},
		{levelTrimAll, func(a, b string) bool {
			return strings.TrimSpace(a) == strings.TrimSpace(b)
		}},
		{levelUnicodeNormalized, func(a, b string) bool {
			return normalizeUnicode(strings.TrimSpace(a)) == normalizeUnicode(strings.TrimSpace(b))
		}},
	}
	for _, c := range comparators {
		if pos, ok := findWith(haystack, needle, c.cmp); ok {
			return pos, c.level, true
		}
	}
	return 0, 0, false
}

func findWith(haystack, needle []string, cmp func(a, b string) bool) (int, bool) {
	w := len(needle)
	for start := 0; start <= len(haystack)-w; start++ {
		match := true
		for i := range needle {
			if !cmp(haystack[start+i], needle[i]) {
				match = false
				break
			}
		}
		if match {
			return start, true
		}
	}
	return 0, false
}

func toLines(s string) []string {
	if s == "" {
		return nil
	}
	trimmed := strings.TrimSuffix(s, "\n")
	return strings.Split(trimmed, "\n")
}

func fromLines(lines []string, withTrailing bool) string {
	joined := strings.Join(lines, "\n")
	if withTrailing && joined != "" {
		joined += "\n"
	}
	return joined
}

func detectIndent(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	return line[:len(line)-len(trimmed)]
}

// applyIndent rebases text onto baseIndent, preserving each line's indent
// relative to the first line.
func applyIndent(text, baseIndent string) []string {
	lines := toLines(text)
	if len(lines) == 0 {
		return nil
	}
	replBase := detectIndent(lines[0])
	out := make([]string, len(lines))
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			out[i] = ""
			continue
		}
		lineIndent := detectIndent(line)
		var relative string
		if len(lineIndent) > len(replBase) {
			relative = lineIndent[len(replBase):]
		}
		out[i] = baseIndent + relative + trimmed
	}
	return out
}

// normalizeUnicode maps common smart-punctuation runes (smart quotes,
// dashes, nbsp, etc.) to ASCII equivalents.
func normalizeUnicode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, ch := range s {
		switch ch {
		case '‘', '’', '‚', '‹', '›':
			b.WriteByte('\'')
		case '“', '”', '„', '«', '»':
			b.WriteByte('"')
		case '–', '—', '―', '−':
			b.WriteByte('-')
		case ' ', ' ', ' ':
			b.WriteByte(' ')
		case '…':
			b.WriteString("...")
		case '×':
			b.WriteByte('*')
		case '→':
			b.WriteString("->")
		case '⇒':
			b.WriteString("=>")
		default:
			b.WriteRune(ch)
		}
	}
	return b.String()
}

func fuzzyErr(kind, message string) interpreter.Value {
	return &interpreter.EnumVal{
		TypeName: "Result",
		Variant:  "Err",
		Fields: map[string]interpreter.Value{
			"error": &interpreter.ObjectVal{
				TypeName: "Error",
				Fields: map[string]interpreter.Value{
					"kind":    &interpreter.StringVal{V: kind},
					"message": &interpreter.StringVal{V: message},
				},
			},
		},
	}
}
