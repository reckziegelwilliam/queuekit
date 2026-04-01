// Package cli implements the queuekit command-line interface.
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	client  *Client
)

var rootCmd = &cobra.Command{
	Use:   "queuekit",
	Short: "QueueKit CLI — manage jobs and queues",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		server := viper.GetString("server")
		apiKey := viper.GetString("api_key")
		if server == "" {
			server = "http://localhost:8080"
		}
		client = NewClient(server, apiKey)
		return nil
	},
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default ~/.config/queuekit/config.yaml)")
	rootCmd.PersistentFlags().String("server", "", "server URL (default http://localhost:8080)")
	rootCmd.PersistentFlags().String("api-key", "", "API key for authentication")

	_ = viper.BindPFlag("server", rootCmd.PersistentFlags().Lookup("server"))
	_ = viper.BindPFlag("api_key", rootCmd.PersistentFlags().Lookup("api-key"))
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		configDir := filepath.Join(home, ".config", "queuekit")
		viper.AddConfigPath(configDir)
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
	}

	viper.SetEnvPrefix("QUEUEKIT")
	viper.AutomaticEnv()
	_ = viper.ReadInConfig()
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
