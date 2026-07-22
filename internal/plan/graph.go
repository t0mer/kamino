package plan

import (
	"fmt"
	"sort"

	"github.com/t0mer/kamino/internal/manifest"
)

// Build resolves a profile into a fully ordered plan for arch.
func Build(r *manifest.Resolved, p manifest.Profile, arch string) (*Plan, error) {
	sel, err := Select(r, p, arch)
	if err != nil {
		return nil, err
	}

	index := map[string]manifest.Item{}
	rank := map[string]int{}
	next := 0

	// Rank by category order, then by position within the category file, so
	// that independent items always come out in the same sequence.
	cats := append([]manifest.Category{}, r.Categories...)
	sort.SliceStable(cats, func(i, j int) bool { return cats[i].Order < cats[j].Order })
	for _, c := range cats {
		for _, it := range c.Items {
			index[it.Ref()] = it
			rank[it.Ref()] = next
			next++
		}
	}

	ordered, err := Order(sel.Refs, index, rank)
	if err != nil {
		return nil, err
	}

	out := &Plan{
		ProfileID: p.ID,
		ConfigSHA: r.SHA,
		Arch:      arch,
		Stale:     r.Stale,
	}

	for _, ref := range ordered {
		it := ApplyOverrides(index[ref], p)
		out.Steps = append(out.Steps, Step{
			Ref:      ref,
			Name:     it.Name,
			Type:     string(it.Type),
			Version:  it.Version,
			Implicit: sel.Implicit[ref],
			Item:     it,
		})
		out.Warnings = append(out.Warnings, warningsFor(it, arch)...)
	}

	return out, nil
}

// warningsFor reports non-fatal concerns the operator should see before
// approving a plan that runs as root.
func warningsFor(it manifest.Item, arch string) []string {
	var out []string
	switch it.Type {
	case manifest.ItemTarball, manifest.ItemDeb, manifest.ItemBinary:
		if it.SHA256[arch] == "" {
			out = append(out, fmt.Sprintf("%s: no sha256 declared; download will not be verified", it.Ref()))
		}
	case manifest.ItemScript:
		out = append(out, fmt.Sprintf("%s: runs a shell script as root from %s", it.Ref(), it.Source[arch]))
	case manifest.ItemComposeStack:
		out = append(out, fmt.Sprintf("%s: runs a docker compose stack as root (docker compose up -d)", it.Ref()))
	}
	return out
}

// Order topologically sorts refs so dependencies precede dependents. Ties are
// broken by rank, making the output deterministic and therefore diffable.
func Order(refs []string, index map[string]manifest.Item, rank map[string]int) ([]string, error) {
	selected := map[string]bool{}
	for _, r := range refs {
		selected[r] = true
	}

	indegree := map[string]int{}
	dependents := map[string][]string{}
	for _, ref := range refs {
		if _, ok := indegree[ref]; !ok {
			indegree[ref] = 0
		}
		for _, dep := range index[ref].DependsOn {
			if !selected[dep] {
				continue // dependency outside the selection; Select guarantees this is intentional
			}
			indegree[ref]++
			dependents[dep] = append(dependents[dep], ref)
		}
	}

	// Collect the initially-ready set in input order (not map iteration order)
	// so the subsequent sort receives a deterministic starting point even
	// when ranks tie.
	var ready []string
	for _, ref := range refs {
		if indegree[ref] == 0 {
			ready = append(ready, ref)
		}
	}

	byRank := func(s []string) {
		sort.SliceStable(s, func(i, j int) bool { return rank[s[i]] < rank[s[j]] })
	}
	byRank(ready)

	out := make([]string, 0, len(refs))
	for len(ready) > 0 {
		ref := ready[0]
		ready = ready[1:]
		out = append(out, ref)

		for _, dep := range dependents[ref] {
			indegree[dep]--
			if indegree[dep] == 0 {
				ready = append(ready, dep)
			}
		}
		byRank(ready)
	}

	if len(out) != len(refs) {
		var stuck []string
		for _, ref := range refs {
			if indegree[ref] > 0 {
				stuck = append(stuck, ref)
			}
		}
		sort.Strings(stuck)
		return nil, fmt.Errorf("dependency cycle among %v", stuck)
	}
	return out, nil
}
