package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mdtable-bin-")
	if err != nil {
		os.Stderr.WriteString("cannot make a temporary folder: " + err.Error() + "\n")
		os.Exit(1)
	}
	binPath = filepath.Join(dir, "mdtable")
	if out, err := exec.Command("go", "build", "-o", binPath, ".").CombinedOutput(); err != nil {
		os.Stderr.WriteString("go build failed: " + err.Error() + "\n" + string(out))
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// runBin runs the built tool as a child process in dir.
func runBin(t *testing.T, dir, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		code = ee.ExitCode()
	default:
		t.Fatalf("cannot run the tool: %v", err)
	}
	return so.String(), se.String(), code
}

// format runs the tool on standard input and returns what it prints.
func format(t *testing.T, in string) string {
	t.Helper()
	out, errOut, code := runBin(t, t.TempDir(), in, "-")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, for input %q", code, errOut, in)
	}
	return out
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestJobExamples(t *testing.T) {
	in := "# Prices\n\n|Name|Qty|Note|\n|:--|--:|:-:|\n|apple|3|fresh|\n| 日本 | 12 |\n|pear|7|ok|stale|\n\nDone.\n"
	want := "# Prices\n\n| Name  | Qty | Note  |\n| :---- | --: | :---: |\n| apple |   3 | fresh |\n| 日本  |  12 |       |\n| pear  |   7 |  ok   | stale |\n\nDone.\n"
	out, errOut, code := runBin(t, t.TempDir(), in, "-")
	if out != want || code != 0 {
		t.Errorf("got %q (exit %d), want %q", out, code, want)
	}
	if wantWarn := "mdtable: warning: <stdin>:7: row has 4 cells, the header has 3\n"; errOut != wantWarn {
		t.Errorf("stderr %q, want %q", errOut, wantWarn)
	}
	if got := format(t, "|a|b|\n|-|:-:|\n|xx|y|\n"); got != "| a   |  b  |\n| --- | :-: |\n| xx  |  y  |\n" {
		t.Errorf("second example: %q", got)
	}
}

