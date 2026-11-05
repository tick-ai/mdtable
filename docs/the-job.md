# mdtable: Markdown tables aligned and repaired, in place, the way GitHub renders them

This is the TickHub job this tool is built to, by agents on TickHub.

Agents and people edit Markdown tables by hand and leave them ragged: a cell grows, a column drifts,
a row loses a cell, a delimiter row is too short. The page still renders, but the source is
unreadable and every edit makes a noisy diff. The tools that fix this are run once and forgotten,
or two people's tools align in two ways and every commit shuffles the same table. The job is a
command line tool, **mdtable**, that people put in a pre-commit hook: every table is aligned the
same way every time, **and not one other byte of the file changes**. It reads Markdown the way
GitHub does (what is a table, what is code, what is HTML), so it never "fixes" something that is not
a table. Every rule below says what is read, what is not, and which bytes are written.

## Files and territory

Create or change exactly these files, and nothing else:

* `go.mod`: exactly the module line `module github.com/tick-ai/mdtable`, a blank line, and the line
  `go 1.27`. No `require` lines.
* `main.go`: the tool. `package main`, **Go standard library only** (the `unicode` tables included;
  no other module, nothing fetched, no network, no cgo). It is built with `go build -o mdtable .`
  from the repository's root, passes `go vet ./...`, and is formatted by `gofmt` (`gofmt -l .`
  prints nothing). The referee builds it with Go 1.27.3 (`GOTOOLCHAIN=local`, `GOPROXY=off`,
  `GOFLAGS=-mod=mod`, `CGO_ENABLED=0`, no network).
* `main_test.go`: your own tests, `package main`, with the `testing` package, run by
  `go test ./...`. They write only under `os.TempDir()`, and they pass.
* `README.md`: replaces this repository's stub. Written for developers: what mdtable does and why
  it matters, how to run it, an example (a table before and after), every option, the rules in
  short (what is a table, the width rule, the alignment forms), the exit codes, how to use it in a
  pre-commit hook and in a CI step with `--check`, and the two closing sections "Where it came
  from" and "Licence" (below).

`LICENSE` (Apache-2.0) and `docs/the-job.md` (this text) are given and stay exactly as they are. A
folder `sealed/` that is placed beside your files when the work is checked is not yours.

## How it is run

    mdtable [--check | --write] [--] FILE...

(from a clone: `go build -o mdtable .` and then `./mdtable ...`.) Options and files may come in any
order.

* `FILE`: a path to a Markdown file, relative to the current directory or absolute, read as it is
  (whatever its name ends in). A single dash `-` is standard input. Files are handled in the order
  given; a path given twice is handled twice. A folder is not a file: it cannot be read (see
  "Errors"). Nothing is searched for.
* `--check`: change nothing; list the tables that would change; exit 1 if there is one.
* `--write`: rewrite each file whose new content differs from its bytes, in place.
* With neither, the new content of every file is written to standard output.
* `--`: every argument after it is a `FILE`, whatever it starts with (`-` after it is still
  standard input).
* `-h` or `--help`: print a usage text on standard output, whose first line starts with
  `usage: mdtable`, and exit 0, when the command line holds no usage error.
* A **usage error** prints a message on standard error (its first line starts with `mdtable: `),
  prints nothing on standard output, writes no file, reads no file, and exits with status 2. It is:
  an option that is not one of the above (an argument that starts with `-`, other than `-` and
  `--`, and is before any `--`); `--check` together with `--write`; `-` given twice; `-` together
  with `--write`; no `FILE` at all (unless `--help` is given).

## Reading the file

* The file is read as UTF-8. A file that is not valid UTF-8 cannot be read (see "Errors").
* A U+FEFF at the very start of the file is a byte-order mark: it is set aside, the file is read
  without it, and it is written back first, unchanged.
* A **line** ends at `\n`. A `\r` just before the `\n` belongs to the line ending, not to the line.
  The last line may have no line ending at all. A `\r` anywhere else is a character of the line.
  Line numbers count from 1 (the byte-order mark does not change them). Each line keeps its own
  ending: a file may mix `\n` and `\r\n`, and every line is written back with the ending it had.
* A line is **blank** when it holds nothing but spaces and tabs (or nothing).
* A line is **indented** when its leading spaces and tabs hold a tab or at least four spaces.
  Otherwise it has 0 to 3 leading spaces, and its **rest** is the line without them.
* Only spaces and tabs are ever trimmed (called "trimmed" below). Other white space is a character
  like any other.

## What is a table, and what is not

Mdtable finds the tables of GitHub Flavored Markdown (the table extension of the GFM
specification) at the top level of the file, and nothing else. To know what is at the top level,
it reads the lines in order, one **mode** at a time, with a flag **paragraph** that is false at
the start. This is the whole reading; a line that no rule below mentions is plain text.

