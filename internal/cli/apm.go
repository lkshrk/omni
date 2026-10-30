package cli

import (
	"os"

	"github.com/spf13/cobra"
)

func newAPMCmd(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:                "apm <args...>",
		Short:              "Run omni's private APM copy",
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
				return cmd.Help()
			}
			return state.app.RunAPMAttached(cmd.Context(), os.Stdin, cmd.OutOrStdout(), cmd.ErrOrStderr(), args...)
		},
	}
}
