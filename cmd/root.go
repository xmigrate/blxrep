package cmd

import (
	"fmt"
	"os"

	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/spf13/cobra"
)

var nrApp *newrelic.Application
var rootCmd = &cobra.Command{
	Use:   "blxrep",
	Short: "blxrep CLI application",
	Long:  `This tool is used to do live data replication for disks over network.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if nrApp != nil {
			txn := nrApp.StartTransaction(cmd.Name())
			cmd.SetContext(newrelic.NewContext(cmd.Context(), txn))
		}
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if nrApp != nil {
			if txn := newrelic.FromContext(cmd.Context()); txn != nil {
				txn.End()
			}
		}
	},
}

func GetRootCmd() *cobra.Command {
	return rootCmd
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