func TestFormatting(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"four alignments",
			"|a|b|c|d|\n|---|:--|--:|:-:|\n|1|2|3|4|\n",
			"| a   | b   |   c |  d  |\n| --- | :-- | --: | :-: |\n| 1   | 2   |   3 |  4  |\n"},
		{"wide characters count 2",
			"|日本語|\n|-|\n|a|\n",
			"| 日本語 |\n| ------ |\n| a      |\n"},
		{"combining marks count 0",
			"|e\u0301|\n|-|\n",
			"| e\u0301   |\n| --- |\n"},
		{"zero width space counts 0",
			"|a\u200bb|\n|-|\n",
			"| a\u200bb  |\n| --- |\n"},
		{"emoji count 2",
			"|\U0001F600\U0001F600|\n|-|\n",
			"| \U0001F600\U0001F600 |\n| ---- |\n"},
		{"fewer cells get empty cells",
			"a|b|c\n-|-|-\nx\n",
			"| a   | b   | c   |\n| --- | --- | --- |\n| x   |     |     |\n"},
		{"more cells are kept unpadded",
			"|a|\n|-|\n|x|yy|\n|p|\n|q||\n",
			"| a   |\n| --- |\n| x   | yy |\n| p   |\n| q   |  |\n"},
		{"escaped pipe and code span",
			"|a|b|\n|-|-|\n|x\\|y|`p|q`|\n",
			"| a    | b     |\n| ---- | ----- |\n| x\\|y | `p|q` |\n"},
		{"double backslash then pipe is a border",
			"|a|b|\n|-|-|\n|x\\\\|y|\n",
			"| a   | b   |\n| --- | --- |\n| x\\\\ | y   |\n"},
		{"code span of two backticks",
			"|a|\n|-|\n|``x`|y``|\n",
			"| a        |\n| -------- |\n| ``x`|y`` |\n"},
		{"no outer pipes",
			"a|b\n-|-\nc|d\n",
			"| a   | b   |\n| --- | --- |\n| c   | d   |\n"},
		{"table after a paragraph line",
			"text\na|b\n-|-\n",
			"text\n| a   | b   |\n| --- | --- |\n"},
		{"indentation and trailing spaces are dropped",
			"  |a|b|  \n   |-|-| \n  |x|y|\t\n",
			"| a   | b   |\n| --- | --- |\n| x   | y   |\n"},
		{"line without a pipe is a row",
			"a|b\n-|-\nbar\n\ntext\n",
			"| a   | b   |\n| --- | --- |\n| bar |     |\n\ntext\n"},
		{"ended by a heading, then a new table",
			"|a|b|\n|-|-|\n|x|y|\n# H\n|c|d|\n|-|-|\n",
			"| a   | b   |\n| --- | --- |\n| x   | y   |\n# H\n| c   | d   |\n| --- | --- |\n"},
		{"ended by a thematic break",
			"a|b\n-|-\nx|y\n---\n",
			"| a   | b   |\n| --- | --- |\n| x   | y   |\n---\n"},
		{"setext underline is a row",
			"a|b\n-|-\n===\n",
			"| a   | b   |\n| --- | --- |\n| === |     |\n"},
		{"ended by a block quote",
			"a|b\n-|-\n> q\n",
			"| a   | b   |\n| --- | --- |\n> q\n"},
		{"ended by a fence",
			"a|b\n-|-\n```\nx|y\n```\n",
			"| a   | b   |\n| --- | --- |\n```\nx|y\n```\n"},
		{"ended by an indented line",
			"a|b\n-|-\n    x|y\n",
			"| a   | b   |\n| --- | --- |\n    x|y\n"},
		{"table after a closed comment",
			"<!-- x -->\n|a|\n|-|\n",
			"<!-- x -->\n| a   |\n| --- |\n"},
		{"table after the front matter",
			"---\n|a|\n|-|\n---\n|a|\n|-|\n",
			"---\n|a|\n|-|\n---\n| a   |\n| --- |\n"},
		{"table after a list when the next line is not indented",
			"- item\n\n|a|b|\n|-|-|\n",
			"- item\n\n| a   | b   |\n| --- | --- |\n"},
		{"table after a closed fence",
			"```\nx\n```\n|a|\n|-|\n",
			"```\nx\n```\n| a   |\n| --- |\n"},
		{"table after indented code",
			"    code\n\n|a|\n|-|\n",
			"    code\n\n| a   |\n| --- |\n"},
		{"table after a closed html block",
			"<div>\n\n|a|\n|-|\n",
			"<div>\n\n| a   |\n| --- |\n"},
		{"two tables",
			"|a|\n|-|\n\n|b|\n|-|\n",
			"| a   |\n| --- |\n\n| b   |\n| --- |\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := format(t, c.in); got != c.want {
				t.Errorf("input %q\n got %q\nwant %q", c.in, got, c.want)
			}
			if again := format(t, c.want); again != c.want {
				t.Errorf("not a fixed point: %q becomes %q", c.want, again)
			}
		})
	}
}

