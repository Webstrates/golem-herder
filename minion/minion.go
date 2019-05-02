package minion

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"mime"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/Webstrates/golem-herder/container"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/rs/xid"
)

// List all connected minions
var (
	port    int
	golems  = map[string]*Golem{}
	minions = map[string]map[string]*Minion{}

	mutex = &sync.Mutex{}
	// upgrader upgrades HTTP 1.1 connection to WebSocket
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(*http.Request) bool { return true }, // allow all origins
	}
)

// Golem represents a connected golem
type Golem struct {
	// in is chan to send messages to the golem
	to   chan Message
	done chan bool
}

// Minion represents a connected minion
type Minion struct {
	ID string
	// in is a chan w messages to the minion
	to   chan Message
	from chan Message
	done chan bool
}

// Message is a websocket message
type Message struct {
	Type    int
	Content []byte
}

// ConnectEvent is an event for connects
type ConnectEvent struct {
	Event string
	ID    string `json:",omitempty"`
	Type  string `json:",omitempty"`
}

// Output is the default information returned from a minion lambda execution
type Output struct {
	StdOut string
	StdErr string `json:",omitempty"`
}

// NewGolemNotFound creates and returns a ConnectEvent for a connected minion
func NewGolemNotFound(id string) ConnectEvent {
	return ConnectEvent{
		Event: "golem-not-found",
		ID:    id}
}

// NewMinionConnected creates and returns a ConnectEvent for a connected minion
func NewMinionConnected(id string, t string) ConnectEvent {
	return ConnectEvent{
		Event: "minion-connected",
		ID:    id,
		Type:  t}
}

// NewMinionDisconnected creates and returns a ConnectEvent for a disconnected minion
func NewMinionDisconnected(id string) ConnectEvent {
	return ConnectEvent{
		Event: "minion-disconnected",
		ID:    id}
}

// NewGolemDisconnected creates and returns a ConnectEvent for disconnected golem
func NewGolemDisconnected() ConnectEvent {
	return ConnectEvent{
		Event: "golem-disconnected"}
}

// NewGolemConnected creates and returns a ConnectEvent for connected golem
func NewGolemConnected() ConnectEvent {
	return ConnectEvent{
		Event: "golem-connected"}
}

// Spawn will spawn a new minion given
// * env - environment (Webstrates/<env> image to use)
// * files - a map of filename -> content of files to write
func Spawn(env, output string, files map[string][]byte) ([]byte, string, error) {
	// create a local environment for the container (will get mounted as a volume)
	dir, err := ioutil.TempDir("/tmp", "minion-")
	if err != nil {
		log.WithError(err).Error("Error creating temp dir for minion")
		return nil, "", err
	}

	log.WithField("dir", dir).Info("Created tmp dir")

	err = container.LoadFiles(dir, files)
	if err != nil {
		return nil, "", err
	}

	// create container for minion and run
	// return output (stream) for container
	// remove container image
	mounts := map[string]string{
		dir: "/minion",
	}

	// return stdout iff output == "stdout" else read file (name given by output)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	stdout, stderr, err := container.RunLambda(ctx, filepath.Base(dir), fmt.Sprintf("webstrates/%s", env), "latest", mounts)
	if err != nil {
		return nil, "", err
	}

	o, err := defaultOutput(stdout, stderr)
	if err != nil {
		log.WithError(err).Warn("Error getting default output")
		return nil, "", nil
	}

	if output == "" || output == "stdout" {
		return o, "application/json", nil
	}

	path := filepath.Join(dir, output)
	fileContent, err := ioutil.ReadFile(path)
	if err != nil {
		log.WithError(err).WithField("file", output).Warn("Error reading file for output")
		return o, "application/json", nil
	}
	return fileContent, mime.TypeByExtension(filepath.Ext(path)), nil
}

func defaultOutput(stdout, stderr []byte) ([]byte, error) {
	o := Output{StdOut: string(stdout), StdErr: string(stderr)}
	return json.Marshal(o)
}

