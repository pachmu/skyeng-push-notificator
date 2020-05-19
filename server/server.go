package server

import (
	"fmt"
	"github.com/pkg/errors"
	"net/http"

	"github.com/pachmu/skyeng-push-notificator/internal/skyeng"
)

type Server struct {
	Addr string
	Port int
}

func (s *Server) Serve(user string, client skyeng.Client, changeWordset chan int, stop chan struct{}) error {
	h := handler{
		skyengClient:  client,
		changeWordset: changeWordset,
		stop:          stop,
		user:          user,
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
