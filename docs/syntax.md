# Page syntax

Pages are plain markdown: CommonMark plus GitHub Flavored Markdown (tables, strikethrough, autolinks, footnotes). Raw HTML is allowed and used where markdown has no syntax. The wiki adds only two things: optional frontmatter, and fenced code blocks with the `list` or `widget` language.

A page file `foolslanding/notice/missingpet.md` is the page `foolslanding/notice/missingpet`, served at `/foolslanding/notice/missingpet`. `start` is a namespace's front page: `/foolslanding/` shows `foolslanding/start.md`.

## Links

Standard markdown links. Internal links use the page's URL path, so a link can be pasted straight from the browser's address bar:

```markdown
[Missing pet](/foolslanding/notice/missingpet)
[Siege, day two](/foolslanding/story/siege#day-two)
[Rules](/rules/)
[Elsewhere](https://example.com)
```

Relative paths (`missingpet`, `../story/siege`) also work. Links to pages that do not exist are shown in a different colour.

Page ids are lowercase, with `-` for spaces and punctuation, and accents removed (`Fool's Landing` → `fool-s-landing`).

## Images and media

Standard markdown images; media files are served under `/_media/`:

```markdown
![Map of the coast](/_media/foolslanding/map.jpg)
```

When size or alignment is needed, use an HTML `<img>` tag:

```html
<img src="/_media/foolslanding/map.jpg" width="200" align="right" alt="Map">
```

The server scales images down to the requested `width` (or `height`), so large files are not sent in full. Clicking an image opens the full-size file. Other media files (pdf, mp3) are ordinary links, or `<audio controls src="…">`.

## Boxes and styled text

Use HTML with a class. Leave blank lines around the markdown inside, as CommonMark requires:

```html
<div class="spoiler">

Hidden text, **with markdown**.

</div>

The sign reads <span class="badaawi">Welcome</span>.
```

The classes available come from the namespace's layout CSS (`spoiler`, `noticeboard`, `wood1`, `statusboard`, `badaawi`, ...).

## Frontmatter (optional)

```yaml
---
title: Override title     # default: the page's first heading
toc: false                # default: table of contents when the page has 3+ headings
---
```

## Page listings

A fenced code block with the language `list`. Its body is YAML:

````markdown
```list
ns: skarnholm/notice      # default: the current namespace
exclude: [start]          # page names to leave out; "self" = the current page
sort: created             # title (default), id, created, modified
reverse: true
columns: 2                # default 3
numbered: true
pictures: true            # grid of each page's first image instead of a list
recursive: true           # include pages in sub-namespaces
namespaces: true          # also list sub-namespaces
tree: true                # nested tree of sub-namespaces and pages
heading: Pages            # heading shown above the list (default: none)
```
````

## Widgets

A fenced code block with the language `widget`. The first word is the widget name (from `_widgets/<name>/`); the rest of the body is passed to the widget as YAML:

````markdown
```widget map
data: skarnholm/map.json
```
````

In any other markdown viewer, `list` and `widget` blocks simply show as code.
