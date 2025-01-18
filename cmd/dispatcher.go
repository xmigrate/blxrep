package cmd

import (
	"github.com/xmigrate/blxrep/pkg/dispatcher"
	"github.com/xmigrate/blxrep/tui"

	"github.com/spf13/cobra"
)

var dispatcherCmd = &cobra.Command{
	Use:   "dispatcher",
	Short: "Dispatcher commands",
	Long:  `Dispatcher commands for managing the dispatcher service and checkpoints.`,
	Run: func(cmd *cobra.Command, args []string) {
		dataDir, _ := cmd.Flags().GetString("data-dir")
		tui.RunDispatcherTUI(dataDir)
	},
}

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Show dispatcher checkpoints",
	Run: func(cmd *cobra.Command, args []string) {
		start, _ := cmd.Flags().GetString("start")
		end, _ := cmd.Flags().GetString("end")
		agent, _ := cmd.Flags().GetString("agent")
		dataDir, _ := cmd.Flags().GetString("data-dir")
		disk, _ := cmd.Flags().GetString("disk")
		dispatcher.ShowCheckpoints(start, end, agent, dataDir, disk)
	},
}

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore dispatcher checkpoint",
	Run: func(cmd *cobra.Command, args []string) {
		checkpoint, _ := cmd.Flags().GetString("checkpoint")
		dataDir, _ := cmd.Flags().GetString("data-dir")
		agent, _ := cmd.Flags().GetString("agent")
		dispatcher.Restore(checkpoint, dataDir, agent)
	},
}

func init() {
	rootCmd.AddCommand(dispatcherCmd)

	dispatcherCmd.AddCommand(showCmd)
	dispatcherCmd.Flags().String("data-dir", "", "Data directory")
	showCmd.Flags().String("start", "", "Start timestamp format: YYYYMMDDHHMM")
	showCmd.Flags().String("end", "", "End timestamp format: YYYYMMDDHHMM")
	showCmd.Flags().String("agent", "", "Agent name")
	showCmd.Flags().String("disk", "", "Disk name")
	showCmd.Flags().String("data-dir", "", "Data directory")
	showCmd.MarkFlagRequired("agent")
	showCmd.MarkFlagRequired("data-dir")
	dispatcherCmd.AddCommand(restoreCmd)
	restoreCmd.Flags().String("checkpoint", "", "Checkpoint timestamp")
	restoreCmd.Flags().String("agent", "", "Agent name")
	restoreCmd.Flags().String("data-dir", "", "Data directory")
	restoreCmd.MarkFlagRequired("checkpoint")
	restoreCmd.MarkFlagRequired("agent")
	restoreCmd.MarkFlagRequired("data-dir")
}
