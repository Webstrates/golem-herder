package token

import (
	"crypto/rsa"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	jwt "github.com/dgrijalva/jwt-go"
	"github.com/rs/xid"
)

type Validator interface {
	Validate(token string) (*jwt.Token, error)
}

type Generator interface {
	Generate(subject string, claims jwt.MapClaims) (*jwt.Token, error)
}

func NewManager(pub, priv string) (*Manager, error) {
	pubKey, err := publicKey(pub)
	if err != nil {
		return nil, err
	}
	privKey, err := privateKey(priv)
	if err != nil {
		return nil, err
	}
	return &Manager{pubKey: pubKey, privKey: privKey}, nil
}

// Manager validates and generates new tokens
type Manager struct {
	pubKey  *rsa.PublicKey
	privKey *rsa.PrivateKey
}

// publicKey returns an rsa.PublicKey given a path to a pem file.
func publicKey(file string) (*rsa.PublicKey, error) {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("Error reading the jwt public key: %v", err)
	}
	publickey, err := jwt.ParseRSAPublicKeyFromPEM(data)
	if err != nil {
		return nil, fmt.Errorf("Error parsing the jwt public key: %s", err)
	}
	return publickey, nil
}

// privateKey returns an rsa.PrivateKey given a path to a pem file.
func privateKey(file string) (*rsa.PrivateKey, error) {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("Error reading the jwt private key: %v", err)
	}
	privatekey, err := jwt.ParseRSAPrivateKeyFromPEM(data)
	if err != nil {
		return nil, fmt.Errorf("Error parsing the jwt private key: %s", err)
	}
	return privatekey, nil
}

// Validate should be called to validate a jwt token.
func (tm *Manager) Validate(token string) (*jwt.Token, error) {
	jwtToken, err := jwt.ParseWithClaims(token, jwt.MapClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			log.WithField("alg", t.Header["alg"]).Warn("Unexpected signing method.")
			return nil, fmt.Errorf("Invalid token")
		}
		return tm.pubKey, nil
	})
	if err == nil && jwtToken.Valid {
		return jwtToken, nil
	}
	return nil, err
}

func (tm *Manager) Generate(subject string, claims jwt.MapClaims) (string, error) {
	guid := xid.New()
	coreClaims := jwt.MapClaims{
		"exp": time.Now().Add(time.Hour * 24100).Unix(),
		"iss": "au/webstrates",
		"iat": time.Now().Unix(),
		"sub": subject,
		"jti": guid.String(),
		"app": "golem-herder"}

	// Override given claims with core
	for k, v := range coreClaims {
		claims[k] = v
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)

	tokenString, err := token.SignedString(tm.privKey)
	if err != nil {
		log.WithError(err).Warn("Could not sign JWT with private key.")
		return "", err
	}

	return tokenString, nil
}

func tokenFromHeader(r *http.Request) (string, bool) {
	// Extract "Authorization: Bearer <token>" header
	bearer := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(bearer), "bearer") && len(bearer) > 6 {
		return bearer[7:], true
	}
	return "", false
}

func tokenFromQueryParam(r *http.Request) (string, bool) {
	token := r.URL.Query().Get("token")
	if token != "" {
		return token, true
	}
	return "", false
}

// ValidatedHandler will return a http handler which validates a request prior to invoking the given handler.
func ValidatedHandler(validator Validator, handler func(w http.ResponseWriter, r *http.Request, token *jwt.Token)) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		token, ok := tokenFromHeader(r)
		if !ok {
			token, _ = tokenFromQueryParam(r)
			log.Info("Got token from query")
		} else {
			log.Info("Got token from header")
		}

		t, err := validator.Validate(token)
		if err != nil {
			log.WithError(err).Warn("Unauthorized")
			http.Error(w, err.Error(), 401 /* Unauthorized */)
			return
		}
		if t == nil {
			log.Warn("Token was invalid")
			http.Error(w, "Token was invalid", 401)
			return
		}
		handler(w, r, t)
	}
}
