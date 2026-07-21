package engine

import (
	"github.com/t0mer/kamino/internal/engine/runners"
	"github.com/t0mer/kamino/internal/manifest"
)

// NewRegistry wires every implemented runner to its item type.
//
// compose_stack and snap are deliberately absent: an item using them fails its
// step with "no runner for item type", which is a clearer outcome than a
// half-implemented installer.
func NewRegistry(d runners.Deps, src runners.ScriptSource, ref string) RunnerFor {
	table := map[manifest.ItemType]runners.Runner{
		manifest.ItemApt:     runners.NewApt(d),
		manifest.ItemDeb:     runners.NewDeb(d),
		manifest.ItemTarball: runners.NewTarball(d),
		manifest.ItemBinary:  runners.NewBinary(d),
		manifest.ItemPip:     runners.NewPip(d),
		manifest.ItemScript:  runners.NewScript(d, src, ref),
	}
	return func(t manifest.ItemType) (runners.Runner, bool) {
		r, ok := table[t]
		return r, ok
	}
}
