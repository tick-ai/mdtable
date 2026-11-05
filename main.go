// Command mdtable aligns and repairs the Markdown tables of files, in place,
// the way GitHub reads them, and changes no other byte of the files.
package main

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const usageLine = "usage: mdtable [--check | --write] [--] FILE..."

const usageText = usageLine + `

Aligns and repairs the Markdown tables of FILE..., the way GitHub reads them.
No byte outside the tables is changed.

  --check   change nothing; print file:line of each table that would change;
            exit 1 if there is one
  --write   rewrite each file whose content would change, in place
  --        every argument after it is a FILE
  -         standard input (once, and not with --write)
  -h, --help
            print this text and exit 0

Without --check or --write the new content of every file goes to standard output.

Exit status: 0 nothing to report, 1 --check found a table to change,
2 usage error or an unreadable or unwritable file.
`

var (
	reK1       = regexp.MustCompile(`(?i)^<(pre|script|style|textarea)([ \t>]|$)`)
	reK1End    = regexp.MustCompile(`(?i)</(pre|script|style|textarea)>`)
	reK4       = regexp.MustCompile(`^<![A-Za-z]`)
	reK6       = regexp.MustCompile(`(?i)^</?(address|article|aside|base|basefont|blockquote|body|caption|center|col|colgroup|dd|details|dialog|dir|div|dl|dt|fieldset|figcaption|figure|footer|form|frame|frameset|h1|h2|h3|h4|h5|h6|head|header|hr|html|iframe|legend|li|link|main|menu|menuitem|nav|noframes|ol|optgroup|option|p|param|search|section|summary|table|tbody|td|tfoot|th|thead|title|tr|track|ul)([ \t]|/?>|$)`)
	reK7       = regexp.MustCompile("^(<[A-Za-z][A-Za-z0-9-]*([ \\t]+[A-Za-z_:][A-Za-z0-9_.:-]*([ \\t]*=[ \\t]*([^ \\t\"'=<>`]+|'[^']*'|\"[^\"]*\"))?)*[ \\t]*/?>|</[A-Za-z][A-Za-z0-9-]*[ \\t]*>)[ \\t]*$")
	reATX      = regexp.MustCompile(`^#{1,6}([ \t]|$)`)
	reSetext   = regexp.MustCompile(`^(=+|-+)[ \t]*$`)
	reThematic = regexp.MustCompile(`^((\*[ \t]*){3,}|(-[ \t]*){3,}|(_[ \t]*){3,})$`)
	reList     = regexp.MustCompile(`^([-+*]|[0-9]{1,9}[.)])([ \t]|$)`)
	reDelim    = regexp.MustCompile(`^:?-+:?$`)
)

// line is one line of a file: its text without the line ending, and the line
// ending it had ("\n", "\r\n" or "" for a last line without one).
type line struct {
	text string
	eol  string
}

func splitLines(s string) []line {
	var ls []line
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			ls = append(ls, line{s, ""})
			break
		}
		text, eol := s[:i], "\n"
		if strings.HasSuffix(text, "\r") {
			text, eol = text[:len(text)-1], "\r\n"
		}
		ls = append(ls, line{text, eol})
		s = s[i+1:]
	}
	return ls
}

// lead measures the run of spaces and tabs at the start of s.
func lead(s string) (n, spaces int, tab bool) {
	for n < len(s) && (s[n] == ' ' || s[n] == '\t') {
		if s[n] == '\t' {
			tab = true
		} else {
			spaces++
		}
		n++
	}
	return n, spaces, tab
}

// classify tells whether a line is blank or indented, and gives its rest
// (the line without its 0 to 3 leading spaces).
func classify(s string) (blank, indented bool, rest string) {
	n, spaces, tab := lead(s)
	if n == len(s) {
		return true, false, ""
	}
	if tab || spaces >= 4 {
		return false, true, ""
	}
	return false, false, s[n:]
}

func isBlank(s string) bool {
	b, _, _ := classify(s)
	return b
}

func trim(s string) string {
	return strings.Trim(s, " \t")
}

