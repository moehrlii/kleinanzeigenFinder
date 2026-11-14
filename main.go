package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/llm"
	"github.com/Kamuyin/kleinanzeigenFinder/logging"
	"github.com/Kamuyin/kleinanzeigenFinder/notify"
	"github.com/Kamuyin/kleinanzeigenFinder/notify/discord"
	"github.com/Kamuyin/kleinanzeigenFinder/notify/telegram"
	"github.com/Kamuyin/kleinanzeigenFinder/notify/whatsapp"
	"github.com/Kamuyin/kleinanzeigenFinder/providers"
	"github.com/Kamuyin/kleinanzeigenFinder/runner"
	"github.com/Kamuyin/kleinanzeigenFinder/storage"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "kleinanzeigen-finder",
	Short: "Monitor Kleinanzeigen searches and notify about new matching listings.",
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the monitor loop",
	RunE: func(cmd *cobra.Command, args []string) error {
		once, _ := cmd.Flags().GetBool("once")
		noNotify, _ := cmd.Flags().GetBool("no-notify")

		appCfg, err := loadAppConfig()
		if err != nil {
			return err
		}

		db, err := storage.Connect(appCfg.App.Database)
		if err != nil {
			return fmt.Errorf("could not connect to db: %w", err)
		}

		seenStore := storage.NewSeenStore(db)
		monRepo := storage.NewMonitorRepository(db)

		catalog := providers.DefaultRegistry()
		activeProviders := make(map[string]providers.Provider)
		for _, definition := range catalog.Available() {
			instance, err := definition.Open(providers.Settings{Browser: appCfg.Scraper, ImmoScout: appCfg.ImmoScout})
			if err != nil {
				return fmt.Errorf("could not initialize %s: %w", definition.Name, err)
			}
			defer instance.Close()
			activeProviders[definition.ID] = instance
		}

		cls, err := llm.NewClassifier(appCfg.LLM)
		if err != nil {
			return fmt.Errorf("could not init classifier: %w", err)
		}

		var tg *telegram.TelegramNotifier
		if appCfg.Telegram.Enabled {
			tg, _ = telegram.NewTelegramNotifier(appCfg.Telegram)
		}

		var disc *discord.DiscordNotifier
		var bot *discord.Bot
		if appCfg.Discord.Enabled {
			disc, err = discord.NewDiscordNotifier(appCfg.Discord, monRepo)
			if err != nil {
				logrus.Warnf("Could not init Discord notifier: %v", err)
			} else if disc != nil {
				bot = discord.NewBot(disc.Session(), disc, monRepo, cls, appCfg.Discord)
				if err := bot.Start(); err != nil {
					logrus.Warnf("Could not start Discord bot: %v", err)
				} else {
					defer bot.Stop()
				}
			}
		}

		var wa *whatsapp.WhatsAppNotifier
		if appCfg.WhatsApp.Enabled {
			wa, err = whatsapp.NewWhatsAppNotifier(appCfg.WhatsApp)
			if err != nil {
				logrus.Warnf("Could not init WhatsApp notifier: %v", err)
			}
		}

		router := &notify.Router{}
		if tg != nil {
			router.Telegram = tg
		}
		if wa != nil {
			router.WhatsApp = wa
		}
		if disc != nil {
			router.Discord = disc
		}

		r := runner.New(appCfg, seenStore, monRepo, activeProviders, cls, router)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			logrus.Info("Shutdown signal received, gracefully stopping...")
			cancel()
		}()

		if once {
			stats, err := r.RunOnce(ctx, !noNotify)
			if err != nil {
				logrus.Errorf("RunOnce error: %v", err)
			}
			logrus.Infof("scanned=%d new=%d rejected=%d notified=%d deferred=%d errors=%d", stats.Scanned, stats.New, stats.Rejected, stats.Notified, stats.Deferred, stats.Errors)
			return nil
		}

		logrus.Info("Starting continuous run...")
		r.RunForever(ctx, !noNotify)
		return nil
	},
}

func init() {
	runCmd.Flags().Bool("once", false, "Run one polling cycle and exit.")
	runCmd.Flags().Bool("no-notify", false, "Evaluate and store matches without sending notifications.")

	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "config.toml", "config file (default is config.toml)")
	rootCmd.AddCommand(runCmd)
}

func loadAppConfig() (*config.AppConfig, error) {
	cfg, err := config.LoadConfig(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	logging.SetupLogging(cfg.Logging)
	return cfg, nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