**Front matter.** When the first line is exactly `---` (after the byte-order mark) and a later
line is exactly `---`, the lines from the first to the first such later line are skipped. Without
a closing line there is no front matter.

**Mode `top`** (the start). Take the next line and apply the first rule that fits:

1. The line is blank: paragraph becomes false.
2. The line is indented: when paragraph is true it continues the paragraph (nothing changes);
   otherwise it starts an **indented code block** (mode `code`). Either way it is not read.
3. The rest starts a **fenced code block**: three or more backticks or three or more `~`, and for
   backticks no backtick in what follows on the line. Mode `fence`, paragraph false.
4. The rest starts an **HTML block** (below). Paragraph false.
5. The rest is an **ATX heading** (`^#{1,6}([ \t]|$)`), or paragraph is true and the rest is a
   **setext underline** (`^(=+|-+)[ \t]*$`), or the rest is a **thematic break** (`^((\*[ \t]*){3,}|(-[ \t]*){3,}|(_[ \t]*){3,})$`).
   Paragraph false.
6. The rest starts a **list item** (`^([-+*]|[0-9]{1,9}[.)])([ \t]|$)`) or a **block quote**
   (it starts with `>`). Mode `container`, paragraph false.
7. Otherwise the line is a paragraph line. If it is the header row of a table (below), the table
   is found and its lines are consumed, and paragraph is false. If not, paragraph becomes true.

**Mode `fence`**: lines are skipped until a line that closes the fence: no more than 3 spaces of
indentation, then a run of the same character at least as long as the opening run, then only spaces
and tabs. That line is skipped too; mode `top`, paragraph false. An unclosed fence lasts to the end
of the file.

**Mode `code`**: blank lines and indented lines are skipped. The first line that is neither is
read in mode `top`.

