package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// typeDir maps a doc type to its on-disk folder (BUILD_SPEC §4.1).
func typeDir(t DocType) string {
	switch t {
	case TypeTask:
		return "tasks"
	case TypeContact:
		return "contacts"
	case TypeChannel:
		return "channels"
	case TypeCompany:
		return "companies"
	case TypeEmail:
		return "emails"
	case TypeMemory:
		return "memory"
	default:
		return "docs"
	}
}

// Store is the on-disk workspace: tabbed HTML files with embedded JSON under a
// root directory, plus the derived INDEX.md.
type Store struct {
	Root string
}

// OpenStore returns a Store rooted at path, creating the layout if needed.
func OpenStore(path string) (*Store, error) {
	s := &Store{Root: path}
	for _, t := range []DocType{TypeDoc, TypeTask, TypeContact, TypeChannel, TypeCompany, TypeEmail, TypeMemory} {
		if err := os.MkdirAll(filepath.Join(path, typeDir(t)), 0o755); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Join(path, "_audit"), 0o755); err != nil {
		return nil, err
	}
	return s, nil
}

// Path returns the HTML file path for a doc slug/type.
func (s *Store) Path(t DocType, slug string) string {
	return filepath.Join(s.Root, typeDir(t), slug+".html")
}

// Save renders the doc to its HTML file and refreshes INDEX.md. Audit fields
// (last_editor) are set here; last_commit is filled by the sync layer.
func (s *Store) Save(d *Doc, actor string) error {
	if actor != "" {
		d.Audit.LastEditor = actor
	}
	page, err := RenderDoc(d)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path(d.Type, d.Slug)), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(s.Path(d.Type, d.Slug), page, 0o644); err != nil {
		return err
	}
	return s.Reindex()
}

// Load reads a doc by slug (type inferred from which folder holds it).
func (s *Store) Load(slug string) (*Doc, error) {
	for _, t := range []DocType{TypeDoc, TypeTask, TypeContact, TypeChannel, TypeCompany, TypeEmail, TypeMemory} {
		page, err := os.ReadFile(s.Path(t, slug))
		if err == nil {
			return ExtractDoc(page)
		}
	}
	return nil, fmt.Errorf("doc %q not found", slug)
}

// LoadByID scans the store for a doc with the given ID.
func (s *Store) LoadByID(id string) (*Doc, error) {
	docs, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, d := range docs {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, fmt.Errorf("doc id %q not found", id)
}

// Resolve finds a doc by slug or by ID prefix.
func (s *Store) Resolve(idOrSlug string) (*Doc, error) {
	if d, err := s.Load(idOrSlug); err == nil {
		return d, nil
	}
	docs, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, d := range docs {
		if strings.HasPrefix(d.ID, idOrSlug) {
			return d, nil
		}
	}
	return nil, fmt.Errorf("doc %q not found", idOrSlug)
}

// List returns every doc in the store.
func (s *Store) List() ([]*Doc, error) {
	var docs []*Doc
	for _, t := range []DocType{TypeDoc, TypeTask, TypeContact, TypeChannel, TypeCompany, TypeEmail, TypeMemory} {
		dir := filepath.Join(s.Root, typeDir(t))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
				continue
			}
			page, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			d, err := ExtractDoc(page)
			if err != nil {
				continue // unindexed file; hygiene reports it
			}
			docs = append(docs, d)
		}
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Updated > docs[j].Updated })
	return docs, nil
}

// Lookup returns a loader function over the store (for BrokenLinks).
func (s *Store) Lookup() func(id string) *Doc {
	return func(id string) *Doc {
		d, err := s.LoadByID(id)
		if err != nil {
			return nil
		}
		return d
	}
}

// WriteIndex regenerates INDEX.md from the docs (BUILD_SPEC §4.4).
func (s *Store) WriteIndex(docs []*Doc) error {
	var b strings.Builder
	b.WriteString("# Index\n\n")
	b.WriteString("| slug | type | title | updated | tags | link-count |\n")
	b.WriteString("|------|------|-------|---------|------|------------|\n")
	for _, d := range docs {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %d |\n",
			d.Slug, d.Type, mdEscape(d.Title), strings.SplitN(d.Updated, "T", 2)[0], strings.Join(d.Tags, " "), linkCount(d))
	}
	return os.WriteFile(filepath.Join(s.Root, "INDEX.md"), []byte(b.String()), 0o644)
}

// Reindex lists and rewrites INDEX.md.
func (s *Store) Reindex() error {
	docs, err := s.List()
	if err != nil {
		return err
	}
	return s.WriteIndex(docs)
}

func mdEscape(s string) string {
	r := strings.NewReplacer("|", "\\|", "\n", " ")
	return r.Replace(s)
}
