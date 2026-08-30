package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/primissus/zjump/internal/alias"
	"github.com/primissus/zjump/internal/config"
	"github.com/primissus/zjump/internal/db"
	"github.com/primissus/zjump/internal/errs"
	"github.com/primissus/zjump/internal/fzf"
	"github.com/primissus/zjump/internal/log"
	"github.com/primissus/zjump/internal/paths"
)

const queryHelp = `Usage: zjump query [OPTIONS] [keywords...]

Search the frecency database and print the best-matching directory.
With --list, print all matching directories. With --interactive,
select via fzf.

Flags:
    -a, --all             List all matches, including nonexistent paths
    -i, --interactive     Select a directory interactively via fzf
    -l, --list            List all matching directories
    -s, --score           Print the frecency score alongside the path
    --type TYPE           Entry type: dir, repo, worktree, alias, or any
                          (default: dir + repo). zjump extension.
    --exclude PATH        Exclude a specific path from results
    --base-dir DIR        Restrict results to paths under a base directory
`

// runQuery implements `zjump query`. Per deviation D-4 the DB is rewritten only
// when the query actually dirtied it (a lazy deletion), unlike zoxide's
// unconditional rewrite; the query result and the (conditional) save both run,
// mirroring zoxide's `self.query(&mut db).and(db.save())`.
func runQuery(args []string) error {
	fs := newFlagSet("query")
	var all, interactive, list, score bool
	fs.BoolVar(&all, "a", false, "")
	fs.BoolVar(&all, "all", false, "")
	fs.BoolVar(&interactive, "i", false, "")
	fs.BoolVar(&interactive, "interactive", false, "")
	fs.BoolVar(&list, "l", false, "")
	fs.BoolVar(&list, "list", false, "")
	fs.BoolVar(&score, "s", false, "")
	fs.BoolVar(&score, "score", false, "")
	var exclude, baseDir, typ string
	fs.StringVar(&exclude, "exclude", "", "")
	fs.StringVar(&baseDir, "base-dir", "", "")
	// --type selects the candidate set (D-6): dir|repo|worktree|alias|any, or
	// (omitted) for the default dir+repo behavior plus the alias fast path (§5.1).
	fs.StringVar(&typ, "type", "", "")

	keywords, err := parseArgs(fs, args)
	if err != nil {
		if err == flag.ErrHelp {
			printCmdHelp(os.Stdout, "query", queryHelp)
			return nil
		}
		return err
	}

	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	if interactive && list {
		return fmt.Errorf("the argument '--interactive' cannot be used with '--list'")
	}
	if err := validateType(typ); err != nil {
		return err
	}

	database, err := openDB()
	if err != nil {
		return err
	}

	p := queryParams{
		keywords:    keywords,
		all:         all,
		interactive: interactive,
		list:        list,
		score:       score,
		exclude:     exclude,
		excludeSet:  set["exclude"],
		baseDir:     baseDir,
		baseDirSet:  set["base-dir"],
		typ:         typ,
	}

	qErr := doQuery(database, p)
	sErr := database.Save()
	if qErr != nil {
		return qErr
	}
	return sErr
}

type queryParams struct {
	keywords    []string
	all         bool
	interactive bool
	list        bool
	score       bool
	exclude     string
	excludeSet  bool
	baseDir     string
	baseDirSet  bool
	typ         string // "", "dir", "repo", "worktree", "alias", "any"
}

// validateType rejects an unknown --type value (§5.1, R2-TYPE-1).
func validateType(typ string) error {
	switch typ {
	case "", "dir", "repo", "worktree", "alias", "any":
		return nil
	default:
		return fmt.Errorf("invalid type: %s", typ)
	}
}

// kindsForType maps a --type value to the DB kinds it selects. Aliases are not a
// stored kind on this port (A-1) and worktrees are enumerated live (§6), so both
// select no DB kinds here.
func kindsForType(typ string) []db.Kind {
	switch typ {
	case "dir":
		return []db.Kind{db.KindDir}
	case "repo":
		return []db.Kind{db.KindRepo}
	case "alias", "worktree":
		return nil
	case "any":
		return []db.Kind{db.KindDir, db.KindRepo}
	default: // "" => today's default candidate set (G-5)
		return []db.Kind{db.KindDir, db.KindRepo}
	}
}

