package cmd

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/Webstrates/golem-herder/daemon"
	"github.com/Webstrates/golem-herder/herder"
	"github.com/Webstrates/golem-herder/minion"
	"github.com/Webstrates/golem-herder/token"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	port          int
	mountdir      string
	tokenPassword string
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the herder!",
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
		dv1.HandleFunc("/proxy/{name}", daemon.ProxyHandler)
		// Tokens
		r.HandleFunc("/token/v1/generate", token.GenerateHandler(m, tokenPassword))
		r.HandleFunc("/token/v1/inspect/{token}", token.InspectHandler(m))

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

	// Flags for serveCmd
	serveCmd.Flags().IntVarP(&port, "port", "p", 81, "Which port to listen on")
	serveCmd.Flags().StringVarP(&mountdir, "mounts", "m", "/mounts", "Base-directory for mounts")
	serveCmd.Flags().Bool("proxy", false, "Whether to connect to a proxy. If you set this flag you should name the container 'webstrates' or whatever string you pass in the 'webstrates' flag")
	serveCmd.Flags().String("url", "emet.cc.au.dk", "The url which this herder can be accessed at. This url should be reachable from the containers/golems running on this machine or - if using the proxy - the proxy")
	serveCmd.Flags().String("webstrates", "webstrates", "The location of the webstrates server - if using the proxy this should be left to the default value (webstrates)")
	serveCmd.Flags().String("golem", "latest", "The version (tag) of the golem image (https://hub.docker.com/r/webstrates/golem/tags/) to use.")
	serveCmd.Flags().StringVarP(&tokenPassword, "token-password", "k", "", "Password required to generate tokens.")

	if err := viper.BindPFlags(serveCmd.Flags()); err != nil {
		log.WithError(err).Warn("Could not bind flags.")
	}
}
