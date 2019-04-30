package golem

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"regexp"

	log "github.com/sirupsen/logrus"
)

// NewGolemReverseProxy creates and returns a proxy for running golem
func NewGolemReverseProxy(prefix string, portLookup func(webstrate string, privatePort int64) (int64, error)) *httputil.ReverseProxy {
	// prefix will be akin to /golem/v1/proxy
	var pathMatch = regexp.MustCompile(prefix + `/([^/]+)/?(.*)`)

	director := func(req *http.Request) {
		// update the url to go to port - remove the path upto and incl the port
		// <host>:<port>/golem/v1/proxy/<webstrate>/<path> is mapped
		// to localhost:<looked-up-port>/<path>
		matches := pathMatch.FindStringSubmatch(req.URL.Path)
		if matches == nil || len(matches) != 3 {
			log.WithField("path", req.URL.Path).Warn("Path was not as expected for proxy")
			return
		}
		webstrate := matches[1]
		// port 9222 is for the developer console
		port, err := portLookup(webstrate, 9222)
		if err != nil {
			log.WithError(err).WithField("webstrate", webstrate).Warn("Could not find golem to proxy")
			return
		}
		req.URL.Scheme = "http"
		req.URL.Host = fmt.Sprintf("localhost:%v", port)
		req.URL.Path = matches[2]
		log.WithField("target-url", req.URL.String()).Info("Proxying")
	}
	return &httputil.ReverseProxy{Director: director}
}
