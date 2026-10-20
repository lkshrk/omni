package pacman

import (
	"github.com/lkshrk/omni/internal/executor"
	"github.com/lkshrk/omni/internal/provider"
)

func init() {
	provider.RegisterConcrete("pacman", func(e executor.Executor) provider.Provider { return New(e) })
	provider.RegisterConcrete("aur", func(e executor.Executor) provider.Provider { return NewAUR(e) })
}
