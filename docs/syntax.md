# Page syntax

Pages are markdown files (`.md`) in the content repo. The path is the page id: `foolslanding/notice/missingpet.md` is the page `foolslanding/notice/missingpet`, served at `/foolslanding/notice/missingpet`. `start` is the namespace's front page: `/foolslanding/` shows `foolslanding/start.md`.

Base syntax is CommonMark plus GFM tables, strikethrough (`~~x~~`), autolinked URLs and footnotes (`[^1]`). Raw HTML is allowed. The wiki adds the following.

## Frontmatter (optional)

```yaml
---
title: Override title      # default: the first heading of the page
toc: false                # default: a table of contents is shown when the page has 3+ headings
---
```

Unknown keys are kept and exposed to listings and widgets.

## Page ids

Ids are normalised before lookup, so links can be written naturally:

- lowercase; accents removed (`é` → `e`)
- `:` and `;` become `/` (the namespace separator), so old `ns:page` ids still resolve
- any character other than letters, digits, `.`, `_`, `-` and `/` becomes `-`; runs of `-` collapse to one
- each segment is trimmed of leading and trailing `.`, `_`, `-`

`[[Fool's Landing]]` therefore links to `fool-s-landing`.

## Wiki-links

```
[[target]]
[[target|label]]
[[target#section|label]]
[[#section]]
[[target|{{image.jpg?100}}]]
[[https://example.com|label]]
```

Resolving `target` against the current page's namespace (`ns`):

| Form | Resolves to |
| --- | --- |
| `page` (no `/`) | `ns/page` |
| `a/b` (contains `/`, no leading `.`) | `a/b`, absolute |
| `/a/b` | `a/b`, absolute |
| `./page`, `../page` | relative to `ns` |
| `a/b/` (trailing `/`) | `a/b/start`, absolute (so `a/` is `a/start`) |
| empty with `#section` | same page |

With no label, the link shows the target page's title, or the target text if the page does not exist. Links to missing pages get the class `wikilink2`; existing ones `wikilink1`.

## Media embeds

```
{{path}}            {{path|caption}}
{{path?200}}        {{path?200x100}}     {{path?x100}}
{{path?nolink}}     {{path?direct}}      {{path?linkonly}}
{{ path}}  right    {{path }}  left      {{ path }}  centre
```

`path` resolves like a wiki-link (bare name = current namespace) inside the media folder. Parameters combine with `&` (`{{a.jpg?200&nolink}}`). Default behaviour: the image links to the full-size file. Non-image files (pdf, mp3, wav) render as a download link; audio files render as an audio player. A media `path` may also be an external URL.

## Containers

```
::: spoiler
Hidden text, **markdown** inside.
:::
```

Renders `<div class="spoiler">…</div>`; several classes are allowed (`::: noticeboard wood1`). Containers can nest; an inner fence may use more colons (`::::`).

## Directives

A line starting with `::` followed by a name. Arguments are a list of words and `key=value` pairs (values may be quoted).

### `::list [namespace] [options]`

Lists the pages in a namespace (default: the current one).

| Option | Meaning |
| --- | --- |
| `exclude` | leave out the current page |
| `exclude=a,b` | leave out these page names |
| `cols=N` | number of columns (default 3) |
| `numbered` | numbered list instead of bullets |
| `sort=title` (default), `sort=id`, `sort=created`, `sort=modified` | order; `natural` sorts numbers by value |
| `reverse` | reverse the order |
| `pictures` | a grid of each page's first image, linking to the page |
| `recursive` | include pages in sub-namespaces |
| `namespaces` | also list sub-namespaces (linking to their start pages) |
| `tree` | nested tree of sub-namespaces and pages |
| `heading="…"` | heading shown above the pages (default none) |

### `::widget name [args]`

Embeds the widget from `_widgets/name/` (`widget.js`, optional `widget.css`).

### `::toc`

Forces the table of contents on, at that position.
