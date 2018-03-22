package herder

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"

	"github.com/Webstrates/golem-herder/golem"
	"github.com/gorilla/mux"
	"github.com/spf13/viper"
)

// TemplateContext is the context which gets used when constructing the emet js init file
type TemplateContext struct {
	ID      string
	BaseURL string
}

// HomeHandler will serve the js emet init file
func HomeHandler(w http.ResponseWriter, r *http.Request) {

	tmpl, err := template.ParseFiles("emet.tmpl.js")
	if err != nil {
		http.Error(w, err.Error(), 500)
	}

	context := TemplateContext{BaseURL: viper.GetString("url")}

	err = tmpl.Execute(w, context)
}

// ListHandler shows the running golems
func ListHandler(w http.ResponseWriter, r *http.Request) {

	golems, err := golem.List()

	data, err := json.Marshal(golems)
	if err != nil {
		http.Error(w, err.Error(), 500)
	}

	w.Write(data)
}

// SpawnHandler will spawn a new golem for the webstrate given by the mux.Vars
func SpawnHandler(w http.ResponseWriter, r *http.Request) {

	// we need id for webstrate
	vars := mux.Vars(r)
	wsid := vars["webstrate"]

	containerID, err := golem.Spawn(wsid)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Write([]byte(fmt.Sprintf("%s lumbering along", containerID)))

}

// ResetHandler will reset/reload the golem on the given webstrate
func ResetHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	wsid := vars["webstrate"]
	containerID, err := golem.Restart(wsid)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Write([]byte(fmt.Sprintf("Reset done - new container %s", containerID)))
}

// KillHandler will kill the golem
func KillHandler(w http.ResponseWriter, r *http.Request) {
	// kill, kill, kill
	vars := mux.Vars(r)
	wsid := vars["webstrate"]

	err := golem.Kill(wsid)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Write([]byte(fmt.Sprintf("Golem for %s is no more", wsid)))
}
