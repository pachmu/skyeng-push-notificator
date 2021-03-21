package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pachmu/skyeng-push-notificator/config"
	"github.com/pachmu/skyeng-push-notificator/internal/bot"
	"github.com/pachmu/skyeng-push-notificator/internal/sender"
	"github.com/pachmu/skyeng-push-notificator/internal/skyeng"
	"github.com/pachmu/skyeng-push-notificator/internal/state"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

var configPath = flag.String("config", "./config/config.yaml", "Path to config file")

func main() {
	flag.Parse()
	conf, err := config.GetConfig(*configPath)
	if err != nil {
		logrus.Fatal(err)
	}
	skyengClient := skyeng.NewClient(conf.Skyeng.User, conf.Skyeng.Password)

	st := state.NewState(time.Second * conf.SendInterval)
	sndr := sender.NewSender(st)
	handler := bot.NewMessageHandler(conf.Bot.User, skyengClient, st)
	bt, err := bot.NewTelegramBot(conf.Bot.Token, handler)
	if err != nil {
		logrus.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errGr, ctx := errgroup.WithContext(ctx)
	errGr.Go(func() error {
		err := sndr.Run(ctx)
		if err != nil {
			return err
		}
		return nil
	})
	logrus.Info("Sender started")

	errGr.Go(func() error {
		err := bt.Run(ctx)
		if err != nil {
			return err
		}
		return nil
	})
	logrus.Info("Bot started")

	logrus.Infof("Server started on port %d", conf.Port)
	errGr.Go(func() error {
		quitCh := make(chan os.Signal, 1)
		signal.Notify(quitCh, os.Kill, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

		<-quitCh
		cancel()
		return nil
	})

	err = errGr.Wait()
	if err != nil {
		logrus.Fatal(err)
	}
	logrus.Info("Process terminated")
}
