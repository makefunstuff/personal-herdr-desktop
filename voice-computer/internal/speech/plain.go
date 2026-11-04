// Package speech turns an agent's chat reply (markdown) into a few plain words to speak: no code,
// paths, IDs, links or logs, and at most two short sentences.
package speech

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	fenceRE     = regexp.MustCompile("(?s)(```|~~~).*?(```|~~~|$)")
	tagRE       = regexp.MustCompile(`</?[A-Za-z][\w-]*(\s[^<>]*)?/?>`)
	imageRE     = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	linkRE      = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	urlRE       = regexp.MustCompile(`\b(https?|ftp|file)://\S+|\bwww\.\S+`)
	inlineRE    = regexp.MustCompile("`+([^`]*)`+")
	plainCodeRE = regexp.MustCompile(`^[A-Za-z][A-Za-z' ]{0,30}$`)
	logLineRE   = regexp.MustCompile(`^((?i:error|fatal|panic|warning)(\[[^\]]*\])?:|\d{4}-\d\d-\d\d|\d\d:\d\d:\d\d|\[(INFO|WARN|WARNING|ERROR|DEBUG|TRACE)\]|(INFO|WARN|ERROR|DEBUG|FATAL|panic|goroutine|Traceback|at |\$ |\+ |diff --git|@@|---|\+\+\+))`)
	headingRE   = regexp.MustCompile(`^#{1,6}\s+`)
	bulletRE    = regexp.MustCompile(`^([-*+•]|\d+[.)])\s+`)
	ruleRE      = regexp.MustCompile(`^([-*_=]\s*){3,}$`)
	parenIDsRE  = regexp.MustCompile(`\(\s*(([A-Z][A-Z0-9]+-\d+|#\d+|PR #?\d+|[0-9a-f]{7,40})[\s,;/]*(and\s+)?)+\)`)
	prRE        = regexp.MustCompile(`\b(PR|pull request|issue)\s+#?\d+\b`)
	hashNumRE   = regexp.MustCompile(`#\d+\b`)
	taskIDRE    = regexp.MustCompile(`\b[A-Z][A-Z0-9]{1,9}-\d+\b`)
	uuidRE      = regexp.MustCompile(`\b[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}\b`)
	hashRE      = regexp.MustCompile(`[\w-]*\b[0-9a-f]{7,64}\b`)
	danglingRE  = regexp.MustCompile(`\s+(in|on|at|into|from|as|to|for|with|via|by|see)\s*\x01[\s\x01]*([.,;:!?]|$|\s(?:into|to|in|on|at|for|from)\b)`) // a word left hanging by a cut
	emphasisRE  = regexp.MustCompile(`\*\*|__|(^|\s)[*_]|[*_]($|[\s.,;:!?])`)
	spaceRE     = regexp.MustCompile(`\s+`)
	beforePunct = regexp.MustCompile(`\s+([.,;:!?])`)
	emptyParen  = regexp.MustCompile(`\(\s*[,;]?\s*\)|\[\s*\]`)
	repeatPunct = regexp.MustCompile(`([,;:])\s*[,;:.]+|\.(\s*\.)+`)
	leadPunct   = regexp.MustCompile(`^[\s,;:.)\]-]+`)
)

// cut marks where something was left out (a path, a link, an id), so a word left hanging there goes too.
const cut = " \x01 "

// Upper-case prefixes of names with a number that aren't task ids.
var notIDs = map[string]bool{"UTF": true, "GPT": true, "ISO": true, "RFC": true, "SHA": true, "MP": true, "COVID": true, "HTTP": true, "IPV": true, "WPA": true}

// Most words a sentence keeps, and most characters in all.
const (
	maxWords = 28
	maxChars = 320
)

// Plain is the reply as speech: its first two short sentences, or "" when nothing speakable is left.
func Plain(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = fenceRE.ReplaceAllString(text, "\n")
	text = tagRE.ReplaceAllString(text, " ")
	var parts []string
	for _, line := range strings.Split(text, "\n") {
		if l := plainLine(line); l != "" {
			parts = append(parts, l)
		}
	}
	return firstSentences(strings.Join(parts, " "), 2)
}