// fenceOpen tells whether rest opens a fenced code block.
func fenceOpen(rest string) (ch byte, n int, ok bool) {
	if rest == "" || (rest[0] != '`' && rest[0] != '~') {
		return 0, 0, false
	}
	ch = rest[0]
	for n < len(rest) && rest[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0, false
	}
	if ch == '`' && strings.IndexByte(rest[n:], '`') >= 0 {
		return 0, 0, false
	}
	return ch, n, true
}

func closesFence(text string, ch byte, n int) bool {
	blank, indented, rest := classify(text)
	if blank || indented {
		return false
	}
	k := 0
	for k < len(rest) && rest[k] == ch {
		k++
	}
	if k < n {
		return false
	}
	return trim(rest[k:]) == ""
}

// htmlOpen tells whether rest starts an HTML block: its kind (0 for none)
// and, for the kinds 1 to 5, whether it ends on that same line.
func htmlOpen(rest string, para bool) (kind int, oneLine bool) {
	if m := reK1.FindStringSubmatchIndex(rest); m != nil {
		return 1, reK1End.MatchString(rest[m[3]:])
	}
	switch {
	case strings.HasPrefix(rest, "<!--"):
		return 2, strings.Contains(rest[4:], "-->")
	case strings.HasPrefix(rest, "<?"):
		return 3, strings.Contains(rest[2:], "?>")
	case reK4.MatchString(rest):
		return 4, strings.Contains(rest[2:], ">")
	case strings.HasPrefix(rest, "<![CDATA["):
		return 5, strings.Contains(rest[9:], "]]>")
	case reK6.MatchString(rest):
		return 6, false
	case !para && reK7.MatchString(rest):
		return 7, false
	}
	return 0, false
}

func htmlEnds(kind int, text string) bool {
	switch kind {
	case 1:
		return reK1End.MatchString(text)
	case 2:
		return strings.Contains(text, "-->")
	case 3:
		return strings.Contains(text, "?>")
	case 4:
		return strings.Contains(text, ">")
	case 5:
		return strings.Contains(text, "]]>")
	}
	return false
}

func isASCIIPunct(c byte) bool {
	return (c >= '!' && c <= '/') || (c >= ':' && c <= '@') || (c >= '[' && c <= '`') || (c >= '{' && c <= '~')
}

// splitRow cuts a row (its rest) into trimmed cells.
func splitRow(s string) []string {
	s = trim(s)
	var pieces []string
	start := 0
	firstBorder, lastBorder := false, false
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && isASCIIPunct(s[i+1]):
			i += 2
		case c == '`':
			j := i
			for j < len(s) && s[j] == '`' {
				j++
			}
			size := j - i
			end := -1
			for k := j; k < len(s); {
				if s[k] != '`' {
					k++
					continue
				}
				m := k
				for m < len(s) && s[m] == '`' {
					m++
				}
				if m-k == size {
					end = m
					break
				}
				k = m
			}
			if end >= 0 {
				i = end
			} else {
				i = j
			}
		case c == '|':
			pieces = append(pieces, s[start:i])
			if i == 0 {
				firstBorder = true
			}
			lastBorder = i == len(s)-1
			start = i + 1
			i++
		default:
			i++
		}
	}
	pieces = append(pieces, s[start:])
	if firstBorder {
		pieces = pieces[1:]
	}
	if lastBorder {
		pieces = pieces[:len(pieces)-1]
	}
	cells := make([]string, len(pieces))
	for k, p := range pieces {
		cells[k] = trim(p)
	}
	return cells
}

// startsOther tells whether a non-blank, non-indented rest starts another
// block, so that it ends a table.
func startsOther(rest string) bool {
	if _, _, ok := fenceOpen(rest); ok {
		return true
	}
	if k, _ := htmlOpen(rest, false); k > 0 {
		return true
	}
	return reATX.MatchString(rest) || reThematic.MatchString(rest) ||
		reList.MatchString(rest) || strings.HasPrefix(rest, ">")
}

