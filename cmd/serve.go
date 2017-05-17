package cmd

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/fsouza/go-dockerclient"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/spf13/cobra"
)

type Emet struct {
	Id      string
	BaseUrl string
}

func GetPort() int {
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

func GetName(id string) string {
	return fmt.Sprintf("golem-%s", id)
}

func ContainersMatching(client *docker.Client, matches func(container *docker.APIContainers) bool) ([]docker.APIContainers, error) {
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

func ContainerHasName(container *docker.APIContainers, name string) bool {
	for _, containerName := range container.Names {
		if containerName == "/"+name {
			return true
		}
	}
	return false
}

func EmetHandler(w http.ResponseWriter, r *http.Request) {

	tmpl, err := template.ParseFiles("emet.tmpl.js")
	if err != nil {
		http.Error(w, err.Error(), 500)
	}

	emet := Emet{BaseUrl: "emet.cc.au.dk"}

	err = tmpl.Execute(w, emet)
}

func HomeHandler(w http.ResponseWriter, r *http.Request) {

}
func ListHandler(w http.ResponseWriter, r *http.Request) {

	client, err := docker.NewClientFromEnv()
	if err != nil {
		log.WithError(err).Error("Error creating docker client")
		http.Error(w, err.Error(), 500)
		return
	}

	golems, err := ContainersMatching(client, func(container *docker.APIContainers) bool {
		return strings.HasPrefix(container.Image, "webstrates/golem")
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
	}

	data, err := json.Marshal(golems)
	if err != nil {
		http.Error(w, err.Error(), 500)
	}

	w.Write(data)
}
func SpawnHandler(w http.ResponseWriter, r *http.Request) {

	// we need id for webstrate
	vars := mux.Vars(r)
	wsid := vars["id"]

	client, err := docker.NewClientFromEnv()
	if err != nil {
		log.WithError(err).Error("Could create docker client")
		http.Error(w, err.Error(), 500)
		return
	}

	//ctx := context.Background()

	repository := "webstrates/golem"
	tag := "latest"

	log.WithFields(log.Fields{"image": fmt.Sprintf("%s:%s", repository, tag)}).Info("Pulling image")

	err = client.PullImage(docker.PullImageOptions{
		Repository: "webstrates/golem",
		Tag:        "latest",
	}, docker.AuthConfiguration{})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	log.WithFields(log.Fields{"image": fmt.Sprintf("%s:%s", repository, tag)}).Info("Pull done")

	// Get current dir
	dir, err := os.Getwd()
	if err != nil {
		log.WithError(err).Error("Could not discover current directory")
		http.Error(w, err.Error(), 500)
		return
	}

	seccomp, err := ioutil.ReadFile(filepath.Join(dir, "chrome.json"))
	if err != nil {
		log.WithError(err).Error("Could not read seccomp profile")
		http.Error(w, err.Error(), 500)
		return
	}

	log.WithFields(log.Fields{"webstrateid": wsid}).Info("Creating container")
	container, err := client.CreateContainer(
		docker.CreateContainerOptions{
			Name: GetName(wsid),
			Config: &docker.Config{
				Image: "webstrates/golem:latest",
				ExposedPorts: map[docker.Port]struct{}{
					"9222/tcp": {},
				},
				Env: []string{fmt.Sprintf("WEBSTRATEID=%s", wsid)},
				Cmd: []string{
					"--headless",
					"--ignore-certificate-errors",
					"--disable-gpu",
					"--remote-debugging-address=0.0.0.0",
					"--remote-debugging-port=9222",
					fmt.Sprintf("http://webstrates/%s", wsid),
				},
			},
			HostConfig: &docker.HostConfig{
				Links: []string{"webstrates"},
				PortBindings: map[docker.Port][]docker.PortBinding{
					"9222/tcp": []docker.PortBinding{
						docker.PortBinding{
							HostIP:   "0.0.0.0",
							HostPort: fmt.Sprintf("%d", GetPort()),
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
		http.Error(w, err.Error(), 500)
		return
	}
	log.WithFields(log.Fields{"webstrateid": wsid, "containerid": container.ID}).Info("Created container, starting ...")

	err = client.StartContainer(container.ID, nil)

	if err != nil {
		log.WithError(err).Error("Error starting container")
		http.Error(w, err.Error(), 500)
		return
	}

	w.Write([]byte(fmt.Sprintf("%s lumbering along", container.ID)))

}
func ResetHandler(w http.ResponseWriter, r *http.Request) {
	// TODO reset handler

	// figure out port
	// send crdp request to reload page -or- restart container
}
func KillHandler(w http.ResponseWriter, r *http.Request) {
	// kill, kill, kill
	vars := mux.Vars(r)
	wsid := vars["id"]

	client, err := docker.NewClientFromEnv()
	if err != nil {
		log.WithError(err).Error("Error creating docker client")
		http.Error(w, err.Error(), 500)
		return
	}

	golems, err := ContainersMatching(client, func(c *docker.APIContainers) bool {
		return strings.HasPrefix(c.Image, "webstrates/golem") && ContainerHasName(c, GetName(wsid))
	})

	if len(golems) != 1 {
		http.Error(w, fmt.Sprintf("Unexpected amount of golems - %d", len(golems)), 500)
		return
	}

	err = client.KillContainer(docker.KillContainerOptions{
		ID: golems[0].ID,
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	err = client.RemoveContainer(docker.RemoveContainerOptions{
		ID:            golems[0].ID,
		RemoveVolumes: true,
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Write([]byte(fmt.Sprintf("Golem for %s is no more", wsid)))
}

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start a remote administration interface",
	Run: func(cmd *cobra.Command, args []string) {
		r := mux.NewRouter()

		r.HandleFunc("/", HomeHandler)
		r.HandleFunc("/emet", EmetHandler)
		r.HandleFunc("/ls", ListHandler)
		r.HandleFunc("/spawn/{id}", SpawnHandler)
		r.HandleFunc("/reset/{id}", ResetHandler)
		r.HandleFunc("/kill/{id}", KillHandler)

		srv := &http.Server{
			Handler:   handlers.CORS()(r),
			Addr:      ":80",
			TLSConfig: &tls.Config{},
			// Good practice: enforce timeouts for servers you create!
			WriteTimeout: 15 * time.Second,
			ReadTimeout:  15 * time.Second,
		}

		log.Fatal(srv.ListenAndServe())
	},
}

func init() {
	RootCmd.AddCommand(serveCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// serveCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// serveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

}
