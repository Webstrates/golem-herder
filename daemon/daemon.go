package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/Webstrates/golem-herder/container"
	"github.com/Webstrates/golem-herder/metering"
	jwt "github.com/dgrijalva/jwt-go"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

var (
	// upgrader upgrades HTTP 1.1 connection to WebSocket
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(*http.Request) bool { return true }, // allow all origins
	}
)

// Options contains configuration options for the daemon spawn.
type Options struct {
	Meter   *metering.Meter
	Restart bool
	Ports   []int
	Files   map[string][]byte
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

	// Change the unique name generation if you want to e.g. restrict to one container of each kind
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
	c, err := container.RunDaemonized(uname, image, "latest", ports, options.Files, labels, options.Restart, options.StdOut, options.StdErr, done)
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

// Attach will attach to an already running daemon and forward stdout/err and allow for stdin
func Attach(token *jwt.Token, name string, in <-chan []byte, out chan<- []byte) error {
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return fmt.Errorf("Could not extract claims from token")
	}
	cs, err := container.List(nil, container.WithLabel("subject", claims["sub"].(string)), true)
	if err != nil {
		return err
	}
	if len(cs) == 0 {
		return fmt.Errorf("Could not find container with name: %s", name)
	}
	c := cs[0]

	// io, io, io
	return container.Attach(c, out, out, in)
}

// AttachHandler handles attach requests
func AttachHandler(w http.ResponseWriter, r *http.Request, token *jwt.Token) {
	vars := mux.Vars(r)
	name, ok := vars["name"]
	if !ok {
		log.Warn("No name given")
		http.Error(w, "No name given", 404)
		return
	}
	log.WithField("name", name).Info("Attaching")
	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.WithError(err).Panic("Error upgrading connection")
		return
	}

	// Connect websocket and channel
	in := make(chan []byte)
	out := make(chan []byte)

	// -- from websocket -> stdin
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				log.WithError(err).WithField("container", name).Warn("Error reading from stdin-websocket")
				close(in)
				return
			}
			in <- data
		}
	}()

	// -- from stdout/err -> websocket
	go func() {
		for line := range out {
			err := conn.WriteMessage(websocket.TextMessage, line)
			if err != nil {
				log.WithError(err).WithField("container", name).Warn("Error writing to stdout-websocket")
				return
			}
		}
	}()

	if err := Attach(token, name, in, out); err != nil {
		log.WithError(err).Warn("Could not attach to %s", name)
		conn.Close()
	}
}

// List the daemons running on this token.
func List(token *jwt.Token) ([]docker.APIContainers, error) {
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("Could not extract claims from token")
	}
	return container.List(nil, container.WithLabel("subject", claims["sub"].(string)), true)
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

	// Consider rest of form values as files
	files := map[string][]byte{}
	for key, values := range r.Form {
		if key != "name" && key != "image" && key != "ports" && key != "token" && len(values) > 0 {
			files[key] = []byte(values[0])
		}
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
		Files:   files,
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
	subject := claims["sub"].(string)
	containers, err := container.List(nil, container.And(container.WithName(name), container.WithLabel("subject", subject)), false)
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
