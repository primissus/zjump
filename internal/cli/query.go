package cli

import (
	"flag"
	"fmt"
	"os"

	"zjump/internal/alias"
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
	var exclude, baseDir string
	fs.StringVar(&exclude, "exclude", "", "")
	fs.StringVar(&baseDir, "base-dir", "", "")

	keywords, err := parseArgs(fs, args)
	if err != nil {
		return err
	}

	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	if interactive && list {
		return fmt.Errorf("the argument '--interactive' cannot be used with '--list'")
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
}

func doQuery(database *db.Database, p queryParams) error {
	// Alias resolution: in default mode only (not --list/--interactive), if
	// there is exactly one keyword and it exactly matches an alias, resolve it
	// directly. Alias beats frecency but does not affect --list or --interactive
	// modes (which stay pure frecency).
	if !p.interactive && !p.list && len(p.keywords) == 1 {
		if resolved, aErr := resolveAlias(p.keywords[0], p); aErr != nil {
			return aErr
		} else if resolved != "" {
			_, werr := fmt.Fprintln(os.Stdout, resolved)
			return errs.PipeExit(werr, "stdout")
		}
	}

	now, err := paths.CurrentTime()
	if err != nil {
		return err
	}
	excludeGlobs, err := config.ExcludeDirs()
	if err != nil {
		return err
	}

	opts := db.NewStreamOptions(now).
		WithKeywords(p.keywords).
		WithExclude(excludeGlobs)
	if p.baseDirSet {
		opts = opts.WithBaseDir(&p.baseDir)
	}
	if !p.all {
		opts = opts.WithExists(true).WithResolveSymlinks(config.ResolveSymlinks())
	}

	stream := db.NewStream(database, opts)

	var excl *string
	if p.excludeSet {
		excl = &p.exclude
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

// resolveAlias checks whether keyword is an exact (case-sensitive) match for a
// stored alias. Returns the target path if so, "" if no alias matches, or an
// error if the alias target is missing on disk or equals --exclude.
func resolveAlias(keyword string, p queryParams) (string, error) {
	dataDir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	store, aErr := alias.Open(dataDir)
	if aErr != nil {
		return "", nil // file missing or corrupt: no aliases, fall through
	}
	target, ok := store.Get(keyword)
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