func doQuery(database *db.Database, p queryParams) error {
	now, err := paths.CurrentTime()
	if err != nil {
		return err
	}

	// --type worktree is a live git enumeration, not a DB query (§6, R2-WT-2).
	if p.typ == "worktree" {
		return queryWorktree(database, p, now)
	}

	excludeGlobs, err := config.ExcludeDirs()
	if err != nil {
		return err
	}

	var excl *string
	if p.excludeSet {
		excl = &p.exclude
	}

	// Alias fast path: default mode only (no --type, not -l/-i) with exactly one
	// keyword. A hit prints the target and returns; otherwise we fall through to
	// the normal dir+repo match stream (feat's resolveAlias, untouched — A-1).
	if p.typ == "" && !p.interactive && !p.list && len(p.keywords) == 1 {
		if resolved, aErr := resolveAlias(p.keywords[0], p); aErr != nil {
			return aErr
		} else if resolved != "" {
			_, werr := fmt.Fprintln(os.Stdout, resolved)
			return errs.PipeExit(werr, "stdout")
		}
	}

	// --type alias resolves against the alias store, matched by name (A-1).
	if p.typ == "alias" {
		return queryAliasType(p, excl)
	}

	log.Debugf("query: keywords=%v (interactive=%v list=%v type=%s)", p.keywords, p.interactive, p.list, p.typ)

	opts := db.NewStreamOptions(now).
		WithKeywords(p.keywords).
		WithExclude(excludeGlobs).
		WithKinds(kindsForType(p.typ)...)
	if p.baseDirSet {
		opts = opts.WithBaseDir(&p.baseDir)
	}
	if !p.all {
		opts = opts.WithExists(true).WithResolveSymlinks(config.ResolveSymlinks())
	}

	stream := db.NewStream(database, opts)

	// --type any: DB dir+repo (by path) plus store aliases (by name).
	if p.typ == "any" {
		return queryAny(stream, p, now, excl)
	}

	switch {
	case p.interactive:
		return queryInteractive(stream, now, p.score, excl)
	case p.list:
		return queryList(stream, now, p.score, excl)
	default:
		return queryFirst(stream, now, p.score, excl)
	}
}

