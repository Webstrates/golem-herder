package container

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/spf13/viper"

	log "github.com/Sirupsen/logrus"
	docker "github.com/fsouza/go-dockerclient"
)

// GetAvailableHostPort returns an available (and random) port on the host machine
func GetAvailableHostPort() int {
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

// List containers matching the given predicate.
func List(client *docker.Client, matches func(container *docker.APIContainers) bool) ([]docker.APIContainers, error) {

	// Create client if it is not given
	if client == nil {
		c, err := docker.NewClientFromEnv()
		if err != nil {
			log.WithError(err).Error("Could not create docker client")
			return nil, err
		}
		client = c
	}

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

// WithName returns a func to match a containers name (for use with e.g. List)
func WithName(name string) func(container *docker.APIContainers) bool {
	return func(container *docker.APIContainers) bool {
		for _, containerName := range container.Names {
			if containerName == "/"+name {
				return true
			}
		}
		return false
	}
}

// WithLabel returns a func to match containers based on their label (for use with e.g. List)
func WithLabel(label, value string) func(container *docker.APIContainers) bool {
	return func(container *docker.APIContainers) bool {
		if labelValue, ok := container.Labels[label]; ok && value == labelValue {
			return true
		}
		return false
	}
}

// Kill the container with the given name and optionally remove mounted volumes.
func Kill(name string, destroyData bool) error {

	client, err := docker.NewClientFromEnv()
	if err != nil {
		log.WithError(err).Error("Could not create docker client")
		return err
	}

	containers, err := List(client, WithName(name))
	if err != nil {
		log.WithError(err).Warn("Error listing containers")
		return err
	}
	if len(containers) != 1 {
		log.WithField("count", len(containers)).Warn("Too many or too few matching containers")
		return fmt.Errorf("Expected 1 container to match name, got %v", len(containers))
	}

	log.WithField("container", containers[0].ID).Info("Killing container")

	err = client.RemoveContainer(docker.RemoveContainerOptions{
		ID:            containers[0].ID,
		Force:         true,
		RemoveVolumes: destroyData,
	})
	if err != nil {
		log.WithError(err).Warn("Error removing container")
		return err
	}

	return nil
}

func run(client *docker.Client, name, repository, tag string, ports map[int]int, mounts map[string]string, labels map[string]string) (*docker.Container, error) {

	log.WithFields(log.Fields{"image": fmt.Sprintf("%s:%s", repository, tag)}).Info("Pulling image")

	err := client.PullImage(docker.PullImageOptions{
		Repository: repository,
		Tag:        tag,
	}, docker.AuthConfiguration{})
	if err != nil {
		return nil, err
	}

	// Construct []Mount
	ms := []docker.Mount{}
	binds := []string{}
	if mounts != nil {
		for s, d := range mounts {
			log.WithField("source", s).WithField("dest", d).Info("Preparing mount")
			ms = append(ms, docker.Mount{Source: s, Destination: d})
			binds = append(binds, fmt.Sprintf("%s:%s", s, d))
		}
	}

	// Construct port bindings
	exposedPorts := map[docker.Port]struct{}{}
	portBindings := map[docker.Port][]docker.PortBinding{}
	if ports != nil {
		for outsidePort, insidePort := range ports {
			insidePortTCP := docker.Port(fmt.Sprintf("%d/tcp", insidePort))
			exposedPorts[insidePortTCP] = struct{}{}
			portBindings[insidePortTCP] = []docker.PortBinding{{
				HostIP:   "0.0.0.0",
				HostPort: fmt.Sprintf("%d", outsidePort),
			},
			}
		}
	}

	container, err := client.CreateContainer(
		docker.CreateContainerOptions{
			Name: name,
			Config: &docker.Config{
				Image:        fmt.Sprintf("%s:%s", repository, tag),
				Labels:       labels,
				ExposedPorts: exposedPorts,
				Mounts:       ms,
				AttachStdout: true,
				AttachStderr: true,
			},
			HostConfig: &docker.HostConfig{
				PortBindings: portBindings,
				Binds:        binds,
			},
		},
	)
	if err != nil {
		log.WithError(err).Error("Error creating container")
		return nil, err
	}
	log.WithField("containerid", container.ID).Info("Created container")

	// Start container
	err = client.StartContainer(container.ID, nil)
	if err != nil {
		log.WithError(err).Error("Error starting container")
		return nil, err
	}

	log.WithField("containerid", container.ID).Info("Container started")

	return container, nil
}

// RunDaemonized will pull, create and start the container piping stdout and stderr to the given channels.
// This function is meant to run longlived, persistent processes.
// A directory (/<name>) will be mounted in the container in which data which must be persisted between sessions can be kept.
func RunDaemonized(name, repository, tag string, ports map[int]int, labels map[string]string, stdout, stderr chan<- []byte, done chan<- bool) error {

	client, err := docker.NewClientFromEnv()
	if err != nil {
		log.WithError(err).Error("Could not create docker client")
		return err
	}

	// Construct mounts
	mounts := map[string]string{
		fmt.Sprintf("%v/%v", viper.GetString("mounts"), name): fmt.Sprintf("/%v", name),
	}

	c, err := run(client, name, repository, tag, ports, mounts, labels)
	if err != nil {
		return err
	}

	// Setup monitor for service - if it does done should be notified
	if done != nil {
		go func() {
			// Cleanup container after it exits
			defer func() {
				err = client.RemoveContainer(docker.RemoveContainerOptions{
					ID:            c.ID,
					Force:         true,
					RemoveVolumes: false,
				})
				if err != nil {
					log.WithError(err).Warn("Error removing container")
				}
			}()
			for {
				<-time.After(time.Second)
				if cntnr, err := client.InspectContainer(c.ID); err != nil || !cntnr.State.Running {
					log.WithField("name", name).WithField("id", c.ID).Info("Container looks dead")
					done <- true
					return
				}
			}
		}()
	}

	if stdout == nil || stderr == nil {
		return nil
	}

	// Use a pipe to run stdout and stderr to channels
	stdoutr, stdoutw := io.Pipe()
	stderrr, stderrw := io.Pipe()
	client.Logs(docker.LogsOptions{
		Stdout:       true,
		Container:    c.ID,
		OutputStream: stdoutw,
		ErrorStream:  stderrw,
	})

	// stdout goes to channel
	go func(r io.Reader, out chan<- []byte) {
		data := make([]byte, 512)
		_, err := r.Read(data)
		out <- data
		if err != nil {
			// stop looking for stdout
			return
		}
	}(stdoutr, stdout)

	// stderr goes to channel
	go func(r io.Reader, out chan<- []byte) {
		data := make([]byte, 512)
		_, err := r.Read(data)
		out <- data
		if err != nil {
			// stop looking for stderr
			return
		}
	}(stderrr, stderr)

	return nil
}

// RunLambda will pull, create and start the container returning its stdout.
// This function is meant to run a shortlived process.
func RunLambda(ctx context.Context, name, repository, tag string, mounts map[string]string) ([]byte, []byte, error) {

	client, err := docker.NewClientFromEnv()
	if err != nil {
		log.WithError(err).Error("Could not create docker client")
		return nil, nil, err
	}

	container, err := run(client, name, repository, tag, nil, mounts, nil)
	if err != nil {
		return nil, nil, err
	}

	// Cleanup
	defer func() {
		err = client.RemoveContainer(docker.RemoveContainerOptions{
			ID:            container.ID,
			Force:         true,
			RemoveVolumes: true,
		})
		if err != nil {
			log.WithError(err).Warn("Error removing container")
		}
	}()

	_, err = client.WaitContainerWithContext(container.ID, ctx)
	if err != nil {
		log.WithError(err).Warn("Error waiting for container to exit")
		return nil, nil, err
	}

	// Use a buffer to capture output
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	client.Logs(docker.LogsOptions{
		Stdout:       true,
		Container:    container.ID,
		OutputStream: &stdout,
		ErrorStream:  &stderr,
	})

	log.WithField("stdout", stdout.String()).WithField("stderr", stderr.String()).Info("Run done")

	return stdout.Bytes(), stderr.Bytes(), nil
}
