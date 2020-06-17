package main

import (
	"crypto/md5"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strconv"
	"time"
)

const (
	chars                    = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	maxNonceInactiveInterval = 12 * time.Hour
)

type NonceInfo struct {
	issued           time.Time
	lastUsed         time.Time
	lastNonceCounter uint64
}

type DigestAuth struct {
	users map[string]string
	// issued nonce values
	nonces map[string](*NonceInfo)
}

type DigestAuthData struct {
	user     string
	realm    string
	nonce    string
	method   string
	uri      string
	response string
	qop      string
	nc       string
	cnonce   string
}

func makeRandomString(l int) string {
	b := make([]byte, l)
	for i := 0; i < l; i++ {
		b[i] = chars[rand.Intn(len(chars))]
	}

	return string(b)
}

func newDigestAuthFromFile(path string) (*DigestAuth, error) {
	r, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	return newDigestAuth(r)
}

func newDigestAuth(file io.Reader) (*DigestAuth, error) {
	csvReader := csv.NewReader(file)
	csvReader.Comma = ':'
	csvReader.Comment = '#'
	csvReader.TrimLeadingSpace = true

	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, err
	}

	h := &DigestAuth{users: make(map[string]string), nonces: make(map[string](*NonceInfo))}

	for _, record := range records {
		// each record has to be in form: "user:realm:md5hash"
		if len(record) != 3 {
			return nil, errors.New("invalid htdigest file format")
		}
		key := record[0] + ":" + record[1]
		value := record[2]
		h.users[key] = value
	}

	rand.Seed(time.Now().UnixNano())

	return h, nil
}

func (h *DigestAuth) validate(data *DigestAuthData) bool {
	lookupKey := data.user + ":" + data.realm
	ha1, exists := h.users[lookupKey]
	if !exists {
		return false
	}

	nonceInfo, nonceExists := h.nonces[data.nonce]
	if !nonceExists {
		return false
	}

	nc, err := strconv.ParseUint(data.nc, 16, 64)
	if err != nil {
		return false
	}

	// reply attack ?
	if nc == nonceInfo.lastNonceCounter {
		return false
	}

	s := data.method + ":" + data.uri
	ha2 := fmt.Sprintf("%x", md5.Sum([]byte(s)))
	s = ha1 + ":" + data.nonce + ":" + data.nc + ":" + data.cnonce + ":" + data.qop + ":" + ha2
	realResponse := fmt.Sprintf("%x", md5.Sum([]byte(s)))

	if data.response == realResponse {
		nonceInfo.lastUsed = time.Now()
		nonceInfo.lastNonceCounter = nc
		return true
	}

	return false
}

func (h *DigestAuth) newNonce() string {
	var nonce string

	for {
		rs := makeRandomString(100)
		nonce = fmt.Sprintf("%x", md5.Sum([]byte(rs)))
		_, exists := h.nonces[nonce]
		if !exists {
			h.addNonce(nonce)
			break
		}
	}

	return nonce
}

func (h *DigestAuth) addNonce(nonce string) {
	h.nonces[nonce] = &NonceInfo{
		issued:           time.Now(),
		lastUsed:         time.Now(),
		lastNonceCounter: 0,
	}
}

func (h *DigestAuth) expireNonces() {
	currentTime := time.Now()
	limit := currentTime.Add(-maxNonceInactiveInterval)
	for key, value := range h.nonces {
		if value.lastUsed.Before(limit) {
			delete(h.nonces, key)
		}
	}
}
