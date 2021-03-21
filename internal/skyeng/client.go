package skyeng

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	errs "github.com/pkg/errors"
)

const (
	// maxHttpRetries represents max count of retries.
	maxHttpRetries = 3
)

type Wordset struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type WordsetsData struct {
	Data []Wordset `json:"data"`
}

type Word struct {
	ID        int `json:"id"`
	MeaningID int `json:"meaningId"`
}

type WordsData struct {
	Data []Word `json:"data"`
}

type Translation struct {
	Text string `json:"text"`
}

type Transcription struct {
	Text string `json:"text"`
}

type Example struct {
	Text string `json:"text"`
}

type Definition struct {
	Text string `json:"text"`
}

type Meaning struct {
	ID            int         `json:"id"`
	MeaningID     int         `json:"meaningId"`
	Text          string      `json:"text"`
	Translation   Translation `json:"translation"`
	Definition    Definition  `json:"definition"`
	Examples      []Example   `json:"examples"`
	Transcription string      `json:"transcription"`
}

var ErrUnauthorized = errors.New("unauthorized")
var ErrWordsetNotFound = errors.New("wordset not found")
var ErrMeaningNotFound = errors.New("meaning not found")

type Client interface {
	GetWordsets(page int) ([]Wordset, error)
	GetWords(ws Wordset) ([]Word, error)
	GetMeaning(w ...Word) ([]Meaning, error)
}

func NewClient(username string, password string) Client {
	return &client{
		authEndpoint:  "https://id.skyeng.ru",
		wordsEndpoint: "https://api.words.skyeng.ru/api",
		dictEndpoint:  "https://dictionary.skyeng.ru/api",
		username:      username,
		password:      password,
		client:        &http.Client{},
	}
}

type client struct {
	authEndpoint  string
	wordsEndpoint string
	dictEndpoint  string
	username      string
	password      string
	client        *http.Client
	token         string
}

const resultMaxPageSize = 100

func (c *client) GetWordsets(page int) ([]Wordset, error) {
	var wordsetsData WordsetsData
	var pageSuff string
	if page == 0 {
		pageSuff = fmt.Sprintf("?pageSize=%d&page=1", resultMaxPageSize)
	} else {
		pageSuff = fmt.Sprintf("?page=%d", page)
	}
	wordsetsURL := fmt.Sprintf("%s/for-vimbox/v1/wordsets.json%s", c.wordsEndpoint, pageSuff)
	err := c.invoke("GET", wordsetsURL, nil, func(resp []byte) error {
		err := json.Unmarshal(resp, &wordsetsData)
		if err != nil {
			return errs.WithStack(err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return wordsetsData.Data, nil
}

func (c *client) GetWords(ws Wordset) ([]Word, error) {
	var words WordsData
	wordsURL := fmt.Sprintf(c.wordsEndpoint+"/v1/wordsets/%d/words.json?pageSize=100&page=1", ws.ID)

	err := c.invoke("GET", wordsURL, nil, func(resp []byte) error {
		err := json.Unmarshal(resp, &words)
		if err != nil {
			return errs.WithStack(err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return words.Data, nil
}

func (c *client) GetMeaning(words ...Word) ([]Meaning, error) {
	var meanings []Meaning
	var m []string
	for _, word := range words {
		m = append(m, strconv.Itoa(word.MeaningID))
	}
	meaningIDs := strings.Join(m, ",")
	wordsURL := fmt.Sprintf(c.dictEndpoint+"/for-services/v2/meanings?ids=%s", meaningIDs)

	err := c.invoke("GET", wordsURL, nil, func(resp []byte) error {
		err := json.Unmarshal(resp, &meanings)
		if err != nil {
			return errs.WithStack(err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(meanings) == 0 {
		return nil, errs.Wrapf(ErrMeaningNotFound, "meaningID: %s", meaningIDs)
	}
	return meanings, nil
}

func (c *client) invoke(method string, URL string, body []byte, f func(resp []byte) error) error {
	for i := 0; i < maxHttpRetries; i++ {
		var respBody []byte
		err := func() error {
			req, err := http.NewRequest(method, URL, bytes.NewBuffer(body))
			if err != nil {
				return errs.WithStack(err)
			}
			req.Header = http.Header{
				"authorization": []string{"Bearer " + c.token},
			}

			resp, err := c.client.Do(req)

			if err != nil {
				return errs.WithStack(err)
			}
			if resp.StatusCode == http.StatusUnauthorized {
				return errs.WithStack(ErrUnauthorized)
			}

			defer resp.Body.Close()
			respBody, err = ioutil.ReadAll(resp.Body)
			if err != nil {
				return errs.WithStack(err)
			}

			return nil
		}()
		if err != nil {
			if errors.Is(err, ErrUnauthorized) {
				c.token, err = c.auth()
				if err != nil {
					return err
				}
				continue
			}

			return err
		}
		err = f(respBody)
		if err != nil {
			return err
		}
		break
	}

	return nil
}

func (c *client) auth() (string, error) {
	resp, err := http.Get(c.authEndpoint + "/en/frame/login")
	if err != nil {
		return "", errs.WithStack(err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)

	cmpl, err := regexp.Compile("name=\"csrfToken\" value=\"([^\"]+)\"")
	if err != nil {
		return "", errs.WithStack(err)
	}
	submch := cmpl.FindSubmatch(body)
	if len(submch) != 2 {
		return "", errs.WithStack(errors.New("failed to get csrfToken, failed to parse html"))
	}
	csrfToken := string(submch[1])

	sessionGlbl, err := getSessionGlobal(resp.Cookies())

	loginUrl := c.authEndpoint + "/en/frame/login-submit"
	req, err := http.NewRequest("POST", loginUrl,
		strings.NewReader(url.Values{"csrfToken": {csrfToken}, "username": {c.username}, "password": {c.password}}.Encode()))
	if err != nil {
		return "", errs.WithStack(err)
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{
		Name:  "session_global",
		Value: sessionGlbl,
	})
	resp, err = c.client.Do(req)

	if err != nil {
		return "", errs.WithStack(err)
	}
	sessionGlbl, err = getSessionGlobal(resp.Cookies())

	jwtUrl := c.authEndpoint + "/user-api/v1/auth/jwt"
	req, err = http.NewRequest("POST", jwtUrl, nil)
	if err != nil {
		return "", errs.WithStack(err)
	}
	req.AddCookie(&http.Cookie{
		Name:  "session_global",
		Value: sessionGlbl,
	})
	resp, err = c.client.Do(req)

	if err != nil {
		return "", errs.WithStack(err)
	}
	token, err := getAccessToken(resp.Cookies())
	if err != nil {
		return "", err
	}

	return token, nil
}

func getSessionGlobal(cookies []*http.Cookie) (string, error) {
	var sessionGlbl string
	for _, cc := range cookies {
		if cc.Name == "session_global" {
			sessionGlbl = cc.Value
		}
	}
	if sessionGlbl == "" {
		return "", errs.WithStack(errors.New("failed to get session_global cookie"))
	}
	return sessionGlbl, nil
}

func getAccessToken(cookies []*http.Cookie) (string, error) {
	var token string
	for _, cc := range cookies {
		if cc.Name == "token_global" {
			token = cc.Value
		}
	}
	if token == "" {
		return "", errs.WithStack(errors.New("failed to get token cookie"))
	}
	return token, nil
}