// tableAt tells whether the paragraph line i is the header row of a table,
// and where the table ends (the index after its last line).
func tableAt(lines []line, i int, hrest string) (end int, ok bool) {
	if i+1 >= len(lines) {
		return 0, false
	}
	_, indented, drest := classify(lines[i+1].text)
	if indented || !strings.Contains(drest, "|") || reList.MatchString(drest) {
		return 0, false
	}
	head := splitRow(hrest)
	delim := splitRow(drest)
	if len(delim) == 0 || len(delim) != len(head) {
		return 0, false
	}
	for _, c := range delim {
		if !reDelim.MatchString(c) {
			return 0, false
		}
	}
	k := i + 2
	for k < len(lines) {
		blank, indented, rest := classify(lines[k].text)
		if blank || indented || startsOther(rest) {
			break
		}
		k++
	}
	return k, true
}

type mode int

const (
	mTop mode = iota
	mCode
	mFence
	mHTMLEnd
	mHTMLBlank
	mContainer
)

type span struct{ start, end int }

// findTables reads the lines from index first on, and returns the tables.
func findTables(lines []line, first int) []span {
	var tables []span
	m := mTop
	para := false
	var fenceCh byte
	fenceLen := 0
	htmlKind := 0
	i := first
	for i < len(lines) {
		text := lines[i].text
		switch m {
		case mFence:
			if closesFence(text, fenceCh, fenceLen) {
				m = mTop
				para = false
			}
			i++
		case mCode:
			blank, indented, _ := classify(text)
			if blank || indented {
				i++
			} else {
				m = mTop
			}
		case mHTMLEnd:
			if htmlEnds(htmlKind, text) {
				m = mTop
				para = false
			}
			i++
		case mHTMLBlank:
			if isBlank(text) {
				m = mTop
			} else {
				i++
			}
		case mContainer:
			n, spaces, tab := lead(text)
			if n < len(text) {
				if spaces < 2 && !tab {
					rest := text[n:]
					_, _, fence := fenceOpen(rest)
					k, _ := htmlOpen(rest, false)
					if fence || k > 0 || reATX.MatchString(rest) || reThematic.MatchString(rest) {
						m = mTop
						continue
					}
				}
				i++
				continue
			}
			j := i + 1
			for j < len(lines) && isBlank(lines[j].text) {
				j++
			}
			if j < len(lines) {
				n2, spaces2, _ := lead(lines[j].text)
				if spaces2 >= 2 || (n2 > 0 && lines[j].text[0] == '\t') {
					i = j
					continue
				}
			}
			m = mTop
		default:
			blank, indented, rest := classify(text)
			switch {
			case blank:
				para = false
				i++
			case indented:
				if !para {
					m = mCode
				}
				i++
			default:
				if ch, n, ok := fenceOpen(rest); ok {
					m, fenceCh, fenceLen, para = mFence, ch, n, false
					i++
					continue
				}
				if k, one := htmlOpen(rest, para); k > 0 {
					para = false
					htmlKind = k
					switch {
					case k >= 6:
						m = mHTMLBlank
					case !one:
						m = mHTMLEnd
					}
					i++
					continue
				}
				if reATX.MatchString(rest) || (para && reSetext.MatchString(rest)) || reThematic.MatchString(rest) {
					para = false
					i++
					continue
				}
				if reList.MatchString(rest) || strings.HasPrefix(rest, ">") {
					m = mContainer
					para = false
					i++
					continue
				}
				if end, ok := tableAt(lines, i, rest); ok {
					tables = append(tables, span{i, end})
					i = end
					para = false
					continue
				}
				para = true
				i++
			}
		}
	}
	return tables
}

// charWidth is the display width of a character.
func charWidth(r rune) int {
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	switch {
	case r >= 0x1100 && r <= 0x115F,
		r >= 0x2E80 && r <= 0x303E,
		r >= 0x3041 && r <= 0x33FF,
		r >= 0x3400 && r <= 0x4DBF,
		r >= 0x4E00 && r <= 0x9FFF,
		r >= 0xA000 && r <= 0xA4CF,
		r >= 0xA960 && r <= 0xA97F,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE6F,
		r >= 0xFF01 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x1F300 && r <= 0x1F64F,
		r >= 0x1F680 && r <= 0x1F6FF,
		r >= 0x1F900 && r <= 0x1F9FF,
		r >= 0x20000 && r <= 0x3FFFD:
		return 2
	}
	return 1
}

