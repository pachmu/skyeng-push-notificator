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
	"github.com/pachmu/skyeng-push-notificator/internal/storage"
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

type botActions map[string]func(m *tgbotapi.Message, chatParams []string) (tgbotapi.Chattable, error)
type botCallbacks map[string]func(query *tgbotapi.CallbackQuery, args []string) (tgbotapi.Chattable, error)

type bot struct {
	handler *MessageHandler
	bot     *tgbotapi.BotAPI
}

func (b *bot) Run(ctx context.Context) error {
	err := b.handler.init(b.bot)
	if err != nil {
		return err
	}
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
func NewMessageHandler(user string, client skyeng.Client, state *state.State, storage storage.Storage) *MessageHandler {
	return &MessageHandler{
		skyengClient: client,
		user:         user,
		state:        state,
		storage:      storage,
	}
}

// MessageHandler represents bot message handling functionality.
type MessageHandler struct {
	api          *tgbotapi.BotAPI
	user         string
	actions      botActions
	state        *state.State
	storage      storage.Storage
	skyengClient skyeng.Client
	callbacks    botCallbacks
	data         *storage.Data
}

func (h *MessageHandler) init(api *tgbotapi.BotAPI) error {
	h.api = api
	h.actions = botActions{
		actionStart: func(m *tgbotapi.Message, params []string) (tgbotapi.Chattable, error) {
			resp := h.getReplyText(m, "Hello "+m.From.UserName+"! Please choose the action:")
			h.withActionKeyboard(resp)
			return resp, nil
		},
		actionStartRandom: func(m *tgbotapi.Message, params []string) (tgbotapi.Chattable, error) {
			resp, err := h.startRandomSending(m.Chat.ID)
			if err != nil {
				return nil, err
			}
			return resp, nil
		},
		actionGetWordsets: func(m *tgbotapi.Message, params []string) (tgbotapi.Chattable, error) {
			resp := h.getReplyText(m, "")
			err := h.showWordsets(resp, 1)
			if err != nil {
				return nil, err
			}
			return resp, nil
		},
		actionSuspend: func(m *tgbotapi.Message, params []string) (tgbotapi.Chattable, error) {
			h.state.SuspendWork()
			return h.getReplyText(m, "Work suspended!"), nil
		},
		actionChangeInterval: func(m *tgbotapi.Message, params []string) (tgbotapi.Chattable, error) {
			if len(params) == 0 {
				return nil, errors.New("Interval value required")
			}
			interval, err := strconv.Atoi(params[0])
			if err != nil {
				return nil, errors.Wrap(err, "failed to parse interval value")
			}
			h.state.ChangeTimeInterval(time.Duration(interval))
			h.data.Interval = time.Duration(interval)
			err = h.storage.WriteData(h.data)
			if err != nil {
				return nil, err
			}
			return h.getReplyText(m, "Time interval changed!"), nil
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

	navigate := func(query *tgbotapi.CallbackQuery, args []string) (tgbotapi.Chattable, error) {
		page, err := argToInt(args, "failed to get wordsets for undefined page")
		if err != nil {
			return nil, err
		}
		resp := h.getReplyText(query.Message, "")
		err = h.showWordsets(resp, page)
		if err != nil {
			return nil, err
		}
		return resp, nil
	}

	h.callbacks = botCallbacks{
		callbackGetWords: func(query *tgbotapi.CallbackQuery, args []string) (tgbotapi.Chattable, error) {
			wordsetID, err := argToInt(args, "failed to get words from undefined wordset")
			if err != nil {
				return nil, err
			}
			resp := h.getReplyText(query.Message, "")
			err = h.showWords(resp, wordsetID, strings.Join(args[1:], " "))
			if err != nil {
				return nil, err
			}
			return resp, nil
		},
		callbackGetWord: func(query *tgbotapi.CallbackQuery, args []string) (tgbotapi.Chattable, error) {
			if len(args) < 3 {
				return nil, errors.New("not enough args")
			}
			wordsetID, err := argToInt(args, "failed to get wordset id from args")
			if err != nil {
				return nil, err
			}
			meaningID, err := argToInt(args[1:], "failed to get word by id")
			if err != nil {
				return nil, err
			}
			wordsetName := strings.Join(args[2:], " ")

			resp, err := h.showWord(query.Message, wordsetID, wordsetName, meaningID)
			if err != nil {
				return nil, err
			}
			return resp, nil
		},
		callbackShowExamples: func(query *tgbotapi.CallbackQuery, args []string) (tgbotapi.Chattable, error) {
			meaningID, err := argToInt(args, "failed to get meaning by id")
			if err != nil {
				return nil, err
			}
			resp := h.getReplyText(query.Message, "")

			err = h.showExamples(resp, meaningID)
			if err != nil {
				return nil, err
			}
			return resp, nil
		},
		callbackShowDefinition: func(query *tgbotapi.CallbackQuery, args []string) (tgbotapi.Chattable, error) {
			meaningID, err := argToInt(args, "failed to get meaning by id")
			if err != nil {
				return nil, err
			}
			resp := h.getReplyText(query.Message, "")

			err = h.showDefinition(resp, meaningID)
			if err != nil {
				return nil, err
			}
			return resp, nil
		},
		callbackNextWordsetPage: navigate,
		callbackPrevWordsetPage: navigate,
		callbackSetWordset: func(query *tgbotapi.CallbackQuery, args []string) (tgbotapi.Chattable, error) {
			wordsetID, err := argToInt(args, "failed to get wordset ID from undefined args")
			if err != nil {
				return nil, err
			}
			if len(args) < 2 {
				return nil, errors.New("not enough args")
			}
			wordsetName := strings.Join(args[1:], "")
			resp, err := h.startWordsetSending(query.Message.Chat.ID, wordsetID, wordsetName)
			if err != nil {
				return nil, err
			}
			return resp, nil
		},
	}

	err := h.setupState()
	if err != nil {
		return err
	}
	return nil
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

	var resp tgbotapi.Chattable
	switch {
	case upd.Message != nil:
		resp, err = h.handleActions(upd.Message)
	case upd.CallbackQuery != nil:
		resp, err = h.handleCallback(upd.CallbackQuery)
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

func (h *MessageHandler) getReplyText(m *tgbotapi.Message, txt string) *tgbotapi.MessageConfig {
	resp := tgbotapi.NewMessage(m.Chat.ID, "")
	resp.Text = txt
	return &resp
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

func (h *MessageHandler) handleActions(msg *tgbotapi.Message) (tgbotapi.Chattable, error) {
	logrus.Infof("Message [%+v]", msg)
	words := strings.Split(msg.Text, " ")
	if len(words) == 0 {
		return h.getReplyText(msg, "Unknown command"), nil
	}
	cmd, ok := h.actions[words[0]]
	if !ok {
		return h.getReplyText(msg, "Unknown command"), nil
	}

	resp, err := cmd(msg, words[1:])
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (h *MessageHandler) handleCallback(query *tgbotapi.CallbackQuery) (tgbotapi.Chattable, error) {
	logrus.Infof("Callback [%+v]", query.Data)
	if len(query.Data) == 0 {
		return nil, errors.WithStack(errors.New("failed to execute callback, data is empty"))
	}
	args := strings.Split(query.Data, " ")
	if len(args) <= 1 {
		return nil, errors.WithStack(errors.New("failed to execute callback, args is not sufficient"))
	}
	callback, ok := h.callbacks[args[0]]
	if !ok {
		return h.getReplyText(query.Message, "Unknown callback"), nil
	}

	resp, err := callback(query, args[1:])
	if err != nil {
		return nil, err
	}
	ans, err := h.api.AnswerCallbackQuery(tgbotapi.CallbackConfig{
		CallbackQueryID: query.ID,
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if !ans.Ok {
		return nil, errors.WithStack(errors.New(string(ans.Result)))
	}
	return resp, nil
}

func (h *MessageHandler) startWordsetSending(chatID int64, wordsetID int, wordsetName string) (tgbotapi.Chattable, error) {
	h.state.SetWordsetCallback(func() error {
		resp := tgbotapi.NewMessage(chatID, "")
		err := h.showWords(&resp, wordsetID, wordsetName)
		if err != nil {
			return err
		}
		_, err = h.api.Send(resp)
		if err != nil {
			return errors.WithStack(err)
		}
		return nil
	})
	h.data.WordsetID = wordsetID
	h.data.WordsetName = wordsetName
	h.data.ChatID = chatID
	err := h.storage.WriteData(h.data)
	if err != nil {
		return nil, err
	}
	resp := tgbotapi.NewMessage(chatID, "Wordset changed!")
	return &resp, nil
}

func (h *MessageHandler) startRandomSending(chatID int64) (tgbotapi.Chattable, error) {
	f, err := h.getRandomPeriodicSenderCallback(chatID)
	if err != nil {
		return nil, err
	}
	h.state.SetWordsetCallback(f)
	h.data.Random = true
	h.data.ChatID = chatID
	err = h.storage.WriteData(h.data)
	if err != nil {
		return nil, err
	}
	resp := tgbotapi.NewMessage(chatID, "Random sending started!")
	return &resp, nil
}

func (h *MessageHandler) getRandomPeriodicSenderCallback(chatID int64) (func() error, error) {
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
		wordsetResp := tgbotapi.NewMessage(chatID, "")
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
	resp.Text = "Choose word to show translation and examples."
	resp.ParseMode = tgbotapi.ModeHTML
	buttons, err := h.getWordsMarkup(wordsetID, wordsetName, 0, nil)
	if err != nil {
		return err
	}
	resp.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons...)

	return nil

}

func (h *MessageHandler) getWordsMarkup(wordsetID int, wordsetName string, meaningID int, buttons [][]tgbotapi.InlineKeyboardButton) ([][]tgbotapi.InlineKeyboardButton, error) {
	words, err := h.skyengClient.GetWords(skyeng.Wordset{ID: wordsetID})
	if err != nil {
		return nil, err
	}
	meanings, err := h.skyengClient.GetMeaning(words...)
	if err != nil {
		return nil, err
	}
	var wordsButtons [][]tgbotapi.InlineKeyboardButton
	for _, m := range meanings {
		if m.ID == meaningID {
			wordsButtons = append(wordsButtons, buttons...)
			continue
		}
		wordsButtons = append(wordsButtons, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(m.Text, fmt.Sprintf("%s %d %d %s", callbackGetWord, wordsetID, m.ID, wordsetName)),
		})
	}
	wordsButtons = append(wordsButtons, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(
			"Choose wordset", fmt.Sprintf("%s %d %s", callbackSetWordset, wordsetID, wordsetName),
		),
	})

	return wordsButtons, nil
}

func (h *MessageHandler) showWord(message *tgbotapi.Message, wordsetID int, wordsetName string, meaningID int) (*tgbotapi.EditMessageReplyMarkupConfig, error) {
	meanings, err := h.skyengClient.GetMeaning(skyeng.Word{
		MeaningID: meaningID,
	})
	if err != nil {
		return nil, err
	}
	if len(meanings) == 0 {
		return nil, errors.WithStack(errors.New("failed to get word meaning"))
	}
	builder := strings.Builder{}
	for _, m := range meanings {
		builder.WriteString(fmt.Sprintf("%s [%s] %s", m.Text, m.Transcription, m.Translation))
	}
	var buttons [][]tgbotapi.InlineKeyboardButton
	buttons = append(buttons, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(
			builder.String(), "123",
		),
	})
	buttons = append(buttons, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(
			"Show definition", fmt.Sprintf("%s %d", callbackShowDefinition, meaningID),
		),
		tgbotapi.NewInlineKeyboardButtonData(
			"Show examples", fmt.Sprintf("%s %d", callbackShowExamples, meaningID),
		),
	})
	buttons, err = h.getWordsMarkup(wordsetID, wordsetName, meaningID, buttons)
	if err != nil {
		return nil, err
	}
	resp := tgbotapi.NewEditMessageReplyMarkup(message.Chat.ID, message.MessageID, tgbotapi.NewInlineKeyboardMarkup(buttons...))
	return &resp, nil
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

func (h *MessageHandler) setupState() error {
	data, err := h.storage.GetData()
	if err != nil {
		return err
	}
	h.data = data
	if data.Interval != 0 {
		h.state.ChangeTimeInterval(data.Interval)
	}
	if data.Random {
		_, err = h.startRandomSending(data.ChatID)
		if err != nil {
			return err
		}
		return nil
	} else if data.WordsetID != 0 {
		_, err = h.startWordsetSending(data.ChatID, data.WordsetID, data.WordsetName)
		if err != nil {
			return err
		}
	}
	return nil
}
