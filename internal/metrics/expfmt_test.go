package metrics

import (
	"math"
	"strings"
	"testing"
)

func TestWriteEscapesAndSkipsEmptyFamilies(t *testing.T) {
	var b strings.Builder
	err := Write(&b,
		Gauge("manifold_users", "Users by state.\nOne line.").Add(3, "state", "active").Add(1, "state", "limited"),
		Counter("manifold_unused_total", "No samples."),
		Gauge("manifold_node_up", `Node "up".`).Add(1, "node", `fra "1"\a`+"\n"),
		Gauge("manifold_load", "Load.").Add(math.Inf(1)),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := `# HELP manifold_users Users by state.\nOne line.
# TYPE manifold_users gauge
manifold_users{state="active"} 3
manifold_users{state="limited"} 1
# HELP manifold_node_up Node "up".
# TYPE manifold_node_up gauge
manifold_node_up{node="fra \"1\"\\a\n"} 1
# HELP manifold_load Load.
# TYPE manifold_load gauge
manifold_load +Inf
`
	if b.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}
