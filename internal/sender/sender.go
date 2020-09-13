package sender

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/pachmu/skyeng-push-notificator/internal/pushover"
	"github.com/pachmu/skyeng-push-notificator/internal/skyeng"
)

// NewSender returns Sender struct.
func NewSender(skyeng skyeng.Client, pushover pushover.Client, terminate chan struct{}, timeInterval time.Duration) *Sender {
	return &Sender{
		skyeng:         skyeng,
		pushover:       pushover,
		timeInterval:   timeInterval,
		changeWordset:  make(chan int),
		changeInterval: make(chan time.Duration),
		suspend:        make(chan struct{}),
		terminate:      terminate,
	}
}

// Sender represents logic for operating push messages.
type Sender struct {
	skyeng         skyeng.Client
	pushover       pushover.Client
	timeInterval   time.Duration
	changeWordset  chan int
	changeInterval chan time.Duration
	suspend        chan struct{}
	terminate      chan struct{}
}

// Send executes main application logic.
func (s *Sender) Send() error {
	wordsets, err := s.skyeng.GetWordsets(0)
	if err != nil {
		return err
	}
	wordsetsMap := make(map[int]skyeng.Wordset, len(wordsets))
	for _, w := range wordsets {
		wordsetsMap[w.ID] = w
	}

	rand.Seed(time.Now().Unix())
	getRandWordsetID := func() int {
		num := rand.Intn(len(wordsets))
		return wordsets[num].ID
	}
	wordsetID := getRandWordsetID()
	ticker := time.NewTicker(s.timeInterval)
	run := true
	suspended := false
	for run {
		var wordset skyeng.Wordset
		var ok bool
		if wordset, ok = wordsetsMap[wordsetID]; !ok {
			return fmt.Errorf("failed to get wordset, wrong ID %q", wordsetID)
		}
		words, err := s.skyeng.GetWords(wordset)
		if err != nil {
			return err
		}

		if len(words) == 0 {
			wordsetID = getRandWordsetID()
			time.Sleep(time.Second)
			continue
		}
		rand.Shuffle(len(words), func(i, j int) {
			words[i], words[j] = words[j], words[i]
		})
	loop:
		for _, w := range words {
			if !suspended {
				meanings, err := s.skyeng.GetMeaning(w)
				if err != nil {
					return err
				}
				m := meanings[0]
				err = s.pushover.SendPush(wordset.Title, m.Text+
					strings.Repeat(" ", 5)+m.Transcription+
					strings.Repeat(" ", 40)+m.Translation.Text)
				if err != nil {
					return err
				}
			}

			select {
			case wordsetID = <-s.changeWordset:
				ticker.Stop()
				ticker = time.NewTicker(s.timeInterval)
				break loop
			case s.timeInterval = <-s.changeInterval:
				ticker.Stop()
				ticker = time.NewTicker(s.timeInterval)
				break loop
			case <-ticker.C:
				continue
			case <-s.suspend:
				ticker.Stop()
				suspended = true
				continue
			case <-s.terminate:
				ticker.Stop()
				run = false
				break loop
			}
		}
	}
	return nil
}

// ChangeCurrentWordset changes sending wordset.
func (s *Sender) ChangeCurrentWordset(wordsetID int) {
	s.changeWordset <- wordsetID
}

// ChangeTimeInterval changes sending interval.
func (s *Sender) ChangeTimeInterval(interval time.Duration) {
	s.changeInterval <- interval
}

// SuspendWork suspending sender.
func (s *Sender) SuspendWork() {
	s.suspend <- struct{}{}
}
