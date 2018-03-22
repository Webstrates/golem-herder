package golem

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/Webstrates/golem-herder/container"
	"github.com/spf13/viper"

	docker "github.com/fsouza/go-dockerclient"
)

func getName(id string) string {
	return fmt.Sprintf("golem-%s", id)
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

	// Links
	var links []string
	if viper.GetBool("proxy") {
		links = []string{viper.GetString("webstrates")}
	}

	log.WithFields(log.Fields{"webstrateid": webstrateID}).Info("Creating container")
	container, err := client.CreateContainer(
		docker.CreateContainerOptions{
			Name: getName(webstrateID),
			Config: &docker.Config{
				Image:  "webstrates/golem:latest",
				Labels: map[string]string{"webstrate": webstrateID},
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
					fmt.Sprintf("http://%s/%s", viper.GetString("webstrates"), webstrateID),
				},
			},
			HostConfig: &docker.HostConfig{
				Links: links,
				PortBindings: map[docker.Port][]docker.PortBinding{
					"9222/tcp": []docker.PortBinding{{
						HostIP:   "0.0.0.0",
						HostPort: fmt.Sprintf("%d", container.GetAvailableHostPort()),
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

	golems, err := container.List(client, func(c *docker.APIContainers) bool {
		return strings.HasPrefix(c.Image, "webstrates/golem") && container.WithName(getName(webstrateID))(c)
	}, false)

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

	return container.List(client, func(container *docker.APIContainers) bool {
		return strings.HasPrefix(container.Image, "webstrates/golem")
	}, false)
}
