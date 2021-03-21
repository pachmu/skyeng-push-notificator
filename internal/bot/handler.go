package bot

import (
	"context"
	"fmt"
	"math/rand"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/pachmu/skyeng-push-notificator/internal/skyeng"
	"github.com/pachmu/skyeng-push-notificator/internal/state"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// TelegramBot represents bot api.
type TelegramBot interface {
	Run(ctx context.Context) error
}

// NewTelegramBot returns telegram api compatible struct.
func NewTelegramBot(token string, handler *MessageHandler) (TelegramBot, error) {
	b, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	handler.api = b
	return &bot{
		handler: handler,
		bot:     b,
	}, nil
}

const (
	actionStart          = "/start"
	actionGetWordsets    = "/show_wordsets"
	actionSuspend        = "/suspend"
	actionChangeInterval = "/interval"
	actionStartRandom    = "/start_random"
)

const (
	callbackNextWordsetPage = "next"
	callbackPrevWordsetPage = "prev"
	callbackGetWords        = "get_words"
	callbackGetWord         = "get_word"
	callbackSetWordset      = "set_wordset"
	callbackShowDefinition  = "show_definition"
	callbackShowExamples    = "show_examples"
)

type botActions map[string]func(m *tgbotapi.Message, resp *tgbotapi.MessageConfig, params []string) error
type botCallbacks map[string]func(resp *tgbotapi.MessageConfig, args []string) error

type bot struct {
	handler *MessageHandler
	bot     *tgbotapi.BotAPI
}

func (b *bot) Run(ctx context.Context) error {
	b.bot.Debug = false

	logrus.Infof("Authorized on account %s", b.bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates, err := b.bot.GetUpdatesChan(u)
	if err != nil {
		return errors.WithStack(err)
	}

	// Do not handle a large backlog of old messages
	time.Sleep(time.Millisecond * 500)
	updates.Clear()

	for {
		var update tgbotapi.Update
		select {
		case update = <-updates:
			err := b.handler.handle(update)
			if err != nil {
				logrus.Error(err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// NewMessageHandler returns MessageHandler.
func NewMessageHandler(user string, client skyeng.Client, state *state.State) *MessageHandler {
	h := &MessageHandler{
		skyengClient: client,
		user:         user,
		state:        state,
	}
	h.actions = botActions{
		actionStart: func(m *tgbotapi.Message, resp *tgbotapi.MessageConfig, params []string) error {
			h.replyText(resp, "Hello "+m.From.UserName+"! Please choose the action:")
			h.withActionKeyboard(resp)
			return nil
		},
		actionStartRandom: func(m *tgbotapi.Message, resp *tgbotapi.MessageConfig, params []string) error {
			f, err := h.getRandomPeriodicSenderCallback(resp)
			if err != nil {
				return err
			}
			state.SetWordsetCallback(f)
			resp.Text = "Random sending started!"

			return nil
		},
		actionGetWordsets: func(m *tgbotapi.Message, resp *tgbotapi.MessageConfig, params []string) error {
			return h.showWordsets(resp, 1)
		},
		actionSuspend: func(m *tgbotapi.Message, resp *tgbotapi.MessageConfig, params []string) error {
			h.state.SuspendWork()
			resp.Text = "Work suspended!"

			return nil
		},
		actionChangeInterval: func(m *tgbotapi.Message, resp *tgbotapi.MessageConfig, params []string) error {
			if len(params) == 0 {
				return errors.New("Interval value required")
			}
			interval, err := strconv.Atoi(params[0])
			if err != nil {
				return errors.Wrap(err, "failed to parse interval value")
			}
			h.state.ChangeTimeInterval(time.Duration(interval) * time.Minute)
			resp.Text = "Time interval changed!"

			return nil
		},
	}

	argToInt := func(args []string, errMsg string) (int, error) {
		if len(args) == 0 {
			return 0, errors.New(errMsg)
		}
		num, err := strconv.Atoi(args[0])
		if err != nil {
			return 0, errors.WithStack(err)
		}

		return num, nil
	}

	navigate := func(resp *tgbotapi.MessageConfig, args []string) error {
		page, err := argToInt(args, "failed to get wordsets for undefined page")
		if err != nil {
			return err
		}

		return h.showWordsets(resp, page)
	}

	h.callbacks = botCallbacks{
		callbackGetWords: func(resp *tgbotapi.MessageConfig, args []string) error {
			wordsetID, err := argToInt(args, "failed to get words from undefined wordset")
			if err != nil {
				return err
			}
			return h.showWords(resp, wordsetID, strings.Join(args[1:], " "))
		},
		callbackGetWord: func(resp *tgbotapi.MessageConfig, args []string) error {
			meaningID, err := argToInt(args, "failed to get word by id")
			if err != nil {
				return err
			}
			return h.showWord(resp, meaningID)
		},
		callbackShowExamples: func(resp *tgbotapi.MessageConfig, args []string) error {
			meaningID, err := argToInt(args, "failed to get meaning by id")
			if err != nil {
				return err
			}
			return h.showExamples(resp, meaningID)
		},
		callbackShowDefinition: func(resp *tgbotapi.MessageConfig, args []string) error {
			meaningID, err := argToInt(args, "failed to get meaning by id")
			if err != nil {
				return err
			}
			return h.showDefinition(resp, meaningID)
		},
		callbackNextWordsetPage: navigate,
		callbackPrevWordsetPage: navigate,
		callbackSetWordset: func(resp *tgbotapi.MessageConfig, args []string) error {
			wordsetID, err := argToInt(args, "failed to get wordset ID from undefined args")
			if err != nil {
				return err
			}
			if len(args) < 2 {
				return errors.New("not enough args")
			}
			state.SetWordsetCallback(func() error {
				wordsetResp := tgbotapi.NewMessage(resp.ChatID, "")
				err := h.showWords(&wordsetResp, wordsetID, args[1])
				if err != nil {
					return err
				}
				_, err = h.api.Send(wordsetResp)
				if err != nil {
					return errors.WithStack(err)
				}
				return nil
			})

			resp.Text = "Wordset changed!"

			return nil
		},
	}

	return h
}

// MessageHandler represents bot message handling functionality.
type MessageHandler struct {
	api          *tgbotapi.BotAPI
	user         string
	actions      botActions
	state        *state.State
	skyengClient skyeng.Client
	callbacks    botCallbacks
}

func (h *MessageHandler) handle(upd tgbotapi.Update) error {
	defer func() {
		if err := recover(); err != nil {
			logrus.Error(err, string(debug.Stack()))
		}
	}()

	user := getUser(upd)
	if user == "" {
		return nil
	}
	logrus.Infof("Message from user [%s]", user)

	err := h.authorize(user)
	if err != nil {
		return err
	}
	msg := getMessage(upd)
	resp := tgbotapi.NewMessage(msg.Chat.ID, "")

	switch {
	case upd.Message != nil:
		err = h.handleActions(upd.Message, &resp)
	case upd.CallbackQuery != nil:
		err = h.handleCallback(upd.CallbackQuery, &resp)
	default:
		return nil
	}
	if err != nil {
		return err
	}

	_, err = h.api.Send(resp)
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func getUser(upd tgbotapi.Update) string {
	var user string
	if upd.Message != nil {
		user = upd.Message.From.UserName
	}
	if upd.CallbackQuery != nil {
		user = upd.CallbackQuery.From.UserName
	}

	return user
}

func getMessage(upd tgbotapi.Update) *tgbotapi.Message {
	var message *tgbotapi.Message
	if upd.Message != nil {
		message = upd.Message
	}
	if upd.CallbackQuery != nil {
		message = upd.CallbackQuery.Message
	}

	return message
}

func (h *MessageHandler) replyText(resp *tgbotapi.MessageConfig, txt string) {
	resp.Text = txt
}

func (h *MessageHandler) withActionKeyboard(resp *tgbotapi.MessageConfig) {
	resp.ReplyMarkup = tgbotapi.NewReplyKeyboard(
		[]tgbotapi.KeyboardButton{
			tgbotapi.NewKeyboardButton(actionGetWordsets),
		},
		[]tgbotapi.KeyboardButton{
			tgbotapi.NewKeyboardButton(actionSuspend),
		},
	)
}

func (h *MessageHandler) handleActions(msg *tgbotapi.Message, resp *tgbotapi.MessageConfig) error {
	logrus.Infof("Message [%+v]", msg)
	words := strings.Split(msg.Text, " ")
	if len(words) == 0 {
		h.replyText(resp, "Unknown command")

		return nil
	}
	cmd, ok := h.actions[words[0]]
	if !ok {
		h.replyText(resp, "Unknown command")

		return nil
	}

	err := cmd(msg, resp, words[1:])
	if err != nil {
		return err
	}

	return nil
}

func (h *MessageHandler) handleCallback(query *tgbotapi.CallbackQuery, resp *tgbotapi.MessageConfig) error {
	logrus.Infof("Callback [%+v]", query.Data)
	if len(query.Data) == 0 {
		return errors.WithStack(errors.New("failed to execute callback, data is empty"))
	}
	args := strings.Split(query.Data, " ")
	if len(args) <= 1 {
		return errors.WithStack(errors.New("failed to execute callback, args is not sufficient"))
	}
	callback, ok := h.callbacks[args[0]]
	if !ok {
		h.replyText(resp, "Unknown callback")

		return nil
	}

	err := callback(resp, args[1:])
	if err != nil {
		return err
	}

	return nil
}

func (h *MessageHandler) getRandomPeriodicSenderCallback(resp *tgbotapi.MessageConfig) (func() error, error) {
	wordsets, err := h.skyengClient.GetWordsets(0)
	if err != nil {
		return nil, err
	}
	rand.Seed(time.Now().Unix())
	getRandWordsetID := func() (int, string) {
		num := rand.Intn(len(wordsets))
		return wordsets[num].ID, wordsets[num].Title
	}
	return func() error {
		wordsetID, name := getRandWordsetID()
		wordsetResp := tgbotapi.NewMessage(resp.ChatID, "")
		err := h.showWords(&wordsetResp, wordsetID, name)
		if err != nil {
			return err
		}
		_, err = h.api.Send(wordsetResp)
		if err != nil {
			return errors.WithStack(err)
		}
		return nil
	}, nil
}

func (h *MessageHandler) showWordsets(resp *tgbotapi.MessageConfig, page int) error {
	wordsets, err := h.skyengClient.GetWordsets(page)
	if err != nil {
		return err
	}
	prevPage := page - 1
	var nextPage int
	if len(wordsets) == 0 {
		nextPage = page
	} else {
		nextPage = page + 1
	}
	var buttons [][]tgbotapi.InlineKeyboardButton
	for _, ws := range wordsets {
		buttons = append(buttons, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(ws.Title, fmt.Sprintf("get_words %d %s", ws.ID, ws.Title)),
		})
	}
	var navigation []tgbotapi.InlineKeyboardButton
	if page-1 > 0 {
		navigation = append(
			navigation,
			tgbotapi.NewInlineKeyboardButtonData(" ⬅️", fmt.Sprintf("prev %d", prevPage)),
		)
	}
	navigation = append(
		navigation,
		tgbotapi.NewInlineKeyboardButtonData("➡️", fmt.Sprintf("next %d", nextPage)),
	)
	tgbotapi.NewInlineKeyboardRow()
	buttons = append(buttons, navigation)
	resp.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons...)
	resp.Text = "Choose wordset for more actions."

	return nil
}

func (h *MessageHandler) showWords(resp *tgbotapi.MessageConfig, wordsetID int, wordsetName string) error {
	words, err := h.skyengClient.GetWords(skyeng.Wordset{ID: wordsetID})
	if err != nil {
		return err
	}
	meanings, err := h.skyengClient.GetMeaning(words...)
	if err != nil {
		return err
	}
	var buttons [][]tgbotapi.InlineKeyboardButton
	for _, m := range meanings {
		buttons = append(buttons, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(m.Text, fmt.Sprintf("%s %d", callbackGetWord, m.ID)),
		})
	}
	buttons = append(buttons, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(
			"Choose wordset", fmt.Sprintf("%s %d %s", callbackSetWordset, wordsetID, wordsetName),
		),
	})
	resp.Text = "Choose word to show translation and examples."
	resp.ParseMode = tgbotapi.ModeHTML
	resp.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons...)

	return nil

}

func (h *MessageHandler) showWord(resp *tgbotapi.MessageConfig, meaningID int) error {
	meanings, err := h.skyengClient.GetMeaning(skyeng.Word{
		MeaningID: meaningID,
	})
	if err != nil {
		return err
	}
	if len(meanings) == 0 {
		return errors.WithStack(errors.New("failed to get word meaning"))
	}
	builder := strings.Builder{}
	for _, m := range meanings {
		builder.WriteString(fmt.Sprintf("%s\n %s\n %s\n", m.Text, m.Transcription, m.Translation))
	}
	var buttons [][]tgbotapi.InlineKeyboardButton
	buttons = append(buttons, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(
			"Show definition", fmt.Sprintf("%s %d", callbackShowDefinition, meaningID),
		),
	})
	buttons = append(buttons, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(
			"Show examples", fmt.Sprintf("%s %d", callbackShowExamples, meaningID),
		),
	})
	resp.Text = builder.String()
	resp.ParseMode = tgbotapi.ModeHTML
	resp.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons...)

	return nil
}