func textWidth(s string) int {
	w := 0
	for _, r := range s {
		w += charWidth(r)
	}
	return w
}

type align int

const (
	alignNone align = iota
	alignLeft
	alignRight
	alignCenter
)

func alignOf(cell string) align {
	l := strings.HasPrefix(cell, ":")
	r := strings.HasSuffix(cell, ":")
	switch {
	case l && r:
		return alignCenter
	case r:
		return alignRight
	case l:
		return alignLeft
	}
	return alignNone
}

func pad(s string, width int, a align) string {
	d := width - textWidth(s)
	if d <= 0 {
		return s
	}
	switch a {
	case alignRight:
		return strings.Repeat(" ", d) + s
	case alignCenter:
		return strings.Repeat(" ", d/2) + s + strings.Repeat(" ", d-d/2)
	}
	return s + strings.Repeat(" ", d)
}

func delimCell(width int, a align) string {
	switch a {
	case alignLeft:
		return ":" + strings.Repeat("-", width-1)
	case alignRight:
		return strings.Repeat("-", width-1) + ":"
	case alignCenter:
		return ":" + strings.Repeat("-", width-2) + ":"
	}
	return strings.Repeat("-", width)
}

func joinRow(cells []string) string {
	return "| " + strings.Join(cells, " | ") + " |"
}

// formatTable gives the new text of each line of the table lines[t.start:t.end]
// and the (line number, cell count) of its rows with too many cells.
func formatTable(lines []line, t span) (texts []string, extra [][2]int, n int) {
	_, _, hrest := classify(lines[t.start].text)
	_, _, drest := classify(lines[t.start+1].text)
	head := splitRow(hrest)
	delim := splitRow(drest)
	n = len(head)
	aligns := make([]align, n)
	for j, c := range delim {
		aligns[j] = alignOf(c)
	}
	rows := make([][]string, 0, t.end-t.start-2)
	for k := t.start + 2; k < t.end; k++ {
		_, _, rest := classify(lines[k].text)
		cells := splitRow(rest)
		if len(cells) > n {
			extra = append(extra, [2]int{k + 1, len(cells)})
		}
		for len(cells) < n {
			cells = append(cells, "")
		}
		rows = append(rows, cells)
	}
	widths := make([]int, n)
	for j := range widths {
		widths[j] = 3
	}
	for _, row := range append([][]string{head}, rows...) {
		for j := 0; j < n; j++ {
			widths[j] = max(widths[j], textWidth(row[j]))
		}
	}
	build := func(cells []string) string {
		out := make([]string, 0, len(cells))
		for j, c := range cells {
			if j < n {
				c = pad(c, widths[j], aligns[j])
			}
			out = append(out, c)
		}
		return joinRow(out)
	}
	texts = append(texts, build(head))
	dcells := make([]string, n)
	for j := range dcells {
		dcells[j] = delimCell(widths[j], aligns[j])
	}
	texts = append(texts, joinRow(dcells))
	for _, row := range rows {
		texts = append(texts, build(row))
	}
	return texts, extra, n
}

type result struct {
	out     []byte
	changed []int    // line number of the header row of each table that changes
	warns   []string // the warning lines, without the final newline
}

var errUTF8 = errors.New("not valid UTF-8")

