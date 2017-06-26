package container

import (
	"bytes"
	"context"
	"fmt"
	"io"

	log "github.com/Sirupsen/logrus"
	docker "github.com/fsouza/go-dockerclient"
)

// List containers matching the given predicate.
func List(client *docker.Client, matches func(container *docker.APIContainers) bool) ([]docker.APIContainers, error) {
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

// RunDaemonized will pull, create and start the container piping stdout and stderr to the given channels.
// This function is meant to run longlived, persistent processes.
// A directory (/<name>) will be mounted in the container in which data which must be persisted between sessions can be kept.
func RunDaemonized(name, repository, tag string, ports []int, stdout, stderr chan<- []byte) error {

	client, err := docker.NewClientFromEnv()
	if err != nil {
		log.WithError(err).Error("Could not create docker client")
		return err
	}

	log.WithFields(log.Fields{"image": fmt.Sprintf("%s:%s", repository, tag)}).Info("Pulling image")

	err = client.PullImage(docker.PullImageOptions{
		Repository: repository,
		Tag:        tag,
	}, docker.AuthConfiguration{})
	if err != nil {
		return err
	}

	// Construct []Mount - map /mounts/<name> on host to /<name> in container
	ms := []docker.Mount{docker.Mount{Source: fmt.Sprintf("/mounts/%s", name), Destination: fmt.Sprintf("/%s", name)}}
	binds := []string{fmt.Sprintf("/mount/%s:/%s", name, name)}

	container, err := client.CreateContainer(
		docker.CreateContainerOptions{
			Name: name,
			Config: &docker.Config{
				Image:        fmt.Sprintf("%s:%s", repository, tag),
				Mounts:       ms,
				AttachStdout: true,
				AttachStderr: true,
			},
			HostConfig: &docker.HostConfig{
				Binds: binds,
			},
		},
	)
	if err != nil {
		log.WithError(err).Error("Error creating container")
		return err
	}
	log.WithField("containerid", container.ID).Info("Created container")

	// Start container
	err = client.StartContainer(container.ID, nil)
	if err != nil {
		log.WithError(err).Error("Error starting container")
		return err
	}

	log.WithField("containerid", container.ID).Info("Container started")

	// Use a pipe to run stdout and stderr to channels
	stdoutr, stdoutw := io.Pipe()
	stderrr, stderrw := io.Pipe()
	client.Logs(docker.LogsOptions{
		Stdout:       true,
		Container:    container.ID,
		OutputStream: stdoutw,
		ErrorStream:  stderrw,
	})

	// stdout goes to channel
	go func(r io.Reader, out chan<- []byte) {
		data := make([]byte, 512)
		_, err := r.Read(data)
		out <- data
		if err != nil {
			return
		}
	}(stdoutr, stdout)

	// stderr goes to channel
	go func(r io.Reader, out chan<- []byte) {
		data := make([]byte, 512)
		_, err := r.Read(data)
		out <- data
		if err != nil {
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

	log.WithFields(log.Fields{"image": fmt.Sprintf("%s:%s", repository, tag)}).Info("Pulling image")

	err = client.PullImage(docker.PullImageOptions{
		Repository: repository,
		Tag:        tag,
	}, docker.AuthConfiguration{})
	if err != nil {
		return nil, nil, err
	}

	log.WithFields(log.Fields{"image": fmt.Sprintf("%s:%s", repository, tag)}).Info("Pull done")

	// Construct []Mount
	ms := []docker.Mount{}
	binds := []string{}
	for s, d := range mounts {
		log.WithField("source", s).WithField("dest", d).Info("Preparing mount")
		ms = append(ms, docker.Mount{Source: s, Destination: d})
		binds = append(binds, fmt.Sprintf("%s:%s", s, d))
	}

	container, err := client.CreateContainer(
		docker.CreateContainerOptions{
			Name: name,
			Config: &docker.Config{
				Image:        fmt.Sprintf("%s:%s", repository, tag),
				Mounts:       ms,
				AttachStdout: true,
				AttachStderr: true,
			},
			HostConfig: &docker.HostConfig{
				Binds: binds,
			},
		},
	)
	if err != nil {
		log.WithError(err).Error("Error creating container")
		return nil, nil, err
	}
	log.WithField("containerid", container.ID).Info("Created container")

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

	// Start container
	err = client.StartContainer(container.ID, nil)
	if err != nil {
		log.WithError(err).Error("Error starting container")
		return nil, nil, err
	}

	log.WithField("containerid", container.ID).Info("Container started")

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
