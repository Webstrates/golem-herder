package cmd

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/fsouza/go-dockerclient"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/spf13/cobra"
)

type Emet struct {
	Id      string
	BaseUrl string
}

func EmetHandler(w http.ResponseWriter, r *http.Request) {

	tmpl, err := template.ParseFiles("emet.tmpl.js")
	if err != nil {
		http.Error(w, err.Error(), 500)
	}

	emet := Emet{BaseUrl: "localhost"}

	err = tmpl.Execute(w, emet)
}

func HomeHandler(w http.ResponseWriter, r *http.Request) {

}
func ListHandler(w http.ResponseWriter, r *http.Request) {

	client, err := docker.NewClientFromEnv()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	containers, err := client.ListContainers(docker.ListContainersOptions{All: false})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	golems := []docker.APIContainers{}
	for _, container := range containers {
		if container.Image == "golem" {
			golems = append(golems, container)
		}
	}

	data, err := json.Marshal(golems)
	if err != nil {
		http.Error(w, err.Error(), 500)
	}

	w.Write(data)

	for _, container := range golems {
		fmt.Println(container.ID)
	}
}
func SpawnHandler(w http.ResponseWriter, r *http.Request) {

	// we need id for webstrate

	client, err := docker.NewClientFromEnv()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// TODO figure out port
	// TODO if container is already running then return

	//w.Write([]byte(vars["id"]))

	// TODO use id

	vars := mux.Vars(r)
	wsid := vars["id"]

	//ctx := context.Background()

	err = client.PullImage(docker.PullImageOptions{
		Repository: "webstrates/golem",
		Tag:        "latest",
	}, docker.AuthConfiguration{})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	container, err := client.CreateContainer(
		docker.CreateContainerOptions{
			Name: fmt.Sprintf("golem-%s", wsid),
			Config: &docker.Config{
				Image: "webstrates/golem:latest",
				ExposedPorts: map[docker.Port]struct{}{
					"9222/tcp": {},
				},
			},
		},
	)
	if err != nil {
		fmt.Println("Error creating container" + err.Error())
		http.Error(w, err.Error(), 500)
		return
	}

	fmt.Println(container.ID)

	//client.StartContainer(container.ID,

	// TODO return json

}
func ResetHandler(w http.ResponseWriter, r *http.Request) {
}
func KillHandler(w http.ResponseWriter, r *http.Request) {
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
			Addr:      ":8000",
			TLSConfig: &tls.Config{},
			// Good practice: enforce timeouts for servers you create!
			WriteTimeout: 15 * time.Second,
			ReadTimeout:  15 * time.Second,
		}

		log.Fatal(srv.ListenAndServeTLS("server.crt", "server.key"))
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
