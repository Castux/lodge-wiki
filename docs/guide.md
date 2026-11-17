# User guide

How to read, write and organise pages on this wiki.

## Pages and folders

Every page has an **address**, made of folder names and a page name joined
by `/`:

| Address | What it is |
| --- | --- |
| `/foolslanding/notice/missingpet` | the page `missingpet` in the folder `foolslanding/notice` |
| `/foolslanding/` | the folder's front page (its page named `start`) |
| `/` | the wiki's front page |

Folders (DokuWiki called them *namespaces*) are not created separately: a
folder exists as soon as a page is saved in it, and disappears with its
last page. Give a folder a `start` page to describe it; it is what
`/folder/` shows and what lists of folders link to.

**Names are cleaned up automatically**: lowercase, accents removed, spaces
and punctuation turned into `-`. "Fool's Landing" becomes `fool-s-landing`,
"Château d'Ys" becomes `chateau-d-ys`. Only letters, digits, `-`, `_` and
`.` remain.

**A page's title is not its name.** The title shown on the page, in lists
and in the browser tab is the page's first heading (`# The Siege`). The
name is only the address. Changing the heading changes the title
everywhere; the address stays the same.

### Creating a page

- **New page** in the sidebar asks for a title and a folder (the current
  page's folder by default) and opens the editor. The address is made from
  the title.
- Or link to a page that does not exist yet, save, and follow the link:
  missing pages are shown in a different colour, and opening one offers
  **Create**.
- Or type the address you want in the browser bar.

### Renaming and moving

Moving a page **is** renaming it: **Rename** in the sidebar asks for the
new address. Change the last part to rename, the folders to move, or both
(`foolslanding/story/new-name`).

- The page keeps its history.
- Every link to it, on every page, is updated in the same change, so
  nothing breaks.
- Links written inside the moved page keep pointing at the same pages.

To move or rename a **whole folder** (all its pages and sub-folders, and
optionally its images folder), use **Files**, open the folder and choose
**move**. Widgets that name a data file in a moved folder (for example
`data: skarnholm/mapdata.json`) must be updated by hand.

Pages cannot be deleted from the wiki yet; ask an admin. (Moving a page
into an `archive` folder is often enough.)

### History

Every save is kept. **History** lists a page's changes, with who made them
and their summary. From there you can see any old version, compare two
versions, and **restore** an old one (this is a new change, so nothing is
lost either way).

If someone else saved the page while you were editing it, saving does not
overwrite their work: the editor tells you, and you can copy your text,
reload, and merge by hand.

### Finding your way

- **Search** (or press <kbd>/</kbd>) looks through titles and text.
- **Recent changes** lists the latest edits on the whole wiki.
- **Backlinks** lists the pages that link to the current page.

## Writing pages

Pages are written in **Markdown**, the format used by GitHub, Discord,
Reddit and most note apps. A page is a plain text file: anything you write
here can be read and edited anywhere else.

The editor has **Write**, **Preview** and **Side by side** tabs.
<kbd>Ctrl</kbd>+<kbd>S</kbd> saves; the summary line is optional but helps
others in the history. On a page, <kbd>e</kbd> opens the editor.

### Text

| You type | You get |
| --- | --- |
| `**bold**` | **bold** |
| `*italic*` | *italic* |
| `~~struck~~` | ~~struck~~ |
| `` `code` `` | `code` |
| `<u>underlined</u>` | <u>underlined</u> (Markdown has no underline) |
| `H<sub>2</sub>O`, `x<sup>2</sup>` | H<sub>2</sub>O, x<sup>2</sup> |

**Paragraphs** are separated by a blank line. A single line break inside a
paragraph is ignored (the lines join up). To force a line break, end the
line with a backslash `\` (or two spaces).

To show a character that Markdown would otherwise use (`*`, `_`, `#`,
`[`...), put a backslash before it: `\*not italic\*`.

### Headings

```markdown
# Page title
## Section
### Sub-section
```

The first heading is the page's title. Pages with three headings or more
get a table of contents in the sidebar. Every heading can be linked to with
`#` and its text in lowercase with `-` for spaces: `## Day two` is
`#day-two`.

### Links

