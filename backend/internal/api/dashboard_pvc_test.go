package api

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Roadmap Phase 90: the "PVC near capacity" card says which of three things is true.

func claim(ns, name string) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
}

// promAnswering returns what a Prometheus answers to the card's query: one series per "ns/name=percent".
func promAnswering(series map[string]float64) promFetch {
	return func(_ context.Context, path string, params url.Values) ([]byte, error) {
		if path != "/api/v1/query" || !strings.Contains(params.Get("query"), "kubelet_volume_stats_used_bytes") {
			return nil, errors.New("unexpected query")
		}
		var rows []string
		for k, v := range series {
			ns, name, _ := strings.Cut(k, "/")
			rows = append(rows, fmt.Sprintf(`{"metric":{"namespace":%q,"persistentvolumeclaim":%q},"value":[0,"%g"]}`, ns, name, v))
		}
		return []byte(`{"status":"success","data":{"resultType":"vector","result":[` + strings.Join(rows, ",") + `]}}`), nil
	}
}

func TestSomeClaimsNearTheirSizeAreCountedAndNamedFullestFirst(t *testing.T) {
	pvcs := []corev1.PersistentVolumeClaim{claim("db", "pg"), claim("db", "redis"), claim("app", "data"), claim("app", "idle")}
	got := summarizePVCCapacity(context.Background(), promAnswering(map[string]float64{
		"db/pg": 91.4, "db/redis": 80, "app/data": 12, "app/idle": 79.9,
	}), "", pvcs)
	if got.State != "near" || got.Count != 2 || got.Measured != 4 || got.Total != 4 {
		t.Fatalf("%+v", got)
	}
	if len(got.Sample) != 2 || got.Sample[0] != "db/pg 91%" || got.Sample[1] != "db/redis 80%" {
		t.Fatalf("the fullest claim must come first: %v", got.Sample)
	}
}

func TestNothingNearIsAnHonestZeroWhenUsageIsKnown(t *testing.T) {
	got := summarizePVCCapacity(context.Background(), promAnswering(map[string]float64{"db/pg": 10}), "", []corev1.PersistentVolumeClaim{claim("db", "pg")})
	if got.State != "ok" || got.Count != 0 || got.Measured != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestClaimsWithoutUsageNumbersAreNoDataNeverAFabricatedZero(t *testing.T) {
	pvcs := []corev1.PersistentVolumeClaim{claim("db", "pg")}
	// Prometheus is there but the kubelet reports nothing for these volumes (k3s local-path)
	got := summarizePVCCapacity(context.Background(), promAnswering(nil), "", pvcs)
	if got.State != "nodata" || got.Reason == "" || got.Count != 0 {
		t.Fatalf("%+v", got)
	}
	// there is no Prometheus at all
	got = summarizePVCCapacity(context.Background(), nil, "no Prometheus in this cluster", pvcs)
	if got.State != "nodata" || !strings.Contains(got.Reason, "no Prometheus") {
		t.Fatalf("%+v", got)
	}
	// Prometheus fails
	got = summarizePVCCapacity(context.Background(), func(context.Context, string, url.Values) ([]byte, error) { return nil, errors.New("boom") }, "", pvcs)
	if got.State != "nodata" || got.Reason == "" {
		t.Fatalf("%+v", got)
	}
}

func TestOnlyClaimsThatExistCountAndNoClaimsIsSimplyFine(t *testing.T) {
	// a series for a claim that was deleted must not be counted
	got := summarizePVCCapacity(context.Background(), promAnswering(map[string]float64{"db/gone": 99, "db/pg": 5}), "", []corev1.PersistentVolumeClaim{claim("db", "pg")})
	if got.State != "ok" || got.Count != 0 || got.Measured != 1 {
		t.Fatalf("%+v", got)
	}
	got = summarizePVCCapacity(context.Background(), nil, "", nil)
	if got.State != "ok" || got.Total != 0 {
		t.Fatalf("a cluster with no claims has nothing near capacity: %+v", got)
	}
}

func TestTheSampleIsBounded(t *testing.T) {
	series := map[string]float64{}
	var pvcs []corev1.PersistentVolumeClaim
	for i := 0; i < dashboardSampleLimit+5; i++ {
		series[fmt.Sprintf("ns/p%02d", i)] = 90
		pvcs = append(pvcs, claim("ns", fmt.Sprintf("p%02d", i)))
	}
	got := summarizePVCCapacity(context.Background(), promAnswering(series), "", pvcs)
	if got.Count != dashboardSampleLimit+5 || len(got.Sample) != dashboardSampleLimit {
		t.Fatalf("count %d sample %d", got.Count, len(got.Sample))
	}
}
