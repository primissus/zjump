package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/primissus/zjump/internal/alias"
	"github.com/primissus/zjump/internal/config"
	"github.com/primissus/zjump/internal/db"
	"github.com/primissus/zjump/internal/errs"
	"github.com/primissus/zjump/internal/paths"
)

// completeHelp documents the hidden `zjump complete` subcommand. It is not a
// zoxide feature: the generated bash/zsh completions call it to obtain the
// keyword candidates for `zz <word><TAB>`. Kept out of printUsage (a shell
// implementation detail, not part of the user-facing command tree).
const completeHelp = `Usage: zjump complete [OPTIONS] -- <WORD>

Print keyword-completion candidates for the jump command. This is a
zjump-only extension used by the generated bash/zsh integration; it is
not intended to be called by hand.

Output is one candidate per line, tab-separated:
    <GROUP>	<WORD>	<DISPLAY>

GROUP is one of:
    match        available directories: current dir, aliases, indexed dirs
    completion   up to --top fuzzy-close aliases/directories
    indexed      up to --top highest-frecency indexed directories

Flags:
    --top N      Max entries per non-match section (default: 10)
`

// Completion group tags emitted by `zjump complete`; the shell templates key
// their section rendering off these.
const (
	groupMatch      = "match"
	groupCompletion = "completion"
	groupIndexed    = "indexed"
)

// completionCandidate is one completion word plus the path shown beside it.
type completionCandidate struct {
	group   string
	word    string
	display string
}

// runComplete implements `zjump complete`. It never fails loudly: a missing
// database or alias store simply yields current-directory behavior, because a
// noisy completion would corrupt the interactive shell line.
func runComplete(args []string) error {
	fs := newFlagSet("complete")
	var top int
	fs.IntVar(&top, "top", 10, "")
	rest, err := parseArgs(fs, args)
	if err != nil {
		if err == flag.ErrHelp {
			printCmdHelp(os.Stdout, "complete", completeHelp)
			return nil
		}
		return err
	}
	if top <= 0 {
		top = 10
	}
	word := ""
	if len(rest) > 0 {
		word = rest[len(rest)-1]
	}
	if word == "" {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	for _, c := range buildCompletions(cwd, word, top) {
		if _, werr := fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", c.group, c.word, c.display); werr != nil {
			return errs.PipeExit(werr, "stdout")
		}
	}
	return nil
}

