package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/Webstrates/golem-herder/container"
	"github.com/Webstrates/golem-herder/metering"
	jwt "github.com/dgrijalva/jwt-go"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/gorilla/mux"
)

// Options contains configuration options for the daemon spawn.
type Options struct {
	Meter   *metering.Meter
	Restart bool
	Ports   []int
	StdOut  chan []byte
	StdErr  chan []byte
	Done    chan bool
}

// Info is information about a running deamon.
type Info struct {
	Name    string
	Address string
	Ports   map[int]int
}

// Spawn a daemon with the given options.
func Spawn(token *jwt.Token, name, image string, options Options) (*Info, error) {

	// Construct a new unique (for this token) id from name and token id
	// - we'll assume that token has already been validated
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("Could not read from token claims")
	}

	// TODO change this if you want to e.g. restrict to one container of each kind
	uname := fmt.Sprintf("%s-%v", name, claims["jti"])

	// Get random outside ports
	ports := map[int]int{}
	invertedPorts := map[int]int{}
	for _, insidePort := range options.Ports {
		outsidePort := container.GetAvailableHostPort()
		ports[outsidePort] = insidePort
		invertedPorts[insidePort] = outsidePort
	}

	// Labels for container
	labels := map[string]string{
		"subject": claims["sub"].(string),
		"token":   token.Raw,
		"tokenid": fmt.Sprintf("%v", claims["jti"]),
	}

	done := make(chan bool, 5) // does not need to be synchronized
	c, err := container.RunDaemonized(uname, image, "latest", ports, labels, options.Restart, options.StdOut, options.StdErr, done)
	if err != nil {
		return nil, err
	}

	go func() {
		// Let someone else now that we're done
		defer func() {
			options.Done <- true
		}()
		for {
			t0 := time.Now().UnixNano()
			select {
			case <-time.After(time.Second):
				ms := (time.Now().UnixNano() - t0) / 1e9
				if err := options.Meter.Record(int(ms)); err != nil {
					log.WithError(err).Warn("Could not record time spent - kill and exit")
					if err := container.Kill(container.WithName(uname), false, false); err != nil {
						log.WithError(err).Warn("Error killing container")
					}
					return
				}
			case <-done:
				return
			}
		}
	}()

	return &Info{Address: c.NetworkSettings.Networks["bridge"].IPAddress, Name: uname, Ports: invertedPorts}, nil
}

// List the daemons running on this token.
func List(token *jwt.Token) ([]docker.APIContainers, error) {
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("Could extract claims from token")
	}
	return container.List(nil, container.WithLabel("subject", claims["sub"].(string)))
}

// SpawnHandler handles spawn requests
func SpawnHandler(w http.ResponseWriter, r *http.Request, token *jwt.Token) {
	// Read name, image
	name := r.FormValue("name")
	image := r.FormValue("image")
	ps := r.FormValue("ports")
	var ports []int
	if err := json.Unmarshal([]byte(ps), &ports); err != nil {
		http.Error(w, "Could not unmarshal ports - "+err.Error(), 400)
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		http.Error(w, "Could extract claims from token", 500)
		return
	}

	crd, ok := claims["crd"].(float64)
	if !ok {
		http.Error(w, "Could not extract \"crd\" (credits) from token", 500)
		return
	}

	exp, ok := claims["exp"].(float64)
	if !ok {
		http.Error(w, "Could not extract \"exp\" (expiration) from token", 500)
		return
	}

	// Construct meter - one meter pr sub(ject) aka email
	subject := claims["sub"].(string)
	tokenID := claims["jti"].(string)
	credits := int(crd)
	expiration := int(exp)

	m, err := metering.NewMeter(subject, tokenID, expiration, credits)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Check if meter has resources before spawning
	if credits, err := m.Credits(); err != nil || credits <= 0 {
		http.Error(w, "Not even running on fumes", 402 /* Payment required */)
		return
	}

	done := make(chan bool)

	go func() {
		<-done
		log.WithField("name", name).Info("Daemon is now done")
	}()

	options := Options{
		Meter:   m,
		Restart: true,
		Ports:   ports,
		StdOut:  nil,
		StdErr:  nil,
		Done:    done,
	}

	// TODO support content in similar fashion to lambdaed minions
	info, err := Spawn(token, name, image, options)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	s, err := json.Marshal(info)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Write(s)
}

// Kill a container with the given name iff it is owned by the owner of the token
func Kill(name string, wipe bool, token *jwt.Token) error {
	// Check of subject label is the same as in the token
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return fmt.Errorf("Could extract claims from token")
	}
	subject := claims["subject"].(string)
	containers, err := container.List(nil, container.And(container.WithName(name), container.WithLabel("subject", subject)))
	if err != nil {
		return err
	}
	if len(containers) != 1 {
		return fmt.Errorf("Could not find container to kill")
	}
	return container.Kill(container.WithID(containers[0].ID), wipe, wipe)
}

// ListHandler handles list requests
func ListHandler(w http.ResponseWriter, r *http.Request, token *jwt.Token) {
	containers, err := List(token)
	if err != nil {
		http.Error(w, err.Error(), 500)
	}

	s, err := json.Marshal(containers)
	if err != nil {
		http.Error(w, err.Error(), 500)
	}

	w.Write(s)
}

// KillHandler handles kill requests
func KillHandler(w http.ResponseWriter, r *http.Request, token *jwt.Token) {
	vars := mux.Vars(r)
	name, ok := vars["name"]
	if !ok {
		http.Error(w, "No name given", 404)
		return
	}
	err := Kill(name, r.URL.Query().Get("wipe") == "true", token)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

}