// plainLine is one markdown line as plain words, ending a sentence; "" for code, logs, tables and rules.
func plainLine(line string) string {
	if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
		if !bulletRE.MatchString(strings.TrimSpace(line)) {
			return "" // indented code
		}
	}
	l := strings.TrimSpace(line)
	if l == "" || strings.HasPrefix(l, "|") || ruleRE.MatchString(l) || logLineRE.MatchString(l) {
		return ""
	}
	l = strings.TrimLeft(l, "> ")
	l = headingRE.ReplaceAllString(l, "")
	l = bulletRE.ReplaceAllString(l, "")
	l = imageRE.ReplaceAllString(l, cut)
	l = linkRE.ReplaceAllString(l, "$1")
	l = urlRE.ReplaceAllString(l, cut)
	l = inlineRE.ReplaceAllStringFunc(l, func(m string) string {
		inner := strings.Trim(m, "`")
		if plainCodeRE.MatchString(inner) && !strings.Contains(inner, "_") {
			return inner // a plain word or two, like a phrase to say
		}
		return cut
	})
	l = emphasisRE.ReplaceAllStringFunc(l, func(m string) string { return strings.Trim(m, "*_") })
	l = parenIDsRE.ReplaceAllString(l, cut)
	l = prRE.ReplaceAllString(l, "the $1")
	l = hashNumRE.ReplaceAllString(l, cut)
	l = uuidRE.ReplaceAllString(l, cut)
	l = taskIDRE.ReplaceAllStringFunc(l, func(m string) string {
		if notIDs[m[:strings.Index(m, "-")]] {
			return m // UTF-8, GPT-4: names, not task ids
		}
		return "the task"
	})
	l = hashRE.ReplaceAllStringFunc(l, func(m string) string {
		h := m[strings.LastIndexAny(m, "_-")+1:]
		if strings.ContainsAny(h, "0123456789") && strings.ContainsAny(h, "abcdef") {
			return cut // a commit or other hash
		}
		return m
	})
	var words []string
	for _, w := range strings.Fields(l) {
		if codeWord(w) {
			w = cut + w[len(strings.TrimRight(w, ".,;:!?")):] // its sentence still ends
		}
		words = append(words, w)
	}
	l = strings.Join(words, " ")
	l = danglingRE.ReplaceAllString(l, "$2")
	l = clean(l)
	if !hasLetters(l) {
		return ""
	}
	if r := []rune(l); !strings.ContainsRune(".!?:", r[len(r)-1]) {
		l += "."
	}
	return l
}

// codeWord says whether a word is a path, a file name, an option, an identifier or a log fragment.
func codeWord(w string) bool {
	core := strings.TrimRight(strings.TrimLeft(w, `("'[`), `.,;:!?)"']`)
	if core == "" {
		return false
	}
	switch {
	case strings.HasPrefix(core, "~/"), strings.HasPrefix(core, "./"), strings.HasPrefix(core, "../"),
		strings.HasPrefix(core, "/") && len(core) > 1, strings.HasPrefix(core, "--"), strings.HasPrefix(core, "$"):
		return true
	case strings.Count(core, "/") >= 1 && !strings.Contains(core, " ") && len(core) > 3 && !isAndOr(core):
		return true // a/b/c, src/main.go
	case strings.ContainsAny(core, "_=\\{}<>|`@") || strings.Contains(core, "::") || strings.Contains(core, "()"):
		return true
	case fileName(core):
		return true
	case len(core) >= 20 && strings.ContainsAny(core, "0123456789") && hasLetters(core):
		return true // a token or key
	}
	return false
}

// isAndOr keeps "and/or", "yes/no", "on/off" and the like, which are words.
func isAndOr(w string) bool {
	parts := strings.Split(w, "/")
	if len(parts) != 2 {
		return false
	}
	for _, p := range parts {
		if len(p) == 0 || len(p) > 6 || !isAlnum(p) {
			return false
		}
	}
	return true
}

