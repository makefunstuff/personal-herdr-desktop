package shepherdr

import (
	"context"
	"strings"
	"testing"
)

// Another machine's Shepherdr is read and written (a confirmed send) with curl run there through herdr-core.
func TestRemoteReadsAndSendsThroughHC(t *testing.T) {
	var ran []string
	c := &Client{Name: "gpubox", URL: "http://127.0.0.1:7787", Machine: "gpubox-shepherdr", HC: "hc",
		Exec: func(_ context.Context, name string, args ...string) ([]byte, error) {
			ran = append(ran, name+" "+strings.Join(args, " "))
			return []byte(`{"places": [], "tasks": [{"id": "T-1", "archived": 1793000000000, "report": {"progress": "?", "eta": null, "paused": 0}}], "needs": [{"chat": "c", "text": "Waiting"}], "asks": {"T-1/Agent": "q1"}}` + "\n200"), nil
		}}
	s, err := c.Snapshot(context.Background())
	if err != nil || len(s.Needs) != 1 || s.Needs[0].What() != "Waiting" || s.Asks["T-1/Agent"] != "q1" || len(s.Tasks) != 1 {
		t.Fatalf("%+v %v", s, err)
	}
	if len(ran) != 1 || ran[0] != "hc exec --on gpubox-shepherdr curl -sS -m 20 -w \n%{http_code} http://127.0.0.1:7787/api/snapshot" {
		t.Fatalf("ran %q", ran)
	}
	if err := c.Send(context.Background(), "c", "hi", "x"); err != nil || len(ran) != 2 ||
		ran[1] != `hc exec --on gpubox-shepherdr curl -sS -m 20 -w `+"\n"+`%{http_code} -X POST -H content-type: application/json --data-binary {"chat":"c","cid":"x","human":true,"text":"hi","type":"send"} http://127.0.0.1:7787/api/action` {
		t.Fatalf("remote send: %v %q", err, ran)
	}
}
