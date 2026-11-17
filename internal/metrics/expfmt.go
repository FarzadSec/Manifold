// Package metrics writes the Prometheus text exposition format, version 0.0.4.
package metrics

import (
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
	var b strings.Builder
	for _, f := range families {
		if len(f.samples) == 0 {
			continue
		}
		b.WriteString("# HELP " + f.Name + " " + helpEscaper.Replace(f.Help) + "\n")
		b.WriteString("# TYPE " + f.Name + " " + f.Type + "\n")
		for _, s := range f.samples {
			b.WriteString(f.Name)
			if len(s.labels) > 1 {
				b.WriteByte('{')
				for i := 0; i+1 < len(s.labels); i += 2 {
					if i > 0 {
						b.WriteByte(',')
					}
					b.WriteString(s.labels[i] + `="` + labelEscaper.Replace(s.labels[i+1]) + `"`)
				}
				b.WriteByte('}')
			}
			b.WriteString(" " + formatValue(s.value) + "\n")
		}
	}
	_, err := io.WriteString(out, b.String())
	return err
}