// SpawnHandler is the http handler for minion spawns
func SpawnHandler(w http.ResponseWriter, r *http.Request) {

	env := r.FormValue("env")
	output := r.FormValue("output")

	files := map[string][]byte{}
	for key, values := range r.Form {
		if key != "env" && key != "output" && len(values) > 0 {
			files[key] = []byte(values[0])
		}
	}

	if env == "" {
		http.Error(w, "Missing env POST variable", 400)
		return
	}

	result, mimeType, err := Spawn(env, output, files)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", mimeType)
	w.Write(result)
}

// ConnectHandler is the http handler for minion connects
func ConnectHandler(w http.ResponseWriter, r *http.Request) {

	vars := mux.Vars(r)
	webstrate := vars["webstrate"]

	log.WithField("webstrate", webstrate).Info("Minion connecting")

	mutex.Lock()

	if minions[webstrate] == nil {
		// Init this webstrates minion
		minions[webstrate] = map[string]*Minion{}
	}

	id := xid.New().String()

	// create and append minion, init minion.in
	minion := Minion{
		ID:   id,
		to:   make(chan Message, 100),
		from: make(chan Message, 100),
		done: make(chan bool, 100)}

	minions[webstrate][id] = &minion

	golem := golems[webstrate]

	mutex.Unlock()

	defer func() {
		mutex.Lock()
		delete(minions[webstrate], id)
		mutex.Unlock()
	}()

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.WithError(err).Panic("Error upgrading connection")
		minion.done <- true
		return
	}

	if golem == nil {
		log.Warn("No golem connected yet, will look for golems for a little while")
		for i := 0; i < 100; i++ {
			mutex.Lock()
			golem = golems[webstrate]
			if golem != nil {
				log.Info("Golem found")
				mutex.Unlock()
				break
			}
			mutex.Unlock()
			<-time.After(200 * time.Millisecond)
		}
		if golem == nil {
			nf := NewGolemNotFound(webstrate)
			// TODO use WriteJSON instead of marshaling manually
			if notFound, err := json.Marshal(nf); err != nil {
				conn.WriteMessage(websocket.TextMessage, notFound)
			}
			http.Error(w, "No golem found", 404)
			if err := conn.Close(); err != nil {
				log.WithError(err).Warn("Error closing connection to minion - no golem")
			}
			return
		}
	}

	log.WithField("ID", minion.ID).Info("minion assigned id and ready")

	go func(ws *websocket.Conn, m *Minion) {
		for {
			select {
			case msg := <-m.to:
				if err := ws.WriteMessage(msg.Type, msg.Content); err != nil {
					log.WithError(err).Warn("Error writing to minion websocket")
				}
			case <-golem.done:
				if err := ws.Close(); err != nil {
					log.WithError(err).Warn("Error closing minion websocket")
				}
			}
		}
	}(conn, &minion)

	// Let golem know that minion is here
	if golem != nil {
		t := r.URL.Query().Get("type")
		connected, err := json.Marshal(NewMinionConnected(id, t))
		if err != nil {
			log.WithError(err).Warn("Error serialising connected message, Golem will not be alerted to minion-connect")
		} else {
			golem.to <- Message{Type: websocket.TextMessage, Content: connected}
		}
	}

	// read from websocket and pass to minion.out
	for {
		messageType, messageContent, err := conn.ReadMessage()
		if err != nil {
			log.WithError(err).WithField("minion", id).Warn("Error waiting for/reading message from minion")

			disconnected, err := json.Marshal(NewMinionDisconnected(id))
			if err != nil {
				log.WithError(err).Warn("Error marshaling disconnected event")
			} else {
				log.Debug("Letting golem know that minion disconnected")
				golem.to <- Message{Type: websocket.TextMessage, Content: disconnected}
				minion.from <- Message{Type: websocket.TextMessage, Content: disconnected}
			}

			minion.done <- true
			break
		}
		minion.from <- Message{Type: messageType, Content: messageContent}
	}
	log.WithField("minion", minion).Info("minion done")
}

