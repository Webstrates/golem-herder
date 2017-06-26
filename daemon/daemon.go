package daemon

import (
	"fmt"

	"github.com/Webstrates/golem-herder/container"
	"github.com/Webstrates/golem-herder/metering"
	jwt "github.com/dgrijalva/jwt-go"
)

// SpawnOptions contains configuration options for the daemon spawn.
type SpawnOptions struct {
	Meter  *metering.Meter
	Ports  []int
	StdOut chan []byte
	StdErr chan []byte
}

// Spawn a daemon with the given options.
func Spawn(name, image string, token *jwt.Token, options SpawnOptions) error {

	// Construct a new unique (for this token) id from name and token id
	// - we'll assume that token has already been validated
	uname := fmt.Sprintf("%s-%v", name, token.Header["ID"])

	// wait for done
	done := make(chan bool)

	// Get random outside ports
	ports := map[int]int{}
	for _, insidePort := range options.Ports {
		ports[container.GetAvailableHostPort()] = insidePort
	}

	err := container.RunDaemonized(uname, image, "latest", ports, options.StdOut, options.StdErr, done)
	if err != nil {
		return err
	}

	// TODO Pipe stdout and stderr to websocket, close when done is invoked
	// TODO Return port-mapping

	return nil
}
