# stats [![CircleCI](https://circleci.com/gh/segmentio/stats.svg?style=shield)](https://circleci.com/gh/segmentio/stats) [![Go Report Card](https://goreportcard.com/badge/github.com/segmentio/stats)](https://goreportcard.com/report/github.com/segmentio/stats) [![GoDoc](https://godoc.org/github.com/segmentio/stats?status.svg)](https://godoc.org/github.com/segmentio/stats)

A Go package for abstracting stats collection.

Installation
------------

```
go get github.com/segmentio/stats
```

Quick Start
-----------

### Engine

A core concept of the `stats` package is the `Engine`. Every program importing
the package gets a default engine where all metrics produced are aggregated.  
The program then has to instantiate clients that will consume from the engine
at regular time intervals and report the state of the engine to metrics
collection platforms.

```go
package main

import (
    "github.com/segmentio/stats"
    "github.com/segmentio/stats/datadog"
)

func main() {
    // Creates a new datadog client publishing metrics to localhost:8125
    dd := datadog.NewClient("localhost:8125")

    // Register the client so it receives metrics from the default engine.
    stats.Register(dd)

    // Flush the default stats engine on return to ensure all buffered
    // metrics are sent to the dogstatsd server.
    defer stats.Flush()

    // That's it! Metrics produced by the application will now be reported!
    // ...
}
```

### Metrics

- [Gauges](https://godoc.org/github.com/segmentio/stats#Gauge)
- [Counters](https://godoc.org/github.com/segmentio/stats#Counter)
- [Histograms](https://godoc.org/github.com/segmentio/stats#Histogram)
- [Timers](https://godoc.org/github.com/segmentio/stats#Timer)

```go
package main

import (
    "github.com/segmentio/stats"
    "github.com/segmentio/stats/datadog"
)

func main() {
    stats.Register(datadog.NewClient("localhost:8125"))
    defer stats.Flush()

    // Increment counters.
    stats.Incr("user.login")
    defer stats.Incr("user.logout")

    // Set a tag on a counter increment.
    stats.Incr("user.login", stats.Tag{"user", "luke"})

    // ...
}
```

Monitoring
----------

### Processes

The [github.com/segmentio/stats/httpstats](https://godoc.org/github.com/segmentio/stats/procstats)
exposes an API for creating stats collector on local processes. Stats are
collected for current the process and metrics like goroutines count or memory
usage are reported.

Here's an example of how to use the collector:
```go
package main

import (
    "github.com/segmentio/stats/datadog"
    "github.com/segmentio/stats/procstats"
)


func main() {
     stats.Register(datadog.NewClient("localhost:8125"))
     defer stats.Flush()

    // Start a new collector for the current process, reporting Go metrics.
    c := procstats.StartCollector(procstats.NewGoMetrics())

    // Gracefully stops stats collection.
    defer c.Close()

    // ...
}
```

### HTTP Servers

The [github.com/segmentio/stats/httpstats](https://godoc.org/github.com/segmentio/stats/httpstats)
package exposes a decorator of `http.Handler` that automatically adds metric
colleciton to a HTTP handler, reporting things like request processing time,
error counters, header and body sizes...

Here's an example of how to use the decorator:
```go
package main

import (
    "net/http"

    "github.com/segmentio/stats/datadog"
    "github.com/segmentio/stats/httpstats"
)

func main() {
     stats.Register(datadog.NewClient("localhost:8125"))
     defer stats.Flush()

    // ...

    http.ListenAndServe(":8080", httpstats.NewHandler(
        http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
            // This HTTP handler is automatically reporting metrics for all
            // requests it handles.
            // ...
        }),
    ))
}
```

### HTTP Clients

The [github.com/segmentio/stats/httpstats](https://godoc.org/github.com/segmentio/stats/httpstats)
package exposes a decorator of `http.RoundTripper` which collects and reports
metrics for client requests the same way it's done on the server side.

Here's an exmaple of how to use the decorator:
```go
package main

import (
    "net/http"

    "github.com/segmentio/stats/datadog"
    "github.com/segmentio/stats/httpstats"
)

func main() {
     stats.Register(datadog.NewClient("localhost:8125"))
     defer stats.Flush()

    // Make a new HTTP client with a transport that will report HTTP metrics,
    // set the engine to nil to use the default.
    httpc := &http.Client{
        Transport: httpstats.NewTransport(
            &http.Transport{},
        ),
    }

    // ...
}
```

You can also modify the default HTTP client to automatically get metrics for all
packages using it, this is very convinient to get insights into dependencies.
```go
package main

import (
    "net/http"

    "github.com/segmentio/stats/datadog"
    "github.com/segmentio/stats/httpstats"
)

func main() {
     stats.Register(datadog.NewClient("localhost:8125"))
     defer stats.Flush()

    // Wraps the default HTTP client's transport.
    http.DefaultClient.Transport = httpstats.NewTransport(http.DefaultClient.Transport)

    // ...
}
```