// GolemConnectHandler is the http handler for golem connects
func GolemConnectHandler(w http.ResponseWriter, r *http.Request) {

	vars := mux.Vars(r)
	webstrate := vars["webstrate"]

	log.WithField("webstrate", webstrate).Info("golem connecting")

	mutex.Lock()

	if golems[webstrate] != nil {
		http.Error(w, "Golem already connected", 409 /* Conflict */)
		mutex.Unlock()
		return
	}

	// create golem, init golem.in
	golem := &Golem{
		to:   make(chan Message, 100),
		done: make(chan bool, 100)}

	golems[webstrate] = golem

	mutex.Unlock()

	defer func() {
		mutex.Lock()
		delete(golems, webstrate)
		mutex.Unlock()
	}()

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.WithError(err).Panic("Error upgrading connection")
		golem.done <- true
		mutex.Unlock()
		return
	}

	// hook golem up with websocket
	go func(ws *websocket.Conn, g *Golem, webstrate string) {
		for {
			select {
			case msg := <-g.to:
				if err := ws.WriteMessage(msg.Type, msg.Content); err != nil {
					log.WithError(err).Warn("Could not write message to Golem")
				}
			case <-g.done:
				if err := ws.Close(); err != nil {
					log.WithError(err).Warn("Could not close websocket")
				}
				mutex.Lock()
				delete(golems, webstrate)
				mutex.Unlock()
				return
			}
		}
	}(conn, golem, webstrate)

	// read from golem websocket and respond
	for {
		messageType, messageContent, err := conn.ReadMessage()
		if err != nil {
			log.WithError(err).Warn("Error waiting for/reading message from golem")
			golem.done <- true
			break
		}
		// TODO do something with golem message
		log.WithField("type", messageType).WithField("content", messageContent).Info("Read message from golem")
	}

	log.WithField("webstrate", webstrate).Info("golem done")

}

// GolemMinionConnectHandler handles the golem connecting to an already connected minion
func GolemMinionConnectHandler(w http.ResponseWriter, r *http.Request) {

	vars := mux.Vars(r)
	webstrate := vars["webstrate"]
	minionID := vars["minion"]

	log.WithField("webstrate", webstrate).WithField("minionID", minionID).Info("golem attempting to establish connection")

	mutex.Lock()

	webstrateMinions := minions[webstrate]
	if webstrateMinions == nil {
		http.Error(w, "No such webstrate", 404)
		mutex.Unlock()
		return
	}

	minion := webstrateMinions[minionID]
	if minion == nil {
		http.Error(w, "No such minion registered", 404)
		mutex.Unlock()
		return
	}

	mutex.Unlock()

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.WithError(err).Panic("Error upgrading connection")
		return
	}

	log.WithField("webstrate", webstrate).WithField("minionID", minionID).Info("golem/minion connection ready")

	// Send hello
	event := NewGolemConnected()
	connected, err := json.Marshal(event)
	if err != nil {
		log.WithError(err).Warn("Error marshaling golem-connected event")
	}

	minion.to <- Message{Type: websocket.TextMessage, Content: connected}

	// go read messages from minion and write to ws
	go func(ws *websocket.Conn, minion *Minion) {
		for {
			select {
			case msg := <-minion.from:
				if err := ws.WriteMessage(msg.Type, msg.Content); err != nil {
					log.WithError(err).Warn("Could not forward minion message to golem")
				}
			case <-minion.done:
				// Minion disconnected
				log.Info("golem saw minion was done, closing socket")
				if err := ws.Close(); err != nil {
					log.WithError(err).Warn("minion disconnected, but could not close socket")
				}
				return
			}
		}
	}(conn, minion)

	// read messages from websocket connection and pass to minion.to if found
	for {
		messageType, messageContent, err := conn.ReadMessage()
		if err != nil {
			log.WithError(err).WithField("minion", minion).Warn("Error waiting for/reading message from minion")

			event := NewGolemDisconnected()
			disconnected, err := json.Marshal(event)
			if err != nil {
				log.WithError(err).Warn("error serialising golem-disconnect event")
				break
			}
			minion.to <- Message{Type: websocket.TextMessage, Content: disconnected}
			break
		}
		minion.to <- Message{Type: messageType, Content: messageContent}
	}
	log.WithField("webstrate", webstrate).WithField("minion", minionID).Info("golem/minion session done")
}
