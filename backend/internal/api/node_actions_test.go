package api

import "testing"

type taintRow = struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Effect string `json:"effect"`
}

func TestLabelChangeValidation(t *testing.T) {
	ok := []labelsBody{
		{Set: map[string]string{"node-role.kubernetes.io/worker": ""}},
		{Set: map[string]string{"disk": "ssd"}, Remove: []string{"old"}},
	}
	for _, b := range ok {
		if err := validateLabelChange(b); err != nil {
			t.Errorf("%+v refused: %v", b, err)
		}
	}
	bad := []labelsBody{
		{},
		{Set: map[string]string{"kubernetes.io/hostname": "x"}},
		{Remove: []string{"kubernetes.io/hostname"}},
		{Set: map[string]string{"bad key": "v"}},
		{Set: map[string]string{"k": "not valid!"}},
		{Set: map[string]string{"a": "1"}, Remove: []string{"a"}},
	}
	for _, b := range bad {
		if err := validateLabelChange(b); err == nil {
			t.Errorf("%+v was accepted", b)
		}
	}
}

func TestTaintValidation(t *testing.T) {
	good := taintBody{Taints: []taintRow{{"dedicated", "gpu", "NoSchedule"}, {"dedicated", "gpu", "NoExecute"}}}
	if ts, err := validateTaints(good); err != nil || len(ts) != 2 {
		t.Errorf("good taints: %v %v", ts, err)
	}
	for _, c := range []taintRow{{"", "", "NoSchedule"}, {"k", "", "Sometimes"}, {"k", "bad value!", "NoSchedule"}} {
		if _, err := validateTaints(taintBody{Taints: []taintRow{c}}); err == nil {
			t.Errorf("%+v accepted", c)
		}
	}
	dup := taintBody{Taints: []taintRow{{"k", "v", "NoSchedule"}, {"k", "w", "NoSchedule"}}}
	if _, err := validateTaints(dup); err == nil {
		t.Error("a duplicate key+effect was accepted")
	}
}
