# lodge-wiki

A custom wiki for the Chaser's Lodge group: one Go binary, markdown pages and JSON data files in a git repository, every write a commit.

Built for this group, not as a generic product. The rule: anything generic is core Go code; anything specific to the group (pages, data files, layouts, widgets) lives in the content repo.

- Plan and architecture: [PLAN.md](PLAN.md)
- Content repo: [`lodge-data`](https://github.com/Castux/lodge-data), checked out as the sibling folder `../lodge-data`
- Current DokuWiki copy (migration source): `../public_html`
