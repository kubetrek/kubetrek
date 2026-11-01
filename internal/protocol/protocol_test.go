package protocol

import (
	"encoding/json"
	"testing"
)

func TestMessageJSON(t *testing.T) {
	cases := map[string]Message{
		`{"type":"modified","object":{"kind":"Pod","namespace":"default","name":"web-x2k","owners":["ReplicaSet/web-7d4b9"],"labels":{"app":"web"},"status":{"phase":"Running"}}}`: {
			Type: Modified,
			Object: &Object{
				Kind: "Pod", Namespace: "default", Name: "web-x2k",
				Owners: []string{"ReplicaSet/web-7d4b9"},
				Labels: map[string]string{"app": "web"},
				Status: json.RawMessage(`{"phase":"Running"}`),
			},
		},
		`{"type":"check","check":{"step":"first-deployment","passed":true}}`: {
			Type:  Check,
			Check: &StepCheck{Step: "first-deployment", Passed: true},
		},
	}
	for want, m := range cases {
		got, err := json.Marshal(m)
		if err != nil || string(got) != want {
			t.Errorf("got  %s (%v)\nwant %s", got, err, want)
		}
	}
}
