package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// A join code is open for joinCodeLife, and closed by joinCodeTries packages it
// does not open.
const (
	joinCodeLife  = 15 * time.Minute
	joinCodeTries = 5
	// joinCodeLength is the length of a join code without its dashes: 160
	// random bits in base32.
	joinCodeLength = 32
)

var (
	errNoJoinCode    = errors.New("no join code is open on this node: open one on its Cluster page")
	errWrongJoinCode = errors.New("the join code does not match the one open on this node")
)

// joinCode is the code a node in no cluster opens to be added to one. The node
// that adds it seals the join package with the code, so that only this node
// reads it, and this node seals its answer with it, so that the adding node
// knows this node read it. One code is open at a time, until it opens a
// package, it expires, or joinCodeTries packages fail to open with it.
type joinCode struct {
	mu      sync.Mutex
	code    string
	expires time.Time
	misses  int
}

// open opens a new join code, closing the one open, and gives it with dashes
// every four characters, and when it expires.
func (j *joinCode) open(now time.Time) (string, time.Time, error) {
	random := make([]byte, joinCodeLength*5/8)
	if _, err := rand.Read(random); err != nil {
		return "", time.Time{}, err
	}
	code := base32.StdEncoding.EncodeToString(random)
	j.mu.Lock()
	defer j.mu.Unlock()
	j.code, j.expires, j.misses = code, now.Add(joinCodeLife), 0
	var shown strings.Builder
	for i := 0; i < len(code); i += 4 {
		if i > 0 {
			shown.WriteByte('-')
		}
		shown.WriteString(code[i : i+4])
	}
	return shown.String(), j.expires, nil
}

// use gives the open code to opens, and closes the code once opens accepts it
// or has refused it joinCodeTries times.
func (j *joinCode) use(now time.Time, opens func(code string) bool) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.code == "" || now.After(j.expires) {
		j.code = ""
		return errNoJoinCode
	}
	if opens(j.code) {
		j.code = ""
		return nil
	}
	if j.misses++; j.misses >= joinCodeTries {
		j.code = ""
	}
	return errWrongJoinCode
}

// normalizeJoinCode is code as it was opened: the dashes, the spaces and the
// case an administrator may type it with are dropped.
func normalizeJoinCode(code string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
}

// validJoinCode reports whether code, normalized, is a join code.
func validJoinCode(code string) bool {
	if len(code) != joinCodeLength {
		return false
	}
	_, err := base32.StdEncoding.DecodeString(code)
	return err == nil
}

// joinKey is the key a join package and its answer are sealed with: derived
// from the join code and the salt the adding node chose.
func joinKey(code string, salt []byte) ([]byte, error) {
	return hkdf.Key(sha256.New, []byte(code), salt, "maxiofs cluster join", 32)
}

// sealJoin seals plain with key: AES-256-GCM, the nonce first.
func sealJoin(key, plain []byte) ([]byte, error) {
	aead, err := joinAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plain, nil), nil
}

// openJoin opens what sealJoin sealed with key.
func openJoin(key, sealed []byte) ([]byte, error) {
	aead, err := joinAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < aead.NonceSize() {
		return nil, fmt.Errorf("sealed too short")
	}
	return aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], nil)
}

func joinAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// handleOpenJoinCode opens a join code on this node, to be added to a cluster
// from the console of one of its nodes. POST /api/v1/cluster/join-code
func (s *Server) handleOpenJoinCode(w http.ResponseWriter, r *http.Request) {
	if currentUser := s.getAuthUser(r); currentUser == nil || !s.isGlobalAdmin(currentUser) {
		s.writeError(w, "Access denied: global admin required", http.StatusForbidden)
		return
	}
	if s.clusterManager.IsClusterEnabled() {
		s.writeError(w, "This node is already in a cluster", http.StatusConflict)
		return
	}
	code, expires, err := s.joinCode.open(time.Now())
	if err != nil {
		s.writeError(w, "Failed to open a join code: "+err.Error(), http.StatusInternalServerError)
		return
	}
	logrus.WithField("expires", expires.Format(time.RFC3339)).Info("Join code opened")
	s.writeJSON(w, map[string]interface{}{"join_code": code, "expires_at": expires.Unix()})
}
