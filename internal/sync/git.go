// Package sync wraps git as the version & audit layer (D3, BUILD_SPEC §7).
// Git IS the audit trail: every commit's author is "human:<id>" or
// "agent:<id>". Push/pull to the main share (a bare repo) is the sync
// transport; reconciliation merges at the block level via the store's
// leet-merge rules — never a silent overwrite.
package sync

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"leetoffice/internal/store"
)

var ErrNoChanges = errors.New("no changes to commit")

// Repo is a git-backed store root.
type Repo struct {
	dir  string
	repo *git.Repository
	wt   *git.Worktree
}

// DefaultBranch is the store branch.
const DefaultBranch = "main"

// Init opens or creates the git repo at dir (plain, with worktree).
func Init(dir string) (*Repo, error) {
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		if !errors.Is(err, git.ErrRepositoryAlreadyExists) {
			return nil, err
		}
		repo, err = git.PlainOpen(dir)
		if err != nil {
			return nil, err
		}
	}
	wt, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	r := &Repo{dir: dir, repo: repo, wt: wt}
	// newborn repos default to master; pin HEAD to main
	if ref, err := repo.Head(); err != nil || ref.Name() != plumbing.NewBranchReferenceName(DefaultBranch) {
		if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(
			plumbing.HEAD, plumbing.NewBranchReferenceName(DefaultBranch))); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// InitBare creates (or opens) a bare repo — the main share / coordinator side.
func InitBare(dir string) (*git.Repository, error) {
	repo, err := git.PlainInit(dir, true)
	if err == nil {
		// pin HEAD to main so the served repo advertises the right default
		if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(
			plumbing.HEAD, plumbing.NewBranchReferenceName(DefaultBranch))); err != nil {
			return nil, err
		}
		return repo, nil
	}
	if errors.Is(err, git.ErrRepositoryAlreadyExists) {
		repo, rerr := git.PlainOpen(dir)
		if rerr != nil {
			return nil, rerr
		}
		if ref, herr := repo.Head(); herr != nil {
			_ = repo.Storer.SetReference(plumbing.NewSymbolicReference(
				plumbing.HEAD, plumbing.NewBranchReferenceName(DefaultBranch)))
		} else if ref.Name() != plumbing.NewBranchReferenceName(DefaultBranch) {
			_ = repo.Storer.SetReference(plumbing.NewSymbolicReference(
				plumbing.HEAD, plumbing.NewBranchReferenceName(DefaultBranch)))
		}
		return repo, nil
	}
	return nil, err
}

// actorSignature turns "human:josh" into a git identity (§7.1).
func actorSignature(actor string) *object.Signature {
	kind, id := "human", actor
	if k, i, ok := strings.Cut(actor, ":"); ok {
		kind, id = k, i
	}
	return &object.Signature{
		Name:  actor,
		Email: fmt.Sprintf("%s+%s@leetoffice.local", kind, id),
		When:  time.Now().UTC(),
	}
}

// CommitAll stages everything and commits attributed to actor. Returns the
// commit sha. ErrNoChanges if the tree is clean.
func (r *Repo) CommitAll(actor, msg string) (plumbing.Hash, error) {
	if _, err := r.wt.Add("."); err != nil {
		return plumbing.ZeroHash, err
	}
	status, err := r.wt.Status()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if status.IsClean() {
		return plumbing.ZeroHash, ErrNoChanges
	}
	c, err := r.wt.Commit(msg, &git.CommitOptions{
		Author: actorSignature(actor),
		All:    true,
	})
	if err != nil {
		return plumbing.ZeroHash, err
	}
	r.stampLastCommit(c, actor)
	return c, nil
}

