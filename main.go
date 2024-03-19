package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"tailscale.com/client/tailscale"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

//go:embed ui/*
var ui embed.FS

var (
	port = flag.String("port", "8080", "the port to listen on")
	dev  = flag.Bool("dev", false, "enable dev mode")
)

var latencyHistogram = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "tailscale_whois_latency_milliseconds",
	Help:    "The latency of Tailscale WhoIs requests in milliseconds",
	Buckets: prometheus.DefBuckets,
})

func main() {
	flag.Parse()

	var droppedCounter = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dropped_total",
		Help: "The total number of dropped objects",
	})

	var templateFn func() *template.Template
	if *dev {
		// load assets from local filesystem
		http.Handle("/ui/", http.StripPrefix("/ui/", http.FileServer(http.Dir("ui"))))

		templateFn = func() *template.Template {
			t, _ := template.ParseFiles("./ui/index.html")
			return t
		}
	} else {
		// load assets from embedded filesystem
		uiAssets, _ := fs.Sub(ui, "ui")
		http.Handle("/ui/", http.StripPrefix("/ui/", http.FileServer(http.FS(uiAssets))))

		t, _ := template.ParseFS(uiAssets, "index.html")
		templateFn = func() *template.Template {
			return t
		}
	}

	http.HandleFunc("/api/uuid", func(w http.ResponseWriter, r *http.Request) {
		uuid := uuid.New().String()
		fmt.Fprintf(w, "%s\n", uuid) // write to http response
		if *dev {
			fmt.Printf("%s\n", uuid)     // write to stdout - TODO: maybe only in dev mode?
		}

		droppedCounter.Inc() // Increment the counter
	})

	http.Handle("/metrics", promhttp.Handler())

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var pageData *Page
		whois, err := tailscaleWhois(r.Context(), r.RemoteAddr)
		if err != nil {
			pageData = &Page{}
		} else {
			var firstInitial string
			if whois.UserProfile.DisplayName != "" {
				firstInitial = string(whois.UserProfile.DisplayName[0])
			} else {
				firstInitial = string(whois.UserProfile.LoginName[0])
			}
			pageData = &Page{
				UserProfile:  whois.UserProfile,
				FirstInitial: firstInitial,
			}
		}

		err = templateFn().Execute(w, pageData)
		if err != nil {
			fmt.Printf("error rendering template [%+v] \n", err)
			// TODO: re-render template with nil pageData to not interrupt demo?
		}
	})

	fmt.Printf("Starting server: http://localhost:%s/\n", *port)
	if err := http.ListenAndServe(fmt.Sprintf(":%s", *port), nil); err != nil {
		log.Fatal(err)
	}
}

type Page struct {
	UserProfile  *tailcfg.UserProfile
	FirstInitial string
}

func tailscaleWhois(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error) {
    localClient := &tailscale.LocalClient{}
    start := time.Now() // Start measuring latency

    defer func() {
        latency := float64(time.Since(start)) / float64(time.Millisecond) // Calculate latency in milliseconds
        latencyHistogram.Observe(latency)                                // Record latency in the histogram
    }()

    fmt.Printf("Remote address: %s\n", remoteAddr)

    whois, err := localClient.WhoIs(ctx, remoteAddr)
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        return nil, fmt.Errorf("failed to identify remote host: %w", err)
    }

    if whois.Node.IsTagged() {
        fmt.Printf("Tagged node: %s\n", remoteAddr)
        return nil, fmt.Errorf("tagged nodes do not have a user identity")
    }

    if whois.UserProfile == nil || whois.UserProfile.LoginName == "" {
        fmt.Printf("Missing user profile: %s\n", remoteAddr)
        return nil, fmt.Errorf("failed to identify remote user")
    }

    fmt.Printf("User info [%+v] for [%s]\n", *whois.UserProfile, remoteAddr)
    return whois, nil
}
