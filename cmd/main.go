package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/pachmu/skyeng-push-notificator/config"
	"github.com/pachmu/skyeng-push-notificator/internal/bot"
	"github.com/pachmu/skyeng-push-notificator/internal/pushover"
	"github.com/pachmu/skyeng-push-notificator/internal/sender"
	"github.com/pachmu/skyeng-push-notificator/internal/skyeng"
	"github.com/pachmu/skyeng-push-notificator/server"
	"github.com/sirupsen/logrus"
)

var configPath = flag.String("config", "./config/config.yaml", "Path to config file")

func main() {
	flag.Parse()
	conf, err := config.GetConfig(*configPath)
	if err != nil {
		logrus.Fatal(err)
	}
	skyengClient := skyeng.NewClient(conf.Skyeng.User, conf.Skyeng.Password)

	pushoverClient := pushover.NewClient(conf.Pushover.Token, conf.Pushover.User, conf.Pushover.Device)

	terminateSender := make(chan struct{}, 1)
	terminateBot := make(chan struct{}, 1)

	sndr := sender.NewSender(
		skyengClient,
		pushoverClient,
		terminateSender,
		time.Second*time.Duration(conf.SendInterval),
	)
	wg := sync.WaitGroup{}
	go func() {
		wg.Add(1)
		defer wg.Done()

		err := sndr.Send()
		if err != nil {
			logrus.Error(fmt.Sprintf("%+v", err))
		}
	}()
	logrus.Info("Sender started")

	handler := bot.NewMessageHandler(conf.Bot.User, skyengClient, sndr)
	bt, err := bot.NewTelegramBot(conf.Bot.Token, terminateBot, handler)
	if err != nil {
		logrus.Fatal(err)
	}
	go func() {
		wg.Add(1)
		defer wg.Done()

		err := bt.Run()
		if err != nil {
			logrus.Error(fmt.Sprintf("%+v", err))
		}
	}()
	logrus.Info("Bot started")

	go func() {
		srv := server.Server{
			Addr: "",
			Port: conf.Port,
		}
		err := srv.Serve(conf.Skyeng.User, skyengClient, sndr)
		if err != nil {
			logrus.Error(fmt.Sprintf("%+v", err))
		}
	}()
	logrus.Infof("Server started on port %d", conf.Port)

	quitCh := make(chan os.Signal, 1)
	signal.Notify(quitCh, os.Kill, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	<-quitCh
	terminateSender <- struct{}{}
	terminateBot <- struct{}{}
	wg.Wait()
	logrus.Info("Process terminated")
}
