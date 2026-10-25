package securitychecks

import "testing"

func instSnap(i *Installation) *Snapshot {
	s := &Snapshot{Installation: i, Unavailable: map[string]string{}}
	if i == nil {
		s.Unavailable["installation"] = "not known"
	} else if !i.Chart {
		s.Unavailable["installation-chart"] = "not installed with the chart"
	}
	return s
}

func TestAHardenedInstallationPassesEveryInstallationCheck(t *testing.T) {
	rs := RunAll(instSnap(&Installation{AuthMode: "oidc", CookieSecure: true, SessionStore: "secret", Chart: true, AuditOnVolume: true, NetworkPolicy: true}))
	for _, r := range rs {
		if r.Category == "This installation" && r.Status != Pass {
			t.Errorf("%s: %s %+v", r.ID, r.Status, r.Findings)
		}
	}
}

func TestEachInstallationWeaknessIsReportedWithItsReason(t *testing.T) {
	good := Installation{AuthMode: "oidc", CookieSecure: true, SessionStore: "secret", Chart: true, AuditOnVolume: true, NetworkPolicy: true}
	for id, mod := range map[string]func(i *Installation){
		"inst-auth-disabled":     func(i *Installation) { i.AuthMode = "disabled" },
		"inst-dev-optouts":       func(i *Installation) { i.DevOptOuts = []string{"OIDC_ALLOW_INSECURE=true", "VAULT_ALLOW_INSECURE=true"} },
		"inst-store-unencrypted": func(i *Installation) { i.StoreUnencrypted = true },
		"inst-cookie-insecure":   func(i *Installation) { i.CookieSecure = false },
		"inst-http-keycloak":     func(i *Installation) { i.HTTPKeycloakAllowed = true },
		"inst-audit-ephemeral":   func(i *Installation) { i.AuditOnVolume = false },
		"inst-session-memory":    func(i *Installation) { i.SessionStore = "memory" },
		"inst-no-networkpolicy":  func(i *Installation) { i.NetworkPolicy = false },
	} {
		i := good
		mod(&i)
		r := find(RunAll(instSnap(&i)), id)
		if r.Status != Fail || r.Total == 0 || r.Findings[0].Detail == "" || r.Remediation == "" {
			t.Errorf("%s: %+v", id, r)
		}
	}
}

func TestTwoDevelopmentSwitchesAreTwoFindings(t *testing.T) {
	r := find(RunAll(instSnap(&Installation{AuthMode: "oidc", CookieSecure: true, SessionStore: "secret", Chart: true, DevOptOuts: []string{"A=true", "B=true"}})), "inst-dev-optouts")
	if r.Total != 2 {
		t.Fatalf("%+v", r)
	}
}

func TestWhatTheChartDidNotSayIsUnknownNeverPass(t *testing.T) {
	// not installed with the chart: the audit-volume and NetworkPolicy facts are unknown, the others still work
	rs := RunAll(instSnap(&Installation{AuthMode: "oidc", CookieSecure: true, SessionStore: "secret"}))
	for _, id := range []string{"inst-audit-ephemeral", "inst-no-networkpolicy"} {
		if r := find(rs, id); r.Status != Unknown || r.Reason == "" {
			t.Errorf("%s: %+v", id, r)
		}
	}
	if r := find(rs, "inst-cookie-insecure"); r.Status != Pass {
		t.Errorf("a known fact should still be checked: %+v", r)
	}
	// nothing known at all
	for _, r := range RunAll(instSnap(nil)) {
		if r.Category == "This installation" && r.Status != Unknown {
			t.Errorf("%s answered %s without knowing anything", r.ID, r.Status)
		}
	}
}

func TestAnInstallationWithSignInOffDoesNotAlsoNagAboutItsCookieAndSessions(t *testing.T) {
	rs := RunAll(instSnap(&Installation{AuthMode: "disabled", Chart: true, AuditOnVolume: true, NetworkPolicy: true}))
	for _, id := range []string{"inst-cookie-insecure", "inst-session-memory"} {
		if r := find(rs, id); r.Status != Pass {
			t.Errorf("%s: there are no sessions to protect: %+v", id, r)
		}
	}
	if r := find(rs, "inst-auth-disabled"); r.Status != Fail {
		t.Errorf("%+v", r)
	}
}

func TestAWideTrustedProxyRangeIsReported(t *testing.T) {
	good := Installation{AuthMode: "oidc", CookieSecure: true, SessionStore: "secret", Chart: true, AuditOnVolume: true, NetworkPolicy: true}
	if r := find(RunAll(instSnap(&good)), "inst-proxies-broad"); r.Status != Pass {
		t.Fatalf("a narrow or empty list must pass: %+v", r)
	}
	wide := good
	wide.BroadTrustedProxies = []string{"10.0.0.0/8", "172.16.0.0/12"}
	if r := find(RunAll(instSnap(&wide)), "inst-proxies-broad"); r.Status != Fail || r.Total != 2 || r.Findings[0].Detail == "" {
		t.Fatalf("%+v", r)
	}
}