var fileExts = map[string]bool{"go": true, "ts": true, "tsx": true, "md": true, "json": true,
	"jsonc": true, "yaml": true, "yml": true, "toml": true, "py": true, "sh": true, "lua": true, "qml": true, "rs": true,
	"c": true, "h": true, "cpp": true, "html": true, "css": true, "txt": true, "log": true, "sock": true, "service": true,
	"conf": true, "cfg": true, "ini": true, "lock": true, "mod": true, "sum": true, "png": true, "jpg": true, "wav": true,
	"apk": true, "gguf": true, "env": true, "key": true, "pem": true, "sql": true, "db": true, "zip": true, "tar": true, "gz": true}

// fileName says whether w looks like name.ext (with a known extension) or a dotted identifier (a.b.c).
func fileName(w string) bool {
	i := strings.LastIndex(w, ".")
	if i <= 0 || i == len(w)-1 {
		return false
	}
	if fileExts[strings.ToLower(w[i+1:])] {
		return true
	}
	// x.y.z with letters: a dotted name (app.plugin, some_app.settings), not a number like 1.5
	return strings.Count(w, ".") >= 2 && hasLetters(w) && !strings.ContainsAny(w, " ")
}

func isAlnum(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func hasLetters(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// clean drops symbols speech can't say (emoji among them) and tidies the spaces and punctuation left.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), unicode.IsSpace(r):
			return r
		case strings.ContainsRune(".,;:!?'\"()-%&+", r):
			return r
		case r == '’' || r == '‘':
			return '\''
		case r == '“' || r == '”':
			return '"'
		case r == '—' || r == '–':
			return ','
		}
		return ' '
	}, s)
	for i := 0; i < 2; i++ {
		s = emptyParen.ReplaceAllString(s, " ")
		s = spaceRE.ReplaceAllString(s, " ")
		s = beforePunct.ReplaceAllString(s, "$1")
		s = repeatPunct.ReplaceAllString(s, "$1")
	}
	s = leadPunct.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// firstSentences keeps the first n sentences, each cut to maxWords, and capitalises each.
func firstSentences(s string, n int) string {
	var out []string
	total := 0
	for _, sent := range sentences(s) {
		if len(out) == n {
			break
		}
		sent = shorten(sent)
		if sent == "" {
			continue
		}
		r := []rune(sent)
		r[0] = unicode.ToUpper(r[0])
		sent = string(r)
		if total > 0 && total+len(sent) > maxChars {
			break
		}
		out = append(out, sent)
		total += len(sent) + 1
	}
	return strings.Join(out, " ")
}

// sentences splits at . ! ? (and a closing colon) followed by a space, but not after e.g. or i.e.
func sentences(s string) []string {
	var out []string
	start := 0
	rs := []rune(s)
	for i, r := range rs {
		if !strings.ContainsRune(".!?:", r) || (i+1 < len(rs) && rs[i+1] != ' ') {
			continue
		}
		word := strings.ToLower(lastWord(string(rs[start : i+1])))
		if word == "e.g." || word == "i.e." || word == "etc." || word == "vs." || word == "mr." || word == "dr." {
			continue
		}
		if r == ':' {
			continue // a label ("Short:") runs on into what it introduces
		}
		if sent := strings.TrimSpace(string(rs[start : i+1])); sent != "" {
			out = append(out, sent)
		}
		start = i + 1
	}
	if rest := strings.TrimSpace(string(rs[start:])); rest != "" && hasLetters(rest) {
		out = append(out, rest)
	}
	return out
}

func lastWord(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

// shorten cuts a long sentence at a comma or dash before maxWords, else at maxWords, and ends it with a full stop.
func shorten(sent string) string {
	words := strings.Fields(sent)
	if len(words) <= maxWords {
		return sent
	}
	cut := maxWords
	for i := maxWords - 1; i >= maxWords/2; i-- {
		if strings.HasSuffix(words[i], ",") || strings.HasSuffix(words[i], ";") || strings.HasSuffix(words[i], ":") {
			cut = i + 1
			break
		}
	}
	out := strings.TrimRight(strings.Join(words[:cut], " "), ",;:-")
	return out + "."
}
