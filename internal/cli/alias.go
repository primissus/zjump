package cli

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/primissus/zjump/internal/alias"
	"github.com/primissus/zjump/internal/config"
	"github.com/primissus/zjump/internal/db"
	"github.com/primissus/zjump/internal/errs"
	"github.com/primissus/zjump/internal/log"
	"github.com/primissus/zjump/internal/paths"
)

const aliasHelp = `Usage: zjump alias [options] [<name> <dir>]

List, create, or delete directory aliases.

With no arguments, list all aliases (name<tab>path per line).
With <name> <dir>, create or update an alias.
With --delete <name>, remove an alias.
With --pick, interactively select an alias via fzf and print its path.
With --pick --score, rank the picker by each target's frecency score.

Flags:
    -d, --delete NAME   Delete an alias by name
    --pick              Interactively pick an alias (fzf) and print its path
    -s, --score         With --pick, show target frecency scores (best first)
`

// runAlias implements `zjump alias`. With no args it lists; with a name and
// directory it creates/overwrites; with -d/--delete it removes.
func runAlias(args []string) error {
	fs := newFlagSet("alias")
	var delete bool
	fs.BoolVar(&delete, "d", false, "")
	fs.BoolVar(&delete, "delete", false, "")
	var pick, score bool
	fs.BoolVar(&pick, "pick", false, "")
	fs.BoolVar(&score, "s", false, "")
	fs.BoolVar(&score, "score", false, "")

	rest, err := parseArgs(fs, args)
	if err != nil {
		if err == flag.ErrHelp {
			printCmdHelp(os.Stdout, "alias", aliasHelp)
			return nil
		}
		return err
	}

	dataDir, err := config.DataDir()
	if err != nil {
		return err
	}
	store, err := alias.Open(dataDir)
	if err != nil {
		return err
	}
	log.Debugf("alias: delete=%v pick=%v score=%v rest=%v", delete, pick, score, rest)

	if pick {
		if delete || len(rest) > 0 {
			return fmt.Errorf("alias --pick cannot be combined with --delete or positional args")
		}
		return runAliasPick(store, score)
	}
	if score {
		return fmt.Errorf("alias --score requires --pick")
	}

	if delete {
		if len(rest) != 1 {
			return fmt.Errorf("alias --delete: exactly one name is required")
		}
		if !store.Delete(rest[0]) {
			log.Errorf("alias not found: %s", rest[0])
			return fmt.Errorf("alias not found: %s", rest[0])
		}
		return store.Save()
	}

	switch len(rest) {
	case 0:
		for _, e := range store.Entries() {
			if _, werr := fmt.Fprintf(os.Stdout, "%s\t%s\n", e.Name, e.Path); werr != nil {
				return errs.PipeExit(werr, "stdout")
			}
		}
		return nil
	case 2:
		name := rest[0]
		target := rest[1]

		if err := alias.ValidateName(name); err != nil {
			return err
		}

		var resolved string
		if config.ResolveSymlinks() {
			resolved, err = paths.Canonicalize(target)
		} else {
			resolved, err = paths.ResolvePath(target)
		}
		if err != nil {
			return err
		}
		if !utf8.ValidString(resolved) {
			return fmt.Errorf("invalid unicode in path: %s", resolved)
		}
		if strings.ContainsAny(resolved, "\n\r") {
			return fmt.Errorf("alias path must not contain newline or carriage-return")
		}
		if info, statErr := os.Stat(resolved); statErr != nil || !info.IsDir() {
			log.Errorf("alias: not a directory: %s", resolved)
			return fmt.Errorf("not a directory: %s", resolved)
		}

		store.Set(name, resolved)
		return store.Save()
	default:
		return fmt.Errorf("alias: expected 0 args (list) or 2 args (name, directory), got %d", len(rest))
	}
}

// runAliasPick collects all alias entries and launches an interactive fzf
// picker, printing the selected alias's path to stdout (for the shell to cd
// into). Mirrors the branch/worktree pick flow. With score set, each row is
// prefixed by its target directory's decayed frecency score and the list is
// sorted best-first — aliases are not stored with a score of their own, so the
// target's frecency is the only sensible ranking.
func runAliasPick(store *alias.Store, score bool) error {
	entries := store.Entries()
	if len(entries) == 0 {
		log.Errorf("no aliases found")
		return fmt.Errorf("no aliases found")
	}

	var scores map[string]db.Rank
	if score {
		var err error
		scores, err = aliasTargetScores()
		if err != nil {
			return err
		}
		sort.SliceStable(entries, func(i, j int) bool {
			si, sj := scores[entries[i].Path], scores[entries[j].Path]
			if si != sj {
				return si > sj
			}
			return entries[i].Name < entries[j].Name
		})
	}

	gitEntries := make([]gitEntry, 0, len(entries))
	for _, e := range entries {
		label := e.Name
		if score {
			label = fmt.Sprintf("%6.1f %s", scores[e.Path], e.Name)
		}
		gitEntries = append(gitEntries, gitEntry{label: label, path: e.Path})
	}
	return gitFzfPickAndPrint(gitEntries)
}

// aliasTargetScores maps every tracked (non-alias) directory path to its clamped
// decayed frecency score. A path absent from the database scores 0.
func aliasTargetScores() (map[string]db.Rank, error) {
	database, err := openDB()
	if err != nil {
		return nil, err
	}
	// H3: surface persistence failures instead of silently discarding them.
	defer func() {
		if err := database.Save(); err != nil {
			log.Errorf("save failed: %v", err)
		}
	}()
	now, err := paths.CurrentTime()
	if err != nil {
		return nil, err
	}
	scores := make(map[string]db.Rank)
	for _, d := range database.Dirs() {
		if d.Kind == db.KindAlias {
			continue
		}
		if _, ok := scores[d.Path]; !ok {
			scores[d.Path] = db.ClampScore(d.Score(now))
		}
	}
	return scores, nil
}
