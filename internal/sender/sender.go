package sender

import (
	"fmt"
	"github.com/pachmu/skyeng-push-notificator/internal/pushover"
	"github.com/pachmu/skyeng-push-notificator/internal/skyeng"
	"math/rand"
	"strings"
	"time"
)

type Sender struct {
	Skyeng        skyeng.Client
	Pushover      pushover.Client
	TimeInterval  time.Duration
	ChangeWordset chan int
	Suspend       chan struct{}
	Terminate     chan struct{}
}

func (s *Sender) Send() error {
	wordsets, err := s.Skyeng.GetWordsets()
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
	ticker := time.NewTicker(s.TimeInterval)
	run := true
	suspended := false
	for run {
		var wordset skyeng.Wordset
		var ok bool
		if wordset, ok = wordsetsMap[wordsetID]; !ok {
			return fmt.Errorf("failed to get wordset, wrong ID %q", wordsetID)
		}
		words, err := s.Skyeng.GetWords(wordset)
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
				meaning, err := s.Skyeng.GetMeaning(w)
				if err != nil {
					return err
				}
				err = s.Pushover.SendPush(wordset.Title, meaning.Text+
					strings.Repeat(" ", 5)+meaning.Transcription+
					strings.Repeat(" ", 40)+meaning.Translation.Text)
				if err != nil {
					return err
				}
			}

			select {
			case wordsetID = <-s.ChangeWordset:
				ticker.Stop()
				ticker = time.NewTicker(s.TimeInterval)
				break loop
			case <-ticker.C:
				continue
			case <-s.Suspend:
				ticker.Stop()
				suspended = true
				continue
			case <-s.Terminate:
				ticker.Stop()
				run = false
				break loop
			}
		}
	}
	return nil
}
