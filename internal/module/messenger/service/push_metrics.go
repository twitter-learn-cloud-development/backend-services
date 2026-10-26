package service

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Published means Redis accepted a transient hint, not device delivery.
var messagePushResults = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "messenger_realtime_publish_total",
	Help: "Best effort message hints by bounded publication outcome; no device delivery guarantee.",
}, []string{"outcome"})

func init() { prometheus.MustRegister(messagePushResults) }