// storeAliasTargets returns the target paths (name-sorted) of store aliases whose
// name matches the keywords. --base-dir filters on the target path (A-1). A
// missing/corrupt store yields no targets, matching resolveAlias's fall-through.
func storeAliasTargets(p queryParams) []string {
	dataDir, err := config.DataDir()
	if err != nil {
		return nil
	}
	store, err := alias.Open(dataDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range store.Entries() {
		if !db.MatchPath(p.keywords, e.Name) {
			continue
		}
		if p.baseDirSet && !hasPathPrefix(e.Path, p.baseDir) {
			continue
		}
		out = append(out, e.Path)
	}
	return out
}

// queryAliasType implements `query --type alias`: match store aliases by name,
// output the target path, --exclude on the target path, -s a no-op (§5.1, A-1).
func queryAliasType(p queryParams, excl *string) error {
	targets := storeAliasTargets(p)
	filtered := withoutExcluded(targets, excl)

	switch {
	case p.interactive:
		return pathOnlyInteractive(filtered)
	case p.list:
		for _, t := range filtered {
			if _, werr := fmt.Fprintln(os.Stdout, t); werr != nil {
				return errs.PipeExit(werr, "stdout")
			}
		}
		return nil
	default:
		if len(filtered) == 0 {
			if len(targets) > 0 {
				return fmt.Errorf("you are already in the only match")
			}
			return fmt.Errorf("no match found")
		}
		_, werr := fmt.Fprintln(os.Stdout, filtered[0])
		return errs.PipeExit(werr, "stdout")
	}
}

// queryAny implements `query --type any`: DB dir+repo (matched by path, best
// first) plus store aliases (matched by name, appended). No git subprocesses.
func queryAny(stream *db.Stream, p queryParams, now db.Epoch, excl *string) error {
	aliases := withoutExcluded(storeAliasTargets(p), excl)

	switch {
	case p.interactive:
		var paths []string
		for {
			dir := stream.Next()
			if dir == nil {
				break
			}
			if excl != nil && dir.Path == *excl {
				continue
			}
			paths = append(paths, dir.Path)
		}
		paths = append(paths, aliases...)
		return pathOnlyInteractive(paths)
	case p.list:
		if err := queryList(stream, now, p.score, excl); err != nil {
			return err
		}
		for _, t := range aliases {
			if _, werr := fmt.Fprintln(os.Stdout, t); werr != nil {
				return errs.PipeExit(werr, "stdout")
			}
		}
		return nil
	default:
		dir := stream.Next()
		for excl != nil && dir != nil && dir.Path == *excl {
			dir = stream.Next()
		}
		if dir != nil {
			_, werr := fmt.Fprintln(os.Stdout, formatDir(dir, now, p.score))
			return errs.PipeExit(werr, "stdout")
		}
		if len(aliases) == 0 {
			return fmt.Errorf("no match found")
		}
		_, werr := fmt.Fprintln(os.Stdout, aliases[0])
		return errs.PipeExit(werr, "stdout")
	}
}

// withoutExcluded drops paths that equal the --exclude value (string compare,
// verbatim — same as existing --exclude semantics).
func withoutExcluded(paths []string, excl *string) []string {
	if excl == nil {
		return paths
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p != *excl {
			out = append(out, p)
		}
	}
	return out
}

// pathOnlyInteractive spawns fzf over a path-only (no score) candidate list and
// prints the selection. Used by --type alias/any, whose aliases have no frecency
// and therefore no score column.
func pathOnlyInteractive(paths []string) error {
	f, err := fzf.New()
	if err != nil {
		return err
	}
	if opts, ok := config.FzfOpts(); ok {
		f.Env("FZF_DEFAULT_OPTS", opts)
	} else {
		f.StdAppearance()
	}
	f.WithNth(1)

	child, err := f.Spawn()
	if err != nil {
		return err
	}

	var selection string
	for _, p := range paths {
		sel, werr := child.Write(p)
		if werr != nil {
			return werr
		}
		if sel != nil {
			selection = *sel
			break
		}
	}
	if selection == "" {
		sel, werr := child.Wait()
		if werr != nil {
			return werr
		}
		selection = sel
	}
	_, werr := fmt.Fprintln(os.Stdout, strings.TrimSpace(selection))
	return errs.PipeExit(werr, "stdout")
}

// hasPathPrefix reports whether path is component-wise under base (so "/foo"
// does not match "/foobar"), mirroring db.pathHasPrefix for the alias-store
// base-dir filter.
func hasPathPrefix(path, base string) bool {
	if path == base {
		return true
	}
	if len(path) > len(base) && strings.HasPrefix(path, base) {
		// Ensure a component boundary: base "/foo" matches "/foo/bar" but not
		// "/foobar".
		rest := path[len(base):]
		if len(base) == 0 || rest[0] == '/' {
			return true
		}
	}
	return false
}

func formatDir(dir *db.Dir, now db.Epoch, score bool) string {
	if score {
		return dir.DisplayScore(now, " ")
	}
	return dir.Display()
}

func queryFirst(stream *db.Stream, now db.Epoch, score bool, excl *string) error {
	dir := stream.Next()
	if dir == nil {
		log.Errorf("no match found")
		return fmt.Errorf("no match found")
	}
	for excl != nil && dir.Path == *excl {
		dir = stream.Next()
		if dir == nil {
			log.Errorf("you are already in the only match")
			return fmt.Errorf("you are already in the only match")
		}
	}
	_, werr := fmt.Fprintln(os.Stdout, formatDir(dir, now, score))
	return errs.PipeExit(werr, "stdout")
}

func queryList(stream *db.Stream, now db.Epoch, score bool, excl *string) error {
	for {
		dir := stream.Next()
		if dir == nil {
			return nil
		}
		if excl != nil && dir.Path == *excl {
			continue
		}
		if _, werr := fmt.Fprintln(os.Stdout, formatDir(dir, now, score)); werr != nil {
			return errs.PipeExit(werr, "stdout")
		}
	}
}

func queryInteractive(stream *db.Stream, now db.Epoch, score bool, excl *string) error {
	child, err := queryFzf()
	if err != nil {
		return err
	}

	var selection string
	for {
		dir := stream.Next()
		if dir == nil {
			sel, werr := child.Wait()
			if werr != nil {
				return werr
			}
			selection = sel
			break
		}
		if excl != nil && dir.Path == *excl {
			continue
		}
		sel, werr := child.Write(dir.DisplayScore(now, "\t"))
		if werr != nil {
			return werr
		}
		if sel != nil {
			selection = *sel
			break
		}
	}

	// Strip the 7-char score prefix (6-char score + tab) unless --score.
	out := selection
	if !score {
		if len(selection) < 7 {
			return fmt.Errorf("could not read selection from fzf")
		}
		out = selection[7:]
	}
	_, werr := fmt.Fprint(os.Stdout, out)
	return errs.PipeExit(werr, "stdout")
}

// queryFzf builds the interactive query fzf child: the built-in arg set + preview
// pane, unless _ZJUMP_FZF_OPTS replaces them (which also disables the preview).
func queryFzf() (*fzf.Child, error) {
	f, err := fzf.New()
	if err != nil {
		return nil, err
	}
	if opts, ok := config.FzfOpts(); ok {
		f.Env("FZF_DEFAULT_OPTS", opts)
	} else {
		f.StdAppearance()
		f.EnablePreview()
	}
	return f.Spawn()
}

// resolveAlias checks whether keyword matches a stored alias (exact match
// first, then unique prefix match). Returns the target path if so, "" if no
// alias matches, or an error if the alias target is missing on disk or equals
// --exclude.
func resolveAlias(keyword string, p queryParams) (string, error) {
	dataDir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	store, aErr := alias.Open(dataDir)
	if aErr != nil {
		return "", nil // file missing or corrupt: no aliases, fall through
	}
	target, ok := store.Match(keyword)
	if !ok {
		return "", nil
	}

	if p.excludeSet && target == p.exclude {
		return "", fmt.Errorf("you are already in the only match")
	}

	info, statErr := os.Stat(target)
	if statErr != nil || !info.IsDir() {
		return "", fmt.Errorf("alias %q points to a directory that no longer exists: %s", keyword, target)
	}

	return target, nil
}
