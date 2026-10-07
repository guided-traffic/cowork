package metrics

import (
	"maps"
	"slices"
	"strconv"

	dto "github.com/prometheus/client_model/go"
)

// Sample is one value a scrape answers: a counter's or a gauge's under its
// family's name; a histogram's and a summary's as _count, _sum and their
// _bucket or quantile values, as the text format writes them.
type Sample struct {
	Name   string
	Labels map[string]string
	Value  float64
}

// Samples gathers the registry as a scrape would answer it, for the tests of
// every package that records: they read what they recorded without importing
// the client library. A collector that fails returns its error beside what
// the others gathered.
func (m *Metrics) Samples() ([]Sample, error) {
	if m == nil {
		return nil, nil
	}
	families, err := m.registry.Gather()
	var out []Sample
	for _, f := range families {
		for _, metric := range f.GetMetric() {
			out = append(out, samplesOf(f.GetName(), f.GetType(), metric)...)
		}
	}
	return out, err
}

func samplesOf(name string, kind dto.MetricType, metric *dto.Metric) []Sample {
	labels := map[string]string{}
	for _, l := range metric.GetLabel() {
		labels[l.GetName()] = l.GetValue()
	}
	with := func(extra, value string) map[string]string {
		l := maps.Clone(labels)
		l[extra] = value
		return l
	}
	switch kind {
	case dto.MetricType_COUNTER:
		return []Sample{{name, labels, metric.GetCounter().GetValue()}}
	case dto.MetricType_GAUGE:
		return []Sample{{name, labels, metric.GetGauge().GetValue()}}
	case dto.MetricType_HISTOGRAM:
		h := metric.GetHistogram()
		out := []Sample{{name + "_count", labels, float64(h.GetSampleCount())}, {name + "_sum", labels, h.GetSampleSum()}}
		for _, b := range h.GetBucket() {
			out = append(out, Sample{name + "_bucket", with("le", strconv.FormatFloat(b.GetUpperBound(), 'g', -1, 64)), float64(b.GetCumulativeCount())})
		}
		return out
	case dto.MetricType_SUMMARY:
		s := metric.GetSummary()
		out := []Sample{{name + "_count", labels, float64(s.GetSampleCount())}, {name + "_sum", labels, s.GetSampleSum()}}
		for _, q := range s.GetQuantile() {
			out = append(out, Sample{name, with("quantile", strconv.FormatFloat(q.GetQuantile(), 'g', -1, 64)), q.GetValue()})
		}
		return out
	}
	return []Sample{{name, labels, metric.GetUntyped().GetValue()}}
}

// Sum adds up the samples named name whose labels hold every given pair —
// name, value, name, value, … —, the others' labels not compared.
func Sum(samples []Sample, name string, labels ...string) float64 {
	var total float64
	for _, s := range samples {
		if s.Name == name && holds(s.Labels, labels) {
			total += s.Value
		}
	}
	return total
}

// Has reports whether a sample named name holds every given label pair.
func Has(samples []Sample, name string, labels ...string) bool {
	return slices.ContainsFunc(samples, func(s Sample) bool { return s.Name == name && holds(s.Labels, labels) })
}

func holds(have map[string]string, pairs []string) bool {
	for i := 0; i+1 < len(pairs); i += 2 {
		if v, ok := have[pairs[i]]; !ok || v != pairs[i+1] {
			return false
		}
	}
	return true
}