func (h *MessageHandler) showDefinition(resp *tgbotapi.MessageConfig, meaningID int) error {
	meanings, err := h.skyengClient.GetMeaning(skyeng.Word{
		MeaningID: meaningID,
	})
	if err != nil {
		return err
	}
	if len(meanings) == 0 {
		return errors.WithStack(errors.New("failed to get word meaning"))
	}
	builder := strings.Builder{}
	for _, m := range meanings {
		builder.WriteString(fmt.Sprintf("%s\n %s", m.Text, m.Definition.Text))
	}
	resp.Text = builder.String()

	return nil
}

func (h *MessageHandler) showExamples(resp *tgbotapi.MessageConfig, meaningID int) error {
	meanings, err := h.skyengClient.GetMeaning(skyeng.Word{
		MeaningID: meaningID,
	})
	if err != nil {
		return err
	}
	if len(meanings) == 0 {
		return errors.WithStack(errors.New("failed to get word meaning"))
	}
	builder := strings.Builder{}
	for _, m := range meanings {
		builder.WriteString(fmt.Sprintf("%s\n", m.Text))
		for _, e := range m.Examples {
			builder.WriteString(fmt.Sprintf("%s\n", e.Text))
		}
	}
	resp.Text = builder.String()

	return nil
}

func (h *MessageHandler) authorize(user string) error {
	if user == "" || user != h.user {
		return fmt.Errorf("I don't know you, %s", user)
	}

	return nil
}
