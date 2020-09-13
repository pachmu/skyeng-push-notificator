package server

import (
	"fmt"
	"net/http"

	"github.com/pachmu/skyeng-push-notificator/internal/sender"
	"github.com/pachmu/skyeng-push-notificator/internal/skyeng"
	"github.com/pkg/errors"
)

type Server struct {
	Addr string
	Port int
}

func (s *Server) Serve(user string, client skyeng.Client, sender *sender.Sender) error {
	h := handler{
		skyengClient: client,
		sender:       sender,
		user:         user,
	}
	http.HandleFunc("/get_wordsets", h.getWordsets)
	http.HandleFunc("/set_wordset", h.setWordset)
	http.HandleFunc("/stop_sending", h.stopSending)

	err := http.ListenAndServe(fmt.Sprintf("%s:%d", s.Addr, s.Port), nil)
	if err != nil {
		return errors.WithStack(err)
	}
	return nil
}
