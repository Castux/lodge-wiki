# Widgets

A widget is a small JavaScript tool that lives in the wiki's content
repository and draws itself into pages: a map, a calendar, statistics, a
list of recent changes. Tinkerers and admins write them from **Files**.

## Using one

In a page:

````markdown
```widget map
data: atlas/places.json
zoom: 2
```
````

The first word after `widget` is the widget's name (`a-z`, `0-9`, `-`);
the lines inside are its settings, in YAML. Other Markdown viewers show the
block as code.

## Writing one

```
_widgets/<name>/widget.js    required
_widgets/<name>/widget.css   optional
_widgets/<name>/README.md    optional: its documentation
```

The wiki loads `widget.js` (and `widget.css`) on every page that uses the
widget, after its own script, then the widget registers itself:

```js
wiki.widget("map", async (box, config) => {
  // box: the element to draw in (div.widget.widget-map)
  // config: the block's settings, as an object ({data: "atlas/places.json", zoom: 2})
  const { data, rev } = await wiki.read(config.data);
  box.textContent = Object.keys(data).length + " places";
});
```

The function runs once for each block of that widget on the page. An
error it throws before its first `await` is shown in its box; catch the
later ones yourself.

`README.md` is added to the wiki's Help page, under Widgets: describe the
settings there, for the people who will use it.

## Reading and writing files

`window.wiki` gives widgets the content repository's pages (`.md`) and data
files (`.json`), outside the special `_` folders:

```js
const { data, rev } = await wiki.read("atlas/places.json");
// data: the parsed JSON (or a page's Markdown text); rev: its revision

const newRev = await wiki.write("atlas/places.json", data, rev, {
  summary: "Added the old mill",   // the change's summary in the history
  widget: "map",                    // noted in the summary
});

const { pages, namespaces } = await wiki.list("atlas");
// pages: [{id, title, url, created, modified, author, image}], namespaces: ["atlas/north", …]
```

Every write is a commit by the logged-in person, who must be an editor or
above; readers can only read. A write names the revision it started from
(`"new"` to create a file): if someone changed the file since, it fails
with `err.conflict = true` (and `err.rev`, the current revision) instead
of overwriting. Read again, apply the change, write again. Writes are
limited to 30 a minute per person. JSON must be valid.

## Choosing a page

`wiki.findPage` shows the wiki's page finder (the one of Ctrl+K), for a
field that links to a page:

```js
const got = await wiki.findPage({
  placeholder: "Link to a page…", // the finder's prompt
  query: input.value,              // initial search text
  text: true,                      // a last row keeps the typed text as is
});
// got: {id, title, url} for a page, {text} for the typed text, null if closed
```

## Other data

Plain `GET` requests, for logged-in readers, returning JSON:

| Address | Returns |
| --- | --- |
| `/_/titles` | every page: `[{id, t (title), u (address)}]` (cached by the browser, cheap to ask again) |
| `/_/list?ns=<folder>` | what `wiki.list` returns |
| `/_/log?since=YYYY-MM-DD` | the commits since that date, oldest first: `[{hash, author, time, message, files: [{status, path, from}]}]` (`status`: `A`, `M`, `D`, `R`) |
| `/_/media?ns=<folder>` | the uploaded files of a folder: `{ns, folders, files: [{name, url, size, image}]}` |
| `/_/raw/<path>` | a page's Markdown or a data file, with its revision in the `X-Revision` header (what `wiki.read` uses) |

Uploaded files are at `/_media/<path>`; images can be asked for at a
width with `?w=<pixels>`.

## Safety

A widget's code runs in every reader's browser with their rights, which
is why only tinkerers and admins can change `_widgets`. Every write it
makes is in the history, under the reader's name, and can be undone.
