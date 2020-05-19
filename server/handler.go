package server

import (
	"encoding/json"
	"errors"
	"github.com/pachmu/skyeng-push-notificator/internal/skyeng"
	log "github.com/sirupsen/logrus"
	"net/http"
)

type handler struct {
	skyengClient  skyeng.Client
	changeWordset chan int
	user          string
	stop          chan struct{}
}

func (h *handler) getWordsets(w http.ResponseWriter, req *http.Request) {
	if !h.auth(w, req) {
		return
	}
	wordsets, err := h.skyengClient.GetWordsets()
	if err != nil {
		log.Error("failed to get wordsets from skyeng, got ", err)
		w.WriteHeader(http.StatusInternalServerError)

		return
	}
	err = json.NewEncoder(w).Encode(wordsets)
	if err != nil {
		log.Error("failed to encode wordsets, got ", err)
		w.WriteHeader(http.StatusInternalServerError)

		return
	}
}

func (h *handler) setWordset(w http.ResponseWriter, req *http.Request) {
	if !h.auth(w, req) {
		return
	}
	var wordset map[string]int
	err := json.NewDecoder(req.Body).Decode(&wordset)
	ID, ok := wordset["ID"]
	if !ok {
		log.Error("failed to get ID from request, got ", err)
		w.WriteHeader(http.StatusBadRequest)

		return
	}
	_, err = h.skyengClient.GetWords(
		skyeng.Wordset{
			ID: ID,
		},
	)
	if err != nil {
		log.Errorf("failed to get words for wordset %d, got %v", ID, err)
		if errors.Is(err, skyeng.ErrWordsetNotFound) {
			w.WriteHeader(http.StatusNotFound)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}

		return
	}
	h.changeWordset <- ID
}

func (h *handler) stopSending(w http.ResponseWriter, req *http.Request) {
	if !h.auth(w, req) {
		return
	}
	h.stop <- struct{}{}
	log.Info("Sending stopped")
}

func (h *handler) auth(w http.ResponseWriter, req *http.Request) bool {
	auth := req.Header.Get("authorization")
	if auth != h.user {
		log.Error("authorization failed, user:", auth)
		w.WriteHeader(http.StatusUnauthorized)

		return false
	}

	return true
}
