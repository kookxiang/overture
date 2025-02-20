// Copyright (c) 2016 shawn1m. All rights reserved.
// Use of this source code is governed by The MIT License (MIT) that can be
// found in the LICENSE file.

// Package core implements the essential features.
package core

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/shawn1m/overture/core/config"
	"github.com/shawn1m/overture/core/inbound"
	"github.com/shawn1m/overture/core/outbound"
	log "github.com/sirupsen/logrus"
)

var (
	srv  *inbound.Server
	conf *config.Config
)

// Initiate the server with config file
func InitServer(configFilePath string) {
	conf = config.NewConfig(configFilePath)
	Start()
}

func Start() {
	// New dispatcher without RemoteClientBundle, RemoteClientBundle must be initiated when server is running
	dispatcher := outbound.Dispatcher{
		PrimaryDNS:                  conf.PrimaryDNS,
		AlternativeDNS:              conf.AlternativeDNS,
		BootstrapDNS:                conf.BootstrapDNS,
		OnlyPrimaryDNS:              conf.OnlyPrimaryDNS,
		WhenPrimaryDNSAnswerNoneUse: conf.WhenPrimaryDNSAnswerNoneUse,
		IPNetworkPrimarySet:         conf.IPNetworkPrimarySet,
		IPNetworkAlternativeSet:     conf.IPNetworkAlternativeSet,
		DomainPrimaryList:           conf.DomainPrimaryList,
		DomainAlternativeList:       conf.DomainAlternativeList,

		RedirectIPv6Record:       conf.IPv6UseAlternativeDNS,
		AlternativeDNSConcurrent: conf.AlternativeDNSConcurrent,
		MinimumTTL:               conf.MinimumTTL,
		DomainTTLMap:             conf.DomainTTLMap,

		Hosts: conf.Hosts,
		Cache: conf.Cache,
	}
	dispatcher.Init()

	srv = inbound.NewServer(conf.BindAddress, conf.DebugHTTPAddress, dispatcher, conf.RejectQType, conf.DohEnabled)
	srv.HTTPMux.HandleFunc("/reload/config", ReloadConfigHandler)
	srv.HTTPMux.HandleFunc("/reload", ReloadHandler)
	srv.HTTPMux.HandleFunc("/config", ConfigHandler)

	go srv.Run()
}

// Stop server
func Stop() {
	srv.Stop()
}

// ReloadHandler is passed to http.Server for handle "/reload" request
func ReloadHandler(w http.ResponseWriter, r *http.Request) {
	conf = config.NewConfig(conf.FilePath)
	Reload()
	io.WriteString(w, "Reloaded")
}

func ConfigHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	jsonBinary, _ := json.Marshal(conf)
	io.WriteString(w, string(jsonBinary))
}

func ReloadConfigHandler(w http.ResponseWriter, r *http.Request) {
	b, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	err = json.Unmarshal(b, &conf)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	Reload()
	io.WriteString(w, "Reloaded")
}

// Reload config and restart server
func Reload() {
	log.Infof("Reloading")
	Stop()
	// Have to wait seconds (may be waiting for server shutdown completly) or we will get config parse ERROR. Unknown reason.
	time.Sleep(time.Second)
	Start()
}
