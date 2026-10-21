package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lkshrk/omni/internal/actions"
	"github.com/lkshrk/omni/internal/app"
	syncprogress "github.com/lkshrk/omni/internal/sync"
)

func newReconcileCmd(state *rootState) *cobra.Command {
	var (
		message        string
		skipPrivileged bool
		force          bool
	)

	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Sync, upgrade, repair dotfiles, and back up dotfile changes",
		Long:  actions.MustLongDescription(actions.Reconcile),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ok, err := confirmAction(cmd, state, actions.MustConfirmDescription(actions.Reconcile))
			if err != nil || !ok {
				return err
			}
			live := newLiveProgress(cmd.OutOrStdout(), cmdOut(cmd))
			result, err := state.app.Reconcile(live.Context(cmd.Context()), app.ReconcileOptions{
				BackupMessage:  message,
				SkipPrivileged: skipPrivileged,
				Force:          force,
				Progress:       live.Step,
				ToolProgress: func(event syncprogress.ProgressEvent) {
					if !event.Done {
						return
					}
					name := app.ToolNameWithVersion(event.Tool.Name, event.TargetVersion)
					if event.Err != nil {
						live.Println(fmt.Sprintf("  ! failed: %s (%s): %v", name, event.Tool.Provider, event.Err))
						return
					}
					if reason, ok := skippedUpgradeProgressReason(event.Message); ok {
						live.Println(fmt.Sprintf("  - skipped: %s (%s): %s", name, event.Tool.Provider, reason))
						return
					}
					live.Println(fmt.Sprintf("  ✓ done: %s (%s)", name, event.Tool.Provider))
				},
			})
			live.Done()
			printReconcileSummary(cmd, result)
			return err
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "Dotfile backup message")
	cmd.Flags().BoolVar(&skipPrivileged, "skip-privileged", false, "skip package actions that need sudo/root access")
	cmd.Flags().BoolVar(&force, "force", false, "bypass update quarantine for tool upgrades")
	return cmd
}

func printReconcileSummary(cmd *cobra.Command, result *app.ReconcileResult) {
	if result == nil {
		return
	}
	out := cmdOut(cmd)
	fmt.Fprintf(out, "%s.\n", app.ReconcileSummaryText(result, "Reconcile complete"))
	printReconcileIssues(cmd, result)
}

func printReconcileIssues(cmd *cobra.Command, result *app.ReconcileResult) {
	out := cmdOut(cmd)
	lines := app.ReconcileIssueLines(result)
	if len(lines) == 0 {
		return
	}
	fmt.Fprintln(out, "\nOpen issues:")
	for _, line := range lines {
		fmt.Fprintf(out, "  ⚠ %s\n", line)
	}
}
