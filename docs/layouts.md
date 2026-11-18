# Layouts

A wiki's look is a few files in its content repository, edited from
**Files** like any page (by tinkerers and admins). Without any, the wiki
uses a plain built-in layout.

## Which files apply

```
_layout/page.html          the page template
_layout/style.css          the stylesheet
_layout/sidebar.md         the sidebar, in Markdown (optional)
_layout/…                  anything else it needs: fonts, images, scripts
stories/_layout/style.css  more style for everything under stories/
stories/_layout/sidebar.md a different sidebar there
```

For a page, the wiki looks at the `_layout` folders from the page's
folder up to the top:

- **The template** is the nearest `page.html`.
- **The stylesheets** are every `style.css` from that template's folder
  down to the page's folder, outermost first. So a folder can change
  colours and fonts with a short `style.css` of its own and keep the
  template.
- **The sidebar** is the nearest `sidebar.md`, shown only to people who
  can read the wiki.

Files in `_layout` folders (except `.md`) are served as they are, to
everyone, so the template can use them: `/_layout/fonts/x.woff2`,
`/_layout/theme.js`.

A template that does not parse is refused when saving, so a typo cannot
take the site down.

## page.html

A Go [html/template](https://pkg.go.dev/html/template). The built-in
layout is a complete, short example to start from:
[internal/server/ui/default-layout.html](../internal/server/ui/default-layout.html).

A template must output `{{.Head}}` in the `<head>`, `{{.Scripts}}` before
`</body>`, and the stylesheets:

```html
{{range .Styles}}<link rel="stylesheet" href="{{.}}">{{end}}
```

### Fields

| Field | What it holds |
| --- | --- |
| `.Title` | the page's title (its first heading), or the special page's name |
| `.SiteTitle` | the site's name (`--title`) |
| `.ID` | the page's id (`stories/the-siege`); special pages start with `_` |
| `.URL` | the page's address |
| `.Mode` | what is shown: `show` (a page), `missing`, `edit`, `history`, `revision`, `diff`, `move`, `delete`, `backlinks`, `create`, `search`, `index`, `recent`, `files`, `help`, `login`, `account`, `admin`, `users`. Handy as a class: `<body class="mode-{{.Mode}}">` |
| `.Exists` | whether the page exists |
| `.Content` | the main content, as HTML: the page, the editor, a history list... |
| `.TOC` | the table of contents (`<nav class="toc">`), for pages with three headings or more; empty otherwise |
| `.Sidebar` | the rendered `sidebar.md`, or empty |
| `.Crumbs` | the trail from the front page: each has `.Title`, `.URL`, and `.Last` for the current page |
| `.Tools` | the actions this person can take here, see below |
| `.Modified`, `.Author` | the page's last change and who made it (zero for special pages) |
| `.Backlinks` | how many pages link here |
| `.User` | the logged-in account's name, or empty |
| `.CanRead`, `.CanEdit`, `.IsAdmin` | what this person can do (`.CanRead` is false on the login page of a private wiki) |
| `.Styles` | the stylesheet addresses, outermost first |
| `.Layout` | the address of the folder holding this `page.html`, e.g. `/_layout/` |
| `.Head` | the core stylesheet and widget styles, for `<head>` |
| `.Scripts` | the core scripts and the page's widgets, for the end of `<body>` |

### Tools

Each tool has `.Name`, `.Label`, `.URL`, `.Active` (it is the current
view) and `.Post` (it must be a form posting to `.URL`: the logout). The
list only holds
what this person may do, in this order:

| Name | Label |
| --- | --- |
| `edit` | Edit, or Create on a missing page |
| `history`, `move`, `delete`, `backlinks` | the page's History, Rename, Delete, Backlinks |
| `recent`, `create`, `files`, `help` | Recent changes, New page, Files, Help |
| `account`, `admin`, `logout` / `login` | Your account, Admin (for admins), Log out or Log in |

A template can show them all in a loop, or pick them by name:

```html
{{range .Tools}}{{if eq .Name "edit" "history"}}<a href="{{.URL}}">{{.Label}}</a>{{end}}{{end}}
```

### Functions

| Function | |
| --- | --- |
| `date` | a time as `2026/11/18 14:32` |
| `ago` | a time as `3 days ago` |
| `pageurl` | a page id's address |
| `static` | the address of a built-in file, e.g. `{{static "core.css"}}` |

### Keyboard and page finder

The core script gives every layout these: <kbd>Ctrl</kbd>+<kbd>K</kbd>
opens the page finder, and so does a click on
any link to `/_/search`; <kbd>e</kbd> follows the link marked
`data-tool="edit"`, so give the Edit tool's link that attribute
(`<a href="{{.URL}}" data-tool="{{.Name}}">`).

## What pages are made of

The rendered Markdown is standard HTML, plus a few classes to style:

| HTML | What |
| --- | --- |
| `a.missing` | a link to a page that does not exist yet |
| `a.external` | a link to another site |
| `a.image > img` | an image (linking to the full file); presets add the classes `small`, `medium`, `full`, `left`, `right` |
| `figure > figcaption` | an image with a caption |
| `p.gallery` | several images together |
| `div.table-wrap > table` | a table, scrollable on small screens |
| `nav.toc` | the table of contents |
| `div.pagelist`, `.pagelist-heading` | a `list` block; `ul` with a `--columns` CSS variable, `.tree` for trees, `.date` for dates, `.no-picture` for picture cards without one |
| `div.widget.widget-<name>` | where a widget draws itself |

The core stylesheet, loaded before the layout's, gives the built-in pages
(editor, history, files...) a plain look and the image layout. Its image
rules have no specificity (`:where()`), so a layout can restyle any of
them. The built-in pages use `lw-` classes (`.lw-editor`, `.lw-history`,
`.lw-button`...), which a layout may restyle too.

## The sidebar

`_layout/sidebar.md` is ordinary Markdown, rendered like a page (links,
lists, even widgets), and placed where the template puts `{{.Sidebar}}`.
A bold line followed by a list of links is a natural way to write sections.

## Boxes and other classes

Where Markdown has no syntax, editors write HTML with a class, and the
layout's stylesheet gives it a look:

```html
<div class="spoiler">

Hidden **Markdown** here.

</div>
```

Which classes exist is up to the layout; the user guide tells editors that
the wiki lists them on one of its pages, so write one.
