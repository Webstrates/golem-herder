package golem

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strings"

	log "github.com/Sirupsen/logrus"

	docker "github.com/fsouza/go-dockerclient"
)

func getPort() int {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		panic(err)
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		panic(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func getName(id string) string {
	return fmt.Sprintf("golem-%s", id)
}

func containersMatching(client *docker.Client, matches func(container *docker.APIContainers) bool) ([]docker.APIContainers, error) {
	containers, err := client.ListContainers(docker.ListContainersOptions{All: false})
	if err != nil {
		log.WithError(err).Error("Error listing containers")
		return nil, err
	}

	matching := []docker.APIContainers{}
	for _, container := range containers {
		if matches(&container) {
			matching = append(matching, container)
		}
	}
	return matching, nil
}

func containerHasName(container *docker.APIContainers, name string) bool {
	for _, containerName := range container.Names {
		if containerName == "/"+name {
			return true
		}
	}
	return false
}

// Spawn will create a new container and inject a golem into it
func Spawn(webstrateID string) (string, error) {

	client, err := docker.NewClientFromEnv()
	if err != nil {
		log.WithError(err).Error("Could not create docker client")
		return "", err
	}

	repository := "webstrates/golem"
	tag := "latest"

	log.WithFields(log.Fields{"image": fmt.Sprintf("%s:%s", repository, tag)}).Info("Pulling image")

	err = client.PullImage(docker.PullImageOptions{
		Repository: "webstrates/golem",
		Tag:        "latest",
	}, docker.AuthConfiguration{})
	if err != nil {
		return "", err
	}
	log.WithFields(log.Fields{"image": fmt.Sprintf("%s:%s", repository, tag)}).Info("Pull done")

	// Get current dir
	dir, err := os.Getwd()
	if err != nil {
		log.WithError(err).Error("Could not discover current directory")
		return "", err
	}

	seccomp, err := ioutil.ReadFile(filepath.Join(dir, "chrome.json"))
	if err != nil {
		log.WithError(err).Error("Could not read seccomp profile")
		return "", err
	}

	log.WithFields(log.Fields{"webstrateid": webstrateID}).Info("Creating container")
	container, err := client.CreateContainer(
		docker.CreateContainerOptions{
			Name: getName(webstrateID),
			Config: &docker.Config{
				Image: "webstrates/golem:latest",
				ExposedPorts: map[docker.Port]struct{}{
					"9222/tcp": {},
				},
				Env: []string{fmt.Sprintf("WEBSTRATEID=%s", webstrateID)},
				Cmd: []string{
					"--headless",
					"--ignore-certificate-errors",
					"--disable-gpu",
					"--remote-debugging-address=0.0.0.0",
					"--remote-debugging-port=9222",
					fmt.Sprintf("http://webstrates/%s", webstrateID),
				},
			},
			HostConfig: &docker.HostConfig{
				Links: []string{"webstrates"},
				PortBindings: map[docker.Port][]docker.PortBinding{
					"9222/tcp": []docker.PortBinding{
						docker.PortBinding{
							HostIP:   "0.0.0.0",
							HostPort: fmt.Sprintf("%d", getPort()),
						},
					},
				},
				SecurityOpt: []string{
					fmt.Sprintf("seccomp=%s", string(seccomp)),
				},
			},
		},
	)
	if err != nil {
		log.WithError(err).Error("Error creating container")
		return "", err
	}
	log.WithFields(log.Fields{"webstrateid": webstrateID, "containerid": container.ID}).Info("Created container, starting ...")

	err = client.StartContainer(container.ID, nil)

	if err != nil {
		log.WithError(err).Error("Error starting container")
		return "", err
	}
	return container.ID, nil
}

// Kill will kill the container running the given golem
func Kill(webstrateID string) error {

	client, err := docker.NewClientFromEnv()
	if err != nil {
		log.WithError(err).Error("Error creating docker client")
		return err
	}

	golems, err := containersMatching(client, func(c *docker.APIContainers) bool {
		return strings.HasPrefix(c.Image, "webstrates/golem") && containerHasName(c, getName(webstrateID))
	})

	if len(golems) != 1 {
		return fmt.Errorf("Unexpected amount of golems - %d", len(golems))
	}

	err = client.KillContainer(docker.KillContainerOptions{
		ID: golems[0].ID,
	})
	if err != nil {
		return err
	}

	err = client.RemoveContainer(docker.RemoveContainerOptions{
		ID:            golems[0].ID,
		RemoveVolumes: true,
	})
	if err != nil {
		return err
	}
	return nil
}

// Restart will kill, recreate and start a given golem
func Restart(webstrateID string) (string, error) {
	err := Kill(webstrateID)
	if err != nil {
		return "", err
	}
	return Spawn(webstrateID)
}

// PortOf returns the public port mapped to the given privatePort for
// a golem on the given webstrate.
func PortOf(webstrate string, privatePort int64) (int64, error) {
	// Not very effective to do a list each time the port is needed
	golems, err := List()
	if err != nil {
		return -1, err
	}

	for _, golem := range golems {
		if ws, ok := golem.Labels["webstrate"]; ok && webstrate == ws {
			for _, port := range golem.Ports {
				if port.PrivatePort == privatePort {
					return port.PublicPort, nil
				}
			}
		}
	}

	return -1, fmt.Errorf("No container found for webstrate")
}

// List the running golems
func List() ([]docker.APIContainers, error) {

	client, err := docker.NewClientFromEnv()
	if err != nil {
		log.WithError(err).Error("Error creating docker client")
		return nil, err
	}

	golems, err := containersMatching(client, func(container *docker.APIContainers) bool {
		return strings.HasPrefix(container.Image, "webstrates/golem")
	})
	if err != nil {
		return nil, err
	}

	return golems, nil

}