func TestLeftAlone(t *testing.T) {
	cases := []struct{ name, in string }{
		{"empty", ""},
		{"no table", "hello\nworld"},
		{"backtick fence", "```\n|a|b|\n|-|-|\n```\n"},
		{"tilde fence unclosed", "~~~\n|a|\n|-|\n"},
		{"fence closed by a longer run", "````\n```\n|a|\n|-|\n````\n"},
		{"indented code", "    |a|b|\n    |-|-|\n"},
		{"tab indented code", "\t|a|b|\n\t|-|-|\n"},
		{"html block kind 6", "<div>\n|a|b|\n|-|-|\n"},
		{"html block kind 1", "<pre>\n|a|\n\n|-|\n</pre>\n"},
		{"html comment", "<!--\n|a|\n|-|\n-->\n"},
		{"html kind 7", "<span>\n|a|\n|-|\n"},
		{"front matter", "---\n|a|b|\n|-|-|\n---\n"},
		{"list item", "- |a|b|\n  |-|-|\n"},
		{"numbered list item", "1. x\n   |a|b|\n   |-|-|\n"},
		{"list item with an indented table after a blank line", "- item\n\n  |a|b|\n  |-|-|\n"},
		{"block quote", "> |a|b|\n> |-|-|\n"},
		{"wrong delimiter row", "|a|b|\n|-|\n|x|y|\n"},
		{"delimiter with a letter", "|a|b|\n|--|x|\n"},
		{"delimiter that is a list item", "a|b\n- | -\n"},
		{"delimiter without a pipe", "a\n---\n"},
		{"delimiter indented", "|a|\n    |-|\n"},
		{"indented line continues a paragraph", "text\n    |a|\n    |-|\n"},
		{"already aligned", "| a   | b   |\n| --- | --- |\n| x   | y   |\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := format(t, c.in); got != c.in {
				t.Errorf("changed %q into %q", c.in, got)
			}
		})
	}
}

func TestBytesKept(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"bom, mixed endings, no final newline",
			"\uFEFF|a|b|\r\n|-|-|\n|x|y|",
			"\uFEFF| a   | b   |\r\n| --- | --- |\n| x   | y   |"},
		{"text around a table",
			"x  \t\n\n|a|b|  \r\n|-|-|\r\n|x|y|\r\n\r\ntail",
			"x  \t\n\n| a   | b   |\r\n| --- | --- |\r\n| x   | y   |\r\n\r\ntail"},
		{"carriage return elsewhere is a character",
			"a\rb\n|a|\n|-|\n",
			"a\rb\n| a   |\n| --- |\n"},
		{"last line of the table has no ending",
			"|a|\n|-|",
			"| a   |\n| --- |"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := format(t, c.in); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestIdempotentAndCheckClean(t *testing.T) {
	in := "---\ntitle: x\n---\n# Prices\n\n|Name|Qty|Note|\n|:--|--:|:-:|\n|apple|3|fresh|\n| 日本 | 12 |\n|pear|7|ok|stale|\n\n" +
		"text\na|b\n-|-\nbar\n\n- |x|\n\n```\n|a|\n```\n\n|`a|b`|c\\|d|\n|-|-|\n"
	once := format(t, in)
	if twice := format(t, once); twice != once {
		t.Errorf("not a fixed point:\n%q\n%q", once, twice)
	}
	dir := t.TempDir()
	writeFile(t, dir, "x.md", once)
	out, errOut, code := runBin(t, dir, "", "--check", "x.md")
	if wantWarn := "mdtable: warning: x.md:10: row has 4 cells, the header has 3\n"; out != "" || errOut != wantWarn || code != 0 {
		t.Errorf("--check on the output: %q %q %d", out, errOut, code)
	}
}

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	content := "# T\n\n|a|b|\n|-|-|\n|x|y|\n\n| c   | d   |\n| --- | --- |\n| 1   | 2   |\n\n|e|\n|-|\n"
	p := writeFile(t, dir, "a.md", content)
	writeFile(t, dir, "ok.md", "| a   |\n| --- |\n")
	out, errOut, code := runBin(t, dir, "", "--check", "a.md", "ok.md")
	if out != "a.md:3\na.md:11\n" || errOut != "" || code != 1 {
		t.Errorf("got %q %q %d", out, errOut, code)
	}
	if b, _ := os.ReadFile(p); string(b) != content {
		t.Errorf("--check changed the file")
	}
	out, errOut, code = runBin(t, dir, "", "ok.md", "--check")
	if out != "" || errOut != "" || code != 0 {
		t.Errorf("clean file: %q %q %d", out, errOut, code)
	}
	out, _, code = runBin(t, dir, "|a|\n|-|\n", "--check", "-")
	if out != "<stdin>:1\n" || code != 1 {
		t.Errorf("stdin: %q %d", out, code)
	}
}

