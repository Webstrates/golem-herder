package cmd

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Webstrates/golem-herder/daemon"
	"github.com/Webstrates/golem-herder/herder"
	"github.com/Webstrates/golem-herder/minion"
	"github.com/Webstrates/golem-herder/token"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	port     int
	mountdir string
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start a remote administration interface",
	Run: func(cmd *cobra.Command, args []string) {

		// Validation
		m, err := token.NewManager(pubKey, privKey)
		if err != nil {
			panic(err)
		}

		r := mux.NewRouter()

		gv1 := r.PathPrefix("/golem/v1").Subrouter()

		// TODO move these handlers to golem
		gv1.HandleFunc("/", herder.HomeHandler)
		gv1.HandleFunc("/ls", herder.ListHandler)
		gv1.HandleFunc("/spawn/{webstrate}", herder.SpawnHandler)
		gv1.HandleFunc("/reset/{webstrate}", herder.ResetHandler)
		gv1.HandleFunc("/kill/{webstrate}", herder.KillHandler)

		// bah, does not work due to absolute urls in html page
		//proxyPrefix := "/proxy"
		//proxy := golem.NewGolemReverseProxy(proxyPrefix, golem.PortOf)
		//gv1.PathPrefix(proxyPrefix).Handler(proxy)

		// Connect a golem. Golem will get status info and connect information on this socket.
		gv1.HandleFunc("/connect/{webstrate}", minion.GolemConnectHandler)

		// Connect a golem and a specific minion
		gv1.HandleFunc("/connect-to/{webstrate}/{minion}", minion.GolemMinionConnectHandler)

		mv1 := r.PathPrefix("/minion/v1").Subrouter()
		// Connect a minion
		mv1.HandleFunc("/connect/{webstrate}", minion.ConnectHandler)
		mv1.HandleFunc("/spawn", minion.SpawnHandler).Methods("POST")

		// Daemons
		dv1 := r.PathPrefix("/daemon/v1").Subrouter()
		dv1.HandleFunc("/spawn", token.ValidatedHandler(m, daemon.SpawnHandler)).Methods("POST")
		dv1.HandleFunc("/ls", token.ValidatedHandler(m, daemon.ListHandler))
		dv1.HandleFunc("/kill/{name}", token.ValidatedHandler(m, daemon.KillHandler))
		dv1.HandleFunc("/attach/{name}", token.ValidatedHandler(m, daemon.AttachHandler))

		// Tokens
		//r.HandleFunc("/token/v1/generate", token.GenerateHandler)
		//r.HandleFunc("/token/v1/inspect/{token}", meter.InspectHandler)

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
	serveCmd.Flags().StringVarP(&mountdir, "mounts", "m", "/mounts", "Base-directory for mounts")

	viper.BindPFlag("mounts", serveCmd.Flag("mounts"))
}
