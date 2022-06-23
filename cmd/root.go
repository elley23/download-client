/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"download/client"
	"fmt"
	"github.com/spf13/viper"
	"os"

	"github.com/spf13/cobra"
)

var url string
var dir string
var goroutines int
var rangeSize int64
var chanLen int64

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "download",
	Short: "download a file.",
	Long:  `You can download a file using url.`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	Run: func(cmd *cobra.Command, args []string) {

		if len(url) == 0 {
			fmt.Println("Please input the url...")
			cmd.Help()
			return
		}

		//viper.GetString()

		client.RangeSize = rangeSize
		client.ChanCnt = chanLen
		client.Goroutines = goroutines
		client.Dir = dir
		err := client.DownloadFileGo(url)
		if err != nil {
			cmd.Help()
			return
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

var cfgFile string

func init() {

	cobra.OnInitialize(initConfig)
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.xpower.yaml)")
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (default is ./conf/config.ini)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	//rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	rootCmd.Flags().StringVarP(&url, "url", "u", "", "url (Mandatory)")
	rootCmd.Flags().StringVarP(&dir, "dir", "d", "", "Destination file's directory")

}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigFile("./conf/config.ini")
	}

	//viper.AutomaticEnv()

	err := viper.ReadInConfig()
	if err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}

	goroutines = viper.GetInt("default.goroutines")
	rangeSize = viper.GetInt64("default.range_size")
	chanLen = viper.GetInt64("default.channel_length")
}
