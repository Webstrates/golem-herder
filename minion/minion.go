package minion

import (
	"encoding/json"
	"net/http"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	uuid "github.com/satori/go.uuid"
)

// List all connected minions
var (
	port    int
	golems  map[string]*Golem
	minions map[string]map[string]*Minion

	mutex = &sync.Mutex{}
	// upgrader upgrades HTTP 1.1 connection to WebSocket
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(*http.Request) bool { return true }, // allow all origins
	}
)

type Golem struct {
	// in is chan to send messages to the golem
	to   chan Message
	done chan bool
}
type Minion struct {
	ID string
	// in is a chan w messages to the minion
	to   chan Message
	from chan Message
	done chan bool
}

type Message struct {
	Type    int
	Content []byte
}

type ConnectEvent struct {
	Event string
	ID    string `json:",omitempty"`
	Type  string `json:",omitempty"`
}

func NewMinionConnected(id string, t string) ConnectEvent {
	return ConnectEvent{
		Event: "minion-connected",
		ID:    id,
		Type:  t}
}

func NewMinionDisconnected(id string) ConnectEvent {
	return ConnectEvent{
		Event: "minion-disconnected",
		ID:    id}
}

func NewGolemDisconnected() ConnectEvent {
	return ConnectEvent{
		Event: "golem-disconnected"}
}

func NewGolemConnected() ConnectEvent {
	return ConnectEvent{
		Event: "golem-connected"}
}

func MinionConnectHandler(w http.ResponseWriter, r *http.Request) {

	vars := mux.Vars(r)
	webstrate := vars["webstrate"]

	mutex.Lock()

	if minions[webstrate] == nil {
		// Init this webstrates minion
		minions[webstrate] = map[string]*Minion{}
	}

	id := uuid.NewV4().String()

	// create and append minion, init minion.in
	minion := Minion{
		ID:   id,
		to:   make(chan Message, 100),
		from: make(chan Message, 100),
		done: make(chan bool, 100)}

	minions[webstrate][id] = &minion

	golem := golems[webstrate]

	mutex.Unlock()

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.WithError(err).Panic("Error upgrading connection")
		minion.done <- true
		return
	}

	go func(ws *websocket.Conn, m *Minion) {
		for {
			select {
			case msg := <-m.to:
				if err := ws.WriteMessage(msg.Type, msg.Content); err != nil {
					log.WithError(err).Warn("Error writing to minion websocket")
				}
			case <-minion.done:
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
			}

			// Cleanup
			mutex.Lock()
			delete(minions[webstrate], id)
			mutex.Unlock()

			minion.done <- true
			break
		}
		minion.from <- Message{Type: messageType, Content: messageContent}
	}
}

func GolemConnectHandler(w http.ResponseWriter, r *http.Request) {

	vars := mux.Vars(r)
	webstrate := vars["webstrate"]

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
}

func GolemMinionConnectHandler(w http.ResponseWriter, r *http.Request) {

	vars := mux.Vars(r)
	webstrate := vars["webstrate"]
	minionID := vars["minion"]

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
				// Golem disconnected
				event := NewGolemDisconnected()
				disconnected, err := json.Marshal(event)
				if err != nil {
					log.WithError(err).Warn("Could not marshal golem-disconnect message")
				}
				err = ws.WriteMessage(websocket.TextMessage, disconnected)
				if err != nil {
					log.WithError(err).Warn("Could not alert golem that minion left")
				}
				// Let golem know that minion disconnected
				event = NewMinionDisconnected(minion.ID)
				disconnected, err = json.Marshal(event)
				if err != nil {
					log.WithError(err).Warn("Error marshaling disconnected event")
					return
				}
				ws.WriteMessage(websocket.TextMessage, disconnected)

				if err := ws.Close(); err != nil {
					log.WithError(err).Warn("Error closing websocket")
				}
				return
			}
		}
	}(conn, minion)

	// read messages from websocket connection and pass to minion.to if found
	for {
		messageType, messageContent, err := conn.ReadMessage()
		if err != nil {
			log.WithError(err).WithField("minion", minion).Warn("Error waiting for/reading message from golem")
			minion.done <- true
			return
		}
		minion.to <- Message{Type: messageType, Content: messageContent}
	}
	log.WithField("webstrate", webstrate).WithField("minion", minionID).Info("golem/minion session done")
}
