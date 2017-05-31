package herder

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"

	"github.com/Webstrates/golem-herder/golem"
	"github.com/gorilla/mux"
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
	wsid := vars["webstrate"]

	containerID, err := golem.Spawn(wsid)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Write([]byte(fmt.Sprintf("%s lumbering along", containerID)))

}

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