func TestWarningsInEveryMode(t *testing.T) {
	in := "x\n\n|a|\n|-|\n|1|2|3|\n"
	want := "mdtable: warning: <stdin>:5: row has 3 cells, the header has 1\n"
	for _, args := range [][]string{{"-"}, {"--check", "-"}} {
		_, errOut, _ := runBin(t, t.TempDir(), in, args...)
		if errOut != want {
			t.Errorf("%v: stderr %q, want %q", args, errOut, want)
		}
	}
	dir := t.TempDir()
	writeFile(t, dir, "w.md", in)
	out, errOut, code := runBin(t, dir, "", "--write", "w.md")
	if errOut != "mdtable: warning: w.md:5: row has 3 cells, the header has 1\n" || out != "w.md\n" || code != 0 {
		t.Errorf("--write: %q %q %d", out, errOut, code)
	}
}

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	changed := writeFile(t, dir, "changed.md", "|a|b|\n|-|-|\n")
	same := writeFile(t, dir, "same.md", "| a   |\n| --- |\n")
	if err := os.Chmod(changed, 0o640); err != nil {
		t.Fatal(err)
	}
	old := time.Unix(1_000_000_000, 0)
	for _, p := range []string{changed, same} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	out, errOut, code := runBin(t, dir, "", "--write", "changed.md", "same.md")
	if out != "changed.md\n" || errOut != "" || code != 0 {
		t.Fatalf("got %q %q %d", out, errOut, code)
	}
	if b, _ := os.ReadFile(changed); string(b) != "| a   | b   |\n| --- | --- |\n" {
		t.Errorf("content %q", b)
	}
	if st, _ := os.Stat(changed); st.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	if st, _ := os.Stat(same); !st.ModTime().Equal(old) {
		t.Errorf("unchanged file was touched: %v", st.ModTime())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("left %d entries in the folder", len(entries))
	}
	out, _, code = runBin(t, dir, "", "--write", "changed.md")
	if out != "" || code != 0 {
		t.Errorf("second run: %q %d", out, code)
	}
}

func TestWithoutOptionsOrderAndRepeat(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.md", "|a|\n|-|\n")
	writeFile(t, dir, "b.md", "plain")
	writeFile(t, dir, "empty.md", "")
	out, errOut, code := runBin(t, dir, "", "a.md", "b.md", "empty.md", "a.md")
	want := "| a   |\n| --- |\nplain| a   |\n| --- |\n"
	if out != want || errOut != "" || code != 0 {
		t.Errorf("got %q %q %d", out, errOut, code)
	}
	abs := filepath.Join(dir, "a.md")
	if out, _, _ = runBin(t, t.TempDir(), "", abs); out != "| a   |\n| --- |\n" {
		t.Errorf("absolute path: %q", out)
	}
}

func TestErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.md", "|a|\n|-|\n\xff\xfe\n")
	writeFile(t, dir, "good.md", "|a|\n|-|\n")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runBin(t, dir, "", "missing.md", "bad.md", "sub", "good.md")
	if out != "| a   |\n| --- |\n" || code != 2 {
		t.Errorf("got %q (exit %d)", out, code)
	}
	lines := strings.Split(strings.TrimSuffix(errOut, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("stderr %q", errOut)
	}
	for i, name := range []string{"missing.md", "bad.md", "sub"} {
		if !strings.HasPrefix(lines[i], "mdtable: "+name+": ") {
			t.Errorf("line %d: %q", i, lines[i])
		}
	}
	out, _, code = runBin(t, dir, "", "--check", "bad.md", "good.md")
	if out != "good.md:1\n" || code != 2 {
		t.Errorf("--check: %q (exit %d)", out, code)
	}
	out, errOut, code = runBin(t, dir, "\xff", "-")
	if out != "" || code != 2 || !strings.HasPrefix(errOut, "mdtable: <stdin>: ") {
		t.Errorf("stdin: %q %q %d", out, errOut, code)
	}
	out, _, code = runBin(t, dir, "", "--write", "bad.md", "good.md")
	if out != "good.md\n" || code != 2 {
		t.Errorf("--write: %q (exit %d)", out, code)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".mdtable-") {
			t.Errorf("temporary file left: %s", e.Name())
		}
	}
}

