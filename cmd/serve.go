package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/gorilla/mux"
	"github.com/spf13/cobra"
)

func EmetHandler(w http.ResponseWriter, r *http.Request) {

}

func HomeHandler(w http.ResponseWriter, r *http.Request) {

}
func ListHandler(w http.ResponseWriter, r *http.Request) {
	cli, err := client.NewEnvClient()
	if err != nil {
		http.Error(w, err.Error(), 500)
	}
	containers, err := cli.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		http.Error(w, err.Error(), 500)
	}

	golems := []types.Container{}
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
	vars := mux.Vars(r)

	fmt.Println(vars["id"])

	// TODO use id

	ctx := context.Background()
	cli, err := client.NewEnvClient()
	if err != nil {
		http.Error(w, err.Error(), 500)
	}

	imageName := "webstrates/golem"

	out, err := cli.ImagePull(ctx, imageName, types.ImagePullOptions{})
	if err != nil {
		http.Error(w, err.Error(), 500)
	}
	io.Copy(os.Stdout, out)

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: imageName,
	}, nil, nil, "")
	if err != nil {
		http.Error(w, err.Error(), 500)
	}

	if err := cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		http.Error(w, err.Error(), 500)
	}

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
			Handler: r,
			Addr:    ":8000",
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
