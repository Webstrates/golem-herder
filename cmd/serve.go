package cmd

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Webstrates/golem-herder/herder"
	"github.com/Webstrates/golem-herder/minion"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/spf13/cobra"
)

var port int

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start a remote administration interface",
	Run: func(cmd *cobra.Command, args []string) {
		r := mux.NewRouter()

		gv1 := r.PathPrefix("/golem/v1").Subrouter()

		gv1.HandleFunc("/", herder.HomeHandler)
		gv1.HandleFunc("/ls", herder.ListHandler)
		gv1.HandleFunc("/spawn/{webstrate}", herder.SpawnHandler)
		gv1.HandleFunc("/reset/{webstrate}", herder.ResetHandler)
		gv1.HandleFunc("/kill/{webstrate}", herder.KillHandler)

		// Connect a golem. Golem will get status info and connect information on this socket.
		gv1.HandleFunc("/connect/{webstrate}", minion.GolemConnectHandler)

		// Connect a golem and a specific minion
		gv1.HandleFunc("/connect-to/{webstrate}/{minion}", minion.GolemMinionConnectHandler)

		mv1 := r.PathPrefix("/minion/v1").Subrouter()
		// Connect a minion
		mv1.HandleFunc("/connect/{webstrate}", minion.MinionConnectHandler)

		srv := &http.Server{
			Handler:   handlers.CORS()(r),
			Addr:      fmt.Sprintf(":%v", port),
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

	serveCmd.Flags().IntVarP(&port, "port", "p", 81, "Which port to listen on")
}