// buildCompletions assembles the three candidate sections for the partial
// keyword word, given the shell's current working directory:
//
//   - match: current-directory subdirectories, then aliases, then indexed
//     directories whose basename has word as a prefix (the same order the
//     single-Tab longest-common-prefix uses, current dir first).
//   - completion: up to top fuzzy-close (subsequence) aliases/directories not
//     already in match, closest first.
//   - indexed: up to top highest-frecency directories, regardless of word.
//
// Words are de-duplicated across sections so the single-Tab common prefix is
// computed over each word exactly once and the sections stay distinct.
func buildCompletions(cwd, word string, top int) []completionCandidate {
	lower := strings.ToLower(word)
	var out []completionCandidate
	shown := map[string]bool{}

	add := func(group, w, display string) {
		if w == "" || shown[w] {
			return
		}
		shown[w] = true
		out = append(out, completionCandidate{group: group, word: w, display: display})
	}

	// match (1/3): subdirectories of the current directory.
	if entries, err := os.ReadDir(cwd); err == nil {
		var names []string
		for _, e := range entries {
			name := e.Name()
			if !hiddenAllowed(name, word) {
				continue
			}
			if !strings.HasPrefix(strings.ToLower(name), lower) {
				continue
			}
			if !isDir(filepath.Join(cwd, name)) {
				continue
			}
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			add(groupMatch, name, filepath.Join(cwd, name))
		}
	}

	aliases := loadAliases()
	now, _ := paths.CurrentTime()
	dbDirs := loadDBDirs(now)

	// match (2/3): alias names.
	for _, a := range aliases {
		if !strings.HasPrefix(strings.ToLower(a.name), lower) {
			continue
		}
		if !isDir(a.path) {
			continue
		}
		add(groupMatch, a.name, a.path)
	}

	// match (3/3): indexed directories by basename prefix, best first.
	for _, d := range dbDirs {
		base := filepath.Base(d.Path)
		if shown[base] {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(base), lower) {
			continue
		}
		if !isDir(d.Path) {
			continue
		}
		add(groupMatch, base, d.Path)
	}

	// completion: fuzzy subsequence matches not already offered, closest first
	// (smallest gap), ties broken by frecency then name.
	type scored struct {
		word    string
		display string
		gap     int
		score   db.Rank
	}
	var fuzzy []scored
	for _, a := range aliases {
		if shown[a.name] {
			continue
		}
		gap, ok := subseqGap(lower, strings.ToLower(a.name))
		if !ok || !isDir(a.path) {
			continue
		}
		fuzzy = append(fuzzy, scored{word: a.name, display: a.path, gap: gap})
	}
	for _, d := range dbDirs {
		base := filepath.Base(d.Path)
		if shown[base] {
			continue
		}
		gap, ok := subseqGap(lower, strings.ToLower(base))
		if !ok || !isDir(d.Path) {
			continue
		}
		fuzzy = append(fuzzy, scored{word: base, display: d.Path, gap: gap, score: d.Score(now)})
	}
	sort.SliceStable(fuzzy, func(i, j int) bool {
		if fuzzy[i].gap != fuzzy[j].gap {
			return fuzzy[i].gap < fuzzy[j].gap
		}
		if fuzzy[i].score != fuzzy[j].score {
			return fuzzy[i].score > fuzzy[j].score
		}
		return fuzzy[i].word < fuzzy[j].word
	})
	for i := 0; i < len(fuzzy) && i < top; i++ {
		add(groupCompletion, fuzzy[i].word, fuzzy[i].display)
	}

	// indexed: top frecency directories, regardless of the typed word.
	count := 0
	for _, d := range dbDirs {
		if count >= top {
			break
		}
		base := filepath.Base(d.Path)
		if shown[base] {
			continue
		}
		if !isDir(d.Path) {
			continue
		}
		before := len(out)
		add(groupIndexed, base, d.Path)
		if len(out) > before {
			count++
		}
	}

	return out
}

// aliasEntry is a (name, target path) alias pair.
type aliasEntry struct {
	name string
	path string
}

// loadAliases opens the alias store and returns its entries (name-sorted).
// Errors yield no aliases, matching the completion fall-through elsewhere.
func loadAliases() []aliasEntry {
	dataDir, err := config.DataDir()
	if err != nil {
		return nil
	}
	store, err := alias.Open(dataDir)
	if err != nil {
		return nil
	}
	entries := store.Entries()
	out := make([]aliasEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, aliasEntry{name: e.Name, path: e.Path})
	}
	return out
}

// loadDBDirs opens the frecency database and returns its non-alias entries,
// best-first (highest decayed score first). It deliberately bypasses the query
// Stream so completion never lazily deletes or rewrites the database as a side
// effect of pressing Tab.
func loadDBDirs(now db.Epoch) []db.Dir {
	database, err := openDB()
	if err != nil {
		return nil
	}
	dirs := make([]db.Dir, 0, len(database.Dirs()))
	for _, d := range database.Dirs() {
		if d.IsAlias() {
			continue
		}
		dirs = append(dirs, d)
	}
	sort.SliceStable(dirs, func(i, j int) bool {
		si, sj := dirs[i].Score(now), dirs[j].Score(now)
		if si != sj {
			return si > sj
		}
		return dirs[i].Path < dirs[j].Path
	})
	return dirs
}

// isDir reports whether path exists and is a directory (following symlinks).
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// hiddenAllowed mirrors native shell directory completion: dotfiles are only
// offered when the user has already typed a leading dot.
func hiddenAllowed(name, word string) bool {
	if !strings.HasPrefix(name, ".") {
		return true
	}
	return strings.HasPrefix(word, ".")
}

// subseqGap reports whether word is a subsequence of cand (both already
// lowercased) and, if so, the number of unmatched characters. A smaller gap is
// a closer match; this is a deliberately cheap fuzzy metric (no edit distance,
// which is out of scope per N-5).
func subseqGap(word, cand string) (int, bool) {
	if word == "" {
		return len(cand), true
	}
	i := 0
	for j := 0; j < len(cand) && i < len(word); j++ {
		if cand[j] == word[i] {
			i++
		}
	}
	if i != len(word) {
		return 0, false
	}
	return len(cand) - len(word), true
}
