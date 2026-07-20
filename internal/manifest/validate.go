package manifest

import (
	"fmt"
	"sort"
	"strings"
)

// Severity distinguishes fatal problems from advisory ones.
type Severity string

// Problem severities.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Problem is a single validation finding, located precisely enough that the
// operator can go and fix the exact field in their config repo.
type Problem struct {
	File     string
	Ref      string
	Field    string
	Message  string
	Severity Severity
}

func (p Problem) String() string {
	var b strings.Builder
	if p.File != "" {
		fmt.Fprintf(&b, "%s: ", p.File)
	}
	if p.Ref != "" {
		fmt.Fprintf(&b, "item %q: ", p.Ref)
	}
	if p.Field != "" {
		fmt.Fprintf(&b, "%s: ", p.Field)
	}
	b.WriteString(p.Message)
	return b.String()
}

// Problems is a collection of validation findings.
type Problems []Problem

// Errors returns only the fatal problems.
func (ps Problems) Errors() Problems { return ps.bySeverity(SeverityError) }

// Warnings returns only the advisory problems.
func (ps Problems) Warnings() Problems { return ps.bySeverity(SeverityWarning) }

func (ps Problems) bySeverity(s Severity) Problems {
	var out Problems
	for _, p := range ps {
		if p.Severity == s {
			out = append(out, p)
		}
	}
	return out
}

// HasErrors reports whether any problem is fatal.
func (ps Problems) HasErrors() bool { return len(ps.Errors()) > 0 }

// Error renders every fatal problem on its own line.
func (ps Problems) Error() string {
	errs := ps.Errors()
	lines := make([]string, 0, len(errs))
	for _, p := range errs {
		lines = append(lines, p.String())
	}
	return strings.Join(lines, "\n")
}

// downloadTypes are item types that fetch a per-arch artifact.
var downloadTypes = map[ItemType]bool{ItemTarball: true, ItemDeb: true, ItemBinary: true}

// packageTypes are item types that must declare packages.
var packageTypes = map[ItemType]bool{ItemApt: true, ItemPip: true, ItemSnap: true}

// Validate checks a resolved config repo and returns every problem found.
// It deliberately does not stop at the first error: an operator fixing their
// own repo wants the whole list, not one problem per round trip.
func Validate(r *Resolved) Problems {
	var ps Problems

	if r.Manifest.Schema != 1 {
		ps = append(ps, Problem{
			File: "manifest.yaml", Field: "schema", Severity: SeverityError,
			Message: fmt.Sprintf("unsupported schema version %d, want 1", r.Manifest.Schema),
		})
	}

	index := map[string]Item{}
	seenCategories := map[string]bool{}

	for _, c := range r.Categories {
		file := fmt.Sprintf("categories/%s.yaml", c.ID)
		if seenCategories[c.ID] {
			ps = append(ps, Problem{File: file, Field: "id", Severity: SeverityError,
				Message: fmt.Sprintf("duplicate category id %q", c.ID)})
		}
		seenCategories[c.ID] = true

		seenItems := map[string]bool{}
		for _, it := range c.Items {
			if seenItems[it.ID] {
				ps = append(ps, Problem{File: file, Ref: it.Ref(), Field: "id",
					Severity: SeverityError, Message: "duplicate item id"})
			}
			seenItems[it.ID] = true
			index[it.Ref()] = it
			ps = append(ps, validateItem(file, it)...)
		}
	}

	ps = append(ps, validateDeps(index)...)
	return ps
}

func validateItem(file string, it Item) Problems {
	var ps Problems
	problem := func(field, msg string, sev Severity) {
		ps = append(ps, Problem{File: file, Ref: it.Ref(), Field: field, Message: msg, Severity: sev})
	}

	if !it.Type.Valid() {
		problem("type", fmt.Sprintf("unknown item type %q", it.Type), SeverityError)
		return ps
	}

	if packageTypes[it.Type] && len(it.Packages) == 0 {
		problem("packages", fmt.Sprintf("type %q requires packages", it.Type), SeverityError)
	}

	if it.CheckContains != "" && it.Check == "" {
		problem("check_contains", "check_contains requires check", SeverityError)
	}

	arches := it.Arch
	if len(arches) == 0 {
		arches = Arches
	}

	if downloadTypes[it.Type] {
		var missingSHA []string
		for _, a := range arches {
			if it.Source[a] == "" {
				problem("source", fmt.Sprintf("source missing for arch %s", a), SeverityError)
			}
			if it.SHA256[a] == "" {
				missingSHA = append(missingSHA, a)
			}
		}
		if len(missingSHA) > 0 {
			problem("sha256", fmt.Sprintf("no sha256 declared for %s; download will not be verified", strings.Join(missingSHA, ", ")), SeverityWarning)
		}
	}

	for arch, url := range it.Source {
		if url != "" && !strings.HasPrefix(url, "https://") {
			problem("source", fmt.Sprintf("source for arch %s must use https, got %q", arch, url), SeverityError)
		}
	}

	return ps
}

// validateDeps checks that every dependency resolves, then that the graph is
// acyclic. A cycle is reported once with its path, because a cycle poisons all
// downstream analysis and repeating it per node would be noise.
func validateDeps(index map[string]Item) Problems {
	var ps Problems

	refs := make([]string, 0, len(index))
	for ref := range index {
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	for _, ref := range refs {
		for _, dep := range index[ref].DependsOn {
			if _, ok := index[dep]; !ok {
				ps = append(ps, Problem{
					Ref: ref, Field: "depends_on", Severity: SeverityError,
					Message: fmt.Sprintf("depends on unknown item %q", dep),
				})
			}
		}
	}
	if len(ps) > 0 {
		return ps // cycle detection is meaningless while refs are dangling
	}

	if path := findCycle(refs, index); path != nil {
		ps = append(ps, Problem{
			Field: "depends_on", Severity: SeverityError,
			Message: fmt.Sprintf("dependency cycle: %s", strings.Join(path, " -> ")),
		})
	}
	return ps
}

// findCycle returns the first cycle path found, or nil.
func findCycle(refs []string, index map[string]Item) []string {
	const (
		white = 0 // unvisited
		grey  = 1 // on the current stack
		black = 2 // fully explored
	)
	colour := map[string]int{}
	var stack []string
	var cycle []string

	var visit func(string) bool
	visit = func(ref string) bool {
		colour[ref] = grey
		stack = append(stack, ref)
		for _, dep := range index[ref].DependsOn {
			switch colour[dep] {
			case grey:
				start := 0
				for i, s := range stack {
					if s == dep {
						start = i
						break
					}
				}
				cycle = append(append([]string{}, stack[start:]...), dep)
				return true
			case white:
				if visit(dep) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		colour[ref] = black
		return false
	}

	for _, ref := range refs {
		if colour[ref] == white && visit(ref) {
			return cycle
		}
	}
	return nil
}