// stampLastCommit writes the commit sha into each saved doc's audit field.
// It re-saves the docs touched by the commit and amends nothing — the sha is
// informational for audit_query fallback, git log remains authoritative.
func (r *Repo) stampLastCommit(sha plumbing.Hash, actor string) {
	s, err := store.OpenStore(r.dir)
	if err != nil {
		return
	}
	docs, err := s.List()
	if err != nil {
		return
	}
	for _, d := range docs {
		if d.Audit.LastCommit == sha.String() {
			continue
		}
		d.Audit.LastCommit = sha.String()
		_ = s.Save(d, actor) // best-effort; INDEX refresh is idempotent
	}
}

// AddRemote registers (or replaces) the main-share remote.
func (r *Repo) AddRemote(name, url string) error {
	if err := r.repo.DeleteRemote(name); err != nil {
		_ = err // may not exist
	}
	_, err := r.repo.CreateRemote(&config.RemoteConfig{
		Name: name,
		URLs: []string{url},
		Fetch: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/" + name + "/*"),
		},
	})
	return err
}

// remoteHead fetches from remote and returns its head hash for the branch.
// plumbing.ZeroHash means the remote is reachable but has no branch yet.
func (r *Repo) remoteHead(name string) (plumbing.Hash, error) {
	err := r.repo.Fetch(&git.FetchOptions{RemoteName: name, Force: true})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) &&
		!errors.Is(err, git.ErrNonFastForwardUpdate) &&
		!errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return plumbing.ZeroHash, err
	}
	ref, err := r.repo.Reference(plumbing.NewRemoteReferenceName(name, DefaultBranch), true)
	if err != nil {
		return plumbing.ZeroHash, nil // empty remote — nothing fetched yet
	}
	return ref.Hash(), nil
}

// Sync performs the auto-rejoin sequence (§6.5): fetch → block-merge →
// commit → push. Different-block edits merge cleanly; same-block edits keep
// both versions with a conflict flag (returned as Conflicts).
type SyncResult struct {
	Pulled     bool
	Pushed     bool
	Merged     bool
	Conflicts  []store.Conflict
	HeadCommit string
}

func (r *Repo) Sync(remoteName, actor string) (*SyncResult, error) {
	res := &SyncResult{}
	head, err := r.repo.Head()
	if err != nil {
		// unborn HEAD (fresh clone): check out the remote tip, or seed by push
		remoteHash, rerr := r.remoteHead(remoteName)
		if rerr != nil {
			return nil, rerr
		}
		if remoteHash == plumbing.ZeroHash {
			pushed, err := r.push(remoteName)
			res.Pushed = pushed
			return res, err
		}
		if err := r.wt.Checkout(&git.CheckoutOptions{
			Hash:   remoteHash,
			Branch: plumbing.NewBranchReferenceName(DefaultBranch),
			Create: true, Force: true,
		}); err != nil {
			return nil, err
		}
		res.Pulled = true
		return res, nil
	}
	remoteHash, err := r.remoteHead(remoteName)
	if err != nil {
		return nil, err
	}
	if remoteHash == plumbing.ZeroHash { // reachable but empty → we seed it
		pushed, err := r.push(remoteName)
		res.Pushed = pushed
		return res, err
	}
	if remoteHash == head.Hash() {
		// fully up to date — nothing to pull or push
		return res, nil
	}

	base, err := r.mergeBase(head.Hash(), remoteHash)
	if err != nil {
		return nil, fmt.Errorf("merge-base(head=%s, remote=%s): %w", head.Hash(), remoteHash, err)
	}

	if base == remoteHash { // we're ahead only → just push
		pushed, err := r.push(remoteName)
		res.Pushed = pushed
		return res, err
	}

	if base == head.Hash() { // remote ahead only → fast-forward
		if err := r.wt.Reset(&git.ResetOptions{Mode: git.HardReset, Commit: remoteHash}); err != nil {
			return nil, err
		}
		res.Pulled = true
		return res, nil
	}

	// Diverged: three-way block merge of the store, then commit & push.
	conflicts, err := r.mergeInto(remoteHash, base, actor)
	if err != nil {
		return res, fmt.Errorf("merge-into(remote=%s, base=%s): %w", remoteHash, base, err)
	}
	res.Merged = true
	res.Pulled = true
	res.Conflicts = conflicts
	if err != nil {
		return res, err
	}
	_, perr := r.push(remoteName)
	res.Pushed = perr == nil
	err = perr
	final, _ := r.repo.Head()
	if final != nil {
		res.HeadCommit = final.Hash().String()
	}
	return res, nil
}

// push uploads local commits; pushed=false when the remote already had
// everything (go-git's already-up-to-date), so idle cycles never masquerade
// as activity in logs and SyncResults.
func (r *Repo) push(remoteName string) (pushed bool, err error) {
	err = r.repo.Push(&git.PushOptions{
		RemoteName: remoteName,
		RefSpecs:   []config.RefSpec{"+refs/heads/" + DefaultBranch + ":refs/heads/" + DefaultBranch},
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repo) mergeBase(a, b plumbing.Hash) (plumbing.Hash, error) {
	ca, err := object.GetCommit(r.repo.Storer, a)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	cb, err := object.GetCommit(r.repo.Storer, b)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	bases, err := ca.MergeBase(cb)
	if err != nil || len(bases) == 0 {
		return plumbing.ZeroHash, errors.New("no merge base")
	}
	return bases[0].Hash, nil
}

// changedFiles maps path→blob hash for files that differ between two commits.
func (r *Repo) changedFiles(from, to plumbing.Hash) (map[string]plumbing.Hash, error) {
	tFrom, err := commitTree(r.repo, from)
	if err != nil {
		return nil, err
	}
	tTo, err := commitTree(r.repo, to)
	if err != nil {
		return nil, err
	}
	out := map[string]plumbing.Hash{}
	for path, h := range tTo {
		if fh, ok := tFrom[path]; !ok || fh != h {
			out[path] = h
		}
	}
	for path := range tFrom {
		if _, ok := tTo[path]; !ok {
			out[path] = plumbing.ZeroHash // deleted
		}
	}
	return out, nil
}

func commitTree(repo *git.Repository, h plumbing.Hash) (map[string]plumbing.Hash, error) {
	out := map[string]plumbing.Hash{}
	if h == plumbing.ZeroHash { // root commit: diff against an empty tree
		return out, nil
	}
	c, err := object.GetCommit(repo.Storer, h)
	if err != nil {
		return nil, err
	}
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	err = tree.Files().ForEach(func(f *object.File) error {
		out[f.Name] = f.Hash
		return nil
	})
	return out, err
}

func fileAt(repo *git.Repository, commit plumbing.Hash, path string) ([]byte, error) {
	if commit == plumbing.ZeroHash {
		return nil, os.ErrNotExist
	}
	c, err := object.GetCommit(repo.Storer, commit)
	if err != nil {
		return nil, err
	}
	f, err := c.File(path)
	if err != nil {
		return nil, err
	}
	rc, err := f.Blob.Reader()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// blobAt reads a blob directly by hash (changedFiles maps path → blob hash).
func blobAt(repo *git.Repository, hash plumbing.Hash) ([]byte, error) {
	b, err := object.GetBlob(repo.Storer, hash)
	if err != nil {
		return nil, err
	}
	rc, err := b.Reader()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// mergeInto three-way merges remote into HEAD at the store level: for each
// file changed on both sides, block-merge the embedded JSON docs; files changed
// on one side only are taken from that side. Writes the result to the worktree
// and commits it with two parents.
func (r *Repo) mergeInto(remoteHash, base plumbing.Hash, actor string) ([]store.Conflict, error) {
	head, err := r.repo.Head()
	if err != nil {
		return nil, err
	}
	oursFiles, err := r.changedFiles(base, head.Hash())
	if err != nil {
		return nil, fmt.Errorf("diff(base→head): %w", err)
	}
	theirsFiles, err := r.changedFiles(base, remoteHash)
	if err != nil {
		return nil, fmt.Errorf("diff(base→remote): %w", err)
	}

	var conflicts []store.Conflict
	take := map[string]plumbing.Hash{} // path → version to write
	written := map[string]bool{}       // path → already written by block merge

	for path, oh := range oursFiles {
		th, both := theirsFiles[path]
		if !both {
			take[path] = oh // only ours changed
			continue
		}
		if oh == th {
			take[path] = oh // same change
			continue
		}
		// both changed
		if isStoreDoc(path) {
			merged, conf, err := r.mergeDocFile(path, base, head.Hash(), remoteHash)
			if err != nil {
				return conflicts, fmt.Errorf("merge %s: %w", path, err)
			}
			conflicts = append(conflicts, conf...)
			if err := os.MkdirAll(filepath.Dir(filepath.Join(r.dir, path)), 0o755); err != nil {
				return conflicts, err
			}
			if err := os.WriteFile(filepath.Join(r.dir, path), merged, 0o644); err != nil {
				return conflicts, err
			}
			written[path] = true
		} else {
			// non-doc file changed on both sides: keep ours and flag
			take[path] = oh
			conflicts = append(conflicts, store.Conflict{Slug: path, Resolved: "ours-retained-non-doc"})
		}
	}
	for path, th := range theirsFiles {
		if _, ours := oursFiles[path]; ours {
			continue
		}
		take[path] = th // only theirs changed
	}

	// Apply taken versions to the worktree.
	for path, h := range take {
		full := filepath.Join(r.dir, path)
		if h == plumbing.ZeroHash {
			_ = os.Remove(full)
			_, _ = r.wt.Remove(path)
			continue
		}
		if written[path] {
			continue
		}
		blob, err := blobAt(r.repo, h)
		if err != nil {
			return conflicts, err
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return conflicts, err
		}
		if err := os.WriteFile(full, blob, 0o644); err != nil {
			return conflicts, err
		}
	}

	// Regenerate derived files post-merge (§7.2).
	if s, err := store.OpenStore(r.dir); err == nil {
		_ = s.Reindex()
	}

	if _, err := r.wt.Add("."); err != nil {
		return conflicts, err
	}
	status, err := r.wt.Status()
	if err != nil {
		return conflicts, err
	}
	if status.IsClean() {
		// merge produced identical content; just move HEAD
		return conflicts, r.wt.Reset(&git.ResetOptions{Mode: git.HardReset, Commit: remoteHash})
	}
	_, err = r.wt.Commit("merge: block-level merge of main share", &git.CommitOptions{
		Author: actorSignature(actor),
		Parents: []plumbing.Hash{
			head.Hash(), remoteHash,
		},
	})
	return conflicts, err
}

func (r *Repo) mergeDocFile(path string, base, ours, theirs plumbing.Hash) ([]byte, []store.Conflict, error) {
	readDoc := func(commit plumbing.Hash) (*store.Doc, error) {
		raw, err := fileAt(r.repo, commit, path)
		if err != nil {
			return nil, err
		}
		return store.ExtractDoc(raw)
	}
	baseDoc, err1 := readDoc(base)
	oursDoc, err2 := readDoc(ours)
	theirsDoc, err3 := readDoc(theirs)
	if err2 != nil || err3 != nil {
		return nil, nil, fmt.Errorf("cannot read ours/theirs versions: %v/%v", err2, err3)
	}
	if err1 != nil { // no base version → treat as empty base
		baseDoc = &store.Doc{Blocks: []store.Block{}}
	}
	merged, conflicts := store.MergeDocs(baseDoc, oursDoc, theirsDoc)
	page, err := store.RenderDoc(merged)
	return page, conflicts, err
}

func isStoreDoc(path string) bool {
	return strings.HasSuffix(path, ".html") &&
		(strings.HasPrefix(path, "docs/") || strings.HasPrefix(path, "tasks/") ||
			strings.HasPrefix(path, "contacts/") || strings.HasPrefix(path, "channels/") ||
			strings.HasPrefix(path, "companies/") || strings.HasPrefix(path, "emails/") ||
			strings.HasPrefix(path, "memory/"))
}

// AuditEntry is one attributed change from git history (§7.1).
type AuditEntry struct {
	Commit string    `json:"commit"`
	Actor  string    `json:"actor"`
	When   time.Time `json:"when"`
	Msg    string    `json:"msg"`
	Files  []string  `json:"files"`
}

// AuditLog walks git history; optional filters by doc path, since, actor.
func (r *Repo) AuditLog(docPath string, since time.Time, actor string, limit int) ([]AuditEntry, error) {
	head, err := r.repo.Head()
	if err != nil {
		return nil, nil // unborn repo: no history yet, not an error
	}
	iter, err := r.repo.Log(&git.LogOptions{From: head.Hash(), Order: git.LogOrderCommitterTime})
	if err != nil {
		return nil, err
	}
	var out []AuditEntry
	err = iter.ForEach(func(c *object.Commit) error {
		if c.Committer.When.Before(since) {
			return nil
		}
		a := c.Author.Name
		if actor != "" && a != actor {
			return nil
		}
		e := AuditEntry{Commit: c.Hash.String(), Actor: a, When: c.Committer.When, Msg: c.Message}
		parent := plumbing.ZeroHash
		if c.NumParents() > 0 {
			parent = c.ParentHashes[0]
		}
		files, err := r.changedFiles(parent, c.Hash)
		if err == nil {
			for p := range files {
				if docPath == "" || p == docPath {
					e.Files = append(e.Files, p)
				}
			}
		}
		if len(e.Files) > 0 || docPath == "" {
			out = append(out, e)
			if limit > 0 && len(out) >= limit {
				return storer_stop
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, storer_stop) {
		return out, err
	}
	return out, nil
}

var storer_stop = errors.New("stop iteration")

// HeadHash returns the current HEAD commit hash ("" when unborn).
func (r *Repo) HeadHash() (string, error) {
	ref, err := r.repo.Head()
	if err != nil {
		return "", nil // unborn repo — nothing synthesized yet
	}
	return ref.Hash().String(), nil
}

// FileAtCommit returns a file's blob bytes as of HEAD (back=0) or the Nth
// ancestor commit (back=1 = HEAD's parent). Used by the MCP diff tool.
func (r *Repo) FileAtCommit(path string, back int) ([]byte, error) {
	head, err := r.repo.Head()
	if err != nil {
		return nil, err
	}
	c, err := object.GetCommit(r.repo.Storer, head.Hash())
	if err != nil {
		return nil, err
	}
	for i := 0; i < back; i++ {
		if c.NumParents() == 0 {
			return nil, os.ErrNotExist
		}
		if c, err = c.Parent(0); err != nil {
			return nil, err
		}
	}
	return fileAt(r.repo, c.Hash, path)
}

// Diff returns block-level stats and a unified-ish diff between two versions
// of a doc by slug path. fromVersion/toVersion select by doc.Version field:
// version 0 means "the version in the parent commit".
type DiffResult struct {
	Unified       string `json:"unified_diff"`
	BlocksAdded   int    `json:"blocks_added"`
	BlocksRemoved int    `json:"blocks_removed"`
}

// DiffDocs diffs two store docs (used by the MCP diff tool; version lookup is
// the caller's job via history).
func DiffDocs(from, to *store.Doc) DiffResult {
	fm := map[string]bool{}
	for _, b := range from.Blocks {
		fm[b.ID] = true
	}
	tm := map[string]bool{}
	for _, b := range to.Blocks {
		tm[b.ID] = true
	}
	added, removed := 0, 0
	for _, b := range to.Blocks {
		if !fm[b.ID] {
			added++
		}
	}
	for _, b := range from.Blocks {
		if !tm[b.ID] {
			removed++
		}
	}
	return DiffResult{
		Unified:       store.UnifiedBlockDiff(from, to),
		BlocksAdded:   added,
		BlocksRemoved: removed,
	}
}
