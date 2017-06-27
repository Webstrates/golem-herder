package daemon

import (
	"fmt"

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
	Name  string
	Ports map[int]int
}

// Spawn a daemon with the given options.
func Spawn(token *jwt.Token, name, image string, options Options) (Info, error) {

	// Construct a new unique (for this token) id from name and token id
	// - we'll assume that token has already been validated
	uname := fmt.Sprintf("%s-%v", name, token.Header["ID"])

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
		"tokeníd": fmt.Sprintf("%v", token.Header["ID"]),
	}

	err := container.RunDaemonized(uname, image, "latest", ports, labels, options.StdOut, options.StdErr, options.Done)
	if err != nil {
		return Info{}, err
	}

	return Info{Name: uname, Ports: invertedPorts}, nil
}

// List the daemons running on this token.
func List(token *jwt.Token) ([]docker.APIContainers, error) {
	return container.List(nil, container.WithLabel("token", token.Raw))
}