// process builds the new content of a file.
func process(name string, data []byte) (result, error) {
	var res result
	if !utf8.Valid(data) {
		return res, errUTF8
	}
	s := string(data)
	bom := ""
	if strings.HasPrefix(s, "\uFEFF") {
		bom = "\uFEFF"
		s = s[len(bom):]
	}
	lines := splitLines(s)
	first := 0
	if len(lines) > 0 && lines[0].text == "---" {
		for j := 1; j < len(lines); j++ {
			if lines[j].text == "---" {
				first = j + 1
				break
			}
		}
	}
	for _, t := range findTables(lines, first) {
		texts, extra, n := formatTable(lines, t)
		changed := false
		for k, txt := range texts {
			if lines[t.start+k].text != txt {
				changed = true
				lines[t.start+k].text = txt
			}
		}
		if changed {
			res.changed = append(res.changed, t.start+1)
		}
		for _, e := range extra {
			res.warns = append(res.warns, "mdtable: warning: "+name+":"+strconv.Itoa(e[0])+
				": row has "+strconv.Itoa(e[1])+" cells, the header has "+strconv.Itoa(n))
		}
	}
	var b strings.Builder
	b.Grow(len(data))
	b.WriteString(bom)
	for _, l := range lines {
		b.WriteString(l.text)
		b.WriteString(l.eol)
	}
	res.out = []byte(b.String())
	return res, nil
}

type options struct {
	check, write, help bool
	files              []string
}

// parseArgs reads the command line; the second result is the first usage
// error, or "".
func parseArgs(args []string) (options, string) {
	var o options
	problem := ""
	note := func(s string) {
		if problem == "" {
			problem = s
		}
	}
	dashes := 0
	rest := false
	for _, a := range args {
		switch {
		case rest:
			o.files = append(o.files, a)
		case a == "--":
			rest = true
		case a == "-":
			o.files = append(o.files, a)
		case a == "--check":
			o.check = true
		case a == "--write":
			o.write = true
		case a == "-h" || a == "--help":
			o.help = true
		case strings.HasPrefix(a, "-"):
			note("unknown option " + a)
		default:
			o.files = append(o.files, a)
		}
	}
	for _, f := range o.files {
		if f == "-" {
			dashes++
		}
	}
	if o.check && o.write {
		note("--check and --write cannot be used together")
	}
	if dashes > 1 {
		note("- (standard input) given more than once")
	}
	if dashes > 0 && o.write {
		note("- (standard input) cannot be used with --write")
	}
	if len(o.files) == 0 && !o.help {
		note("no FILE given")
	}
	return o, problem
}

func reason(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err.Error()
	}
	return err.Error()
}

// writeAtomic replaces the file at path by data, through a temporary file in
// the same folder that is renamed over it.
func writeAtomic(path string, data []byte) (err error) {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mdtable-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	done := false
	defer func() {
		if !done {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	_ = f.Sync()
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	done = true
	return nil
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	o, problem := parseArgs(args)
	if problem != "" {
		_, _ = io.WriteString(stderr, "mdtable: "+problem+"\n"+usageLine+"\n")
		return 2
	}
	if o.help {
		_, _ = io.WriteString(stdout, usageText)
		return 0
	}
	out := bufio.NewWriter(stdout)
	defer out.Flush()
	failed, listed := false, false
	for _, name := range o.files {
		var data []byte
		var err error
		if name == "-" {
			name = "<stdin>"
			data, err = io.ReadAll(stdin)
		} else {
			data, err = os.ReadFile(name)
		}
		if err != nil {
			failed = true
			_, _ = io.WriteString(stderr, "mdtable: "+name+": "+reason(err)+"\n")
			continue
		}
		res, err := process(name, data)
		if err != nil {
			failed = true
			_, _ = io.WriteString(stderr, "mdtable: "+name+": "+reason(err)+"\n")
			continue
		}
		for _, w := range res.warns {
			_, _ = io.WriteString(stderr, w+"\n")
		}
		switch {
		case o.check:
			for _, ln := range res.changed {
				listed = true
				_, _ = io.WriteString(out, name+":"+strconv.Itoa(ln)+"\n")
			}
		case o.write:
			if string(res.out) == string(data) {
				continue
			}
			if err := writeAtomic(name, res.out); err != nil {
				failed = true
				_, _ = io.WriteString(stderr, "mdtable: "+name+": "+reason(err)+"\n")
				continue
			}
			_, _ = io.WriteString(out, name+"\n")
		default:
			_, _ = out.Write(res.out)
		}
	}
	switch {
	case failed:
		return 2
	case listed:
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
