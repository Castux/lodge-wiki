package server

import (
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Castux/lodge-wiki/docs"
	"github.com/Castux/lodge-wiki/internal/wiki"
)

// help shows the user guide, with each widget's README.md (from
// _widgets/<name>/ in the content repo) in place of "<!-- widgets -->".
func (s *Server) help(w http.ResponseWriter, r *http.Request) {
	body := strings.Replace(docs.Guide, "<!-- widgets -->", s.widgetDocs(), 1)
	res := s.render.Render(wiki.ParsePage("_help", []byte(body)))
	s.show(w, r, &View{ID: "_help", Title: "Help", Mode: "help",
		Content: template.HTML(res.HTML), TOC: template.HTML(res.TOC)})
}

func (s *Server) widgetDocs() string {
	files, _ := filepath.Glob(filepath.Join(s.store.File("_widgets"), "*", "README.md"))
	sort.Strings(files)
	var b strings.Builder
	for _, f := range files {
		if text, err := os.ReadFile(f); err == nil {
			b.WriteString(strings.TrimSpace(string(text)) + "\n\n")
		}
	}
	return b.String()
}
