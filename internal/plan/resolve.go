package plan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/t0mer/kamino/internal/manifest"
)

// Select resolves a profile into the set of item refs it covers on arch.
//
// Missing dependencies are pulled in automatically and marked implicit, so
// profiles stay terse. But a dependency the profile explicitly excluded is a
// hard error: an exclude is a deliberate statement by the operator, and
// silently overriding it would be a surprise at root privileges. Likewise, a
// dependency (direct or transitive) that does not support the target arch is
// a hard error rather than being silently dropped or silently included: the
// depending item cannot be installed without it.
//
// If a ref matches both an include and an exclude pattern, the exclude wins
// silently. A profile with no include patterns at all yields an empty
// selection and no error.
func Select(r *manifest.Resolved, p manifest.Profile, arch string) (Selection, error) {
	index := map[string]manifest.Item{}
	var order []string
	for _, c := range r.Categories {
		for _, it := range c.Items {
			index[it.Ref()] = it
			order = append(order, it.Ref())
		}
	}

	included, err := expand(p.Include, index, order, "include")
	if err != nil {
		return Selection{}, err
	}
	excluded, err := expand(p.Exclude, index, order, "exclude")
	if err != nil {
		return Selection{}, err
	}

	selected := map[string]bool{}
	for ref := range included {
		if excluded[ref] {
			continue
		}
		if !index[ref].SupportsArch(arch) {
			continue
		}
		selected[ref] = true
	}

	implicit := map[string]bool{}
	queue := make([]string, 0, len(selected))
	for ref := range selected {
		queue = append(queue, ref)
	}
	sort.Strings(queue)

	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]

		for _, dep := range index[ref].DependsOn {
			if selected[dep] {
				continue
			}
			if excluded[dep] {
				return Selection{}, fmt.Errorf(
					"item %q depends on %q, which profile %q explicitly excluded: "+
						"remove the exclude or drop %q from the profile",
					ref, dep, p.ID, ref)
			}
			depItem, ok := index[dep]
			if !ok {
				return Selection{}, fmt.Errorf("item %q depends on unknown item %q", ref, dep)
			}
			if !depItem.SupportsArch(arch) {
				return Selection{}, fmt.Errorf(
					"item %q depends on %q, which does not support arch %q: "+
						"remove %q from the profile or drop the dependency",
					ref, dep, arch, ref)
			}
			selected[dep] = true
			implicit[dep] = true
			queue = append(queue, dep)
		}
	}

	refs := make([]string, 0, len(selected))
	for ref := range selected {
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	return Selection{Refs: refs, Implicit: implicit}, nil
}

// expand turns include/exclude patterns into concrete refs. A pattern that
// matches nothing is an error: it is almost always a typo, and silently
// ignoring it would install the wrong set of software.
func expand(patterns []string, index map[string]manifest.Item, order []string, field string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, pattern := range patterns {
		matched := false
		for _, ref := range order {
			if matchRef(pattern, ref) {
				out[ref] = true
				matched = true
			}
		}
		if !matched {
			return nil, fmt.Errorf("profile %s pattern %q matches no items", field, pattern)
		}
	}
	return out, nil
}

// matchRef supports exact refs and a single trailing "category/*" wildcard.
func matchRef(pattern, ref string) bool {
	if pattern == ref {
		return true
	}
	if strings.HasSuffix(pattern, "/*") {
		return strings.HasPrefix(ref, strings.TrimSuffix(pattern, "*"))
	}
	return false
}

// ApplyOverrides returns it with any profile override applied.
func ApplyOverrides(it manifest.Item, p manifest.Profile) manifest.Item {
	o, ok := p.Overrides[it.Ref()]
	if !ok {
		return it
	}
	if o.Version != "" {
		it.Version = o.Version
	}
	return it
}
