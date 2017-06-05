package prometheus

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/segmentio/stats"
)

type metricType int

const (
	untyped metricType = iota
	counter
	gauge
	histogram
	summary
)

func metricTypeOf(t stats.MetricType) metricType {
	switch t {
	case stats.CounterType:
		return counter
	case stats.GaugeType:
		return gauge
	case stats.HistogramType:
		return histogram
	default:
		return untyped
	}
}

func (t metricType) String() string {
	switch t {
	case untyped:
		return "untyped"
	case counter:
		return "counter"
	case gauge:
		return "gauge"
	case histogram:
		return "histogram"
	case summary:
		return "summary"
	default:
		return "unknown"
	}
}

type metric struct {
	mtype  metricType
	name   string
	help   string
	value  float64
	time   time.Time
	labels labels
}

func metricNameOf(namespace string, name string) string {
	if len(namespace) == 0 {
		return name
	}
	b := make([]byte, 0, len(namespace)+len(name)+1)
	b = appendMetricName(b, namespace)
	b = append(b, '_')
	b = appendMetricName(b, name)
	return string(b)
}

func (m metric) rootName() string {
	if m.mtype == histogram {
		return m.name[:strings.LastIndexByte(m.name, '_')]
	}
	return m.name
}

type metricStore struct {
	mutex   sync.RWMutex
	entries map[string]*metricEntry
}

func (store *metricStore) lookup(mtype metricType, name string, help string) *metricEntry {
	store.mutex.RLock()
	entry := store.entries[name]
	store.mutex.RUnlock()

	// The program may choose to change the type of a metric, this is likely a
	// pretty bad idea but I don't think we have enough context here to tell if
	// it's a bug or a feature so we just accept to mutate the entry.
	if entry == nil || entry.mtype != mtype {
		store.mutex.Lock()

		if store.entries == nil {
			store.entries = make(map[string]*metricEntry)
		}

		if entry = store.entries[name]; entry == nil || entry.mtype != mtype {
			entry = newMetricEntry(mtype, name, help)
			store.entries[name] = entry
		}

		store.mutex.Unlock()
	}

	return entry
}

func (store *metricStore) update(metric metric, buckets []float64) {
	entry := store.lookup(metric.mtype, metric.name, metric.help)
	state := entry.lookup(metric.labels)
	state.update(metric.mtype, metric.value, metric.time, buckets)
}

func (store *metricStore) collect(metrics []metric) []metric {
	store.mutex.RLock()

	for _, entry := range store.entries {
		metrics = entry.collect(metrics)
	}

	store.mutex.RUnlock()
	return metrics
}

type metricEntry struct {
	mutex  sync.RWMutex
	mtype  metricType
	name   string
	help   string
	bucket string
	sum    string
	count  string
	states metricStateMap
}

func newMetricEntry(mtype metricType, name string, help string) *metricEntry {
	entry := &metricEntry{
		mtype:  mtype,
		name:   name,
		help:   help,
		states: make(metricStateMap),
	}

	if mtype == histogram {
		// Here we cache those metric names to avoid having to recompute them
		// every time we collect the state of the metrics.
		entry.bucket = name + "_bucket"
		entry.sum = name + "_sum"
		entry.count = name + "_count"
	}

	return entry
}

func (entry *metricEntry) lookup(labels labels) *metricState {
	key := labels.hash()

	entry.mutex.RLock()
	state := entry.states.find(key, labels)
	entry.mutex.RUnlock()

	if state == nil {
		entry.mutex.Lock()

		if state = entry.states.find(key, labels); state == nil {
			state = newMetricState(labels)
			entry.states.put(key, state)
		}

		entry.mutex.Unlock()
	}

	return state
}

func (entry *metricEntry) collect(metrics []metric) []metric {
	entry.mutex.RLock()

	if len(entry.states) != 0 {
		for _, states := range entry.states {
			for _, state := range states {
				metrics = state.collect(metrics, entry.mtype, entry.name, entry.help, entry.bucket, entry.sum, entry.count)
			}
		}
	}

	entry.mutex.RUnlock()
	return metrics
}

type metricState struct {
	// immutable
	labels labels
	// mutable
	mutex   sync.Mutex
	buckets metricBuckets
	value   float64
	sum     float64
	count   uint64
	time    time.Time
}

func newMetricState(labels labels) *metricState {
	return &metricState{
		labels: labels.copy(),
	}
}

func (state *metricState) update(mtype metricType, value float64, time time.Time, buckets []float64) {
	state.mutex.Lock()

	switch mtype {
	case counter:
		state.value += value

	case gauge:
		state.value = value

	case histogram:
		if len(state.buckets) != len(buckets) {
			state.buckets = makeMetricBuckets(buckets, state.labels)
		}
		state.buckets.update(value)
		state.sum += value
		state.count++
	}

	state.time = time
	state.mutex.Unlock()
}

func (state *metricState) collect(metrics []metric, mtype metricType, name, help, bucketName, sumName, countName string) []metric {
	state.mutex.Lock()

	switch mtype {
	case histogram:
		for _, bucket := range state.buckets {
			metrics = append(metrics, metric{
				mtype:  mtype,
				name:   bucketName,
				help:   help,
				value:  float64(bucket.count),
				time:   state.time,
				labels: bucket.labels,
			})
		}
		metrics = append(metrics,
			metric{
				mtype:  mtype,
				name:   sumName,
				help:   help,
				value:  state.sum,
				time:   state.time,
				labels: state.labels,
			},
			metric{
				mtype:  mtype,
				name:   countName,
				help:   help,
				value:  float64(state.count),
				time:   state.time,
				labels: state.labels,
			},
		)

	default:
		metrics = append(metrics, metric{
			mtype:  mtype,
			name:   name,
			help:   help,
			value:  state.value,
			time:   state.time,
			labels: state.labels,
		})

	}

	state.mutex.Unlock()
	return metrics
}

type metricStateMap map[uint64][]*metricState

func (m metricStateMap) put(key uint64, state *metricState) {
	m[key] = append(m[key], state)
}

func (m metricStateMap) find(key uint64, labels labels) *metricState {
	states := m[key]

	for _, state := range states {
		if state.labels.equal(labels) {
			return state
		}
	}

	return nil
}

type metricBucket struct {
	limit  float64
	count  uint64
	labels labels
}

type metricBuckets []metricBucket

func makeMetricBuckets(buckets []float64, labels labels) metricBuckets {
	b := make(metricBuckets, len(buckets))
	for i := range buckets {
		b[i].limit = buckets[i]
		b[i].labels = labels.copyAppend(label{"le", ftoa(buckets[i])})
	}
	return b
}

func (m metricBuckets) update(value float64) {
	for i := range m {
		if value <= m[i].limit {
			m[i].count++
			break
		}
	}
}

func ftoa(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

type byNameAndLabels []metric

func (metrics byNameAndLabels) Len() int {
	return len(metrics)
}

func (metrics byNameAndLabels) Swap(i int, j int) {
	metrics[i], metrics[j] = metrics[j], metrics[i]
}

func (metrics byNameAndLabels) Less(i int, j int) bool {
	m1 := &metrics[i]
	m2 := &metrics[j]
	return m1.name < m2.name || (m1.name == m2.name && m1.labels.less(m2.labels))
}
