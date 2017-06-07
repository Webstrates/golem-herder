package container

import (
	"bytes"
	"fmt"

	log "github.com/Sirupsen/logrus"
	docker "github.com/fsouza/go-dockerclient"
)

// Run will pull, create and start the container returning its stdout
func Run(name, repository, tag string, mounts map[string]string) ([]byte, []byte, error) {

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
		client.RemoveContainer(docker.RemoveContainerOptions{
			ID:            container.ID,
			Force:         true,
			RemoveVolumes: true,
		})
		if err != nil {
			log.WithError(err).Warn("Could not remove container")
		}
	}()
	err = client.StartContainer(container.ID, nil)

	if err != nil {
		log.WithError(err).Error("Error starting container")
		return nil, nil, err
	}

	log.WithField("containerid", container.ID).Info("Container started")

	// TODO provide context to cancel after 60 seconds
	_, err = client.WaitContainer(container.ID)
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
