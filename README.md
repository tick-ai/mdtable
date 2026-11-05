# mdtable

mdtable aligns and repairs the Markdown tables of your files, the way GitHub reads them, and changes not one other byte.

    mdtable [--check | --write] [--] FILE...

Before:

    |Name|Qty|Note|
    |:--|--:|:-:|
    |apple|3|fresh|
    | 日本 | 12 |
    |pear|7|ok|stale|

After `mdtable --write notes.md`:

    | Name  | Qty | Note  |
    | :---- | --: | :---: |
    | apple |   3 | fresh |
    | 日本  |  12 |       |
    | pear  |   7 |  ok   | stale |

The short row got its empty cell, the wide characters of `日本` count 2, and the row with one cell too many
is kept as it is and reported on standard error:

    mdtable: warning: notes.md:7: row has 4 cells, the header has 3

## Why

Hand-edited tables go ragged: a cell grows, a column drifts, a row loses a cell. The page still renders, but the
source is unreadable and every edit makes a noisy diff. mdtable aligns every table the same way every time, so it
is safe in a pre-commit hook: it reads Markdown as GitHub does (what is a table, what is code, what is HTML), so
it never "fixes" something that is not a table, and it never rewrites a file whose bytes would not change.

## Install

From a clone:

    go build -o mdtable .

or

    go install github.com/tick-ai/mdtable@latest

It needs Go 1.27 and nothing else (standard library only). It is not in any package index of its own: there is no
`brew`, `apt`, `npm` or `pip` package.

## Use

    mdtable [--check | --write] [--] FILE...

Options and files may come in any order. Files are handled in the order given (a path given twice is handled
twice); nothing is searched for, and a file is read whatever its name ends in.

| Option | What it does |
| ------ | ------------ |
| (none) | Writes the new content of every file to standard output, one after another, with nothing between them. |
| `--check` | Changes nothing. Prints `file:line` (the line of the header row) for each table that would change, and exits 1 if there is one. With nothing to change it prints nothing and exits 0. |
| `--write` | Rewrites each file whose new content differs, in place, and prints its name. Files that would not change are not touched at all (not opened for writing; their modification time stays). |
| `--` | Every argument after it is a `FILE`, whatever it starts with. |
| `-` | As a `FILE`: standard input, named `<stdin>` (once, and not with `--write`). |
| `-h`, `--help` | Prints the usage on standard output (first line `usage: mdtable ...`) and exits 0. |

`--write` writes a new temporary file in the folder of the file (`.mdtable-*.tmp`), gives it the permission bits
of the file and renames it over the file, so the file is either as it was or completely new, and nothing is left
behind, also when an error happened.

## The rules, in short

**What is a table.** A paragraph line (the header row), followed by a delimiter row: not indented, with a `|`, the
same number of cells as the header row, every cell being hyphens with an optional `:` on either side. The body
rows are the lines after it, up to a blank line, an indented line, or a line that starts another block (a fenced
code block, an HTML block, a heading, a thematic break, a list item, a block quote). Every other line is a body
row, with or without a `|`, as in GitHub. A header with a wrong delimiter row is not a table.

**What is left alone.** Tables in fenced code, indented code, HTML blocks, front matter (`---` ... `---` at the
start of the file), list items and block quotes are not read and not touched. The full reading, line by line,
is in `docs/the-job.md`.

**Cells.** A row is cut at its `|`. A `|` after a backslash (`\|`), or inside a code span (backticks of equal
length), is not a border. Cells are trimmed of spaces and tabs; their content is kept as written.

**Width.** Characters in the East Asian Wide and Fullwidth ranges the job fixes (CJK, Hangul, fullwidth forms, most
emoji) count 2; combining marks and zero-width characters (Unicode categories Mn, Me and Cf) count 0; every other
character counts 1. A column is as wide as its widest cell, at least 3.

**Alignment forms.** Read from the delimiter row, written for a column of width `w`:

| Alignment | Delimiter cell (w = 3) | Padding |
| --------- | ---------------------- | ------- |
| none | `---` (`w` hyphens) | spaces after |
| left | `:--` | spaces after |
| right | `--:` | spaces before |
| center | `:-:` | half (rounded down) before, the rest after |

The header row is aligned like the body. A row with fewer cells than the header gets empty cells; a row with
more keeps its extra cells, unpadded, and a warning is printed. No row is ever cut.

**Every other byte is kept.** Lines outside tables are written back exactly: CRLF and LF per line, a missing final
newline, a byte-order mark, trailing spaces and tabs. Indentation and trailing spaces of the lines of a table are
not kept. mdtable never adds or removes a line. Formatting is idempotent: its output is its own fixed point.

## What it does not do

* It does not touch anything but the tables of the files it is given.
* It does not read tables inside list items and block quotes.
* It never rewrites a file whose bytes would not change.

## Exit codes

| Code | Meaning |
| ---- | ------- |
| 0 | No error, and (with `--check`) no table would change. |
| 1 | Only with `--check`: no error, and at least one table would change. |
| 2 | A usage error, or at least one error. It wins over 1. |

A usage error (an unknown option, `--check` with `--write`, `-` twice or with `--write`, no `FILE`) prints
`mdtable: ...` on standard error, reads and writes no file, and prints nothing on standard output. An unreadable
file (missing, a folder, no permission), a file that is not valid UTF-8, or a file that cannot be written prints
`mdtable: <name>: <reason>` on standard error and prints nothing on standard output for that file; the other
files are still handled.

## In a pre-commit hook

`.git/hooks/pre-commit`:

    #!/bin/sh
    files=$(git diff --cached --name-only --diff-filter=ACM -- '*.md')
    [ -z "$files" ] && exit 0
    echo "$files" | xargs mdtable --write | xargs -r git add

For the [pre-commit](https://pre-commit.com) framework, a local hook:

    repos:
      - repo: local
        hooks:
          - id: mdtable
            name: mdtable
            entry: mdtable --write
            language: system
            types: [markdown]

## In a CI step

`--check` changes nothing, lists `file:line` for each table that is not aligned, and fails the step with exit 1:

    - run: go install github.com/tick-ai/mdtable@latest
    - run: mdtable --check $(git ls-files '*.md')

## Where it came from

Built by agents on TickHub, as TickHub job `1bc21191-8e49-43b4-9ec7-d0902d10009d`. The job, the hand-ins and the
receipt are on TickHub's public log: https://tick.company/hub/#/job/1bc21191-8e49-43b4-9ec7-d0902d10009d

`docs/the-job.md` is the job as it was given.

AI does the work. Tick makes it trustworthy. https://tick.company

## Licence

Apache-2.0, Copyright 2026 Parsa Esmaeili, see `LICENSE`.
