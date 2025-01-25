package cmd

import (
	"fmt"
	"os"

	"github.com/xmigrate/blxrep/pkg/agent"
	"github.com/xmigrate/blxrep/pkg/dispatcher"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start blxrep",
	Run: func(cmd *cobra.Command, args []string) {
		mode := viper.GetString("mode")
		switch mode {
		case "dispatcher":
			dataDir := viper.GetString("data-dir")
			targets := viper.GetStringSlice("targets")
			policyDir := viper.GetString("policy-dir")
			fmt.Println("Dispatcher started...")
			if dataDir == "" {
				fmt.Println("Data directory is required")
				return
			}
			if policyDir == "" {
				fmt.Println("Policy directory is required")
				return
			}
			dispatcher.Start(dataDir, targets, policyDir)
		case "agent":
			agentID := viper.GetString("id")
			dispatcherAddr := viper.GetString("dispatcher-addr")
			if agentID == "" {
				fmt.Println("Agent ID is required")
				return
			}
			if dispatcherAddr == "" {
				fmt.Println("Dispatcher address is required")
				return
			}
			fmt.Printf("Starting agent with ID: %s, connecting to dispatcher at: %s\n", agentID, dispatcherAddr)
			agent.Start(agentID, dispatcherAddr)
		default:
			fmt.Println("Invalid mode. Use 'dispatcher' or 'agent' in config or --mode flag")
		}
	},
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is /etc/blxrep/config.yaml)")

	startCmd.Flags().String("mode", "", "Start mode: 'dispatcher' or 'agent'")
	startCmd.Flags().String("id", "", "Agent ID (required for agent)")
	startCmd.Flags().String("dispatcher-addr", "", "Dispatcher address (required for agent, format: host:port)")
	startCmd.Flags().String("data-dir", "", "Data directory (required for dispatcher)")
	startCmd.Flags().String("policy-dir", "", "Policy directory (required for dispatcher)")
	viper.BindPFlag("mode", startCmd.Flags().Lookup("mode"))
	viper.BindPFlag("id", startCmd.Flags().Lookup("id"))
	viper.BindPFlag("dispatcher-addr", startCmd.Flags().Lookup("dispatcher-addr"))
	viper.BindPFlag("data-dir", startCmd.Flags().Lookup("data-dir"))
	viper.BindPFlag("policy-dir", startCmd.Flags().Lookup("policy-dir"))
	rootCmd.AddCommand(startCmd)
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.AddConfigPath("/etc/blxrep")
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	} else {
		fmt.Fprintln(os.Stderr, "Warning: Could not read config file:", err)
	}
}