```markdown
[Missing pet](/foolslanding/notice/missingpet)
[Day two of the siege](/foolslanding/story/siege#day-two)
[The rules](/rules/)
[A page in the same folder](missingpet)
[A page one folder up](../lore)
[Elsewhere](https://example.com)
<https://example.com>
```

The simplest way: open the page, copy the address from the browser bar,
and keep only the part after the domain name (`/foolslanding/...`). Full
addresses (`https://...`) on their own become links too.

A link without a `/` at the start is relative to the current page's folder.
A link ending with `/` points at a folder's front page.

Links to pages that do not exist are shown in a different colour (a
reminder to write them); links to other websites are marked too.

### Lists

```markdown
- An item
- Another
    - Nested (indent by 4 spaces)

1. First
2. Second

- [ ] A task
- [x] A task done
```

### Quotes, lines, footnotes

```markdown
> Quoted text, which can span
> several lines.

---

The siege lasted three days.[^1]

[^1]: Four, according to the goblins.
```

`---` on its own line, with blank lines around it, draws a horizontal
line. Footnotes are numbered automatically and gathered at the bottom of
the page, whatever name you give them (`[^1]`, `[^goblins]`).

### Tables

```markdown
| Name   | Class  | Level |
| ------ | ------ | ----: |
| Marak  | Ranger |     4 |
| Ysolde | Bard   |     3 |
```

The second line is required. Colons set the alignment: `:---` left,
`:---:` centred, `---:` right. Columns do not need to line up in the text.
Cells hold one line of text (use `<br>` for a line break inside a cell).
Wide tables scroll sideways on small screens.

### Code and preformatted text

````markdown
```
Text shown exactly as typed,
   spaces included, in a fixed-width font.
```
````

### Images and other files

Images and files (pdf, sound) are uploaded to the wiki, in folders that
mirror the page folders: `/_media/foolslanding/map.jpg`.

In the editor, **Images…** shows the files of the page's folder: click one
to insert it, or **Upload…** a new one (jpg, png, gif, webp, svg, pdf, mp3,
wav, ogg, m4a, txt; up to 64 MB). Pasting this works as well:

```markdown
![The coast, seen from the tower](/_media/foolslanding/map.jpg)
[The rules as a pdf](/_media/rules/rules.pdf)
```

The text in square brackets describes the image for people who cannot see
it. Clicking an image opens it full size.

For **size and placement**, use an HTML image tag instead:

```html
<img src="/_media/foolslanding/map.jpg" width="300" align="right" alt="Map">
```

| Attribute | Effect |
| --- | --- |
| `width="300"` or `height="200"` | display size in pixels; the wiki sends a scaled-down copy, so big photos load fast |
| `align="right"` / `align="left"` | float the image beside the text |
| `align="center"` | centre it on its own line |
| `alt="..."` | the description |

An image alone at the very top of a page, right before the title and
without a size or alignment, is shown as a wide **banner** with the title
over its lower edge.

Sound files can be played in the page:

```html
<audio controls src="/_media/foolslanding/tavern-song.mp3"></audio>
```

**Files** in the sidebar lists every uploaded file, with which pages use
it; files can be renamed or moved there (pages using them are updated) and
deleted once no page uses it (a deleted file goes to a bin and can be
recovered by an admin). Unlike pages, uploaded files have no history:
uploading a file with an existing name asks before replacing it.

### Boxes and special styles

Where Markdown has no syntax, HTML can be used directly. The wiki's style
sheet provides a few boxes:

```html
<div class="spoiler">

Blurred until hovered or tapped. **Markdown** works inside.

</div>

The sign reads <span class="badaawi">Welcome</span>.
```

Leave a blank line after the opening tag and before the closing tag,
otherwise the Markdown inside is not processed.

| Class | Effect |
| --- | --- |
| `spoiler` | hides its content until hovered or tapped |
| `badaawi` | writes in the Badaawi script |
| `noticeboard` with `wood1`, `wood2` or `wood3` | a wooden board with pinned notices (put a picture listing inside) |
| `statusboard` with `on_quest`, `looking_for_quest`, `retired`, `dead` or `missing` | character portraits on a scene |

