package pushover

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"io/ioutil"
	"net/http"
)

// Client represents Pushover interface.
type Client interface {
	SendPush(title string, message string) error
}

// NewClient returns Client compatible functionality.
func NewClient(token string, user string, device string) Client {
	return &client{
		endpoint: "https://api.pushover.net",
		token:    token,
		user:     user,
		device:   device,
	}
}

type client struct {
	endpoint string
	token    string
	user     string
	device   string
}

// SendPush sends push to Pushover.
func (c client) SendPush(title string, message string) error {
	msg := map[string]interface{}{
		"token":   c.token,
		"user":    c.user,
		"device":  c.device,
		"title":   title,
		"message": message,
	}

	b, err := json.Marshal(msg)
	if err != nil {
		return errors.WithStack(err)
	}

	resp, err := http.Post(c.endpoint+"/1/messages.json", "application/json", bytes.NewBuffer(b))
	if err != nil {
		return errors.WithStack(err)
	}
	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return errors.WithStack(fmt.Errorf("failed to send push message, got status %d, message %s", resp.StatusCode, respBody))
	}
	logrus.Info("Push sent: " + message)
	return nil
}

// MockPushover is fake Pushover client
type MockPushover struct{}

// SendPush is fake pushover method.
func (t MockPushover) SendPush(title string, message string) error {
	fmt.Printf("Push sended: title %q, message %q \n", title, message)
	return nil
}
