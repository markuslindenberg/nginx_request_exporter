// Copyright 2016 Markus Lindenberg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"

	"gopkg.in/mcuadros/go-syslog.v2"
)

const (
	namespace = "nginx_request"
)

func main() {
	var (
		listenAddress = flag.String("web.listen-address", ":9147", "Address to listen on for web interface and telemetry.")
		metricsPath   = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
		syslogAddress = flag.String("nginx.syslog-address", "127.0.0.1:9514", "Syslog listen address/socket for Nginx.")
	)
	flag.Parse()

	// Listen to signals
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGTERM, syscall.SIGINT)

	// Set up syslog server
	channel := make(syslog.LogPartsChannel, 20000)
	handler := syslog.NewChannelHandler(channel)
	server := syslog.NewServer()
	server.SetFormat(syslog.RFC3164)
	server.SetHandler(handler)

	var err error
	if strings.HasPrefix(*syslogAddress, "unix:") {
		err = server.ListenUnixgram(strings.TrimPrefix(*syslogAddress, "unix:"))
	} else {
		err = server.ListenUDP(*syslogAddress)
	}
	if err != nil {
		log.Fatal(err)
	}
	err = server.Boot()
	if err != nil {
		log.Fatal(err)
	}

	// Setup metrics
	syslogMessages := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "exporter_syslog_messages",
		Help:      "Current total syslog messages received.",
	})
	err = prometheus.Register(syslogMessages)
	if err != nil {
		log.Fatal(err)
	}
	syslogParseFailures := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "exporter_syslog_parse_failure",
		Help:      "Number of errors while parsing syslog messages.",
	})
	err = prometheus.Register(syslogParseFailures)
	if err != nil {
		log.Fatal(err)
	}
	var msgs int64
	go func() {
		for part := range channel {
			syslogMessages.Inc()
			msgs++
			tag, _ := part["tag"].(string)
			if tag != "nginx" {
				log.Warn("Ignoring syslog message with wrong tag")
				syslogParseFailures.Inc()
				continue
			}
			server, _ := part["hostname"].(string)
			if server == "" {
				log.Warn("Hostname missing in syslog message")
				syslogParseFailures.Inc()
				continue
			}

			content, _ := part["content"].(string)
			if content == "" {
				log.Warn("Ignoring empty syslog message")
				syslogParseFailures.Inc()
				continue
			}

			metrics, labels, err := parseMessage(content)
			if err != nil {
				log.Error(err)
				continue
			}
			for _, metric := range metrics {
				var collector prometheus.Collector
				collector = prometheus.NewHistogramVec(prometheus.HistogramOpts{
					Namespace: namespace,
					Name:      metric.Name,
					Help:      fmt.Sprintf("Nginx request log value for %s", metric.Name),
				}, labels.Names)
				if err := prometheus.Register(collector); err != nil {
					if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
						collector = are.ExistingCollector.(*prometheus.HistogramVec)
					} else {
						log.Error(err)
						continue
					}
				}
				collector.(*prometheus.HistogramVec).WithLabelValues(labels.Values...).Observe(metric.Value)
			}
		}
	}()

	// Setup HTTP server
	http.Handle(*metricsPath, prometheus.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
             <head><title>Nginx Request Exporter</title></head>
             <body>
             <h1>Nginx Request Exporter</h1>
             <p><a href='` + *metricsPath + `'>Metrics</a></p>
             </body>
             </html>`))
	})

	go func() {
		log.Infof("Starting Server: %s", *listenAddress)
		log.Fatal(http.ListenAndServe(*listenAddress, nil))
	}()

	s := <-sigchan
	log.Infof("Received %v, terminating", s)
	log.Infof("Messages received: %d", msgs)
	err = server.Kill()
	if err != nil {
		log.Error(err)
	}
	os.Exit(0)
}