func TestUsage(t *testing.T) {
	dir := t.TempDir()
	ragged := "|a|\n|-|\n"
	writeFile(t, dir, "a.md", ragged)
	bad := [][]string{
		{"--bogus", "a.md"},
		{"-x"},
		{"--check", "--write", "a.md"},
		{"-", "-"},
		{"--write", "-"},
		{"--write", "a.md", "-"},
		{},
		{"--check"},
		{"--write"},
		{"--help", "--bogus"},
		{"--help", "--check", "--write", "a.md"},
		{"--write", "a.md", "--bogus"},
		{"--write", "a.md", "--", "-", "-"},
	}
	for _, args := range bad {
		out, errOut, code := runBin(t, dir, "stdin", args...)
		if out != "" || code != 2 || !strings.HasPrefix(errOut, "mdtable: ") {
			t.Errorf("%v: %q %q %d", args, out, errOut, code)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.md")); string(b) != ragged {
		t.Errorf("a usage error wrote a file")
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"-h", "--check"}, {"a.md", "--help"}} {
		out, errOut, code := runBin(t, t.TempDir(), "", args...)
		if !strings.HasPrefix(out, "usage: mdtable") || code != 0 || errOut != "" {
			t.Errorf("%v: %q %q %d", args, out, errOut, code)
		}
	}
}

func TestDoubleDash(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "-x.md", "|a|\n|-|\n")
	for _, args := range [][]string{{"--", "-x.md"}, {"--check", "--", "-x.md"}} {
		out, _, code := runBin(t, dir, "", args...)
		wantOut, wantCode := "| a   |\n| --- |\n", 0
		if args[0] == "--check" {
			wantOut, wantCode = "-x.md:1\n", 1
		}
		if out != wantOut || code != wantCode {
			t.Errorf("%v: %q %d", args, out, code)
		}
	}
	out, _, code := runBin(t, dir, "|b|\n|-|\n", "--", "-")
	if out != "| b   |\n| --- |\n" || code != 0 {
		t.Errorf("stdin after --: %q %d", out, code)
	}
	if _, errOut, code := runBin(t, dir, "", "--", "-x.md", "--bogus"); code != 2 || !strings.HasPrefix(errOut, "mdtable: --bogus: ") {
		t.Errorf("a file named --bogus: %q %d", errOut, code)
	}
}

func TestSplitRow(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"|a|b|", []string{"a", "b"}},
		{"a", []string{"a"}},
		{"|", []string{}},
		{"||", []string{""}},
		{"  | a |  ", []string{"a"}},
		{`a\|b|c`, []string{`a\|b`, "c"}},
		{`\\|a`, []string{`\\`, "a"}},
		{"`a|b`|c", []string{"`a|b`", "c"}},
		{"`a|b|c", []string{"`a", "b", "c"}},
		{"``a`b|c``", []string{"``a`b|c``"}},
	}
	for _, c := range cases {
		if got := splitRow(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitRow(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCharWidth(t *testing.T) {
	cases := []struct {
		r    rune
		want int
	}{
		{'a', 1}, {'\t', 1}, {'é', 1}, {'日', 2}, {'한', 2}, {'！', 2}, {'ｱ', 1},
		{0x0301, 0}, {0x200B, 0}, {0x200D, 0}, {0xFE0F, 0}, {0x20E3, 0},
		{0x1F600, 2}, {0x1F680, 2}, {0x1F900, 2}, {0x20000, 2}, {0x1100, 2},
		{0x2603, 1}, {0x1F700, 1},
	}
	for _, c := range cases {
		if got := charWidth(c.r); got != c.want {
			t.Errorf("charWidth(%U) = %d, want %d", c.r, got, c.want)
		}
	}
}