Folders can have their own look (Skarnholm does): their style sheet may add
other classes.

### Page settings (optional)

A page can start with a few settings between two `---` lines. Most pages
need none.

```yaml
---
title: The Siege of Kobold Mound   # instead of the first heading
toc: false                         # no table of contents (true: always one)
---
```

### What exactly is supported

For the curious: pages are [CommonMark](https://commonmark.org) with the
[GitHub extensions](https://github.github.com/gfm/) (tables, strikethrough,
task lists, automatic links) and footnotes. HTML is allowed anywhere.
Nothing else is special, except the optional settings above and the two
blocks below (`list` and `widget`), which other Markdown viewers simply
show as code.

## Page listings

A `list` block shows the pages of a folder, and stays up to date by itself:

````markdown
```list
ns: skarnholm/notice
sort: created
reverse: true
```
````

With no settings at all, it lists the pages of the current folder, by
title, in three columns. Settings, all optional:

| Setting | Values | What it does |
| --- | --- | --- |
| `ns` | a folder: `skarnholm/notice`, `../lore`, `/` | the folder to list (default: the current page's); same rules as links |
| `exclude` | `[start, self, old-notes]` | page names to leave out; `self` is the current page |
| `recursive` | `true` | include pages of all sub-folders too |
| `namespaces` | `true` | also list the sub-folders (linking to their front pages), above the pages |
| `tree` | `true` | a nested tree of sub-folders and their pages instead of a flat list |
| `sort` | `title` (default), `id`, `created`, `modified` | order: by title, by address, by creation date, by last change |
| `reverse` | `true` | reverse the order (newest first with `created` or `modified`) |
| `natural` | `true` | numbers in names sort by value: "part 2" before "part 10" |
| `columns` | a number, default `3` | how many columns |
| `numbered` | `true` | a numbered list |
| `pictures` | `true` | a grid of each page's first image instead of a list; pages without an image show their title |
| `heading` | text | a heading above the pages |
| `namespaces_heading` | text | a heading above the sub-folders |
| `label` | a template, default `{title}` | what each entry shows, see below |
| `dates` | `true` | show the date of the last change after each entry |

In `label`: `{title}` the page title, `{name}` the last part of its
address, `{id}` the full address, `{user}` who created the page,
`{editor}` who changed it last, `{date}` the date of its last change.

Examples:

````markdown
```list
sort: created
reverse: true
columns: 1
label: "{title} ({user})"
exclude: [self]
```

```list
ns: foolslanding/chronicles
sort: id
natural: true
numbered: true
columns: 1
```

```list
recursive: true
tree: true
```
````

The notice board, as a wooden board of pinned pictures:

````markdown
<div class="noticeboard wood1">

```list
ns: foolslanding/notice
pictures: true
sort: created
reverse: true
exclude: [start]
```

</div>
````

## Widgets

A `widget` block puts an interactive tool in the page. The first word after
`widget` is its name; the lines inside are its settings:

````markdown
```widget map
data: skarnholm/mapdata.json
```
````

Widgets read and write **data files** (`.json`) kept in the wiki like
pages: every change a widget makes is saved with your name in the history,
and can be restored like a page edit. Data files can also be opened and
edited from **Files**.

<!-- widgets -->

## Accounts

Accounts are created by an admin, who gives each one a role:

| Role | Can |
| --- | --- |
| reader | read every page and its history |
| editor | also edit, create, rename, upload, use widgets that save |
| admin | also manage accounts and the wiki's settings (`_config`) |

**Your account** in the sidebar changes your password, real name and
email (only admins see these two).

## How the wiki is built (for admins)

Everything (pages, data files, layouts, widgets, accounts) is stored in a
git repository, one commit per change, so the whole wiki can be copied,
backed up and restored. Uploaded files are stored next to it, not in git.

- `_layout/page.html` and `_layout/style.css` give the wiki its look; a
  folder can have its own `_layout/` (Skarnholm does), which applies to
  everything under it.
- `_widgets/<name>/widget.js` (with optional `widget.css` and
  `README.md`, the documentation shown above) defines a widget.
- `_config/` holds the accounts, notification rules and the list of
  uploaded files. Only admins can see it.
