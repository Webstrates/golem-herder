package cmd

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/Webstrates/golem-herder/golem"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/spf13/cobra"
)

type TemplateContext struct {
	Id      string
	BaseUrl string
}

func HomeHandler(w http.ResponseWriter, r *http.Request) {

	tmpl, err := template.ParseFiles("emet.tmpl.js")
	if err != nil {
		http.Error(w, err.Error(), 500)
	}

	context := TemplateContext{BaseUrl: "emet.cc.au.dk"}

	err = tmpl.Execute(w, context)
}

func ListHandler(w http.ResponseWriter, r *http.Request) {

	golems, err := golem.List()

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

	containerID, err := golem.Spawn(wsid)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Write([]byte(fmt.Sprintf("%s lumbering along", containerID)))

}

func ResetHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	wsid := vars["id"]
	containerID, err := golem.Restart(wsid)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Write([]byte(fmt.Sprintf("Reset done - new container %s", containerID)))
}

func KillHandler(w http.ResponseWriter, r *http.Request) {
	// kill, kill, kill
	vars := mux.Vars(r)
	wsid := vars["id"]

	err := golem.Kill(wsid)
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

		gv1 := r.PathPrefix("/golem/v1").Subrouter()

		gv1.HandleFunc("/", HomeHandler)
		gv1.HandleFunc("/ls", ListHandler)
		gv1.HandleFunc("/spawn/{id}", SpawnHandler)
		gv1.HandleFunc("/reset/{id}", ResetHandler)
		gv1.HandleFunc("/kill/{id}", KillHandler)

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