**HTML blocks.** A rest starts an HTML block of the first kind that fits (regular expressions are
Go's `regexp` syntax, matched against the rest):

* kind 1: `(?i)^<(pre|script|style|textarea)([ \t>]|$)`; kind 2: starts with `<!--`; kind 3:
  starts with `<?`; kind 4: `^<![A-Za-z]`; kind 5: starts with `<![CDATA[`. A block of kind 1 to 5
  ends at the first line that holds its end marker: for kind 1 a closing tag
  `</pre>`, `</script>`, `</style>` or `</textarea>` (any case); for kind 2 `-->`; for 3 `?>`; for
  4 `>`; for 5 `]]>`. On the opening line the marker is looked for only after the opening marker
  (after `<` and the tag name for kind 1, after `<!--`, `<?`, `<!` and `<![CDATA[` for the others);
  when it is there, the block is that one line and the mode stays `top`. Otherwise the mode is
  `html`, and the following lines are skipped up to and including the first line that holds the
  end marker anywhere in it.
* kind 6: `(?i)^</?(NAME)([ \t]|/?>|$)` where NAME is one of: address, article, aside, base,
  basefont, blockquote, body, caption, center, col, colgroup, dd, details, dialog, dir, div, dl,
  dt, fieldset, figcaption, figure, footer, form, frame, frameset, h1, h2, h3, h4, h5, h6, head,
  header, hr, html, iframe, legend, li, link, main, menu, menuitem, nav, noframes, ol, optgroup,
  option, p, param, search, section, summary, table, tbody, td, tfoot, th, thead, title, tr, track,
  ul.
* kind 7, only when paragraph is false: a complete open or closing tag alone on the line,
  ``^(<[A-Za-z][A-Za-z0-9-]*([ \t]+[A-Za-z_:][A-Za-z0-9_.:-]*([ \t]*=[ \t]*([^ \t"'=<>`]+|'[^']*'|"[^"]*"))?)*[ \t]*/?>|</[A-Za-z][A-Za-z0-9-]*[ \t]*>)[ \t]*$``.
* Kinds 6 and 7 put the mode in `html`; the following lines are skipped up to, not including,
  the next blank line, which is read in mode `top`.

**Mode `container`** (a list item or a block quote and what belongs to it; none of it is read,
and so no table in it is touched). For each line:

* a non-blank line is skipped, unless it has fewer than 2 leading spaces and no tab and it opens a
  fenced code block, starts an HTML block (of kinds 1 to 7, as when paragraph is false), is an ATX
  heading or is a thematic break: then the mode is `top` and the line is read there;
* a blank line: look at the next non-blank line. When there is one and its leading spaces and tabs
  hold at least 2 spaces or begin with a tab, the lines up to it are skipped and it is read in this
  mode. Otherwise the mode is `top` and the blank line is read there.

### The table

A paragraph line (rule 7) is the **header row** of a table when the line right after it exists, is
not indented, and is a **delimiter row**:

* its rest holds a `|`, and does not start a list item (so `- | -` is a list item, not a delimiter
  row);
* split into cells (below), it has the same number of cells as the header row, at least one, and
  every cell is, once trimmed, one or more `-` with an optional `:` before and an optional `:`
  after (`^:?-+:?$`).

Without that, the line is a plain paragraph line. The **body rows** are the lines that follow the
delimiter row, one after the other, up to the first line that is blank, or is indented, or whose
rest opens a fenced code block, starts an HTML block (of any kind 1 to 7), is an ATX heading, is a
thematic break, starts a list item, or starts a block quote. **Every other line is a body row,
whether it holds a `|` or not**: `bar` is a body row with one cell, as in GitHub. (A setext
underline such as `===` is a body row; `---` is a thematic break and ends the table.) The table is
its header row, its delimiter row and its body rows. After it, paragraph is false and the next line
is read in mode `top`.

A header row that is a line of a paragraph is the table's header: lines of the paragraph before it
are plain text, and stay as they are.

**Cells.** To split a row, take its rest (for a body row too, whatever the table's indentation was),
trimmed. Read it from left to right, byte by byte:

* a backslash followed by an ASCII punctuation character (``!"#$%&'()*+,-./:;<=>?@[\]^_`{|}~``): both
  are passed over (so `\|` is not a border, and `\\|` is a backslash and a border);
* a run of N backticks (a maximal run) followed later in the row by a run of exactly N backticks
  (the first one): the opening run, everything up to and including that closing run is passed over
  (a **code span**; a `|` in it is not a border). A run with no such partner is plain text;
* a `|` is a **border**.

The row is cut at its borders. When the first character of the row is a border, the empty piece
before it is dropped; when the last character of the row is a border, the empty piece after it is
dropped (a row that is only `|` has no cell). Each remaining piece, trimmed, is a **cell**; its
content is kept as written (backslashes, code spans, inner spaces and all). A row without a border
is one cell.

The **alignment** of a column is read from its delimiter cell (trimmed): with a colon before and
after, **center**; only after, **right**; only before, **left**; neither, **none**.

## What is written

A table is written as lines, one for each line it had, in the same order, so a table of k lines is
k lines. Each new line is written with the line ending of the line it replaces (or none, for a last
line without an ending). Let `n` be the number of cells of the header row.

* **Cells of a row.** A body row with fewer than `n` cells gets empty cells (empty strings) up to
  `n`. A body row with more than `n` cells keeps its extra cells, as they are (see "Warnings"). No
  row is ever cut.
* **Width of a text** (the display width of a cell): the sum of the widths of its characters
  (Unicode code points). A character has width **0** when it is in the Unicode category Mn, Me or Cf
  (Go's `unicode.Mn`, `unicode.Me`, `unicode.Cf`: combining marks, zero-width characters such as
  U+200B and U+200D, variation selectors). Otherwise it has width **2** when it is in one of these
  ranges (the East Asian Wide and Fullwidth characters, as this job fixes them): U+1100 to U+115F,
  U+2E80 to U+303E, U+3041 to U+33FF, U+3400 to U+4DBF, U+4E00 to U+9FFF, U+A000 to U+A4CF, U+A960
  to U+A97F, U+AC00 to U+D7A3, U+F900 to U+FAFF, U+FE30 to U+FE6F, U+FF01 to U+FF60, U+FFE0 to
  U+FFE6, U+1F300 to U+1F64F, U+1F680 to U+1F6FF, U+1F900 to U+1F9FF, U+20000 to U+3FFFD.
  Every other character has width **1** (a tab too). These ranges are the rule; the Unicode
  database's own list of wide characters is not consulted.
* **Width of a column**: the largest of 3 and the widths of the cells of that column (header row
  and body rows, for the first `n` cells of each row, after the empty cells were added).
* **A padded cell**: the cell's content with spaces added to reach the width of its column; `d` is
  the number of spaces to add. Left or none: all `d` after the content. Right: all `d` before it.
  Center: `d / 2` (rounded down) before it and the rest after it. This applies to the header row's
  cells too.
* **A row** is `| `, then its cells joined with ` | `, then ` |`: the padded cells for the first
  `n` cells, and each extra cell as it is (not padded), so there is no trailing space after the
  last pipe, a pipe at both ends, and one space inside each pipe. An empty extra cell makes two
  spaces between its pipes.
* **The delimiter row** is written the same way, its cells being, for a column of width `w`:
  none: `w` hyphens; left: `:` and `w-1` hyphens; right: `w-1` hyphens and `:`; center: `:`, `w-2`
  hyphens and `:`. (Width 3 gives `---`, `:--`, `--:`, `:-:`.)
* Indentation (0 to 3 spaces) and trailing spaces of the lines of a table are not kept.

Example. This file:

    # Prices

    |Name|Qty|Note|
    |:--|--:|:-:|
    |apple|3|fresh|
    | 日本 | 12 |
    |pear|7|ok|stale|

    Done.

is written as:

    # Prices

    | Name  | Qty | Note  |
    | :---- | --: | :---: |
    | apple |   3 | fresh |
    | 日本  |  12 |       |
    | pear  |   7 |  ok   | stale |

    Done.

(`日本` has width 4, so it is padded with one space to the column's width 5; the last row has four
cells and the header three, so `stale` stays as it is, and a warning is printed.) Another one:

    |a|b|
    |-|:-:|
    |xx|y|

becomes

    | a   |  b  |
    | --- | :-: |
    | xx  |  y  |

(`b` is in a column of width 3, so `d` is 2 and one space goes before and one after; `y` the
same.)

## Everything else is kept

The file's bytes outside the lines of tables are written exactly as they were: every line, with its
ending, its trailing spaces, its tabs, in the same order; a missing final line ending stays missing;
a byte-order mark stays. A file with no table is written as it is. Mdtable never adds a line, never
removes a line, and never changes a line ending. Text inside code (fenced or indented), inside HTML
blocks, in front matter, in list items and in block quotes is never touched.

Writing the same file again changes nothing: the output of mdtable, given as input, is itself.

## Warnings

For every body row of a table that has more than `n` cells, mdtable prints one line on standard
error, in the order of the file (files in the order given):

    mdtable: warning: <name>:<line>: row has <k> cells, the header has <n>

`<name>` is the path as it was given (`<stdin>` for standard input), `<line>` the row's line number
(from 1) and `<k>` its number of cells. A warning does not change the exit status. Warnings are
printed in every mode.

## What it prints and does

For each file, in order, read it, build its new content, and compare it with the bytes the file
holds. A **table changes** when any of its new lines is different from the line it replaces (the
line endings are the same, so the text of the line decides).

* **Without `--check` or `--write`**: the new content of each file is written to standard output,
  one after the other, with nothing added between them. With a file that is empty, nothing is
  written for it.
* **`--check`**: nothing is written anywhere. For each table that changes, in the order of the
  file, standard output gets one line `<name>:<line>`, where `<line>` is the line number of the
  table's header row. Exit 1 if any line was printed (and no error occurred), else 0. When
  nothing would change, standard output is empty.
* **`--write`**: a file whose new content is not its bytes is rewritten, and standard output gets
  one line `<name>` for it. A file whose new content is its bytes is not touched at all (not
  opened for writing; its modification time stays), and prints nothing. The write goes to a new
  temporary file in the same folder as the file (its name starts with `.mdtable-` and ends with
  `.tmp`), which is given the file's permission bits and renamed over the file. So the file is
  either as it was or completely new. No temporary file is left behind, also when an error
  happened. Nothing else is ever created, renamed or written. (Symbolic links are not specified.)
  When nothing was rewritten, standard output is empty.

Every line printed on standard output ends with `\n`.

## Errors and exit codes

An **error** is: a `FILE` that cannot be read (it does not exist, it is a folder, no permission),
a file that is not valid UTF-8, standard input that cannot be read as UTF-8, or a file that
cannot be written (with `--write`). It prints one line on standard error,
`mdtable: <name>: <reason>` (the reason is yours), and writes nothing to standard output for that
file in any mode (no content, no `--check` line), changes nothing in that file, and does not stop
the other files, which are handled as usual.

* **0**: no error, and (with `--check`) no table would change.
* **1**: only with `--check`: no error, and at least one table would change.
* **2**: a usage error, or at least one error. It wins over 1.

## README: the two closing sections

"Where it came from" says, in a few lines: built by agents on TickHub, as TickHub job `<job id>`,
with a link to `https://tick.company/hub/#/job/<job id>`, where the job, the hand-ins and the
receipt are on TickHub's public log; that `docs/the-job.md` is the job as it was given; and the
line "AI does the work. Tick makes it trustworthy. https://tick.company". The job id is given to
the developer with the job.

"Licence" says: Apache-2.0, Copyright 2026 Parsa Esmaeili, see `LICENSE`.

The README's way to run it today is `go build -o mdtable .` from a clone and
`go install github.com/tick-ai/mdtable@latest`. It is not in any package index of its own, so the
README must not claim a package-manager install (`brew`, `apt`, `npm`, `pip`) works. It says
plainly what the tool does not do: it does not touch anything but the tables of the files it is
given, it does not read tables inside list items and block quotes, and it never rewrites a file
whose bytes would not change.
