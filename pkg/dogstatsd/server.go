// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2018 Datadog, Inc.

package dogstatsd

import (
	"bytes"
	"expvar"
	"fmt"
	"runtime"
	"sync"

	log "github.com/cihub/seelog"

	"github.com/DataDog/datadog-agent/pkg/config"
	"github.com/DataDog/datadog-agent/pkg/dogstatsd/listeners"
	"github.com/DataDog/datadog-agent/pkg/metrics"
	"github.com/DataDog/datadog-agent/pkg/tagger"
	"github.com/DataDog/datadog-agent/pkg/util"
)

var (
	dogstatsdExpvar = expvar.NewMap("dogstatsd")
)

// Server represent a Dogstatsd server
type Server struct {
	sync.RWMutex
	listeners  []listeners.StatsdListener
	packetIn   chan *listeners.Packet
	Statistics *util.Stats
	Started    bool
	packetPool *listeners.PacketPool
}

// NewServer returns a running Dogstatsd server
func NewServer(metricOut chan<- *metrics.MetricSample, eventOut chan<- metrics.Event, serviceCheckOut chan<- metrics.ServiceCheck) (*Server, error) {
	var stats *util.Stats
	if config.Datadog.GetBool("dogstatsd_stats_enable") == true {
		buff := config.Datadog.GetInt("dogstatsd_stats_buffer")
		s, err := util.NewStats(uint32(buff))
		if err != nil {
			log.Errorf("dogstatsd: unable to start statistics facilities")
		}
		stats = s
	}

	packetChannel := make(chan *listeners.Packet, 100)
	packetPool := listeners.NewPacketPool(config.Datadog.GetInt("dogstatsd_buffer_size"))
	tmpListeners := make([]listeners.StatsdListener, 0, 2)

	socketPath := config.Datadog.GetString("dogstatsd_socket")
	if len(socketPath) > 0 {
		unixListener, err := listeners.NewUDSListener(packetChannel, packetPool)
		if err != nil {
			log.Errorf(err.Error())
		} else {
			tmpListeners = append(tmpListeners, unixListener)
		}
	}
	if config.Datadog.GetInt("dogstatsd_port") > 0 {
		udpListener, err := listeners.NewUDPListener(packetChannel, packetPool)
		if err != nil {
			log.Errorf(err.Error())
		} else {
			tmpListeners = append(tmpListeners, udpListener)
		}
	}

	if len(tmpListeners) == 0 {
		return nil, fmt.Errorf("listening on neither udp nor socket, please check your configuration")
	}

	s := &Server{
		Started:    true,
		Statistics: stats,
		packetIn:   packetChannel,
		listeners:  tmpListeners,
		packetPool: packetPool,
	}
	s.handleMessages(metricOut, eventOut, serviceCheckOut)

	return s, nil
}

func (s *Server) handleMessages(metricOut chan<- *metrics.MetricSample, eventOut chan<- metrics.Event, serviceCheckOut chan<- metrics.ServiceCheck) {
	if s.Statistics != nil {
		go s.Statistics.Process()
	}

	for _, l := range s.listeners {
		go l.Listen()
	}

	// Run min(2, GoMaxProcs-2) workers, we dedicate a core to the
	// listener goroutine and another to aggregator + forwarder
	workers := runtime.GOMAXPROCS(-1) - 2
	if workers < 2 {
		workers = 2
	}

	for i := 0; i < workers; i++ {
		go s.worker(metricOut, eventOut, serviceCheckOut)
	}
}

func (s *Server) worker(metricOut chan<- *metrics.MetricSample, eventOut chan<- metrics.Event, serviceCheckOut chan<- metrics.ServiceCheck) {
	for {
		s.RLock()
		if s.Started == false {
			s.RUnlock()
			return
		}
		s.RUnlock()

		packet := <-s.packetIn
		var originTags []string

		if packet.Origin != listeners.NoOrigin {
			var err error
			log.Tracef("dogstatsd receive from %s: %s", packet.Origin, packet.Contents)
			originTags, err = tagger.Tag(packet.Origin, false)
			if err != nil {
				log.Errorf(err.Error())
			}
			log.Tracef("tags for %s: %s", packet.Origin, originTags)
		} else {
			log.Tracef("dogstatsd receive: %s", packet.Contents)
		}

		for {
			message := nextMessage(&packet.Contents)
			if message == nil {
				break
			}

			if s.Statistics != nil {
				s.Statistics.StatEvent(1)
			}

			if bytes.HasPrefix(message, []byte("_sc")) {
				serviceCheck, err := parseServiceCheckMessage(message)
				if err != nil {
					log.Errorf("dogstatsd: error parsing service check: %s", err)
					dogstatsdExpvar.Add("ServiceCheckParseErrors", 1)
					continue
				}
				if len(originTags) > 0 {
					serviceCheck.Tags = append(serviceCheck.Tags, originTags...)
				}
				dogstatsdExpvar.Add("ServiceCheckPackets", 1)
				serviceCheckOut <- *serviceCheck
			} else if bytes.HasPrefix(message, []byte("_e")) {
				event, err := parseEventMessage(message)
				if err != nil {
					log.Errorf("dogstatsd: error parsing event: %s", err)
					dogstatsdExpvar.Add("EventParseErrors", 1)
					continue
				}
				if len(originTags) > 0 {
					event.Tags = append(event.Tags, originTags...)
				}
				dogstatsdExpvar.Add("EventPackets", 1)
				eventOut <- *event
			} else {
				sample, err := parseMetricMessage(message)
				if err != nil {
					log.Errorf("dogstatsd: error parsing metrics: %s", err)
					dogstatsdExpvar.Add("MetricParseErrors", 1)
					continue
				}
				if len(originTags) > 0 {
					sample.Tags = append(sample.Tags, originTags...)
				}
				dogstatsdExpvar.Add("MetricPackets", 1)
				metricOut <- sample
			}
		}
		// Return the packet object back to the object pool for reuse
		s.packetPool.Put(packet)
	}
}

// Stop stops a running Dogstatsd server
func (s *Server) Stop() {
	for _, l := range s.listeners {
		l.Stop()
	}
	if s.Statistics != nil {
		s.Statistics.Stop()
	}
	s.Lock()
	s.Started = false
	s.Unlock()
}
