/*
Copyright © 2024 Vishnu KS vishnu@xmigrate.cloud
*/
package main

import (
	"fmt"
	"os"

	"github.com/xmigrate/blxrep/cmd"
	"github.com/xmigrate/blxrep/tui"
	"github.com/xmigrate/blxrep/utils"

	_ "embed"

	"github.com/spf13/cobra"
)

var publicKeyData []byte

func main() {
	utils.PrintAnimatedLogo()

	utils.PublicKeyData = publicKeyData

	rootCmd := cmd.GetRootCmd()

	// Modify the dispatcher command to use the TUI
	for _, subCmd := range rootCmd.Commands() {
		if subCmd.Use == "tui" {
			originalRun := subCmd.Run
			subCmd.Run = func(cmd *cobra.Command, args []string) {
				dataDir, _ := cmd.Flags().GetString("data-dir")
				agent, _ := cmd.Flags().GetString("agent")
				if dataDir != "" && agent != "" {
					tui.RunDispatcherTUI(dataDir)
				} else {
					// Fall back to original behavior if flags are not set
					originalRun(cmd, args)
				}
			}
			break
		}
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
