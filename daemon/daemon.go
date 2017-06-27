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
)

// Options contains configuration options for the daemon spawn.
type Options struct {
	Meter  *metering.Meter
	Ports  []int
	StdOut chan []byte
	StdErr chan []byte
	Done   chan bool
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
		"token":   token.Raw,
		"tokenid": fmt.Sprintf("%v", claims["jti"]),
	}

	done := make(chan bool, 5) // does not need to be synchronized
	c, err := container.RunDaemonized(uname, image, "latest", ports, labels, options.StdOut, options.StdErr, done)
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
			case <-time.After(1000 * time.Millisecond):
				ms := (time.Now().UnixNano() - t0) / 1e6
				if err := options.Meter.RecordMilliseconds(int(ms)); err != nil {
					log.WithError(err).Warn("Could not record time spent - kill and exit")
					if err := container.Kill(uname, false, false); err != nil {
						log.WithError(err).Warn("Error killing container")
					}
					return
				}
			case <-done:
				return
			}
		}
	}()

	return &Info{Address: c.NetworkSettings.IPAddress, Name: uname, Ports: invertedPorts}, nil
}

// List the daemons running on this token.
func List(token *jwt.Token) ([]docker.APIContainers, error) {
	return container.List(nil, container.WithLabel("token", token.Raw))
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

	time, ok := claims["tims"].(float64)
	if !ok {
		http.Error(w, "Could not extract TimeInMilliseconds from token", 500)
		return
	}

	// Construct meter
	m, err := metering.NewMeter(claims["jti"].(string), int(time))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Check if meter has resources before spawning
	if ms, err := m.MillisecondsRemaining(); err != nil || ms <= 0 {
		http.Error(w, "Not even running on fumes", 402 /* Payment required */)
		return
	}

	done := make(chan bool)

	go func() {
		<-done
		log.WithField("name", name).Info("Daemon is now done")
	}()

	options := Options{
		Meter:  m,
		Ports:  ports,
		StdOut: nil,
		StdErr: nil,
		Done:   done,
	}

	// TODO support content in similar fashion to labdaed minions
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
