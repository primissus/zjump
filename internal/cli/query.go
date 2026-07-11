package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"zjump/internal/config"
	"zjump/internal/db"
	"zjump/internal/errs"
	"zjump/internal/fzf"
	"zjump/internal/paths"
)

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

// kindsForType maps a --type value to the DB kinds it selects. Worktree is not
// a stored kind — it is enumerated live (§6) — so it never reaches here.
func kindsForType(typ string) []db.Kind {
	switch typ {
	case "dir":
		return []db.Kind{db.KindDir}
	case "repo":
		return []db.Kind{db.KindRepo}
	case "alias":
		return []db.Kind{db.KindAlias}
	case "any":
		return []db.Kind{db.KindDir, db.KindRepo, db.KindAlias}
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

	// Alias fast path (§5.2, D-6): default mode only (no --type, not -l/-i) with
	// exactly one keyword. A hit prints the target and returns; otherwise we fall
	// through to the normal dir+repo match stream (G-8).
	if p.typ == "" && !p.list && !p.interactive && len(p.keywords) == 1 {
		if handled, err := aliasFastPath(database, p, now, excl); handled || err != nil {
			return err
		}
	}

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

	switch {
	case p.interactive:
		return queryInteractive(stream, now, p.score, excl)
	case p.list:
		return queryList(stream, now, p.score, excl)
	default:
		return queryFirst(stream, now, p.score, excl)
	}
}

// aliasFastPath implements §5.2 (G-6/G-7/G-8). It returns handled=true when it
// resolved and printed an alias target (after bumping its rank), or false to
// fall through to normal keyword matching.
//
// Candidate collection follows the spec's exact-XOR-prefix rule: if an alias
// name equals the keyword, the candidate set is that single alias; otherwise it
// is every name-prefix alias. Candidates whose target equals --exclude are
// dropped; if the set empties, we fall through (G-8).
func aliasFastPath(database *db.Database, p queryParams, now db.Epoch, excl *string) (bool, error) {
	keyword := p.keywords[0]
	dirs := database.Dirs()

	excluded := func(d *db.Dir) bool { return excl != nil && d.Path == *excl }

	var winner *db.Dir
	if exact := database.FindAlias(keyword); exact != nil {
		// Exact name match: the candidate set is just this alias.
		if !excluded(exact) {
			winner = exact
		}
	} else {
		// No exact match: best-scoring surviving prefix candidate wins; ties
		// break by lexicographically smaller name for determinism.
		for i := range dirs {
			d := &dirs[i]
			if !d.IsAlias() || !strings.HasPrefix(d.Name, keyword) || excluded(d) {
				continue
			}
			if winner == nil || betterAlias(d, winner, now) {
				winner = d
			}
		}
	}
	if winner == nil {
		return false, nil // fall through
	}

	database.TouchAlias(winner.Name, now) // G-6: rank += 1, last_accessed = now
	_, werr := fmt.Fprintln(os.Stdout, formatDir(winner, now, p.score))
	return true, errs.PipeExit(werr, "stdout")
}

// betterAlias reports whether a outranks b: higher decayed score first, then
// lexicographically smaller name (§5.2 step 3).
func betterAlias(a, b *db.Dir, now db.Epoch) bool {
	sa, sb := a.Score(now), b.Score(now)
	if sa != sb {
		return sa > sb
	}
	return a.Name < b.Name
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
		return fmt.Errorf("no match found")
	}
	for excl != nil && dir.Path == *excl {
		dir = stream.Next()
		if dir == nil {
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
		f.Args(
			"--exact",
			"--no-sort",
			"--bind=ctrl-z:ignore,btab:up,tab:down",
			"--cycle",
			"--keep-right",
			"--border=sharp",
			"--height=45%",
			"--info=inline",
			"--layout=reverse",
			"--tabstop=1",
			"--exit-0",
		)
		f.EnablePreview()
	}
	return f.Spawn()
}
