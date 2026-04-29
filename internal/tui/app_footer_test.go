package main

import "testing"

func footerTestModel() model {
	m := newModel("127.0.0.1:3000", "", nil)
	m.serviceItems = []serviceItem{
		{name: "billing", status: "stopped"},
		{name: "users", status: "ready"},
	}
	m.serviceSelection = 0
	return m
}

func TestServiceActionDoesNotSetFooterFeedback(t *testing.T) {
	m := footerTestModel()
	m.setPersistentFooter("connected")

	updatedModel, cmd := m.Update(serviceActionDoneMsg{action: ServiceActionStart, result: ServiceActionSuccess, serviceName: "billing"})
	updated := updatedModel.(model)

	if updated.footerMessage != "connected" {
		t.Fatalf("footer message = %q, want %q", updated.footerMessage, "connected")
	}
	if updated.footerTransient {
		t.Fatal("footer should stay persistent after service action")
	}
	if cmd != nil {
		t.Fatal("service action should not schedule footer command")
	}
}

func TestTransientFooterStillExpiresForGlobalMessage(t *testing.T) {
	m := footerTestModel()

	cmd := m.setTransientFooter("actions not available in all logs view")
	if m.footerMessage != "actions not available in all logs view" {
		t.Fatalf("footer message = %q, want %q", m.footerMessage, "actions not available in all logs view")
	}
	if !m.footerTransient {
		t.Fatal("expected footer to be transient for global message")
	}
	if cmd == nil {
		t.Fatal("expected footer expiry command")
	}

	token := m.footerToken
	updatedModel, _ := m.Update(footerMessageExpiredMsg{token: token})
	expired := updatedModel.(model)

	if expired.footerMessage != "" {
		t.Fatalf("footer message after expiry = %q, want empty", expired.footerMessage)
	}
	if expired.footerTransient {
		t.Fatal("footer should not remain transient after expiry")
	}
}

func TestPersistentFooterInvalidatesOldTransientExpiry(t *testing.T) {
	m := footerTestModel()

	_ = m.setTransientFooter("actions not available in all logs view")
	oldToken := m.footerToken
	m.setPersistentFooter("connected")

	updatedModel, _ := m.Update(connectDoneMsg{result: ConnectResult{Status: StatusConnected, Services: []Service{{Name: "billing", Status: "ready"}}}})
	persistent := updatedModel.(model)
	if persistent.footerMessage != "connected" {
		t.Fatalf("footer message = %q, want %q", persistent.footerMessage, "connected")
	}
	if persistent.footerTransient {
		t.Fatal("connected footer must not be transient")
	}

	updatedModel, _ = persistent.Update(footerMessageExpiredMsg{token: oldToken})
	afterOldExpiry := updatedModel.(model)
	if afterOldExpiry.footerMessage != "connected" {
		t.Fatalf("stale expiry should not clear persistent footer, got %q", afterOldExpiry.footerMessage)
	}
}
