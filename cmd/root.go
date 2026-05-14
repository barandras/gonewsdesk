/*
Copyright © 2026 Andras Bartha <andras@bartha.dev>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/barandras/gonewsdesk/internal/newsdesk"
	"github.com/barandras/gonewsdesk/pkg/news"
	"github.com/barandras/gonewsdesk/pkg/news/alpaca"
	"github.com/barandras/gonewsdesk/pkg/news/stocklabs"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "gonewsdesk",
	Short: "A CLI-GUI application to monitor the latest news that can impact the financial markets",
	Long: `GoNewsDesk is a CLI-GUI application to monitor the latest news that can impact the financial markets.
It uses multiple news sources to get the latest news and displays it in a GUI.
Each news source can be configured, enabled or disabled one by one.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runApp(); err != nil {
			log.Fatal(err)
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	configEnvValue := os.Getenv("CONFIG") // specify config file location in this env variable
	if len(configEnvValue) > 0 {
		cfgFile = configEnvValue
	}

	err := rootCmd.Execute()
	if err != nil {
		log.Fatalf("Error while running the root cmd: %v", err)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./config.yaml)")
	rootCmd.PersistentFlags().BoolP("debug", "d", false, "enable debug mode")
	rootCmd.PersistentFlags().Bool("flag-compat", false, "replace regional-indicator flag emoji with ASCII tags for terminals that mis-render them")

	viper.BindPFlags(rootCmd.Flags())
	viper.BindPFlags(rootCmd.PersistentFlags())
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.AddConfigPath(".")
		viper.SetConfigName("config")
	}

	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		log.Println("Using config file:", viper.ConfigFileUsed())
	}

	if viper.GetBool("debug") {
		log.Println("Debug mode enabled")
	}
}

func runApp() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	debug := viper.GetBool("debug")

	providers, err := buildNewsProviders(ctx, debug)
	if err != nil {
		return err
	}

	exclude, include := filterIncludeExcludeFromConfig()
	np, err := news.NewNewsProcessor(news.NewNewsProcessorParams{
		Ctx:             ctx,
		NewsProviders:   providers,
		ExcludeKeywords: exclude,
		IncludeKeywords: include,
	})
	if err != nil {
		return err
	}

	desk := newsdesk.NewNewsDesk(newsdesk.NewNewsDeskParams{
		Ctx:               ctx,
		NewsProcessor:     np,
		HighlightKeywords: highlightKeywordsFromConfig(),
		TruncateTileBody:  truncateTileBodyEnabledFromConfig(),
		TileBodyMaxChars:  tileBodyMaxCharsFromConfig(),
		ShortHeadlineOnly: shortHeadlineOnlyFromConfig(),
		FlagCompat:        viper.GetBool("flag-compat"),
		OnShutdown:        stop,
		Debug:             debug,
	})
	return desk.Run()
}

func buildNewsProviders(ctx context.Context, debug bool) ([]news.NewsProvider, error) {
	var providers []news.NewsProvider

	if viper.GetBool("stocklabs.enabled") {
		p := stocklabs.NewStocklabsNewsProvider(stocklabs.NewStocklabsNewsProviderParams{
			Ctx:               ctx,
			Debug:             debug,
			IncludeHistorical: viper.GetBool("stocklabs.includeHistorical"),
		})
		providers = append(providers, p)
	}

	if alp := viper.Sub("alpaca"); alp != nil && alp.GetBool("enabled") {
		id := strings.TrimSpace(alp.GetString("apiKeyID"))
		sec := strings.TrimSpace(alp.GetString("apiKeySecret"))
		if id != "" && sec != "" {
			p := alpaca.NewAlpacaNewsProvider(alpaca.NewAlpacaNewsProviderParams{
				Ctx:               ctx,
				Debug:             debug,
				IncludeHistorical: alp.GetBool("includeHistorical"),
				AlpacaCredentials: alpaca.AlpacaCredentials{
					APIKeyID:     id,
					APIKeySecret: sec,
				},
			})
			providers = append(providers, p)
		} else {
			log.Printf("alpaca is enabled in config but apiKeyID or apiKeySecret is empty; skipping alpaca")
		}
	}

	if len(providers) == 0 {
		return nil, fmt.Errorf("no news sources to run: enable stocklabs and/or alpaca (with keys) in config")
	}
	return providers, nil
}

func filterIncludeExcludeFromConfig() (exclude, include []string) {
	f := viper.Sub("filter")
	if f == nil {
		return nil, nil
	}
	return f.GetStringSlice("excludeKeywords"),
		f.GetStringSlice("includeKeywords")
}

func highlightKeywordsFromConfig() []string {
	f := viper.Sub("filter")
	if f == nil {
		return nil
	}
	return f.GetStringSlice("highlightKeywords")
}

func truncateTileBodyEnabledFromConfig() bool {
	ui := viper.Sub("ui")
	if ui == nil {
		return false
	}
	return ui.GetBool("truncateTileBody")
}

func tileBodyMaxCharsFromConfig() int {
	ui := viper.Sub("ui")
	if ui == nil {
		return newsdesk.DefaultTileBodyMaxChars
	}
	maxChars := ui.GetInt("tileBodyMaxChars")
	if maxChars <= 0 {
		return newsdesk.DefaultTileBodyMaxChars
	}
	return maxChars
}

func shortHeadlineOnlyFromConfig() bool {
	ui := viper.Sub("ui")
	if ui == nil {
		return false
	}
	return ui.GetBool("shortHeadlineOnly")
}
