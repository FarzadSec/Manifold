// Package metrics writes the Prometheus text exposition format, version 0.0.4.
package metrics

import (
	"bufio"
	"io"
	"math"
	"strconv"
	"strings"
)

// ContentType is what a scrape answer is served as.
const ContentType = "text/plain; version=0.0.4; charset=utf-8"

// Family is one metric name with its samples, written together as the format requires.
type Family struct {
	Name, Type, Help string
	samples          []sample
}

type sample struct {
	labels []string
	value  float64
}

func Gauge(name, help string) *Family   { return &Family{Name: name, Type: "gauge", Help: help} }
func Counter(name, help string) *Family { return &Family{Name: name, Type: "counter", Help: help} }

// Add records a sample; labels are name, value pairs.
func (f *Family) Add(value float64, labels ...string) *Family {
	f.samples = append(f.samples, sample{labels: labels, value: value})
	return f
}

var (
	helpEscaper  = strings.NewReplacer(`\`, `\\`, "\n", `\n`)
	labelEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)
)

func formatValue(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	case math.IsNaN(v):
		return "NaN"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// Write writes the families that have samples, in the order given.
func Write(out io.Writer, families ...*Family) error {
	w := bufio.NewWriter(out)
	for _, f := range families {
		if len(f.samples) == 0 {
			continue
		}
		w.WriteString("# HELP " + f.Name + " " + helpEscaper.Replace(f.Help) + "\n")
		w.WriteString("# TYPE " + f.Name + " " + f.Type + "\n")
		for _, s := range f.samples {
			w.WriteString(f.Name)
			if len(s.labels) > 1 {
				w.WriteByte('{')
				for i := 0; i+1 < len(s.labels); i += 2 {
					if i > 0 {
						w.WriteByte(',')
					}
					w.WriteString(s.labels[i] + `="` + labelEscaper.Replace(s.labels[i+1]) + `"`)
				}
				w.WriteByte('}')
			}
			w.WriteString(" " + formatValue(s.value) + "\n")
		}
	}
	return w.Flush()
}
