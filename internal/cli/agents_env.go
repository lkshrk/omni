package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func newAgentsEnvCmd(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "env",
		Short: "List the environment variables each client needs for its MCP servers",
		Long: "List, per client, the environment variable names its deployed MCP servers reference as " +
			"${VAR}, read from APM's global lockfile. Prints names only, never values.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			vars, err := state.app.AgentsEnv()
			if err != nil {
				return err
			}
			if len(vars) == 0 {
				fmt.Fprintln(cmdOut(cmd), "No MCP environment variables referenced.")
				return nil
			}
			clients := make([]string, 0, len(vars))
			for c := range vars {
				clients = append(clients, c)
			}
			sort.Strings(clients)
			for _, c := range clients {
				fmt.Fprintf(cmdOut(cmd), "%s: %s\n", c, strings.Join(vars[c], " "))
			}
			return nil
		},
	}
}
